package quic

import (
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/qerr"
	"math"
	"time"
)

const (
	minPathTimer = 10 * time.Millisecond
	// XXX (QDC): To avoid idling...
	maxPathTimer = 1 * time.Second

	LOSS_RATE_WINDOW_SIZE      = 2000
	LOSS_RATE_SMOOTHING_FACTOR = 0.75
)

type path struct {
	lastpacketnumber protocol.PacketNumber
	pathID protocol.PathID
	conn   connection
	sess   sessionI

	rttStats *congestion.RTTStats

	sentPacketHandler     ackhandler.SentPacketHandler
	receivedPacketHandler ackhandler.ReceivedPacketHandler

	backup utils.AtomicBool

	active    utils.AtomicBool
	closeChan chan *qerr.QuicError

	locAddrID      protocol.AddressID
	remAddrID      protocol.AddressID
	validRemAddrID bool // When the remote announce a lost address, the remAddrID is no more valid until a PATHS frame has been received

	potentiallyFailed utils.AtomicBool
	// The path might be flaky, keep this information
	wasPotentiallyFailed utils.AtomicBool
	// It might be useful to know that this path faced a RTO at some point
	facedRTO utils.AtomicBool

	sentPacket chan struct{}

	// It is now the responsibility of the path to keep its packet number
	packetNumberGenerator *packetNumberGenerator

	lastRcvdPacketNumber protocol.PacketNumber
	// Used to calculate the next packet number from the truncated wire
	// representation, and sent back in public reset packets
	largestRcvdPacketNumber protocol.PacketNumber

	leastUnacked protocol.PacketNumber

	lastNetworkActivityTime time.Time

	timer *utils.Timer

	fec *utils.AtomicBool

	// metadata for windowed loss rate measurement
	windowedLossRate struct {
		// actual ratio (use this value!)
		ratio float64

		// recordings for next window (never directly use these values!)
		nLosses uint
		nAcks   uint
	}
}

// FIXME this is why we should change the PathID when network changes...
func (p *path) setupReusePath(oliaSenders map[protocol.PathID]*congestion.OliaSender) {
	var cong congestion.SendAlgorithm

	if p.sess.GetVersion() >= protocol.VersionMP && oliaSenders != nil && p.pathID != protocol.InitialPathID {
		cong = congestion.NewOliaSender(oliaSenders, p.rttStats, protocol.InitialCongestionWindow, protocol.DefaultMaxCongestionWindow)
		oliaSenders[p.pathID] = cong.(*congestion.OliaSender)
	}

	p.active.Set(true)
	p.validRemAddrID = true
	p.potentiallyFailed.Set(false)
	p.wasPotentiallyFailed.Set(false)
	p.facedRTO.Set(false)
}

// setup initializes values that are independent of the perspective
func (p *path) setup(oliaSenders map[protocol.PathID]*congestion.OliaSender, redundancyController fec.RedundancyController) {
	p.rttStats = &congestion.RTTStats{}

	var cong congestion.SendAlgorithmWithDebugInfo

	cc := p.sess.GetConfig().CongestionControl
	utils.Debugf("Start Congestion control: %d", cc)
	if cc == protocol.CongestionControlOlia &&
		p.sess.GetVersion() >= protocol.VersionMP && oliaSenders != nil && p.pathID != protocol.InitialPathID {
		// OLIA congestion control on this path
		cong = congestion.NewOliaSender(oliaSenders, p.rttStats, protocol.InitialCongestionWindow, protocol.DefaultMaxCongestionWindow)
		oliaSenders[p.pathID] = cong.(*congestion.OliaSender)
	} else if cc != protocol.CongestionControlOlia {
		// CUBIC congestion control on this path
		clock := congestion.DefaultClock{}
		reno := cc == protocol.CongestionControlCubicReno
		cong = congestion.NewCubicSender(clock, p.rttStats, reno, protocol.InitialCongestionWindow, protocol.DefaultMaxCongestionWindow)
	}

	sentPacketHandler := ackhandler.NewSentPacketHandler(p.rttStats, cong, p.onRTO,
		func(pn protocol.PacketNumber) {
			// on packet lost
			redundancyController.OnPacketLost(pn)
			p.RecordLoss()
		},
		func(pn protocol.PacketNumber, recovered bool) {
			// on packet received
			redundancyController.OnPacketReceived(pn, recovered)
			if !recovered {
				p.RecordAcknowledgement()
			}
		},
		p.sess.GetConfig().UseFastRetransmit,
		p.pathID,
	)

	if p.pathID != protocol.InitialPathID {
		// A new path has been created, so the handshake completed
		sentPacketHandler.SetHandshakeComplete()
	}

	now := time.Now()

	p.sentPacketHandler = sentPacketHandler
	p.receivedPacketHandler = ackhandler.NewReceivedPacketHandler(p.sess.GetVersion(), p.sess.GetConfig().DisableFECRecoveredFrames)

	p.packetNumberGenerator = newPacketNumberGenerator(protocol.SkipPacketAveragePeriodLength)

	p.closeChan = make(chan *qerr.QuicError, 1)
	p.sentPacket = make(chan struct{}, 1)

	p.timer = utils.NewTimer()
	p.lastNetworkActivityTime = now

	p.active.Set(true)
	p.potentiallyFailed.Set(false)
	p.wasPotentiallyFailed.Set(false)
	p.facedRTO.Set(false)
	p.validRemAddrID = true

	p.fec = &utils.AtomicBool{}

	// Once the path is setup, run it
	go p.run()
}

