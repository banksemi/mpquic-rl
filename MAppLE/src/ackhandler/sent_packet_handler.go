package ackhandler

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/qerr"
	"github.com/lucas-clemente/quic-go/sedpf"
)

const (
	// Maximum reordering in time space before time based loss detection considers a packet lost.
	// In fraction of an RTT.
	timeReorderingFraction = 1.0 / 8
	// The default RTT used before an RTT sample is taken.
	// Note: This constant is also defined in the congestion package.
	defaultInitialRTT = 100 * time.Millisecond
	// defaultRTOTimeout is the RTO time on new connections
	defaultRTOTimeout = 500 * time.Millisecond
	// Minimum time in the future a tail loss probe alarm may be set for.
	minTPLTimeout = 10 * time.Millisecond
	// Minimum time in the future an RTO alarm may be set for.
	minRTOTimeout = 200 * time.Millisecond
	// maxRTOTimeout is the maximum RTO time
	maxRTOTimeout = 60 * time.Second
	// Sends up to two tail loss probes before firing a RTO, as per
	// draft RFC draft-dukkipati-tcpm-tcp-loss-probe
	maxTailLossProbes = 2
	// TCP RFC calls for 1 second RTO however Linux differs from this default and
	// define the minimum RTO to 200ms, we will use the same until we have data to
	// support a higher or lower value
	minRetransmissionTime = 200 * time.Millisecond
	// Minimum tail loss probe time in ms
	minTailLossProbeTimeout = 10 * time.Millisecond

	// EXPERIMENTAL (see draft quic-recovery version 16)
	kReorderingThreshold = 3
)

var (
	// ErrDuplicateOrOutOfOrderAck occurs when a duplicate or an out-of-order ACK is received
	ErrDuplicateOrOutOfOrderAck = errors.New("SentPacketHandler: Duplicate or out-of-order ACK")
	// ErrTooManyTrackedSentPackets occurs when the sentPacketHandler has to keep track of too many packets
	ErrTooManyTrackedSentPackets = errors.New("Too many outstanding non-acked and non-retransmitted packets")
	// ErrAckForSkippedPacket occurs when the client sent an ACK for a packet number that we intentionally skipped
	ErrAckForSkippedPacket = qerr.Error(qerr.InvalidAckData, "Received an ACK for a skipped packet number")
	errAckForUnsentPacket  = qerr.Error(qerr.InvalidAckData, "Received ACK for an unsent package")
)

var errPacketNumberNotIncreasing = errors.New("Already sent a packet with a higher packet number")

type sentPacketHandler struct {
	lastSentPacketNumber protocol.PacketNumber
	skippedPackets       []protocol.PacketNumber

	numNonRetransmittablePackets int // number of non-retransmittable packets since the last retransmittable packet

	LargestAcked protocol.PacketNumber

	largestReceivedPacketWithAck protocol.PacketNumber

	packetHistory      *PacketList
	stopWaitingManager stopWaitingManager

	retransmissionQueue []*Packet

	bytesInFlight protocol.ByteCount

	congestion congestion.SendAlgorithmWithDebugInfo
	rttStats   *congestion.RTTStats

	onRTOCallback func(time.Time) bool

	handshakeComplete bool
	// The number of times the handshake packets have been retransmitted without receiving an ack.
	handshakeCount uint32
	// The number of times an RTO has been sent without receiving an ack.
	rtoCount uint32

	// The number of times a TLP has been sent without receiving an ACK
	tlpCount uint32

	// Was the alarm coming from the TLP computation?
	tlpAlarm bool

	// The time at which the next packet will be considered lost based on early transmit or exceeding the reordering window in time.
	lossTime time.Time

	// The time the last packet was sent, used to set the retransmission timeout
	lastSentTime time.Time

	// The alarm timeout
	alarm      time.Time
	alarmSetOn time.Time

	onPacketLost     func(protocol.PacketNumber)
	onPacketReceived func(protocol.PacketNumber, bool)

	packets         uint64
	retransmissions uint64
	losses          uint64

	useFastRetransmit bool

	pathID protocol.PathID
}

