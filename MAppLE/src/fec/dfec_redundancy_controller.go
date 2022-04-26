package fec

import (
	"fmt"
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/logger"
)

const (
	dFEC_Target       float64 = 0.05
	dFEC_Delta        float64 = 0.33
	dFEC_T            uint    = 1000
	dFEC_MinBlockSize uint    = 4
	dFEC_MaxBlockSize uint    = 256
)

type dFECRedundancyController struct {
	windowSent uint
	windowLost uint
	windowSize uint

	residualsPrev float64
	residualsAvg  float64

	ratio float64
}

var _ RedundancyController = &dFECRedundancyController{}

func NewdFECRedundancyController() RedundancyController {
	fmt.Println("using dFEC redundancy controller")

	return &dFECRedundancyController{
		ratio: 9.,
	}
}

func (rc *dFECRedundancyController) countLoss() {
}

func (rc *dFECRedundancyController) countRecovery() {
}

func (rc *dFECRedundancyController) InsertMeasurement(p protocol.PathID, oSender *congestion.OliaSender, rttStats congestion.RTTStats, sentPacketHandler ackhandler.SentPacketHandler) {
}

// is called whenever a packet is sent
func (rc *dFECRedundancyController) OnPacketSent(pn protocol.PacketNumber, hasFECFrame bool) {
	rc.windowSent++
	rc.windowSize++

	if rc.windowSize > dFEC_T {
		residuals := float64(rc.windowLost) / float64(rc.windowSize-rc.windowLost)

		rc.residualsAvg = (rc.residualsPrev + residuals) / 2.
		rc.residualsPrev = residuals

		if rc.residualsAvg > dFEC_Target {
			rc.ratio = rc.ratio * (1. - dFEC_Delta)
		} else {
			rc.ratio = rc.ratio * (1. + dFEC_Delta)
		}

		rc.windowSize = 0
	}
}

// is called whenever a packet is lost
func (rc *dFECRedundancyController) OnPacketLost(pn protocol.PacketNumber) {
	rc.windowLost++
}

// is called whenever a packet is received
func (rc *dFECRedundancyController) OnPacketReceived(pn protocol.PacketNumber, recovered bool) {
}

// returns the number of data symbols that should compose a single FEC Group
func (rc *dFECRedundancyController) GetNumberOfDataSymbols() uint {
	ds := utils.MaxUint(utils.MinUint(uint(rc.ratio), dFEC_MaxBlockSize), dFEC_MinBlockSize)
	logger.ExpLogInsertFECConfig(ds, 1, 1)
	return ds
}

// returns the maximum number of repair symbols that should be generated for a single FEC Group
func (rc *dFECRedundancyController) GetNumberOfRepairSymbols() uint {
	return 1
}

// returns the number of blocks that must be interleaved for block FEC Schemes
func (rc *dFECRedundancyController) GetNumberOfInterleavedBlocks() uint {
	return 1
}

// returns the window step size for convolutional FEC Schemes
func (rc *dFECRedundancyController) GetWindowStepSize(p protocol.PathID) uint {
	return 1
}
