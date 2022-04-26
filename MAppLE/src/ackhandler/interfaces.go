package ackhandler

import (
	"github.com/lucas-clemente/quic-go/congestion"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

// SentPacketHandler handles ACKs received for outgoing packets
type SentPacketHandler interface {
	// SentPacket may modify the packet
	SentPacket(packet *Packet) error
	ReceivedAck(ackFrame *wire.AckFrame, withPacketNumber protocol.PacketNumber, encLevel protocol.EncryptionLevel, recvTime time.Time) error
	ReceivedRecoveredFrame(frame *wire.RecoveredFrame, encLevel protocol.EncryptionLevel) error
	SetHandshakeComplete()

	// Specific to multipath operation
	SetInflightAsLost()

	SendingAllowed() bool
	GetStopWaitingFrame(force bool) *wire.StopWaitingFrame
	ShouldSendRetransmittablePacket() bool
	DequeuePacketForRetransmission() (packet *Packet)
	GetLeastUnacked() protocol.PacketNumber

	GetAlarmTimeout() time.Time
	OnAlarm()

	DuplicatePacket(packet *Packet)

	ComputeRTOTimeout() time.Duration

	GetStatistics() (uint64, uint64, uint64)

	GetBytesInFlight() protocol.ByteCount
	GetPacketsInFlight() []*Packet
	GetSendAlgorithm() congestion.SendAlgorithmWithDebugInfo
	GetLastSendTime() time.Time
}

// ReceivedPacketHandler handles ACKs needed to send for incoming packets
type ReceivedPacketHandler interface {
	ReceivedPacket(packetNumber protocol.PacketNumber, shouldInstigateAck bool, recovered bool) error
	SetLowerLimit(protocol.PacketNumber)

	GetAlarmTimeout() time.Time
	GetAckFrame() *wire.AckFrame
	GetRecoveredFrame() *wire.RecoveredFrame

	GetStatistics() (uint64, uint64)
	SentRecoveredFrame(f *wire.RecoveredFrame)
}