// NewSentPacketHandler creates a new sentPacketHandler
func NewSentPacketHandler(
	rttStats *congestion.RTTStats,
	cong congestion.SendAlgorithmWithDebugInfo,
	onRTOCallback func(time.Time) bool,
	onPacketLost func(protocol.PacketNumber),
	onPacketAcked func(protocol.PacketNumber, bool),
	useFastRetransmit bool,
	pathID protocol.PathID) SentPacketHandler {

	var congestionControl congestion.SendAlgorithmWithDebugInfo

	if cong != nil {
		congestionControl = cong
	} else {
		utils.Infof("using CUBIC congestion control on path %d\n", pathID)
		congestionControl = congestion.NewCubicSender(
			congestion.DefaultClock{},
			rttStats,
			true, //[> don't use reno since chromium doesn't (why?) <]
			//p.sess.GetConfig().CongestionControl==protocol.CongestionControlCubicReno,
			protocol.InitialCongestionWindow,
			protocol.DefaultMaxCongestionWindow,
		)
	}

	return &sentPacketHandler{
		packetHistory:      NewPacketList(),
		stopWaitingManager: stopWaitingManager{},
		rttStats:           rttStats,
		congestion:         congestionControl,
		onRTOCallback:      onRTOCallback,
		onPacketLost:       onPacketLost,
		onPacketReceived:   onPacketAcked,
		useFastRetransmit:  useFastRetransmit,
		pathID:             pathID,
	}
}

func (h *sentPacketHandler) GetStatistics() (uint64, uint64, uint64) {
	return h.packets, h.retransmissions, h.losses
}

func (h *sentPacketHandler) largestInOrderAcked() protocol.PacketNumber {
	if f := h.packetHistory.Front(); f != nil {
		return f.Value.PacketNumber - 1
	}
	return h.LargestAcked
}

func (h *sentPacketHandler) ShouldSendRetransmittablePacket() bool {
	return h.numNonRetransmittablePackets >= protocol.MaxNonRetransmittablePackets
}

func (h *sentPacketHandler) SetHandshakeComplete() {
	h.handshakeComplete = true
}

func (h *sentPacketHandler) SentPacket(packet *Packet) error {
	if packet.PacketNumber <= h.lastSentPacketNumber {
		//return errPacketNumberNotIncreasing
	}

	if protocol.PacketNumber(len(h.retransmissionQueue)+h.packetHistory.Len()+1) > protocol.MaxTrackedSentPackets {
		return ErrTooManyTrackedSentPackets
	}

	for p := h.lastSentPacketNumber + 1; p < packet.PacketNumber; p++ {
		h.skippedPackets = append(h.skippedPackets, p)

		if len(h.skippedPackets) > protocol.MaxTrackedSkippedPackets {
			h.skippedPackets = h.skippedPackets[1:]
		}
	}

	h.lastSentPacketNumber = packet.PacketNumber
	now := time.Now()

	// Update some statistics
	h.packets++

	//VUVA: RFC2861 CWND Validation
	delta := time.Since(h.lastSentTime)
	rto:=  h.ComputeRTOTimeout()
	if delta> rto{
		h.congestion.Cnwd_restart_after_idle(delta,rto)
	}

	// XXX RTO and TLP are recomputed based on the possible last sent retransmission. Is it ok like this?
	h.lastSentTime = now

	hasRetransmittableOrUnreliableFrames := HasRetransmittableOrUnreliableStreamFrames(packet.Frames)
	hasFECRelatedFrames := HasFECRelatedFrames(packet.Frames)
	//packet.Frames = stripNonRetransmittableExceptedUnrealiableStreamFramesOrFECRelatedFrames(packet.Frames)
	packet.Frames = stripNonRetransmittableExceptedUnrealiableStreamFrames(packet.Frames)
	isRetransmittable := len(packet.Frames) != 0

	if hasRetransmittableOrUnreliableFrames && !hasFECRelatedFrames {
		packet.SendTime = now
		h.bytesInFlight += packet.Length
		h.packetHistory.PushBack(*packet)
		h.numNonRetransmittablePackets = 0
	} else {
		h.numNonRetransmittablePackets++
	}

	h.congestion.OnPacketSent(
		now,
		h.bytesInFlight,
		packet.PacketNumber,
		packet.Length,
		isRetransmittable,
	)
	h.updateLossDetectionAlarm()
	return nil
}

