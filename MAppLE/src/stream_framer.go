package quic

import (
	"github.com/lucas-clemente/quic-go/internal/utils"
	"net"
	"runtime"

	"github.com/lucas-clemente/quic-go/internal/flowcontrol"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"time"
)

type streamFramer struct {
	streamsMap   *streamsMap
	cryptoStream streamI

	connFlowController flowcontrol.ConnectionFlowController

	retransmissionQueue         []*wire.StreamFrame
	isInRetransmissionQueue     map[*wire.StreamFrame]bool
	blockedFrameQueue           []wire.Frame
	addAddressFrameQueue        []*wire.AddAddressFrame
	removeAddressFrameQueue     []*wire.RemoveAddressFrame
	lastBlockedSent             time.Time
	currentRTT                  time.Duration
	pathsFrame                  *wire.PathsFrame
	protectReliableStreamFrames bool
}

func newStreamFramer(
	cryptoStream streamI,
	streamsMap *streamsMap,
	cfc flowcontrol.ConnectionFlowController,
	protectReliableStreamFrames bool,
) *streamFramer {
	return &streamFramer{
		streamsMap:                  streamsMap,
		cryptoStream:                cryptoStream,
		connFlowController:          cfc,
		protectReliableStreamFrames: protectReliableStreamFrames,
		isInRetransmissionQueue:     make(map[*wire.StreamFrame]bool),
	}
}

func (f *streamFramer) AddFrameForRetransmission(frame *wire.StreamFrame) {
	// hotfix when using multipath when duplicated packets are lost: we avoid to queue twice the same frame
	if _, present := f.isInRetransmissionQueue[frame]; !present {
		f.retransmissionQueue = append(f.retransmissionQueue, frame)
		f.isInRetransmissionQueue[frame] = true
	}
}

func (f *streamFramer) PopStreamFrames(maxLen protocol.ByteCount, unreliableLenPenalty protocol.ByteCount) []*wire.StreamFrame {
	fs, currentLen, containsUnreliable := f.maybePopFramesForRetransmission(maxLen, unreliableLenPenalty)
	if containsUnreliable || (f.protectReliableStreamFrames && len(fs) > 0) {
		// note: FECHeaderOverhead  is used to represent the presence of the ProtectedPayloadLength (2 bytes) and the FEC group (6 bytes) in the packet header when the packet contains unreliable stream frames
		maxLen -= unreliableLenPenalty
	}
	streamFrames := f.maybePopNormalFrames(maxLen-currentLen, containsUnreliable || (f.protectReliableStreamFrames && len(fs) > 0), unreliableLenPenalty)
	return append(fs, streamFrames...)
}

func (f *streamFramer) PopBlockedFrame() wire.Frame {
	if len(f.blockedFrameQueue) == 0 {
		return nil
	}
	frame := f.blockedFrameQueue[0]
	f.blockedFrameQueue = f.blockedFrameQueue[1:]
	return frame
}

func (f *streamFramer) AddAddAddressForTransmission(addrID protocol.AddressID, addr net.UDPAddr, backup bool) {
	f.addAddressFrameQueue = append(f.addAddressFrameQueue, &wire.AddAddressFrame{AddrID: addrID, Addr: addr, Backup: backup})
}

func (f *streamFramer) PopAddAddressFrame() *wire.AddAddressFrame {
	if len(f.addAddressFrameQueue) == 0 {
		return nil
	}
	frame := f.addAddressFrameQueue[0]
	f.addAddressFrameQueue = f.addAddressFrameQueue[1:]
	return frame
}

func (f *streamFramer) AddRemoveAddressForTransmission(addrID protocol.AddressID) {
	f.removeAddressFrameQueue = append(f.removeAddressFrameQueue, &wire.RemoveAddressFrame{AddrID: addrID})
}

func (f *streamFramer) PopRemoveAddressFrame() *wire.RemoveAddressFrame {
	if len(f.removeAddressFrameQueue) == 0 {
		return nil
	}
	frame := f.removeAddressFrameQueue[0]
	f.removeAddressFrameQueue = f.removeAddressFrameQueue[1:]
	return frame
}

// AddPathsFrameForTransmission, MUST hold pconnsLock and pathsLock!
func (f *streamFramer) AddPathsFrameForTransmission(s *session) {
	pathInfos := make(map[protocol.PathID]wire.PathInfoSection)
	for pathID, pth := range s.paths {
		if !pth.active.Get() {
			continue
		}
		addr := pth.conn.LocalAddr()
		addrID, ok := s.pathManager.pconnMgr.GetAddrIDOf(addr)
		if !ok {
			// Maybe the interface just disappeared, so don't announce that path
			println("unknown Address ID of " + addr.String())
			continue
		}
		pathInfos[pathID] = wire.PathInfoSection{
			AddrID: addrID,
			RTT:    pth.rttStats.SmoothedRTT(),
		}
	}
	f.pathsFrame = &wire.PathsFrame{ActivePaths: protocol.PathID(len(pathInfos) - 1), PathInfos: pathInfos}
}

