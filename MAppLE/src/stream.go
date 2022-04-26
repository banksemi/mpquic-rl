package quic

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/internal/flowcontrol"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/logger"
)

type streamI interface {
	Stream

	AddStreamFrame(*wire.StreamFrame) error
	RegisterRemoteError(error, protocol.ByteCount) error
	LenOfDataForWriting() protocol.ByteCount
	GetDataForWriting(maxBytes protocol.ByteCount) []byte
	GetWriteOffset() protocol.ByteCount
	Finished() bool
	Cancel(error)
	ShouldSendFin() bool
	SentFin()
	// methods needed for flow control
	GetWindowUpdate(force bool) protocol.ByteCount
	UpdateSendWindow(protocol.ByteCount)
	IsFlowControlBlocked() bool
	GetSendWindowSize() protocol.ByteCount
	// methods needed for statistics
	AddBytesRetrans(n protocol.ByteCount)
}

const bufferingThreshold = 5000

// A Stream assembles the data from StreamFrames and provides a super-convenient Read-Interface
//
// Read() and Write() may be called concurrently, but multiple calls to Read() or Write() individually must be synchronized manually.
type stream struct {
	mutex sync.Mutex

	ctx       context.Context
	ctxCancel context.CancelFunc

	streamID protocol.StreamID
	onData   func()
	// onReset is a callback that should send a RST_STREAM
	onReset func(protocol.StreamID, protocol.ByteCount)

	readPosInFrame int
	writeOffset    protocol.ByteCount
	readOffset     protocol.ByteCount

	// Once set, the errors must not be changed!
	err error

	// cancelled is set when Cancel() is called
	cancelled utils.AtomicBool
	// finishedReading is set once we read a frame with a FinBit
	finishedReading utils.AtomicBool
	// finisedWriting is set once Close() is called
	finishedWriting utils.AtomicBool
	// resetLocally is set if Reset() is called
	resetLocally utils.AtomicBool
	// resetRemotely is set if RegisterRemoteError() is called
	resetRemotely utils.AtomicBool

	frameQueue   *streamFrameSorter
	readChan     chan struct{}
	readDeadline time.Time

	dataForWriting []byte
	finSent        utils.AtomicBool
	rstSent        utils.AtomicBool
	writeChan      chan struct{}
	writeDeadline  time.Time

	flowController flowcontrol.StreamFlowController
	version        protocol.VersionNumber

	unreliable             bool
	retransmissionDeadline time.Duration

	replayBufferSize  uint64
	bufferEnd         protocol.ByteCount
	messageMode       bool
	currentBufferSize *utils.AtomicUint64
	finReceived       *utils.AtomicBool
}

var _ Stream = &stream{}
var _ streamI = &stream{}

type deadlineError struct{}

func (deadlineError) Error() string   { return "deadline exceeded" }
func (deadlineError) Temporary() bool { return true }
func (deadlineError) Timeout() bool   { return true }

var errDeadline net.Error = &deadlineError{}

// newStream creates a new Stream
func newStream(StreamID protocol.StreamID,
	onData func(),
	onReset func(protocol.StreamID, protocol.ByteCount),
	flowController flowcontrol.StreamFlowController,
	version protocol.VersionNumber,
) *stream {
	currentBufferSize := &utils.AtomicUint64{}
	currentBufferSize.Set(0)
	finReceived := &utils.AtomicBool{}
	finReceived.Set(false)
	s := &stream{
		onData:            onData,
		onReset:           onReset,
		streamID:          StreamID,
		flowController:    flowController,
		frameQueue:        newStreamFrameSorter(),
		readChan:          make(chan struct{}, 1),
		writeChan:         make(chan struct{}, 1),
		version:           version,
		currentBufferSize: currentBufferSize,
		finReceived:       finReceived,
	}
	s.ctx, s.ctxCancel = context.WithCancel(context.Background())
	return s
}

// newStream creates a new Stream
func newUnreliableStream(StreamID protocol.StreamID,
	onData func(),
	queueControlFrame func(protocol.StreamID, protocol.ByteCount),
	flowControlManager flowcontrol.StreamFlowController,
	version protocol.VersionNumber,
) *stream {
	s := newStream(StreamID, onData, queueControlFrame, flowControlManager, version)
	s.SetUnreliable(true)
	return s
}