func (h *sentPacketHandler) ReceivedRecoveredFrame(frame *wire.RecoveredFrame, encLevel protocol.EncryptionLevel) error {
	// don't update the rtt because the recovery may have delayed the ack
	ackedPackets, err := h.determineRecoveredPackets(frame)
	if err != nil {
		return err
	}
	log.Printf("recovered packets = %+v", ackedPackets)

	// this informs the FEC controller of successful recoveries by the other party
	for _, p := range ackedPackets {
		h.onPacketReceived(p.Value.PacketNumber, true)
	}

	if frame.RecoveredRanges[0].Last > h.LargestAcked {
		h.LargestAcked = frame.RecoveredRanges[0].Last
	}
	if len(ackedPackets) > 0 {
		for _, p := range ackedPackets {
			if encLevel < p.Value.EncryptionLevel {
				return fmt.Errorf("Received ACK with encryption level %s that acks a packet %d (encryption level %s)", encLevel, p.Value.PacketNumber, p.Value.EncryptionLevel)
			}
			h.onPacketRecovered(p)
			// TODO: maybe trigger the onPacketLost for the redundancy controller
			// the packet has been lost, then recovered. This might be due to congestion.
			//log.Printf("consider the recovered packet %d as lost for the congestion control", p.Value.PacketNumber)
			//h.congestion.OnPacketLost(p.Value.PacketNumber, p.Value.Length, h.bytesInFlight)
		}
	}

	h.detectLostPackets()
	h.updateLossDetectionAlarm()

	h.garbageCollectSkippedPackets()
	h.stopWaitingManager.ReceivedRecovered(frame)

	return nil
}

func (h *sentPacketHandler) ReceivedAck(ackFrame *wire.AckFrame, withPacketNumber protocol.PacketNumber, encLevel protocol.EncryptionLevel, rcvTime time.Time) error {
	if ackFrame.LargestAcked > h.lastSentPacketNumber {
		fmt.Printf("Ack saw largest was 0x%x but path actually saw 0x%x\n", ackFrame.LargestAcked, h.lastSentPacketNumber)
		//return errAckForUnsentPacket
	}

	// duplicate or out-of-order ACK
	if withPacketNumber <= h.largestReceivedPacketWithAck {
		return ErrDuplicateOrOutOfOrderAck
	}
	h.largestReceivedPacketWithAck = withPacketNumber

	// ignore repeated ACK (ACKs that don't have a higher LargestAcked than the last ACK)
	if ackFrame.LargestAcked <= h.largestInOrderAcked() {
		return nil
	}
	h.LargestAcked = ackFrame.LargestAcked

	if h.skippedPacketsAcked(ackFrame) {
		//return ErrAckForSkippedPacket
	}

	rttUpdated := h.maybeUpdateRTT(ackFrame.LargestAcked, ackFrame.DelayTime, rcvTime)

	if rttUpdated {
		h.congestion.MaybeExitSlowStart()
		bandwidthEstimate := float64(congestion.BandwidthFromDelta(
			h.congestion.GetCongestionWindow(),
			h.rttStats.SmoothedRTT(),
		))
		sedpf.InsertMeasurement(h.pathID, ackFrame.DelayTime.Seconds(), bandwidthEstimate)
	}

	ackedPackets, err := h.determineNewlyAckedPackets(ackFrame)
	if err != nil {
		return err
	}

	if len(ackedPackets) > 0 {
		for _, p := range ackedPackets {
			if encLevel < p.Value.EncryptionLevel {
				return fmt.Errorf("Received ACK with encryption level %s that acks a packet %d (encryption level %s)", encLevel, p.Value.PacketNumber, p.Value.EncryptionLevel)
			}
			h.onPacketAcked(p)
			h.onPacketReceived(p.Value.PacketNumber, false)
			h.congestion.OnPacketAcked(p.Value.PacketNumber, p.Value.Length, h.bytesInFlight)
		}
	}

	h.detectLostPackets()
	h.updateLossDetectionAlarm()

	h.garbageCollectSkippedPackets()
	h.stopWaitingManager.ReceivedAck(ackFrame)

	return nil
}