func (f *streamFramer) PopPathsFrame() *wire.PathsFrame {
	if f.pathsFrame == nil {
		return nil
	}
	frame := f.pathsFrame
	f.pathsFrame = nil
	return frame
}

func (f *streamFramer) HasFramesForRetransmission() bool {
	return len(f.retransmissionQueue) > 0
}

func (f *streamFramer) HasFramesToSend() bool {
	// if f.streamsMap == nil || f.streamsMap.openStreams == nil {
	//	return false
	// }

	for _, id := range f.streamsMap.openStreams {
		//if f.streamsMap.streams == nil {
		//return false
		//}

		if s, ok := f.streamsMap.GetStream(id); ok && s.LenOfDataForWriting() > 0 {
			return true
		}
	}
	return false
}

func (f *streamFramer) HasCryptoStreamFrame() bool {
	return f.cryptoStream.LenOfDataForWriting() > 0
}

// TODO(lclemente): This is somewhat duplicate with the normal path for generating frames.
func (f *streamFramer) PopCryptoStreamFrame(maxLen protocol.ByteCount) *wire.StreamFrame {
	if !f.HasCryptoStreamFrame() {
		return nil
	}
	frame := &wire.StreamFrame{
		StreamID: f.cryptoStream.StreamID(),
		Offset:   f.cryptoStream.GetWriteOffset(),
	}
	frameHeaderBytes, _ := frame.MinLength(protocol.VersionWhatever) // can never error
	frame.Data = f.cryptoStream.GetDataForWriting(maxLen - frameHeaderBytes)
	return frame
}

func (f *streamFramer) maybePopFramesForRetransmission(maxTotalLen protocol.ByteCount, unreliableLenPenalty protocol.ByteCount) (res []*wire.StreamFrame, currentLen protocol.ByteCount, containsUnreliable bool) {
	containsUnreliable = false

	for len(f.retransmissionQueue) > 0 {
		frame := f.retransmissionQueue[0]
		if frame.DeadlineExpired() {
			delete(f.isInRetransmissionQueue, frame)
			f.retransmissionQueue = f.retransmissionQueue[1:]
			continue
		} else if frame.Unreliable || f.protectReliableStreamFrames {
			if maxTotalLen <= unreliableLenPenalty {
				break
			}
		}
		frame.DataLenPresent = true

		// TODO: possible underflow when decreasing maxLen
		if (f.protectReliableStreamFrames || frame.Unreliable) && !containsUnreliable { // remove 8 bytes of maxLen if frame is unreliable, and ensure that we only do this once
			containsUnreliable = true
			maxTotalLen -= unreliableLenPenalty
		}

		maxLen := maxTotalLen - currentLen

		frameHeaderLen, _ := frame.MinLength(protocol.VersionWhatever) // can never error
		if currentLen+frameHeaderLen >= maxLen {
			break
		}

		currentLen += frameHeaderLen

		str, err := f.streamsMap.GetOrOpenStream(frame.StreamID)
		if err != nil {
			panic(err)
		}
		if str != nil && str.GetMessageMode() && frame.DataLen() > maxLen-currentLen {
			return
		}

		splitFrame := maybeSplitOffFrame(frame, maxTotalLen-currentLen)
		if splitFrame != nil { // StreamFrame was split
			res = append(res, splitFrame)
			frameLen := splitFrame.DataLen()
			currentLen += frameLen
			// XXX (QDC): to avoid rewriting a lot of tests...
			if f.streamsMap != nil {
				str, err := f.streamsMap.GetOrOpenStream(frame.StreamID)
				if err != nil {
					panic(err)
				}
				str2, ok := str.(*stream)
				if ok && str2.flowController != nil {
					str2.AddBytesRetrans(frameLen)
				}
			}
			break
		}

		delete(f.isInRetransmissionQueue, f.retransmissionQueue[0])
		f.retransmissionQueue = f.retransmissionQueue[1:]
		res = append(res, frame)
		frameLen := frame.DataLen()
		currentLen += frameLen
		// XXX (QDC): to avoid rewriting a lot of tests...
		str, err = f.streamsMap.GetOrOpenStream(frame.StreamID)
		if err != nil {
			panic(err)
		}
		str2, ok := str.(*stream)
		if ok && str2.flowController != nil {
			str2.AddBytesRetrans(frameLen)
		}
	}
	return
}

