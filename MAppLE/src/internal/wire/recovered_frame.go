package wire

import (
	"bytes"
	"errors"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"log"
)

var (
	// ErrInvalidAckRanges occurs when a client sends inconsistent ACK ranges
	ErrInvalidRecoveredRanges = errors.New("RecoveredFrame: Recovered frame contains invalid Recovered ranges")
	// ErrInvalidFirstAckRange occurs when the first ACK range contains no packets
	ErrInvalidFirstRecoveredRange = errors.New("RecoveredFrame: Recovered frame has invalid first Recovered range")
)

var (
	errInconsistentLargestRecovered = errors.New("internal inconsistency: LargestRecovered does not match Recovered ranges")
	errInconsistentLowestRecovered  = errors.New("internal inconsistency: LowestRecovered does not match Recovered ranges")
)

var RecoveredFrameTypeByte byte = 0xc

// An AckFrame is an ACK frame in QUIC
type RecoveredFrame struct {
	RecoveredRanges []RecoveredRange // has to be ordered. The highest ACK range goes first, the lowest ACK range goes last
}

// ParseAckFrame reads an ACK frame
func ParseRecoveredFrame(r *bytes.Reader, version protocol.VersionNumber) (*RecoveredFrame, error) {
	frame := &RecoveredFrame{}

	_, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	// we put the type byte of the AckFrame just after the type byte of the RecoveredFrame
	typeByte, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	hasMissingRanges := false
	if typeByte&0x20 == 0x20 {
		hasMissingRanges = true
	}

	missingSequenceNumberDeltaLen := 2 * (typeByte & 0x03)
	if missingSequenceNumberDeltaLen == 0 {
		missingSequenceNumberDeltaLen = 1
	}

	largestRecovered, err := utils.GetByteOrder(version).ReadUintN(r, 6)
	if err != nil {
		return nil, err
	}

	var numAckBlocks uint8
	if hasMissingRanges {
		numAckBlocks, err = r.ReadByte()
		if err != nil {
			return nil, err
		}
	}

	//if hasMissingRanges && numAckBlocks == 0 {
	//	return nil, ErrInvalidRecoveredRanges
	//}

	ackBlockLength, err := utils.GetByteOrder(version).ReadUintN(r, missingSequenceNumberDeltaLen)
	if err != nil {
		return nil, err
	}
	if ackBlockLength < 1 {
		return nil, ErrInvalidFirstRecoveredRange
	}

	ackRange := RecoveredRange{
		First: protocol.PacketNumber(largestRecovered-ackBlockLength) + 1,
		Last:  protocol.PacketNumber(largestRecovered),
	}
	frame.RecoveredRanges = append(frame.RecoveredRanges, ackRange)

	var inLongBlock bool
	var lastRangeComplete bool = numAckBlocks == 0
	for i := uint8(0); i < numAckBlocks; i++ {
		var gap uint8
		gap, err = r.ReadByte()
		if err != nil {
			return nil, err
		}

		ackBlockLength, err = utils.GetByteOrder(version).ReadUintN(r, missingSequenceNumberDeltaLen)
		if err != nil {
			return nil, err
		}

		length := protocol.PacketNumber(ackBlockLength)

		if inLongBlock {
			frame.RecoveredRanges[len(frame.RecoveredRanges)-1].First -= protocol.PacketNumber(gap) + length
			frame.RecoveredRanges[len(frame.RecoveredRanges)-1].Last -= protocol.PacketNumber(gap)
		} else {
			lastRangeComplete = false
			recoveredRange := RecoveredRange{
				Last: frame.RecoveredRanges[len(frame.RecoveredRanges)-1].First - protocol.PacketNumber(gap) - 1,
			}
			recoveredRange.First = recoveredRange.Last - length + 1
			frame.RecoveredRanges = append(frame.RecoveredRanges, recoveredRange)
		}

		log.Printf("blocklength = %d", ackBlockLength)
		if length > 0 {
			lastRangeComplete = true
		}

		inLongBlock = (ackBlockLength == 0)
	}

	// if the last range was not complete, First and Last make no sense
	// remove the range from frame.AckRanges
	if !lastRangeComplete {
		frame.RecoveredRanges = frame.RecoveredRanges[:len(frame.RecoveredRanges)-1]
	}

	if !frame.validateAckRanges() {
		return nil, ErrInvalidRecoveredRanges
	}

	var numTimestamp byte
	numTimestamp, err = r.ReadByte()
	if err != nil {
		return nil, err
	}

	if numTimestamp > 0 {
		// Delta Largest acked
		_, err = r.ReadByte()
		if err != nil {
			return nil, err
		}
		// First Timestamp
		_, err = utils.GetByteOrder(version).ReadUint32(r)
		if err != nil {
			return nil, err
		}

		for i := 0; i < int(numTimestamp)-1; i++ {
			// Delta Largest acked
			_, err = r.ReadByte()
			if err != nil {
				return nil, err
			}

			// Time Since Previous Timestamp
			_, err = utils.GetByteOrder(version).ReadUint16(r)
			if err != nil {
				return nil, err
			}
		}
	}
	return frame, nil
}