func (h *sentPacketHandler) determineRecoveredPackets(recoveredFrame *wire.RecoveredFrame) ([]*PacketElement, error) {
	var recoveredPackets []*PacketElement
	recoveredRangeIndex := 0
	lowestRecovered := recoveredFrame.RecoveredRanges[len(recoveredFrame.RecoveredRanges)-1].First
	largestRecovered := recoveredFrame.RecoveredRanges[0].Last
	log.Printf("determine recovered packets: lowest = %d, largest = %d", lowestRecovered, largestRecovered)
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		packet := el.Value
		packetNumber := packet.PacketNumber
		log.Printf("CHECK FOR PACKET NUMBER %d", packetNumber)
		// Ignore packets below the LowestAcked
		if packetNumber < lowestRecovered {
			continue
		}
		// Break after LargestAcked is reached
		if packetNumber > largestRecovered {
			break
		}

		recoveredRange := recoveredFrame.RecoveredRanges[len(recoveredFrame.RecoveredRanges)-1-recoveredRangeIndex]

		for packetNumber > recoveredRange.Last && recoveredRangeIndex < len(recoveredFrame.RecoveredRanges)-1 {
			recoveredRangeIndex++
			recoveredRange = recoveredFrame.RecoveredRanges[len(recoveredFrame.RecoveredRanges)-1-recoveredRangeIndex]
		}

		if packetNumber >= recoveredRange.First { // packet i contained in ACK range
			if packetNumber > recoveredRange.Last {
				return nil, fmt.Errorf("BUG: ackhandler would have acked wrong packet 0x%x, while evaluating range 0x%x -> 0x%x", packetNumber, recoveredRange.First, recoveredRange.Last)
			}
			recoveredPackets = append(recoveredPackets, el)
		}
	}

	return recoveredPackets, nil
}

func (h *sentPacketHandler) determineNewlyAckedPackets(ackFrame *wire.AckFrame) ([]*PacketElement, error) {
	var ackedPackets []*PacketElement
	ackRangeIndex := 0
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		packet := el.Value
		packetNumber := packet.PacketNumber

		// Ignore packets below the LowestAcked
		if packetNumber < ackFrame.LowestAcked {
			continue
		}
		// Break after LargestAcked is reached
		if packetNumber > ackFrame.LargestAcked {
			break
		}

		if ackFrame.HasMissingRanges() {
			ackRange := ackFrame.AckRanges[len(ackFrame.AckRanges)-1-ackRangeIndex]

			for packetNumber > ackRange.Last && ackRangeIndex < len(ackFrame.AckRanges)-1 {
				ackRangeIndex++
				ackRange = ackFrame.AckRanges[len(ackFrame.AckRanges)-1-ackRangeIndex]
			}

			if packetNumber >= ackRange.First { // packet i contained in ACK range
				if packetNumber > ackRange.Last {
					return nil, fmt.Errorf("BUG: ackhandler would have acked wrong packet 0x%x, while evaluating range 0x%x -> 0x%x", packetNumber, ackRange.First, ackRange.Last)
				}
				ackedPackets = append(ackedPackets, el)
			}
		} else {
			ackedPackets = append(ackedPackets, el)
		}
	}

	return ackedPackets, nil
}

func (h *sentPacketHandler) maybeUpdateRTT(largestAcked protocol.PacketNumber, ackDelay time.Duration, rcvTime time.Time) bool {
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		packet := el.Value
		if packet.PacketNumber == largestAcked {
			h.rttStats.UpdateRTT(rcvTime.Sub(packet.SendTime), ackDelay, time.Now())
			return true
		}
		// Packets are sorted by number, so we can stop searching
		if packet.PacketNumber > largestAcked {
			break
		}
	}
	return false
}

func (h *sentPacketHandler) hasOutstandingRetransmittablePacket() bool {
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		if el.Value.IsRetransmittable() {
			return true
		}
	}
	return false
}

