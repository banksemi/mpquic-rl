package ackhandler

import (
	"errors"
	"log"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

var errInvalidPacketNumber = errors.New("ReceivedPacketHandler: Invalid packet number")

type receivedPacketHandler struct {
	largestObserved             protocol.PacketNumber
	lowerLimit                  protocol.PacketNumber
	largestObservedReceivedTime time.Time

	packetHistory          *receivedPacketHistory
	recoveredPacketHistory *recoveredPacketHistory

	ackSendDelay       time.Duration
	recoveredSendDelay time.Duration

	packetsReceivedSinceLastAck                int
	packetsReceivedSinceLastRecovered          int
	retransmittablePacketsReceivedSinceLastAck int
	ackQueued                                  bool
	recoveredQueued                            bool
	ackAlarm                                   time.Time
	recoveredAlarm                             time.Time
	lastAck                                    *wire.AckFrame
	lastRecovered                              *wire.RecoveredFrame

	disableRecoveredFrames bool
	version                protocol.VersionNumber

	packets          uint64
	recoveredPackets uint64
}

// NewReceivedPacketHandler creates a new receivedPacketHandler
func NewReceivedPacketHandler(version protocol.VersionNumber, disableRecoveredFrames bool) ReceivedPacketHandler {
	return &receivedPacketHandler{
		packetHistory:          newReceivedPacketHistory(),
		recoveredPacketHistory: newRecoveredPacketHistory(),
		ackSendDelay:           protocol.AckSendDelay,
		recoveredSendDelay:     protocol.AckSendDelay,
		disableRecoveredFrames: disableRecoveredFrames,
		version:                version,
	}
}

// returns (receivedPackets, recoveredPackets)
func (h *receivedPacketHandler) GetStatistics() (uint64, uint64) {
	return h.packets, h.recoveredPackets
}

func (h *receivedPacketHandler) ReceivedPacket(packetNumber protocol.PacketNumber, shouldInstigateAck bool, recovered bool) error {
	if packetNumber == 0 {
		return errInvalidPacketNumber
	}

	// A new packet was received on that path and passes checks, so count it for stats
	h.packets++
	if recovered {
		h.recoveredPackets++
	}

	if packetNumber > h.largestObserved {
		h.largestObserved = packetNumber
		h.largestObservedReceivedTime = time.Now()
	}

	if packetNumber <= h.lowerLimit {
		return nil
	}

	if h.disableRecoveredFrames || !recovered {
		// we received a packet normally
		if err := h.packetHistory.ReceivedPacket(packetNumber); err != nil {
			return err
		}
		h.maybeQueueAck(packetNumber, shouldInstigateAck)
	} else {
		log.Printf("MAYBE QUEUE RECOVERED FRAME")
		// we received a packet by recovering it
		if err := h.recoveredPacketHistory.RecoveredPacket(packetNumber); err != nil {
			return err
		}
		if err := h.packetHistory.ReceivedPacket(packetNumber); err != nil {
			return err
		}
		h.maybeQueueRecovered(packetNumber, recovered)
		//h.maybeQueueAck(packetNumber, shouldInstigateAck)
	}
	return nil
}

// SetLowerLimit sets a lower limit for acking packets.
// Packets with packet numbers smaller or equal than p will not be acked.
func (h *receivedPacketHandler) SetLowerLimit(p protocol.PacketNumber) {
	h.lowerLimit = p
	h.packetHistory.DeleteUpTo(p)
	h.recoveredPacketHistory.DeleteUpTo(p)
}

func (h *receivedPacketHandler) maybeQueueAck(packetNumber protocol.PacketNumber, shouldInstigateAck bool) {
	h.packetsReceivedSinceLastAck++

	if shouldInstigateAck {
		h.retransmittablePacketsReceivedSinceLastAck++
	}

	// always ack the first packet
	if h.lastAck == nil {
		h.ackQueued = true
	}

	if h.version < protocol.Version39 {
		// Always send an ack every 20 packets in order to allow the peer to discard
		// information from the SentPacketManager and provide an RTT measurement.
		// From QUIC 39, this is not needed anymore, since the peer will regularly send a retransmittable packet.
		if h.packetsReceivedSinceLastAck >= protocol.MaxPacketsReceivedBeforeAckSend {
			h.ackQueued = true
		}
	}

	// if the packet number is smaller than the largest acked packet, it must have been reported missing with the last ACK
	// note that it cannot be a duplicate because they're already filtered out by ReceivedPacket()
	if h.lastAck != nil && packetNumber < h.lastAck.LargestAcked {
		h.ackQueued = true
	}

	// check if a new missing range above the previously was created
	if h.lastAck != nil && h.packetHistory.GetHighestAckRange().First > h.lastAck.LargestAcked {
		h.ackQueued = true
	}

	if !h.ackQueued && shouldInstigateAck {
		if h.retransmittablePacketsReceivedSinceLastAck >= protocol.RetransmittablePacketsBeforeAck {
			h.ackQueued = true
		} else {
			if h.ackAlarm.IsZero() {
				h.ackAlarm = time.Now().Add(h.ackSendDelay)
			}
		}
	}

	if h.ackQueued {
		// cancel the ack alarm
		h.ackAlarm = time.Time{}
	}
}

func (h *receivedPacketHandler) maybeQueueRecovered(packetNumber protocol.PacketNumber, hasbeenRecovered bool) {
	h.packetsReceivedSinceLastRecovered++

	if !hasbeenRecovered {
		return
	}

	// always ack the first packet
	if h.lastRecovered == nil {
		h.recoveredQueued = true
	}

	if h.version < protocol.Version39 {
		// Always send an ack every 20 packets in order to allow the peer to discard
		// information from the SentPacketManager and provide an RTT measurement.
		// From QUIC 39, this is not needed anymore, since the peer will regularly send a retransmittable packet.
		if h.packetsReceivedSinceLastRecovered >= protocol.MaxPacketsReceivedBeforeAckSend {
			h.recoveredQueued = true
		}
	}

	// if the packet number is smaller than the largest acked packet, it must have been reported missing with the last ACK
	// note that it cannot be a duplicate because they're already filtered out by ReceivedPacket()
	if h.lastRecovered != nil && packetNumber < h.lastRecovered.RecoveredRanges[0].Last {
		h.recoveredQueued = true
	}

	// check if a new missing range above the previously was created
	if h.lastRecovered != nil && h.recoveredPacketHistory.GetHighestRecoveredRange().First > h.lastRecovered.RecoveredRanges[0].Last {
		h.recoveredQueued = true
	}

	if !h.recoveredQueued {
		if h.recoveredAlarm.IsZero() {
			h.recoveredAlarm = time.Now().Add(h.recoveredSendDelay)
		}
	}

	if h.recoveredQueued {
		// cancel the ack alarm
		log.Printf("queue recovered packet %d", packetNumber)
		h.recoveredAlarm = time.Time{}
	}
}

func (h *receivedPacketHandler) GetAckFrame() *wire.AckFrame {
	if !h.ackQueued && (h.ackAlarm.IsZero() || h.ackAlarm.After(time.Now())) {
		return nil
	}

	ackRanges := h.packetHistory.GetAckRanges()

	var ack *wire.AckFrame
	if len(ackRanges) > 0 {
		ack = &wire.AckFrame{
			LargestAcked:       h.largestObserved,
			LowestAcked:        ackRanges[len(ackRanges)-1].First,
			PacketReceivedTime: h.largestObservedReceivedTime,
		}

		if len(ackRanges) > 1 {
			ack.AckRanges = ackRanges
		}
	} else {
		ack = nil
	}

	h.lastAck = ack
	h.ackAlarm = time.Time{}
	h.ackQueued = false
	h.packetsReceivedSinceLastAck = 0
	h.retransmittablePacketsReceivedSinceLastAck = 0

	return ack
}

func (h *receivedPacketHandler) GetRecoveredFrame() *wire.RecoveredFrame {
	if !h.recoveredQueued {
		return nil
	}
	if !h.recoveredQueued && (h.recoveredAlarm.IsZero() || h.recoveredAlarm.After(time.Now())) {
		return nil
	}

	recoveredRanges := h.recoveredPacketHistory.GetRecoveredRanges()
	recovered := &wire.RecoveredFrame{}

	if len(recoveredRanges) >= 1 {
		recovered.RecoveredRanges = recoveredRanges
	} else {
		log.Printf("no range")
		h.recoveredQueued = false
		return nil
	}

	h.lastRecovered = recovered
	h.recoveredAlarm = time.Time{}
	h.recoveredQueued = false
	h.packetsReceivedSinceLastRecovered = 0

	return recovered
}

func (h *receivedPacketHandler) GetAlarmTimeout() time.Time { return h.ackAlarm }

func (h *receivedPacketHandler) SentRecoveredFrame(f *wire.RecoveredFrame) {
	if f == nil {
		return
	}
	log.Printf("sent recovered frame")
}
