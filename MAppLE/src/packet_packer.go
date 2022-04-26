package quic

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	//"time"

	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/internal/handshake"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	//"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

type packedPacket struct {
	header          *wire.Header
	raw             []byte
	frames          []wire.Frame
	encryptionLevel protocol.EncryptionLevel
	// TODO: maybe remove containsOnlyFECFrames, as  containsUnreliableStreamFrames => !containsOnlyFECFrames ?
	containsOnlyFECFrames          bool
	containsUnreliableStreamFrames bool
	fecFlag                        bool
	fecPayloadID                   protocol.FECPayloadID
}

type packetPacker struct {
	connectionID protocol.ConnectionID
	perspective  protocol.Perspective
	version      protocol.VersionNumber
	cryptoSetup  handshake.CryptoSetup

	fecFramer    *FECFramer
	streamFramer *streamFramer

	controlFrames          []wire.Frame
	isInControlFramesQueue map[wire.Frame]bool
	stopWaiting            map[protocol.PathID]*wire.StopWaitingFrame
	ackFrame               map[protocol.PathID]*wire.AckFrame
	omitConnectionID       bool

	sess *session
}

func newPacketPacker(connectionID protocol.ConnectionID,
	cryptoSetup handshake.CryptoSetup,
	streamFramer *streamFramer,
	perspective protocol.Perspective,
	version protocol.VersionNumber,
	fecFramer *FECFramer,
	session *session,
) *packetPacker {
	return &packetPacker{
		cryptoSetup:            cryptoSetup,
		connectionID:           connectionID,
		perspective:            perspective,
		version:                version,
		streamFramer:           streamFramer,
		stopWaiting:            make(map[protocol.PathID]*wire.StopWaitingFrame),
		ackFrame:               make(map[protocol.PathID]*wire.AckFrame),
		fecFramer:              fecFramer,
		sess:                   session,
		isInControlFramesQueue: make(map[wire.Frame]bool),
	}
}

// PackConnectionClose packs a packet that ONLY contains a ConnectionCloseFrame
func (p *packetPacker) PackConnectionClose(ccf *wire.ConnectionCloseFrame, pth *path) (*packedPacket, error) {
	frames := []wire.Frame{ccf}
	encLevel, sealer := p.cryptoSetup.GetSealer()
	header := p.getHeader(encLevel, pth)
	raw, err := p.writeAndSealPacket(header, frames, sealer, pth)
	return &packedPacket{
		header:          header,
		raw:             raw,
		frames:          frames,
		encryptionLevel: encLevel,
	}, err
}

// PackPing packs a packet that ONLY contains a PingFrame
func (p *packetPacker) PackPing(pf *wire.PingFrame, pth *path) (*packedPacket, error) {
	// Add the PingFrame in front of the controlFrames
	pth.SetLeastUnacked(pth.sentPacketHandler.GetLeastUnacked())
	p.controlFrames = append([]wire.Frame{pf}, p.controlFrames...)
	p.isInControlFramesQueue[pf] = true
	var fpid protocol.FECPayloadID
	// TODO michelfra: not using dummy arguments for this call to PackPacket
	return p.PackPacket(pth, fpid, true)
}

func (p *packetPacker) PackAckPacket(pth *path) (*packedPacket, error) {
	if p.ackFrame[pth.pathID] == nil {
		return nil, errors.New("packet packer BUG: no ack frame queued")
	}
	encLevel, sealer := p.cryptoSetup.GetSealer()
	header := p.getHeader(encLevel, pth)
	frames := []wire.Frame{p.ackFrame[pth.pathID]}
	if p.stopWaiting[pth.pathID] != nil {
		p.stopWaiting[pth.pathID].PacketNumber = header.PacketNumber
		p.stopWaiting[pth.pathID].PacketNumberLen = header.PacketNumberLen
		frames = append(frames, p.stopWaiting[pth.pathID])
		p.stopWaiting[pth.pathID] = nil
	}
	p.ackFrame[pth.pathID] = nil
	raw, err := p.writeAndSealPacket(header, frames, sealer, pth)
	return &packedPacket{
		header:          header,
		raw:             raw,
		frames:          frames,
		encryptionLevel: encLevel,
	}, err
}