func (h *sentPacketHandler) updateLossDetectionAlarm() {
	h.tlpAlarm = false
	// Cancel the alarm if no packets are outstanding
	if h.packetHistory.Len() == 0 {
		h.alarm = time.Time{}
		return
	}
	if !h.handshakeComplete {
		h.alarm = time.Now().Add(h.computeHandshakeTimeout())
	} else if !h.lossTime.IsZero() {
		// Early retransmit timer or time loss detection.
		h.alarm = h.lossTime
	} else if h.rttStats.SmoothedRTT() != 0 && h.tlpCount < maxTailLossProbes {
		// TLP
		h.tlpAlarm = true
		h.alarm = h.lastSentTime.Add(h.computeTLPTimeout())
	} else {
		// RTO
		// check RTO timer...
		h.alarm = h.lastSentTime.Add(utils.MaxDuration(h.ComputeRTOTimeout(), minRetransmissionTime))
		firstPacketTime := h.packetHistory.Front().Value.SendTime
		rtoAlarm := firstPacketTime.Add(utils.MaxDuration(h.ComputeRTOTimeout(), minRetransmissionTime))
		h.alarm = utils.MaxTime(rtoAlarm, time.Now().Add(1*time.Microsecond))

		// ... then look for TLP
		tlpAlarm := h.lastSentTime.Add(utils.MaxDuration(h.ComputeRTOTimeout(), minRetransmissionTime))
		if tlpAlarm.Before(h.alarm) {
			h.alarm = utils.MaxTime(tlpAlarm, time.Now().Add(1*time.Microsecond))
			h.tlpAlarm = true
		}
	}
	h.alarmSetOn = time.Now()
}

func (h *sentPacketHandler) detectLostPackets() {
	h.lossTime = time.Time{}
	now := time.Now()

	maxRTT := float64(utils.MaxDuration(h.rttStats.LatestRTT(), h.rttStats.SmoothedRTT()))
	delayUntilLost := time.Duration((1.0 + timeReorderingFraction) * maxRTT)

	var lostPackets []*PacketElement
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		packet := el.Value

		if packet.PacketNumber > h.LargestAcked {
			break
		}

		timeSinceSent := now.Sub(packet.SendTime)
		if (h.useFastRetransmit && h.LargestAcked >= kReorderingThreshold && packet.PacketNumber <= h.LargestAcked-kReorderingThreshold) || timeSinceSent > delayUntilLost {
			// Update statistics
			h.losses++
			lostPackets = append(lostPackets, el)
		} else if h.lossTime.IsZero() {
			// Note: This conditional is only entered once per call
			h.lossTime = now.Add(delayUntilLost - timeSinceSent)
		}
	}

	if len(lostPackets) > 0 {
		for _, p := range lostPackets {
			timeSinceSent := now.Sub(p.Value.SendTime)
			log.Printf("PACKET LOST: %d, largestAcked = %d, timeSinceSent = %d, delayUntilLost = %d", p.Value.PacketNumber, h.LargestAcked, timeSinceSent, delayUntilLost)
			if !HasRetransmittableFrames(p.Value.Frames) {
				// Copied from h.ReceivedAck
				h.onPacketAcked(p)
			} else {
				h.queuePacketForRetransmission(p)
			}
			h.onPacketLost(p.Value.PacketNumber)
			h.congestion.OnPacketLost(p.Value.PacketNumber, p.Value.Length, h.bytesInFlight)
		}
	}
}

func (h *sentPacketHandler) SetInflightAsLost() {
	var lostPackets []*PacketElement
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		packet := el.Value

		if packet.PacketNumber > h.LargestAcked {
			break
		}

		h.losses++
		lostPackets = append(lostPackets, el)
	}

	if len(lostPackets) > 0 {
		for _, p := range lostPackets {
			h.queuePacketForRetransmission(p)
			// XXX (QDC): should we?
			h.congestion.OnPacketLost(p.Value.PacketNumber, p.Value.Length, h.bytesInFlight)
		}
	}
}