// Read implements io.Reader. It is not thread safe!
func (s *stream) Read(p []byte) (int, error) {
	s.frameQueue.lastCallToRead = time.Now()
	s.mutex.Lock()
	err := s.err
	s.mutex.Unlock()
	if s.cancelled.Get() || s.resetLocally.Get() {
		return 0, err
	}
	if s.finishedReading.Get() {
		return 0, io.EOF
	}

	bytesRead := 0
	defer func() {
		s.currentBufferSize.Decrement(uint64(bytesRead))
	}()
	for bytesRead < len(p) {
		s.mutex.Lock()
		var frame *wire.StreamFrame
		if !s.finReceived.Get() && uint64(s.flowController.GetHighestReceived())-uint64(s.readOffset) < s.replayBufferSize {
			frame = nil
		} else {
			frame = s.frameQueue.Head()
		}
		if frame == nil && bytesRead > 0 {
			err = s.err
			s.mutex.Unlock()
			return bytesRead, err
		}

		var bytesSkipped protocol.ByteCount // number of bytes skipped because of nonCON stream frames, that should be considered as read
		var err error
		for {
			// Stop waiting on errors
			if s.resetLocally.Get() || s.cancelled.Get() {
				err = s.err
				break
			}

			deadline := s.readDeadline
			if !deadline.IsZero() && !time.Now().Before(deadline) {
				err = errDeadline
				break
			}

			if frame != nil {
				// added by michelfra: this if below
				if s.unreliable && frame.Offset > s.readOffset {
					bytesSkipped += frame.Offset - s.readOffset
					s.readOffset = frame.Offset
				}
				s.readPosInFrame = int(s.readOffset - frame.Offset)
				break
			}

			s.mutex.Unlock()
			if deadline.IsZero() {
				// if the stream is not reliable and is there is a reliablity deadline, wake up when this deadline is over
				if s.unreliable && s.frameQueue.reliabilityDeadline.Nanoseconds() != 0 {
					select {
					case <-s.readChan:
					case <-time.After(s.frameQueue.lastCallToRead.Add(s.frameQueue.reliabilityDeadline).Sub(time.Now())):
					}
				} else {
					<-s.readChan
				}
			} else {
				// if the stream is not reliable and is there is a reliability deadline, wake up when this deadline is over
				if s.unreliable && s.frameQueue.reliabilityDeadline.Nanoseconds() != 0 {
					select {
					case <-s.readChan:
					case <-time.After(s.frameQueue.lastCallToRead.Add(s.frameQueue.reliabilityDeadline).Sub(time.Now())):
					case <-time.After(deadline.Sub(time.Now())):
					}
				} else {
					select {
					case <-s.readChan:
					case <-time.After(deadline.Sub(time.Now())):
					}
				}
			}
			s.mutex.Lock()
			if !s.finReceived.Get() && uint64(s.flowController.GetHighestReceived())-uint64(s.readOffset) < s.replayBufferSize {
				frame = nil
			} else {
				frame = s.frameQueue.Head()
			}
		}
		s.mutex.Unlock()

		if err != nil {
			return bytesRead, err
		}

		m := utils.Min(len(p)-bytesRead, int(frame.DataLen())-s.readPosInFrame)

		if bytesRead > len(p) {
			return bytesRead, fmt.Errorf("BUG: bytesRead (%d) > len(p) (%d) in stream.Read", bytesRead, len(p))
		}
		if s.readPosInFrame > int(frame.DataLen()) {
			return bytesRead, fmt.Errorf("BUG: readPosInFrame (%d) > frame.DataLen (%d) in stream.Read", s.readPosInFrame, frame.DataLen())
		}
		copy(p[bytesRead:], frame.Data[s.readPosInFrame:])
		s.readPosInFrame += m
		bytesRead += m

		s.mutex.Lock()
		s.readOffset += protocol.ByteCount(m)
		// when a RST_STREAM was received, the was already informed about the final byteOffset for this stream
		if !s.resetRemotely.Get() {
			if s.replayBufferSize == 0 {
				s.flowController.AddBytesRead(protocol.ByteCount(m))
			} else {
				oldBufferEnd := s.bufferEnd
				newBufferEnd := uint64(s.readOffset + protocol.ByteCount(s.replayBufferSize))
				s.bufferEnd = protocol.ByteCount(utils.MaxUint64(newBufferEnd, uint64(oldBufferEnd))) // the buffer size could have reduce, so be careful not to add the same bytes twice
				bytesToPutInBuffer := uint64(s.getReceivedBytesBetween(oldBufferEnd, protocol.ByteCount(newBufferEnd)))
				// add byteSkypped
				s.flowController.AddBytesRead(protocol.ByteCount(bytesToPutInBuffer))
			}
		}
		s.mutex.Unlock()
		s.onData() // so that a possible WINDOW_UPDATE is sent

		if s.readPosInFrame >= int(frame.DataLen()) {
			fin := frame.FinBit
			s.mutex.Lock()
			s.frameQueue.Pop()
			s.mutex.Unlock()
			if fin {
				s.finishedReading.Set(true)
				return bytesRead, io.EOF
			} else if s.messageMode {
				return bytesRead, nil
			}
		}
	}

	return bytesRead, nil
}