// PackHandshakeRetransmission retransmits a handshake packet, that was sent with less than forward-secure encryption
func (p *packetPacker) PackHandshakeRetransmission(packet *ackhandler.Packet, pth *path) (*packedPacket, error) {
	if packet.EncryptionLevel == protocol.EncryptionForwardSecure {
		return nil, errors.New("PacketPacker BUG: forward-secure encrypted handshake packets don't need special treatment")
	}
	sealer, err := p.cryptoSetup.GetSealerWithEncryptionLevel(packet.EncryptionLevel)
	if err != nil {
		return nil, err
	}
	if p.stopWaiting[pth.pathID] == nil {
		return nil, errors.New("PacketPacker BUG: Handshake retransmissions must contain a StopWaitingFrame")
	}
	header := p.getHeader(packet.EncryptionLevel, pth)
	p.stopWaiting[pth.pathID].PacketNumber = header.PacketNumber
	p.stopWaiting[pth.pathID].PacketNumberLen = header.PacketNumberLen
	frames := append([]wire.Frame{p.stopWaiting[pth.pathID]}, packet.Frames...)
	p.stopWaiting[pth.pathID] = nil
	raw, err := p.writeAndSealPacket(header, frames, sealer, pth)
	return &packedPacket{
		header:          header,
		raw:             raw,
		frames:          frames,
		encryptionLevel: packet.EncryptionLevel,
	}, err
}

// PackPacket packs a new packet
// the other controlFrames are sent in the next packet, but might be queued and sent in the next packet if the packet would overflow MaxPacketSize otherwise
func (p *packetPacker) PackPacket(pth *path, sourceFECPayloadID protocol.FECPayloadID, allowFEC bool) (*packedPacket, error) {
	if p.streamFramer.HasCryptoStreamFrame() {
		return p.packCryptoPacket(pth)
	}

	encLevel, sealer := p.cryptoSetup.GetSealer()

	header := p.getHeader(encLevel, pth)
	headerLength, err := header.GetLength(p.perspective, p.version)
	if err != nil {
		return nil, err
	}
	if p.stopWaiting[pth.pathID] != nil {
		p.stopWaiting[pth.pathID].PacketNumber = header.PacketNumber
		p.stopWaiting[pth.pathID].PacketNumberLen = header.PacketNumberLen
	}

	headerNonFECProtected := *header

	headerFECProtected := *header

	headerFECProtected.FECFlag = true
	headerFECProtected.FECPayloadID = sourceFECPayloadID

	lengthFECProtected, err := headerFECProtected.GetLength(p.perspective, p.version)
	if err != nil {
		return nil, err
	}

	lengthNonFECProtected, err := headerNonFECProtected.GetLength(p.perspective, p.version)
	if err != nil {
		return nil, err
	}

	// TODO (QDC): rework this part with PING
	var isPing bool
	if len(p.controlFrames) > 0 {
		_, isPing = p.controlFrames[0].(*wire.PingFrame)
	}

	var payloadFrames []wire.Frame
	if isPing {
		payloadFrames = []wire.Frame{p.controlFrames[0]}
		// Remove the ping frame from the control frames
		delete(p.isInControlFramesQueue, p.controlFrames[0])
		p.controlFrames = p.controlFrames[1:len(p.controlFrames)]
	} else {
		maxSize := protocol.MaxPacketSize - protocol.ByteCount(sealer.Overhead()) - headerLength
		fflen, _ := (&wire.FECFrame{}).MinLength(p.version)
		// FIXME: the +25 is to avoid splitting FEC Frames because we should take in account the header of the packet that will contain the FEC Frame protecting this packet  !
		payloadFrames, err = p.composeNextPacket(maxSize, p.canSendData(encLevel), allowFEC, pth, lengthFECProtected-lengthNonFECProtected+fflen+25)
		if err != nil {
			return nil, err
		}
	}

	// added by michelfra: check if packet contains only FEC frames (if yes, no FEC frame has to be generated for it)
	containsOnlyFECFrames := len(payloadFrames) > 0
	containsUnreliableStreamFrames := false
	containsStreamFrames := false

	for _, frame := range payloadFrames {
		switch f := frame.(type) {
		case *wire.FECFrame:
			// pass
		case *wire.StreamFrame:
			if f.Unreliable {
				containsUnreliableStreamFrames = true
			}
			containsOnlyFECFrames = false
			containsStreamFrames = true
		default:
			containsOnlyFECFrames = false
		}
	}

	if (p.sess.config.ProtectReliableStreamFrames && containsStreamFrames && p.sess.fecFrameworkSender.fecScheme != nil) || containsUnreliableStreamFrames {
		header.FECFlag = true
		header.FECPayloadID = sourceFECPayloadID
	}

	// Check if we have enough frames to send
	if len(payloadFrames) == 0 {
		return nil, nil
	}
	// Don't send out packets that only contain a StopWaitingFrame
	if len(payloadFrames) == 1 && !isPing && p.stopWaiting[pth.pathID] != nil {
		return nil, nil
	}
	p.stopWaiting[pth.pathID] = nil
	p.ackFrame[pth.pathID] = nil

	raw, err := p.writeAndSealPacket(header, payloadFrames, sealer, pth)
	if err != nil {
		return nil, err
	}
	return &packedPacket{
		header:                         header,
		raw:                            raw,
		frames:                         payloadFrames,
		encryptionLevel:                encLevel,
		containsOnlyFECFrames:          containsOnlyFECFrames,
		containsUnreliableStreamFrames: containsUnreliableStreamFrames,
		fecFlag:                        header.FECFlag,
		fecPayloadID:                   header.FECPayloadID,
	}, nil
}