func (p *path) run() {
	// XXX (QDC): relay everything to the session, maybe not the most efficient
runLoop:
	for {
		// Close immediately if requested
		select {
		case <-p.closeChan:
			break runLoop
		default:
		}

		p.maybeResetTimer()

		select {
		case <-p.closeChan:
			break runLoop
		case <-p.timer.Chan():
			p.timer.SetRead()
			select {
			case p.sess.PathTimersChan() <- p:
			// XXX (QDC): don't remain stuck here!
			case <-p.closeChan:
				break runLoop
			case <-p.sentPacket:
				// Don't remain stuck here!
			}
		case <-p.sentPacket:
			// Used to reset the path timer
		}
	}
	p.active.Set(false)
	if p.sess.PathManager() != nil {
		p.sess.PathManager().wg.Done()
	}
}

func (p *path) SendingAllowed() bool {
	return p.active.Get() && p.sentPacketHandler.SendingAllowed()
}

func (p *path) GetStopWaitingFrame(force bool) *wire.StopWaitingFrame {
	return p.sentPacketHandler.GetStopWaitingFrame(force)
}

func (p *path) GetAckFrame() *wire.AckFrame {
	ack := p.receivedPacketHandler.GetAckFrame()
	if ack != nil {
		ack.PathID = p.pathID
	}

	return ack
}

func (p *path) GetRecoveredFrame() *wire.RecoveredFrame {
	return p.receivedPacketHandler.GetRecoveredFrame()
}

func (p *path) maybeResetTimer() {
	deadline := p.lastNetworkActivityTime.Add(p.idleTimeout())

	if ackAlarm := p.receivedPacketHandler.GetAlarmTimeout(); !ackAlarm.IsZero() {
		deadline = ackAlarm
	}
	if lossTime := p.sentPacketHandler.GetAlarmTimeout(); !lossTime.IsZero() {
		deadline = utils.MinTime(deadline, lossTime)
	}

	deadline = utils.MinTime(utils.MaxTime(deadline, time.Now().Add(minPathTimer)), time.Now().Add(maxPathTimer))

	p.timer.Reset(deadline)
}

func (p *path) idleTimeout() time.Duration {
	// TODO (QDC): probably this should be refined at path level
	cryptoSetup := p.sess.GetCryptoSetup()
	if cryptoSetup != nil {
		config := p.sess.GetConfig()
		if p.active.Get() && (p.pathID != 0 || p.sess.IsHandshakeComplete()) {
			return config.IdleTimeout
		}
		return config.HandshakeTimeout
	}
	return time.Second
}