func (s *stream) Write(p []byte) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.resetLocally.Get() || s.err != nil {
		return 0, s.err
	}
	if s.finishedWriting.Get() {
		return 0, fmt.Errorf("write on closed stream %d", s.streamID)
	}
	if len(p) == 0 {
		return 0, nil
	}

	s.dataForWriting = make([]byte, len(p))
	copy(s.dataForWriting, p)
	//s.dataForWriting = append(s.dataForWriting, p...)
	s.onData()

	var err error
	for {
		deadline := s.writeDeadline
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			err = errDeadline
			break
		}
		if s.dataForWriting == nil || len(s.dataForWriting) < bufferingThreshold || s.err != nil {
			break
		}

		s.mutex.Unlock()
		if deadline.IsZero() {
			<-s.writeChan
		} else {
			select {
			case <-s.writeChan:
			case <-time.After(deadline.Sub(time.Now())):
			}
		}
		s.mutex.Lock()
	}

	if err != nil {
		return 0, err
	}
	if s.err != nil {
		return len(p) - len(s.dataForWriting), s.err
	}
	return len(p), nil
}

func (s *stream) GetWriteOffset() protocol.ByteCount {
	return s.writeOffset
}

func (s *stream) LenOfDataForWriting() protocol.ByteCount {
	s.mutex.Lock()
	var l protocol.ByteCount
	if s.err == nil {
		l = protocol.ByteCount(len(s.dataForWriting))
	}
	s.mutex.Unlock()
	return l
}

func (s *stream) GetDataForWriting(maxBytes protocol.ByteCount) []byte {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.err != nil || s.dataForWriting == nil {
		return nil
	}

	// TODO(#657): Flow control for the crypto stream
	if s.streamID != s.version.CryptoStreamID() {
		maxBytes = utils.MinByteCount(maxBytes, s.flowController.SendWindowSize())
	}
	if maxBytes == 0 {
		return nil
	}

	var ret []byte
	if protocol.ByteCount(len(s.dataForWriting)) > maxBytes {
		if !s.messageMode {
			ret = s.dataForWriting[:maxBytes]
			s.dataForWriting = s.dataForWriting[maxBytes:]
		}
	} else {
		ret = s.dataForWriting
		s.dataForWriting = nil
	}
	if len(s.dataForWriting) < bufferingThreshold {
		s.signalWrite()
	}
	if ret != nil {
		s.writeOffset += protocol.ByteCount(len(ret))
		s.flowController.AddBytesSent(protocol.ByteCount(len(ret)))
	}
	return ret
}

// Close implements io.Closer
func (s *stream) Close() error {
	s.finishedWriting.Set(true)
	s.ctxCancel()
	s.onData()
	return nil
}

func (s *stream) shouldSendReset() bool {
	if s.rstSent.Get() {
		return false
	}
	return (s.resetLocally.Get() || s.resetRemotely.Get()) && !s.finishedWriteAndSentFin()
}

func (s *stream) ShouldSendFin() bool {
	s.mutex.Lock()
	res := s.finishedWriting.Get() && !s.finSent.Get() && s.err == nil && s.dataForWriting == nil
	s.mutex.Unlock()
	return res
}

