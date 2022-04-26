package fec

import (
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"math"
)

type windowedRedundancyControllerMeasure struct {
	LossRate         float64
	SmoothedLossRate float64
	SendRate         float64

	CodingRate float64
}

type windowedRedundancyController struct {
	lower protocol.PacketNumber
	upper protocol.PacketNumber

	smoothedRatio float64
	c             float64 // TODO this value is actually individually calculated for each path

	measurements map[protocol.PathID]windowedRedundancyControllerMeasure
	condingRate  float64
}

var _ RedundancyController = &windowedRedundancyController{}

func NewWindowedRedundancyController() RedundancyController {
	return &windowedRedundancyController{
		upper:        0,
		lower:        0,
		measurements: make(map[protocol.PathID]windowedRedundancyControllerMeasure),
	}
}

func (rc *windowedRedundancyController) InsertMeasurement(p protocol.PathID, oSender *congestion.OliaSender, rttStats congestion.RTTStats, sentPacketHandler ackhandler.SentPacketHandler) {
	measure, ok := rc.measurements[p]
	if !ok {
		measure = windowedRedundancyControllerMeasure{}
	}

	bandwidthEstimate := float64(congestion.BandwidthFromDelta(
		sentPacketHandler.GetSendAlgorithm().GetCongestionWindow(),
		rttStats.SmoothedRTT(),
	))

	div := math.Max(
		oSender.GetSmoothedBytesBetweenTwoLosses(),
		float64(oSender.GetCurrentPacketsSinceLastLoss()),
	)
	var lr float64
	if div <= 0 {
		lr = 0.0
	} else {
		lr = 1.0 / div // MPTCP LAMPS
	}

	measure.SmoothedLossRate = movingAverageFloat64(measure.SmoothedLossRate, lr, 0.99)
	measure.SendRate = bandwidthEstimate
	measure.CodingRate = 1.0 - measure.SmoothedLossRate

	rc.measurements[p] = measure
}

// is called whenever a packet is sent
func (rc *windowedRedundancyController) OnPacketSent(pn protocol.PacketNumber, hasFECFrame bool) {
	if !hasFECFrame && pn > rc.upper {
		rc.upper = pn
	}
}

// is called whenever a packet is lost
func (rc *windowedRedundancyController) OnPacketLost(pn protocol.PacketNumber) {
}

// is called whenever a packet is received
func (rc *windowedRedundancyController) OnPacketReceived(pn protocol.PacketNumber, recovered bool) {
	if pn > rc.lower {
		rc.lower = pn
	}
}

// returns the number of data symbols that should compose a single FEC Group
func (rc *windowedRedundancyController) GetNumberOfDataSymbols() uint {
	numD := utils.MaxUint(1, uint(uint64(rc.upper)-uint64(rc.lower)))
	return numD
}

// returns the maximum number of repair symbols that should be generated for a single FEC Group
func (rc *windowedRedundancyController) GetNumberOfRepairSymbols() uint {
	return 1
}

// returns the number of blocks that must be interleaved for block FEC Schemes
func (rc *windowedRedundancyController) GetNumberOfInterleavedBlocks() uint {
	return 1
}

// returns the window step size for convolutional FEC Schemes
func (rc *windowedRedundancyController) GetWindowStepSize(p protocol.PathID) uint {
	// this is the counterpart to what is referred to as the coding rate in
	// the paper
	// always round up here, this allows us to simplyfy the calculation of the
	// coding rate by allowing us to calculate a C value that equals 0
	// therefore, the WindowSize, when always rounded up, satisfies the inequality
	size := uint(math.Ceil(1.0 / (1.0 - rc.measurements[p].CodingRate)))
	return size
}

func movingAverageFloat64(a float64, b float64, factor float64) float64 {
	return factor*a + (1.0-factor)*b
}