func (p *packetPacker) packCryptoPacket(pth *path) (*packedPacket, error) {
	encLevel, sealer := p.cryptoSetup.GetSealerForCryptoStream()
	header := p.getHeader(encLevel, pth)
	headerLength, err := header.GetLength(p.perspective, p.version)
	if err != nil {
		return nil, err
	}
	maxLen := protocol.MaxPacketSize - protocol.ByteCount(sealer.Overhead()) - protocol.NonForwardSecurePacketSizeReduction - headerLength
	frames := []wire.Frame{p.streamFramer.PopCryptoStreamFrame(maxLen)}
	raw, err := p.writeAndSealPacket(header, frames, sealer, pth)
	if err != nil {
		return nil, err
	}
	return &packedPacket{
		header:          header,
		raw:             raw,
		frames:          frames,
		encryptionLevel: encLevel,
	}, nil
}

func (p *packetPacker) composeNextPacket(
	maxFrameSize protocol.ByteCount,
	canSendStreamFrames bool,
	canSendFECFrames bool,
	pth *path,
	FECProtectionOverhead protocol.ByteCount,
) ([]wire.Frame, error) {
	var payloadLength protocol.ByteCount
	var payloadFrames []wire.Frame

	// STOP_WAITING and ACK will always fit
	if p.stopWaiting[pth.pathID] != nil {
		payloadFrames = append(payloadFrames, p.stopWaiting[pth.pathID])
		l, err := p.stopWaiting[pth.pathID].MinLength(p.version)
		if err != nil {
			return nil, err
		}
		payloadLength += l
	}
	if p.ackFrame[pth.pathID] != nil {
		payloadFrames = append(payloadFrames, p.ackFrame[pth.pathID])
		l, err := p.ackFrame[pth.pathID].MinLength(p.version)
		if err != nil {
			return nil, err
		}
		payloadLength += l
	}

	for len(p.controlFrames) > 0 {
		frame := p.controlFrames[len(p.controlFrames)-1]
		minLength, err := frame.MinLength(p.version)
		if err != nil {
			return nil, err
		}
		if payloadLength+minLength > maxFrameSize {
			break
		}
		payloadFrames = append(payloadFrames, frame)
		payloadLength += minLength
		p.controlFrames = p.controlFrames[:len(p.controlFrames)-1]
		delete(p.isInControlFramesQueue, frame)
	}

	if payloadLength > maxFrameSize {
		return nil, fmt.Errorf("Packet Packer BUG: packet payload (%d) too large (%d)", payloadLength, maxFrameSize)
	}

	if !canSendStreamFrames {
		return payloadFrames, nil
	}

	hasStreamDataToSend := p.streamFramer.HasFramesToSend()
	//hasStreamDataToSend := false

	var fecFrames []*wire.FECFrame
	var takenPayload protocol.ByteCount
	var err error
	// added by michelfra: pop the FEC frames if there are some (priority > stream frames)
	if canSendFECFrames && !p.sess.config.OnlySendFECWhenApplicationLimited && len(payloadFrames) == 0 { // we prefer having a FEC Frame alone in its packet to avoid splitting the frames
		var ff []*wire.FECFrame
		ff, takenPayload, err = p.fecFramer.maybePopFECFrames(maxFrameSize - payloadLength)
		if err != nil {
			return nil, err
		}
		for _, f := range ff {
			payloadFrames = append(payloadFrames, f)
			fecFrames = append(fecFrames, f)
		}
		// update payload length
		payloadLength += takenPayload
	}
	// end added by michelfra

	// temporarily increase the maxFrameSize by 2 bytes
	// this leads to a properly sized packet in all cases, since we do all the packet length calculations with StreamFrames that have the DataLen set
	// however, for the last StreamFrame in the packet, we can omit the DataLen, thus saving 2 bytes and yielding a packet of exactly the correct size
	if !p.sess.config.ProtectReliableStreamFrames {
		maxFrameSize += 2
	}

	if len(fecFrames) == 0 {

		p.streamFramer.currentRTT = pth.rttStats.SmoothedRTT()
		fs := p.streamFramer.PopStreamFrames(maxFrameSize-payloadLength, FECProtectionOverhead)
		if len(fs) != 0 {
			if !p.sess.config.ProtectReliableStreamFrames && !fs[len(fs)-1].Unreliable {
				fs[len(fs)-1].DataLenPresent = false
			}
		}

		// TODO: Simplify
		for _, f := range fs {
			payloadFrames = append(payloadFrames, f)
			//if f.FinBit {
			//	m := pth.rttStats.Windows
			//	mjson, _ := json.Marshal(m)
			//	_ = ioutil.WriteFile(fmt.Sprintf("./%s_%d.json", protocol.FILE_CONTAINING_CWIN, pth.pathID), mjson, 0644)
			//}
		}

		if p.sess.config.ForceSendFECOnIdlePath && len(fecFrames) == 0 && len(fs) == 0 && !hasStreamDataToSend {
			// nothing to send anymore: flush the remaining repair symbols
			err = p.sess.fecFrameworkSender.PushRemainingFrames()
			if err != nil {
				return nil, err
			}

			for _, f := range fecFrames {
				payloadFrames = append(payloadFrames, f)
			}
		}

		for b := p.streamFramer.PopBlockedFrame(); b != nil; b = p.streamFramer.PopBlockedFrame() {
			p.controlFrames = append(p.controlFrames, b)
			p.isInControlFramesQueue[b] = true
		}
	}

	return payloadFrames, nil
}