func (s *stream) SentFin() {
	s.finSent.Set(true)
}

// AddStreamFrame adds a new stream frame
func (s *stream) AddStreamFrame(frame *wire.StreamFrame) error {
	maxOffset := frame.Offset + frame.DataLen()
	s.mutex.Lock()
	if err := s.flowController.UpdateHighestReceived(maxOffset, frame.FinBit); err != nil {
		return err
	}

	defer s.mutex.Unlock()
	if frame.FinBit {
		s.finReceived.Set(true)
	}
	if err := s.frameQueue.Push(frame); err != nil && err != errDuplicateStreamData {
		return err
	} else if err != errDuplicateStreamData {
		if s.replayBufferSize > 0 {
			bEnd := protocol.ByteCount(utils.MaxUint64(uint64(s.readOffset)+s.replayBufferSize, uint64(s.bufferEnd)))
			if frame.Offset < bEnd {
				//put this frame in buffer
				bytesToPutInBuffer := utils.MinUint64(uint64(frame.DataLen()), uint64(bEnd-frame.Offset))
				s.flowController.AddBytesRead(protocol.ByteCount(bytesToPutInBuffer))
			}
		}

		logger.ExpLogInsertGapsInfo(s.streamID, s.frameQueue.gaps.Len())
	}

	s.signalRead()
	return nil
}

// signalRead performs a non-blocking send on the readChan
func (s *stream) signalRead() {
	select {
	case s.readChan <- struct{}{}:
	default:
	}
}

// signalRead performs a non-blocking send on the writeChan
func (s *stream) signalWrite() {
	select {
	case s.writeChan <- struct{}{}:
	default:
	}
}

func (s *stream) SetReadDeadline(t time.Time) error {
	s.mutex.Lock()
	oldDeadline := s.readDeadline
	s.readDeadline = t
	s.mutex.Unlock()
	// if the new deadline is before the currently set deadline, wake up Read()
	if t.Before(oldDeadline) {
		s.signalRead()
	}
	return nil
}

func (s *stream) SetWriteDeadline(t time.Time) error {
	s.mutex.Lock()
	oldDeadline := s.writeDeadline
	s.writeDeadline = t
	s.mutex.Unlock()
	if t.Before(oldDeadline) {
		s.signalWrite()
	}
	return nil
}

func (s *stream) SetDeadline(t time.Time) error {
	_ = s.SetReadDeadline(t)  // SetReadDeadline never errors
	_ = s.SetWriteDeadline(t) // SetWriteDeadline never errors
	return nil
}

// CloseRemote makes the stream receive a "virtual" FIN stream frame at a given offset
func (s *stream) CloseRemote(offset protocol.ByteCount) {
	s.AddStreamFrame(&wire.StreamFrame{FinBit: true, Offset: offset})
}

// Cancel is called by session to indicate that an error occurred
// The stream should will be closed immediately
func (s *stream) Cancel(err error) {
	s.mutex.Lock()
	s.cancelled.Set(true)
	s.ctxCancel()
	// errors must not be changed!
	if s.err == nil {
		s.err = err
		s.signalRead()
		s.signalWrite()
	}
	s.mutex.Unlock()
}

// resets the stream locally
func (s *stream) Reset(err error) {
	if s.resetLocally.Get() {
		return
	}
	s.mutex.Lock()
	s.resetLocally.Set(true)
	s.ctxCancel()
	// errors must not be changed!
	if s.err == nil {
		s.err = err
		s.signalRead()
		s.signalWrite()
	}
	if s.shouldSendReset() {
		s.onReset(s.streamID, s.writeOffset)
		s.rstSent.Set(true)
	}
	s.mutex.Unlock()
}

// resets the stream remotely
func (s *stream) RegisterRemoteError(err error, offset protocol.ByteCount) error {
	if s.resetRemotely.Get() {
		return nil
	}
	s.mutex.Lock()
	s.resetRemotely.Set(true)
	s.ctxCancel()
	// errors must not be changed!
	if s.err == nil {
		s.err = err
		s.signalWrite()
	}
	if err := s.flowController.UpdateHighestReceived(offset, true); err != nil {
		return err
	}
	if s.shouldSendReset() {
		s.onReset(s.streamID, s.writeOffset)
		s.rstSent.Set(true)
	}
	s.mutex.Unlock()
	return nil
}

