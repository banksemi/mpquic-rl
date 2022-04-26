package fec

import (
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
)

type constantRedundancyController struct {
	nRepairSymbols     uint
	nDataSymbols       uint
	nInterleavedBlocks uint
	windowStepSize     uint
}

var _ RedundancyController = &constantRedundancyController{}

func NewConstantRedundancyController(nDataSymbols uint, nRepairSymbols uint, nInterleavedBlocks uint, windowStepSize uint) RedundancyController {
	return &constantRedundancyController{
		nDataSymbols:       nDataSymbols,
		nRepairSymbols:     nRepairSymbols,
		nInterleavedBlocks: nInterleavedBlocks,
		windowStepSize:     windowStepSize,
	}
}

func (rc *constantRedundancyController) InsertMeasurement(p protocol.PathID, oSender *congestion.OliaSender, rttStats congestion.RTTStats, sentPacketHandler ackhandler.SentPacketHandler) {
}

func (c *constantRedundancyController) OnPacketSent(pn protocol.PacketNumber, hasFECFrame bool) {}

func (*constantRedundancyController) OnPacketLost(protocol.PacketNumber) {}

func (*constantRedundancyController) OnPacketReceived(pn protocol.PacketNumber, recovered bool) {}

func (c *constantRedundancyController) GetNumberOfDataSymbols() uint {
	return c.nDataSymbols
}

func (c *constantRedundancyController) GetNumberOfRepairSymbols() uint {
	return c.nRepairSymbols
}

func (c *constantRedundancyController) GetNumberOfInterleavedBlocks() uint {
	return c.nInterleavedBlocks
}

func (c *constantRedundancyController) GetWindowStepSize(protocol.PathID) uint {
	return c.windowStepSize
}