// Write writes an ACK frame.
func (f *RecoveredFrame) Write(b *bytes.Buffer, version protocol.VersionNumber) error {
	b.WriteByte(RecoveredFrameTypeByte)

	typeByte := uint8(0x40)

	missingSequenceNumberDeltaLen := f.getMissingSequenceNumberDeltaLen()
	if missingSequenceNumberDeltaLen != protocol.PacketNumberLen1 {
		typeByte ^= (uint8(missingSequenceNumberDeltaLen / 2))
	}

	if f.HasMissingRanges() {
		typeByte |= 0x20
	}

	b.WriteByte(typeByte)

	largestAcked := f.RecoveredRanges[0].Last

	utils.GetByteOrder(version).WriteUint48(b, uint64(largestAcked)&(1<<48-1))

	log.Printf("WRITE RECOVERED FRAME:")
	for _, r := range f.RecoveredRanges {
		log.Printf("FROM %d TO %d", r.First, r.Last)
	}

	var numRanges uint64
	var numRangesWritten uint64
	if f.HasMissingRanges() {
		numRanges = f.numWritableNackRanges()
		if numRanges > 0xFF {
			panic("AckFrame: Too many ACK ranges")
		}
		b.WriteByte(uint8(numRanges - 1))
	}

	var firstAckBlockLength protocol.PacketNumber

	firstAckBlockLength = largestAcked - f.RecoveredRanges[0].First + 1
	numRangesWritten++
	switch missingSequenceNumberDeltaLen {
	case protocol.PacketNumberLen1:
		b.WriteByte(uint8(firstAckBlockLength))
	case protocol.PacketNumberLen2:
		utils.GetByteOrder(version).WriteUint16(b, uint16(firstAckBlockLength))
	case protocol.PacketNumberLen4:
		utils.GetByteOrder(version).WriteUint32(b, uint32(firstAckBlockLength))
	case protocol.PacketNumberLen6:
		utils.GetByteOrder(version).WriteUint48(b, uint64(firstAckBlockLength)&(1<<48-1))
	}

	for i, ackRange := range f.RecoveredRanges {
		if i == 0 {
			continue
		}

		length := ackRange.Last - ackRange.First + 1
		gap := f.RecoveredRanges[i-1].First - ackRange.Last - 1

		num := gap/0xFF + 1
		if gap%0xFF == 0 {
			num--
		}

		if num == 1 {
			b.WriteByte(uint8(gap))
			switch missingSequenceNumberDeltaLen {
			case protocol.PacketNumberLen1:
				b.WriteByte(uint8(length))
			case protocol.PacketNumberLen2:
				utils.GetByteOrder(version).WriteUint16(b, uint16(length))
			case protocol.PacketNumberLen4:
				utils.GetByteOrder(version).WriteUint32(b, uint32(length))
			case protocol.PacketNumberLen6:
				utils.GetByteOrder(version).WriteUint48(b, uint64(length)&(1<<48-1))
			}
			numRangesWritten++
		} else {
			for i := 0; i < int(num); i++ {
				var lengthWritten uint64
				var gapWritten uint8

				if i == int(num)-1 { // last block
					lengthWritten = uint64(length)
					gapWritten = uint8(1 + ((gap - 1) % 255))
				} else {
					lengthWritten = 0
					gapWritten = 0xFF
				}

				log.Printf("write %+v, num = %+v, i = %+v", gapWritten, num, i)
				b.WriteByte(gapWritten)
				switch missingSequenceNumberDeltaLen {
				case protocol.PacketNumberLen1:
					b.WriteByte(uint8(lengthWritten))
				case protocol.PacketNumberLen2:
					utils.GetByteOrder(version).WriteUint16(b, uint16(lengthWritten))
				case protocol.PacketNumberLen4:
					utils.GetByteOrder(version).WriteUint32(b, uint32(lengthWritten))
				case protocol.PacketNumberLen6:
					utils.GetByteOrder(version).WriteUint48(b, lengthWritten&(1<<48-1))
				}

				numRangesWritten++
			}
		}

		// this is needed if not all AckRanges can be written to the ACK frame (if there are more than 0xFF)
		if numRangesWritten >= numRanges {
			break
		}
	}

	if numRanges != numRangesWritten {
		log.Printf("numRanges = %+v, numRangesWritten = %+v", numRanges, numRangesWritten)
		return errors.New("BUG: Inconsistent number of ACK ranges written")
	}

	b.WriteByte(0) // no timestamps
	return nil
}