func (p *packetPacker) QueueControlFrame(frame wire.Frame, pth *path) {
	switch f := frame.(type) {
	case *wire.StopWaitingFrame:
		p.stopWaiting[pth.pathID] = f
	case *wire.AckFrame:
		p.ackFrame[pth.pathID] = f
	default:
		if _, present := p.isInControlFramesQueue[f]; !present {
			p.controlFrames = append(p.controlFrames, f)
			p.isInControlFramesQueue[f] = true
		}
	}
}

func (p *packetPacker) getHeader(encLevel protocol.EncryptionLevel, pth *path) *wire.Header {
	pnum := pth.packetNumberGenerator.Peek()
	packetNumberLen := protocol.GetPacketNumberLengthForHeader(pnum, pth.leastUnacked)

	var isLongHeader bool
	if p.version.UsesTLS() && encLevel != protocol.EncryptionForwardSecure {
		// TODO: set the Long Header type
		packetNumberLen = protocol.PacketNumberLen4
		isLongHeader = true
		// TODO (QDC): we should raise an error if pth is not path 0
	}

	header := &wire.Header{
		ConnectionID:    p.connectionID,
		PacketNumber:    pnum,
		PacketNumberLen: packetNumberLen,
		IsLongHeader:    isLongHeader,
	}

	if p.omitConnectionID && encLevel == protocol.EncryptionForwardSecure {
		header.OmitConnectionID = true
	}
	if !p.version.UsesTLS() {
		if p.perspective == protocol.PerspectiveServer && encLevel == protocol.EncryptionSecure {
			header.DiversificationNonce = p.cryptoSetup.DiversificationNonce()
		}
		if p.perspective == protocol.PerspectiveClient && encLevel != protocol.EncryptionForwardSecure {
			header.VersionFlag = true
			header.Version = p.version
		}
	} else {
		header.Type = p.cryptoSetup.GetNextPacketType()
		if encLevel != protocol.EncryptionForwardSecure {
			header.Version = p.version
		}
	}

	// XXX (QDC): need a additional check because of tests
	if pth.sess != nil && pth.sess.IsHandshakeComplete() && p.version >= protocol.VersionMP {
		header.PathID = pth.pathID
		// XXX (QDC): in case of doubt, never truncate the connection ID. This might change...
		header.OmitConnectionID = false
	}

	return header
}