func (s *stream) finishedWriteAndSentFin() bool {
	return s.finishedWriting.Get() && s.finSent.Get()
}

func (s *stream) Finished() bool {
	return s.cancelled.Get() ||
		(s.finishedReading.Get() && s.finishedWriteAndSentFin()) ||
		(s.resetRemotely.Get() && s.rstSent.Get()) ||
		(s.finishedReading.Get() && s.rstSent.Get()) ||
		(s.finishedWriteAndSentFin() && s.resetRemotely.Get())
}

func (s *stream) Context() context.Context {
	return s.ctx
}

func (s *stream) StreamID() protocol.StreamID {
	return s.streamID
}

func (s *stream) GetBytesSent() protocol.ByteCount {
	return s.flowController.GetBytesSent()
}

func (s *stream) AddBytesRetrans(n protocol.ByteCount) {
	s.flowController.AddBytesRetrans(n)
}

func (s *stream) GetBytesRetrans() protocol.ByteCount {
	return s.flowController.GetBytesRetrans()
}

func (s *stream) UpdateSendWindow(n protocol.ByteCount) {
	s.flowController.UpdateSendWindow(n)
}

func (s *stream) IsFlowControlBlocked() bool {
	if s.messageMode && s.LenOfDataForWriting() > s.flowController.SendWindowSize() {
		if s.LenOfDataForWriting() > s.flowController.SendWindowSize() {
		}
		return s.LenOfDataForWriting() > s.flowController.SendWindowSize()
	}
	return s.flowController.IsBlocked()
}

func (s *stream) GetSendWindowSize() protocol.ByteCount {
	return s.flowController.SendWindowSize()
}

func (s *stream) GetWindowUpdate(force bool) protocol.ByteCount {
	return s.flowController.GetWindowUpdate(force)
}

func (s *stream) SetRetransmissionDeadline(val time.Duration) {
	s.retransmissionDeadline = val
}

func (s *stream) GetRetransmissionDeadLine() time.Duration {
	return s.retransmissionDeadline
}

func (s *stream) SetReliabilityDeadline(val time.Duration) {
	s.frameQueue.reliabilityDeadline = val
}

func (s *stream) GetReliabilityDeadline() time.Duration {
	return s.frameQueue.reliabilityDeadline
}

func (s *stream) IsUnreliable() bool {
	return s.unreliable && s.frameQueue.unreliable
}

func (s *stream) SetUnreliable(val bool) {
	s.unreliable = val
	s.frameQueue.unreliable = val
}

func (s *stream) SetMessageMode(val bool) {
	s.messageMode = val
}

func (s *stream) GetMessageMode() bool {
	return s.messageMode
}

func (s *stream) GetReplayBufferSize() uint64 {
	s.mutex.Lock()
	retVal := s.replayBufferSize
	s.mutex.Unlock()
	return retVal
}

func (s *stream) SetReplayBufferSize(size uint64) {
	s.mutex.Lock()
	s.replayBufferSize = size
	if s.readOffset+protocol.ByteCount(size) > s.bufferEnd {
		s.flowController.AddBytesRead(s.getReceivedBytesBetween(s.bufferEnd, s.readOffset+protocol.ByteCount(size)))
		s.bufferEnd = s.readOffset + protocol.ByteCount(size)
	}
	s.signalRead()
	s.mutex.Unlock()
}
func (s *stream) getReceivedBytesBetween(begin, end protocol.ByteCount) protocol.ByteCount {
	if end <= begin {
		return 0
	}
	currentIndex := begin
	var nBytes protocol.ByteCount
	for currentGap := s.frameQueue.gaps.Front(); currentGap != nil; currentGap = currentGap.Next() {
		if currentGap.Value.Start > currentIndex {
			nBytes += protocol.ByteCount(utils.MinUint64(uint64(currentGap.Value.Start-currentIndex), uint64(end-currentIndex)))
		}
		if currentGap.Value.End <= end {
			break
		}
		currentIndex = currentGap.Value.End
	}
	return nBytes
}