func (h *sentPacketHandler) OnAlarm() {
	// Do we really have packet to retransmit?
	if !h.hasOutstandingRetransmittablePacket() {
		// Cancel then the alarm
		h.alarm = time.Time{}
		return
	}

	if !h.handshakeComplete {
		h.queueHandshakePacketsForRetransmission()
		h.handshakeCount++
	} else if !h.lossTime.IsZero() {
		// Early retransmit or time loss detection
		h.detectLostPackets()
	} else if h.tlpAlarm && h.tlpCount < maxTailLossProbes {
		// TLP
		h.retransmitTLP()
		h.tlpCount++
	} else {
		// RTO
		potentiallyFailed := false
		if h.onRTOCallback != nil {
			potentiallyFailed = h.onRTOCallback(h.lastSentTime)
		}
		if potentiallyFailed {
			h.retransmitAllPackets()
		} else {
			h.retransmitOldestTwoPackets()
		}
		h.rtoCount++
	}

	h.updateLossDetectionAlarm()
}

func (h *sentPacketHandler) GetAlarmTimeout() time.Time {
	return h.alarm
}

func (h *sentPacketHandler) onPacketAcked(packetElement *PacketElement) {
	if HasRetransmittableOrUnreliableStreamFrames(packetElement.Value.Frames) || HasFECRelatedFrames(packetElement.Value.Frames) {
		h.bytesInFlight -= packetElement.Value.Length
	}
	h.rtoCount = 0
	h.handshakeCount = 0
	h.tlpCount = 0
	h.packetHistory.Remove(packetElement)
}

func (h *sentPacketHandler) GetPacketsInFlight() []*Packet {
	var packets = make([]*Packet, 0, h.packetHistory.Len())
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		packets = append(packets, &el.Value)
	}
	return packets
}

func (h *sentPacketHandler) onPacketRecovered(packetElement *PacketElement) {
	h.onPacketAcked(packetElement)
}

func (h *sentPacketHandler) DequeuePacketForRetransmission() *Packet {
	if len(h.retransmissionQueue) == 0 {
		return nil
	}
	packet := h.retransmissionQueue[0]
	// Shift the slice and don't retain anything that isn't needed.
	h.retransmissionQueue = h.retransmissionQueue[1:]
	// Update statistics
	h.retransmissions++
	return packet
}

func (h *sentPacketHandler) GetLeastUnacked() protocol.PacketNumber {
	return h.largestInOrderAcked() + 1
}

func (h *sentPacketHandler) GetStopWaitingFrame(force bool) *wire.StopWaitingFrame {
	return h.stopWaitingManager.GetStopWaitingFrame(force)
}

func (h *sentPacketHandler) GetBytesInFlight() protocol.ByteCount {
	return h.bytesInFlight
}

func (h *sentPacketHandler) SendingAllowed() bool {
	congestionLimited := h.bytesInFlight > h.congestion.GetCongestionWindow()
	maxTrackedLimited := protocol.PacketNumber(len(h.retransmissionQueue)+h.packetHistory.Len()) >= protocol.MaxTrackedSentPackets
	if congestionLimited {
		utils.Debugf("Congestion limited: bytes in flight %d, window %d",
			h.bytesInFlight,
			h.congestion.GetCongestionWindow())
	}
	// Workaround for #555:
	// Always allow sending of retransmissions. This should probably be limited
	// to RTOs, but we currently don't have a nice way of distinguishing them.
	haveRetransmissions := len(h.retransmissionQueue) > 0
	return !protocol.APPLY_CONGESTION_CONTROL || !maxTrackedLimited && (!congestionLimited || haveRetransmissions)
}

func (h *sentPacketHandler) retransmitTLP() {
	if p := h.packetHistory.Back(); p != nil {
		h.queuePacketForRetransmission(p)
	}
}

func (h *sentPacketHandler) retransmitAllPackets() {
	for h.packetHistory.Len() > 0 {
		h.queueRTO(h.packetHistory.Front())
	}
	log.Printf("RETRANSMIT ALL PACKETS")
	h.congestion.OnRetransmissionTimeout(true)
}