// MinLength of a written frame
func (f *RecoveredFrame) MinLength(version protocol.VersionNumber) (protocol.ByteCount, error) {
	length := protocol.ByteCount(1 + 1 + 2 + 1) // 1 TypeByte, 1 ACK type byte, 2 ACK delay time, 1 Num Timestamp

	missingSequenceNumberDeltaLen := protocol.ByteCount(f.getMissingSequenceNumberDeltaLen())

	if f.HasMissingRanges() {
		length += (1 + missingSequenceNumberDeltaLen) * protocol.ByteCount(f.numWritableNackRanges())
	} else {
		length += missingSequenceNumberDeltaLen
	}

	length += (1 + 2) * 0 /* TODO: num_timestamps */

	return length, nil
}

// HasMissingRanges returns if this frame reports any missing packets
func (f *RecoveredFrame) HasMissingRanges() bool {
	return len(f.RecoveredRanges) > 0
}

func (f *RecoveredFrame) validateAckRanges() bool {
	if len(f.RecoveredRanges) == 0 {
		return false
	}

	// check the validity of every single ACK range
	for _, ackRange := range f.RecoveredRanges {
		if ackRange.First > ackRange.Last {
			return false
		}
	}

	// check the consistency for ACK with multiple NACK ranges
	for i, ackRange := range f.RecoveredRanges {
		if i == 0 {
			continue
		}
		lastAckRange := f.RecoveredRanges[i-1]
		if lastAckRange.First <= ackRange.First {
			return false
		}
		if lastAckRange.First <= ackRange.Last+1 {
			return false
		}
	}

	return true
}

// numWritableNackRanges calculates the number of ACK blocks that are about to be written
// this number is different from len(f.AckRanges) for the case of long gaps (> 255 packets)
func (f *RecoveredFrame) numWritableNackRanges() uint64 {
	if len(f.RecoveredRanges) == 0 {
		return 0
	}

	var numRanges uint64
	for i, ackRange := range f.RecoveredRanges {
		if i == 0 {
			continue
		}

		lastAckRange := f.RecoveredRanges[i-1]
		gap := lastAckRange.First - ackRange.Last - 1
		rangeLength := 1 + uint64(gap)/0xFF
		if uint64(gap)%0xFF == 0 {
			rangeLength--
		}

		if numRanges+rangeLength < 0xFF {
			numRanges += rangeLength
		} else {
			break
		}
	}

	return numRanges + 1
}

func (f *RecoveredFrame) getMissingSequenceNumberDeltaLen() protocol.PacketNumberLen {
	var maxRangeLength protocol.PacketNumber
	for _, ackRange := range f.RecoveredRanges {
		rangeLength := ackRange.Last - ackRange.First + 1
		if rangeLength > maxRangeLength {
			maxRangeLength = rangeLength
		}
	}

	if maxRangeLength <= 0xFF {
		return protocol.PacketNumberLen1
	}
	if maxRangeLength <= 0xFFFF {
		return protocol.PacketNumberLen2
	}
	if maxRangeLength <= 0xFFFFFFFF {
		return protocol.PacketNumberLen4
	}

	return protocol.PacketNumberLen6
}

// AcksPacket determines if this Recovers frame acks a certain packet number
func (f *RecoveredFrame) RecoversPacket(p protocol.PacketNumber) bool {
	// TODO: this could be implemented as a binary search
	for _, ackRange := range f.RecoveredRanges {
		if p >= ackRange.First && p <= ackRange.Last {
			return true
		}
	}
	return false
}