func (f *streamFramer) maybePopNormalFrames(maxTotalLen protocol.ByteCount, containsUnreliable bool, unreliableLenPenalty protocol.ByteCount) (res []*wire.StreamFrame) {
	frame := &wire.StreamFrame{DataLenPresent: true}
	var currentLen protocol.ByteCount
	fn := func(s streamI) (bool, error) {
		if s == nil {
			return true, nil
		}

		// added by michelfra: this if
		if f.protectReliableStreamFrames || s.IsUnreliable() {
			if !containsUnreliable { // remove bytes of maxLen if frame is unreliable, and ensure that we only do this once
				if maxTotalLen <= unreliableLenPenalty {
					// Not enough space to have the additional header bytes of a FEC-protected packet. Continue to find a Reliable Stream
					return true, nil
				}
				// remove the two + six bytes that will be taken by the header for the FECProtectedPayloadLength and FECBlockNumber fields
				maxTotalLen -= unreliableLenPenalty
				containsUnreliable = true
			}
		}

		frame.StreamID = s.StreamID()
		frame.Offset = s.GetWriteOffset()

		frame.Unreliable = s.IsUnreliable()
		if s.IsUnreliable() {
			frame.RetransmitDeadline = s.GetRetransmissionDeadLine()
		}

		// not perfect, but thread-safe since writeOffset is only written when getting data
		frameHeaderBytes, _ := frame.MinLength(protocol.VersionWhatever) // can never error
		if currentLen+frameHeaderBytes > maxTotalLen {
			return false, nil // theoretically, we could find another stream that fits, but this is quite unlikely, so we stop here
		}
		maxLen := maxTotalLen - currentLen - frameHeaderBytes

		// ensure we do not violate the flow control
		maxLen = utils.MinByteCount(f.connFlowController.SendWindowSize(), utils.MinByteCount(maxLen, s.GetSendWindowSize()))
		var data []byte

		ignoreStream := false
		if s.GetMessageMode() {
			if s.LenOfDataForWriting() <= maxLen && s.LenOfDataForWriting() > 0 {
				data = s.GetDataForWriting(maxLen)
				ignoreStream = data == nil
			}
		} else if s.LenOfDataForWriting() > 0 {
			data = s.GetDataForWriting(maxLen)
		}

		runtime.Gosched() // ensure that the Close() occurs before shouldSendFin

		if ignoreStream {
			//copy paste from below
			if time.Now().Sub(f.lastBlockedSent) >= f.currentRTT/time.Duration(100) {
				if !frame.FinBit && s.IsFlowControlBlocked() {
					f.lastBlockedSent = time.Now()
					f.blockedFrameQueue = append(f.blockedFrameQueue, &wire.StreamBlockedFrame{StreamID: s.StreamID()})
				}
				if f.connFlowController.IsBlocked() {
					f.lastBlockedSent = time.Now()
					f.blockedFrameQueue = append(f.blockedFrameQueue, &wire.BlockedFrame{})
				}
			}
		}

		// This is unlikely, but check it nonetheless, the scheduler might have jumped in. Seems to happen in ~20% of cases in the tests.
		shouldSendFin := s.ShouldSendFin()
		if data == nil && !shouldSendFin {
			return true, nil
		}

		if s.ShouldSendFin() {
			frame.FinBit = true
			frame.Unreliable = false
			s.SentFin()
		}

		frame.Data = data

		// Finally, check if we are now FC blocked and should queue a BLOCKED frame
		if !frame.FinBit && s.IsFlowControlBlocked() {
			f.blockedFrameQueue = append(f.blockedFrameQueue, &wire.StreamBlockedFrame{StreamID: s.StreamID()})
		}
		if f.connFlowController.IsBlocked() {
			f.blockedFrameQueue = append(f.blockedFrameQueue, &wire.BlockedFrame{})
		}

		res = append(res, frame)
		currentLen += frameHeaderBytes + frame.DataLen()

		if currentLen == maxTotalLen {
			return false, nil
		}

		frame = &wire.StreamFrame{DataLenPresent: true}
		return true, nil
	}

	f.streamsMap.RoundRobinIterate(fn)
	return
}

// maybeSplitOffFrame removes the first n bytes and returns them as a separate frame. If n >= len(frame), nil is returned and nothing is modified.
func maybeSplitOffFrame(frame *wire.StreamFrame, n protocol.ByteCount) *wire.StreamFrame {
	if n >= frame.DataLen() {
		return nil
	}

	defer func() {
		frame.Data = frame.Data[n:]
		frame.Offset += n
	}()

	return &wire.StreamFrame{
		FinBit:         false,
		StreamID:       frame.StreamID,
		Offset:         frame.Offset,
		Data:           frame.Data[:n],
		DataLenPresent: frame.DataLenPresent,
		Unreliable:     frame.Unreliable,
	}
}