func (p *path) handlePacketImpl(pkt *receivedPacket) (*unpackedPacket, error) {
	if !p.active.Get() {
		// We just got some response from remote!
		p.active.Set(true)
		// If we lost connectivity for local reason, identify the current local address ID
		if p.conn == nil && pkt.rcvPconn != nil {
			p.sess.PathManager().pconnMgr.PconnsLock().RLock()
			p.conn = &conn{pconn: pkt.rcvPconn, currentAddr: pkt.remoteAddr}
			locAddrID, ok := p.sess.PathManager().pconnMgr.GetAddrIDOf(pkt.rcvPconn.LocalAddr())
			if ok {
				p.locAddrID = locAddrID
			}
			p.sess.PathManager().pconnMgr.PconnsLock().RUnlock()
		}
	}

	if !pkt.rcvTime.IsZero() {
		p.lastNetworkActivityTime = pkt.rcvTime
	}
	hdr := pkt.header
	data := pkt.data

	// We just received a new packet on that path, so it works
	p.potentiallyFailed.Set(false)

	// Calculate packet number
	hdr.PacketNumber = protocol.InferPacketNumber(
		hdr.PacketNumberLen,
		p.largestRcvdPacketNumber,
		hdr.PacketNumber,
	)

	packet, err := p.sess.GetUnpacker().Unpack(hdr.Raw, hdr, data, pkt.recovered)
	if utils.Debug() {
		if err != nil {
			utils.Debugf("<- Reading packet 0x%x (%d bytes) for connection %x on path %x", hdr.PacketNumber, len(data)+len(hdr.Raw), hdr.ConnectionID, p.pathID)
		} else {
			utils.Debugf("<- Reading packet 0x%x (%d bytes) for connection %x on path %x, %s", hdr.PacketNumber, len(data)+len(hdr.Raw), hdr.ConnectionID, p.pathID, packet.encryptionLevel)
		}
		hdr.Log()
	}

	// if the decryption failed, this might be a packet sent by an attacker
	// don't update the remote address
	if quicErr, ok := err.(*qerr.QuicError); ok && quicErr.ErrorCode == qerr.DecryptionFailure {
		return nil, err
	}
	if p.sess.GetPerspective() == protocol.PerspectiveServer {
		// update the remote address, even if unpacking failed for any other reason than a decryption error
		p.conn.SetCurrentRemoteAddr(pkt.remoteAddr)
	}
	if err != nil {
		return nil, err
	}

	p.lastRcvdPacketNumber = hdr.PacketNumber
	// Only do this after decrupting, so we are sure the packet is not attacker-controlled
	p.largestRcvdPacketNumber = utils.MaxPacketNumber(p.largestRcvdPacketNumber, hdr.PacketNumber)
	p.sess.MaybeSetLargestRcvdPacketNumber(hdr.PacketNumber)

	// we should send an ack if we receive only FEC frames: it could be because a path is used only for FEC Frames
	containsOnlyFECFrames := true
	for _, f := range packet.frames {
		if _, ok := f.(*wire.FECFrame); !ok {
			containsOnlyFECFrames = false
		}
	}

	isRetransmittable := ackhandler.HasRetransmittableOrUnreliableStreamFrames(packet.frames)
	if err = p.receivedPacketHandler.ReceivedPacket(hdr.PacketNumber, !pkt.recovered && (isRetransmittable || containsOnlyFECFrames), pkt.recovered); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	return packet, nil
}

func (p *path) onRTO(lastSentTime time.Time) bool {
	p.facedRTO.Set(true)
	// Was there any activity since last sent packet?
	// XXX for the experiments: ignore this
	if false && p.lastNetworkActivityTime.Before(lastSentTime) {
		p.potentiallyFailed.Set(true)
		p.wasPotentiallyFailed.Set(true)
		return true
	}
	return false
}

func (p *path) SetLeastUnacked(leastUnacked protocol.PacketNumber) {
	p.leastUnacked = leastUnacked
}

func (p *path) GetCongestionWindow() protocol.ByteCount {
	return p.sentPacketHandler.GetSendAlgorithm().GetCongestionWindow()
}

func (p *path) GetCongestionWindowFree() protocol.ByteCount {
	if !p.SendingAllowed() {
		return 0
	}

	return p.GetCongestionWindow() - p.sentPacketHandler.GetBytesInFlight()
}

func (p *path) maybeUpdateLossAckRatio() {
	if p.windowedLossRate.nAcks == 0 {
		// require at least one ack before calculating a new ratio to
		// avoid divison by zero
		return
	}

	if p.windowedLossRate.nAcks+p.windowedLossRate.nLosses > LOSS_RATE_WINDOW_SIZE {
		p.windowedLossRate.ratio = (1-LOSS_RATE_SMOOTHING_FACTOR)*p.windowedLossRate.ratio +
			LOSS_RATE_SMOOTHING_FACTOR*(float64(p.windowedLossRate.nLosses)/float64(p.windowedLossRate.nAcks))
			//		p.windowedLossRate.ratio = float64(p.windowedLossRate.nLosses) / float64(p.windowedLossRate.nAcks)
		p.windowedLossRate.nAcks = 0
		p.windowedLossRate.nLosses = 0
	}
}

func (p *path) RecordLoss() {
	p.windowedLossRate.nLosses++
	// don't call p.maybeUpdateLossAckRatio() here
	// this avoids the odd division by zero
	p.maybeUpdateLossAckRatio()
}

func (p *path) RecordAcknowledgement() {
	p.windowedLossRate.nAcks++
	p.maybeUpdateLossAckRatio()
}

func (p *path) GetWindowedLossRatio() float64 {
	return p.windowedLossRate.ratio
}

func (p *path) GetLAMPSLossRate() float64 {
	getEstimatedLossRate := func(packetsBetweenTwoLosses, currentPacketsSinceLastLost float64) float64 {
		if packetsBetweenTwoLosses == -1 {
			return 0.0
		}
		return 1.0 / math.Max(packetsBetweenTwoLosses, currentPacketsSinceLastLost) // MPTCP LAMPS
	}

	oSender := p.sess.PathManager().oliaSenders[p.pathID]
	return getEstimatedLossRate(oSender.GetSmoothedBytesBetweenTwoLosses(), float64(oSender.GetCurrentPacketsSinceLastLoss()))
}