func (h *sentPacketHandler) retransmitOldestPacket() {
	if p := h.packetHistory.Front(); p != nil {
		h.queueRTO(p)
	}
}

func (h *sentPacketHandler) retransmitOldestTwoPackets() {
	log.Printf("RETRANSMIT OLDEST TWO")
	h.retransmitOldestPacket()
	h.retransmitOldestPacket()
	h.congestion.OnRetransmissionTimeout(true)
}

func (h *sentPacketHandler) queueRTO(el *PacketElement) {
	packet := &el.Value
	utils.Debugf(
		"\tQueueing packet 0x%x for retransmission (RTO), %d outstanding",
		packet.PacketNumber,
		h.packetHistory.Len(),
	)
	h.queuePacketForRetransmission(el)
	h.onPacketLost(packet.PacketNumber)
	h.losses++
	h.congestion.OnPacketLost(packet.PacketNumber, packet.Length, h.bytesInFlight)
}

func (h *sentPacketHandler) queueHandshakePacketsForRetransmission() {
	var handshakePackets []*PacketElement
	for el := h.packetHistory.Front(); el != nil; el = el.Next() {
		if el.Value.EncryptionLevel < protocol.EncryptionForwardSecure {
			handshakePackets = append(handshakePackets, el)
		}
	}
	for _, el := range handshakePackets {
		h.queuePacketForRetransmission(el)
	}
}

func (h *sentPacketHandler) queuePacketForRetransmission(packetElement *PacketElement) {
	packet := &packetElement.Value
	h.bytesInFlight -= packet.Length
	h.retransmissionQueue = append(h.retransmissionQueue, packet)
	h.packetHistory.Remove(packetElement)
	h.stopWaitingManager.QueuedRetransmissionForPacketNumber(packet.PacketNumber)
}

func (h *sentPacketHandler) DuplicatePacket(packet *Packet) {
	h.retransmissionQueue = append(h.retransmissionQueue, packet)
}

func (h *sentPacketHandler) computeHandshakeTimeout() time.Duration {
	duration := 2 * h.rttStats.SmoothedRTT()
	if duration == 0 {
		duration = 2 * defaultInitialRTT
	}
	duration = utils.MaxDuration(duration, minTPLTimeout)
	// exponential backoff
	// There's an implicit limit to this set by the handshake timeout.
	return duration << h.handshakeCount
}

func (h *sentPacketHandler) ComputeRTOTimeout() time.Duration {
	rto := h.congestion.RetransmissionDelay()
	if rto == 0 {
		rto = defaultRTOTimeout
	}
	rto = utils.MaxDuration(rto, minRTOTimeout)
	// Exponential backoff
	rto = rto << h.rtoCount
	return utils.MinDuration(rto, maxRTOTimeout)
}

func (h *sentPacketHandler) hasMultipleOutstandingRetransmittablePackets() bool {
	return h.packetHistory.Front() != nil && h.packetHistory.Front().Next() != nil
}

func (h *sentPacketHandler) computeTLPTimeout() time.Duration {
	rtt := h.congestion.SmoothedRTT()
	if h.hasMultipleOutstandingRetransmittablePackets() {
		return utils.MaxDuration(2*rtt, rtt*3/2+minRetransmissionTime/2)
	}
	return utils.MaxDuration(2*rtt, minTailLossProbeTimeout)
}

func (h *sentPacketHandler) skippedPacketsAcked(ackFrame *wire.AckFrame) bool {
	for _, p := range h.skippedPackets {
		if ackFrame.AcksPacket(p) {
			return true
		}
	}
	return false
}

func (h *sentPacketHandler) garbageCollectSkippedPackets() {
	lioa := h.largestInOrderAcked()
	deleteIndex := 0
	for i, p := range h.skippedPackets {
		if p <= lioa {
			deleteIndex = i + 1
		}
	}
	h.skippedPackets = h.skippedPackets[deleteIndex:]
}

func (h *sentPacketHandler) GetSendAlgorithm() congestion.SendAlgorithmWithDebugInfo {
	return h.congestion
}

func (h *sentPacketHandler) GetLastSendTime() time.Time{
	return h.lastSentTime
}