func (p *packetPacker) writeAndSealPacket(
	header *wire.Header,
	payloadFrames []wire.Frame,
	sealer handshake.Sealer,
	pth *path,
) ([]byte, error) {
	raw := getPacketBuffer()
	buffer := bytes.NewBuffer(raw)

	if err := header.Write(buffer, p.perspective, p.version); err != nil {
		return nil, err
	}
	payloadStartIndex := buffer.Len()
	for _, frame := range payloadFrames {
		err := frame.Write(buffer, p.version)
		if err != nil {
			return nil, err
		}
	}
	if protocol.ByteCount(buffer.Len()+sealer.Overhead()) > protocol.MaxPacketSize {
		log.Printf("%d frames of packet too large:", len(payloadFrames))
		for _, f := range payloadFrames {
			log.Printf("%T", f)
		}
		return nil, errors.New(fmt.Sprintf("PacketPacker BUG: packet too large: %d VS %d", protocol.ByteCount(buffer.Len()+sealer.Overhead()), protocol.MaxPacketSize))
	}

	raw = raw[0:buffer.Len()]
	// TODO: if we do not use FEC, the FEC scheduler is still rotating  here, maybe should we do a "peek" here and only rotate if it has been used.
	// TODO: there should be an option to send FEC even for other frames than UnreliableStreamFrames, maybe a parameter in the session, or... ?
	if header.FECFlag {
		// FEC-protected: give the packet to FEC framework
		if p.sess.config.OnlySendFECWhenApplicationLimited {
			p.sess.fecFrameworkSender.HandlePacket(raw, header)
		} else {
			_, err := p.sess.fecFrameworkSender.HandlePacketAndMaybePushRS(raw, header)
			if err != nil {
				return nil, err
			}
		}
	}
	_ = sealer.Seal(raw[payloadStartIndex:payloadStartIndex], raw[payloadStartIndex:], header.PacketNumber, raw[:payloadStartIndex])
	raw = raw[0 : buffer.Len()+sealer.Overhead()]

	num := pth.packetNumberGenerator.Pop()
	if num != header.PacketNumber {
		return nil, errors.New("packetPacker BUG: Peeked and Popped packet numbers do not match")
	}

	return raw, nil
}

func (p *packetPacker) canSendData(encLevel protocol.EncryptionLevel) bool {
	if p.perspective == protocol.PerspectiveClient {
		return encLevel >= protocol.EncryptionSecure
	}
	return encLevel == protocol.EncryptionForwardSecure
}

func (p *packetPacker) SetOmitConnectionID() {
	p.omitConnectionID = true
}

func (p *packetPacker) EstimateBufferedPacketCount() uint {
	lenCrypto := p.streamFramer.cryptoStream.LenOfDataForWriting()

	var lenStreams protocol.ByteCount
	for _, id := range p.streamFramer.streamsMap.openStreams {
		if s, ok := p.streamFramer.streamsMap.GetStream(id); ok {
			lenStreams += s.LenOfDataForWriting()
		}
	}

	return uint(lenCrypto + lenStreams/protocol.MaxPacketSize)
}
