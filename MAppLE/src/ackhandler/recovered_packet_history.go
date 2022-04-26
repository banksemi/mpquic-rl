package ackhandler

import (
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

// The receivedPacketHistory stores if a packet number has already been received.
// It does not store packet contents.
type recoveredPacketHistory struct {
	ranges *utils.PacketIntervalList

	lowestInReceivedPacketNumbers protocol.PacketNumber
}

//var errTooManyOutstandingReceivedAckRanges = qerr.Error(qerr.TooManyOutstandingReceivedPackets, "Too many outstanding received ACK ranges")

// newReceivedPacketHistory creates a new received packet history
func newRecoveredPacketHistory() *recoveredPacketHistory {
	return &recoveredPacketHistory{
		ranges: utils.NewPacketIntervalList(),
	}
}

// ReceivedPacket registers a packet with PacketNumber p and updates the ranges
func (h *recoveredPacketHistory) RecoveredPacket(p protocol.PacketNumber) error {

	if h.ranges.Len() == 0 {
		h.ranges.PushBack(utils.PacketInterval{Start: p, End: p})
		return nil
	}

	for el := h.ranges.Back(); el != nil; el = el.Prev() {
		// p already included in an existing range. Nothing to do here
		if p >= el.Value.Start && p <= el.Value.End {
			return nil
		}

		var rangeExtended bool
		if el.Value.End == p-1 { // extend a range at the end
			rangeExtended = true
			el.Value.End = p
		} else if el.Value.Start == p+1 { // extend a range at the beginning
			rangeExtended = true
			el.Value.Start = p
		}

		// if a range was extended (either at the beginning or at the end, maybe it is possible to merge two ranges into one)
		if rangeExtended {
			prev := el.Prev()
			if prev != nil && prev.Value.End+1 == el.Value.Start { // merge two ranges
				prev.Value.End = el.Value.End
				h.ranges.Remove(el)
				return nil
			}
			return nil // if the two ranges were not merge, we're done here
		}

		// create a new range at the end
		if p > el.Value.End {
			h.ranges.InsertAfter(utils.PacketInterval{Start: p, End: p}, el)
			return nil
		}
	}

	// create a new range at the beginning
	h.ranges.InsertBefore(utils.PacketInterval{Start: p, End: p}, h.ranges.Front())

	return nil
}

// DeleteUpTo deletes all entries up to (and including) p
func (h *recoveredPacketHistory) DeleteUpTo(p protocol.PacketNumber) {
	h.lowestInReceivedPacketNumbers = utils.MaxPacketNumber(h.lowestInReceivedPacketNumbers, p+1)

	nextEl := h.ranges.Front()
	for el := h.ranges.Front(); nextEl != nil; el = nextEl {
		nextEl = el.Next()

		if p >= el.Value.Start && p < el.Value.End {
			el.Value.Start = p + 1
		} else if el.Value.End <= p { // delete a whole range
			h.ranges.Remove(el)
		} else { // no ranges affected. Nothing to do
			return
		}
	}
}

// GetAckRanges gets a slice of all AckRanges that can be used in an AckFrame
func (h *recoveredPacketHistory) GetRecoveredRanges() []wire.RecoveredRange {
	if h.ranges.Len() == 0 {
		return nil
	}

	recoveredRanges := make([]wire.RecoveredRange, h.ranges.Len())
	i := 0
	for el := h.ranges.Back(); el != nil; el = el.Prev() {
		recoveredRanges[i] = wire.RecoveredRange{First: el.Value.Start, Last: el.Value.End}
		i++
	}
	return recoveredRanges
}

func (h *recoveredPacketHistory) GetHighestRecoveredRange() wire.RecoveredRange {
	recoveredRange := wire.RecoveredRange{}
	if h.ranges.Len() > 0 {
		r := h.ranges.Back().Value
		recoveredRange.First = r.Start
		recoveredRange.Last = r.End
	}
	return recoveredRange
}

// removes the specified range from the history
func (h *recoveredPacketHistory) RemoveRange(r wire.RecoveredRange) {
	h.lowestInReceivedPacketNumbers = utils.MaxPacketNumber(h.lowestInReceivedPacketNumbers, r.First+1)

	nextEl := h.ranges.Front()
	for el := h.ranges.Front(); nextEl != nil; el = nextEl {
		nextEl = el.Next()
		if r.First > el.Value.End || r.Last < el.Value.Start {
			continue
		}
		if r.First <= el.Value.Start && r.Last >= el.Value.End {
			// remove whole range
			if el.Value.Start == h.lowestInReceivedPacketNumbers && nextEl != nil {
				h.lowestInReceivedPacketNumbers = nextEl.Value.Start
			}
			h.ranges.Remove(el)
		} else if r.First <= el.Value.Start {
			if el.Value.Start == h.lowestInReceivedPacketNumbers {
				h.lowestInReceivedPacketNumbers = r.Last + 1
			}
			el.Value.Start = r.Last + 1
		} else if r.Last >= el.Value.End {
			el.Value.End = r.First + 1
		} else if r.Last < el.Value.Start {
			return
		}
	}
}

// removes all there ranges from the history
func (h *recoveredPacketHistory) RemoveRanges(rs []wire.RecoveredRange) {
	for _, r := range rs {
		h.RemoveRange(r)
	}
}
