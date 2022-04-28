package fec

import (
	"fmt"
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/logger"
	"time"
)

const (
	// the maximum amount of losses+recoveries we count before we try adjusting
	// the settings
	LRRMaxWindowSize uint = 10

	// if this amount of windows were filled without causing a significant change
	// in the loss-recovery-ration, we'll try to opportunistically decrease RF
	LRRMaxWindowsWithoutChange uint = 5

	// LRR factor delta which is large enough so that we can expect a new
	// optimum
	LRRAssumeNewOptimumDelta float64 = 1.0

	// we apply a minimum amount of linear interpolation onto LRR
	// this is the factor with which we add the most recent value
	LRLerpFactor float64 = 0.75

	// minimum LRR change until we modify anything
	LRMinDelta float64 = 0.33

	// the step size is adjusted dynamically, these are its upper and lower
	// bounds
	MaxAdapationRate uint = 4
	MinAdapationRate uint = 1

	// abolute bounds for block coding settings
	MaxXORBlockSize uint = 64
	MinXORBlockSize uint = 4

	// RLNC works differently and thus uses different bounds
	// this config tries to match the value range we have for block codes
	RLCNSourceSymbols uint = 16
	MaxRLCWindowStep  uint = 16
	MinRLCWindowStep  uint = 4

	// Reed-Solomon Specialities
	MaxRSNSourceSymbols uint = 32
	MinRSNSourceSymbols uint = 8
	MaxRSRepairSymbols  uint = 4
	MinRSRepairSymbols  uint = 1

	// Round-Trip Time Related Constants
	MinRTTDelta time.Duration = time.Millisecond * 10
)

type delaySensitiveRedundancyController struct {
	scheme protocol.FECSchemeID

	nDataSymbols         uint
	nDataSymbolsFiltered uint
	nRepairSymbols       uint
	nInterleavedBlocks   uint
	windowStep           uint

	adapationRate            uint
	lrWindowSize             uint
	nLRRWindowsWithoutChange uint
	losses                   uint
	recoveries               uint
	weightedLRRatio          float64

	nPacketsReceivedSinceLoss protocol.PacketNumber

	useRTT        bool
	lowestRTTPath protocol.PathID
	lowestRTT     time.Duration
}

var _ RedundancyController = &delaySensitiveRedundancyController{}

func NewDelaySensitiveRedundancyController(scheme protocol.FECSchemeID, useRTT bool) RedundancyController {
	fmt.Println("using delay sensitive redundancy controller")

	if useRTT {
		fmt.Println("using RTT as additional measure in FEC controller")
	}

	var nDataSymbols uint
	switch scheme {
	case protocol.RLCFECScheme:
		nDataSymbols = RLCNSourceSymbols
	case protocol.XORFECScheme:
		nDataSymbols = MinXORBlockSize
	case protocol.ReedSolomonFECScheme:
		nDataSymbols = MinRSNSourceSymbols
	}

	return &delaySensitiveRedundancyController{
		scheme:               scheme,
		nDataSymbols:         nDataSymbols,
		nDataSymbolsFiltered: nDataSymbols,
		nRepairSymbols:       1,
		nInterleavedBlocks:   1,
		windowStep:           MinRLCWindowStep,
		adapationRate:        MaxAdapationRate,
		useRTT:               useRTT,
		lowestRTTPath:        protocol.InitialPathID,
		lowestRTT:            time.Hour, // just something really large
	}
}

func (rc *delaySensitiveRedundancyController) filterNDataSymbols() {
	if rc.nDataSymbols < 6 {
		rc.nDataSymbolsFiltered = 4
	} else if rc.nDataSymbols < 8 {
		rc.nDataSymbolsFiltered = 6
	} else if rc.nDataSymbols < 10 {
		rc.nDataSymbolsFiltered = 8
	} else if rc.nDataSymbols < 16 {
		rc.nDataSymbolsFiltered = 12
	} else if rc.nDataSymbols < 20 {
		rc.nDataSymbolsFiltered = 16
	} else if rc.nDataSymbols < 32 {
		rc.nDataSymbolsFiltered = 32
	} else {
		rc.nDataSymbolsFiltered = 64
	}
}

func (rc *delaySensitiveRedundancyController) decreaseRFAdditive() {
	//fmt.Println("Decrease Additive")

	switch rc.scheme {
	case protocol.RLCFECScheme:
		rc.windowStep = utils.MinUint(MaxRLCWindowStep, rc.windowStep+rc.adapationRate)
	case protocol.XORFECScheme:
		rc.nDataSymbols = utils.MinUint(MaxXORBlockSize, rc.nDataSymbols+rc.adapationRate)
	case protocol.ReedSolomonFECScheme:
		rc.nDataSymbols = utils.MinUint(MaxRSNSourceSymbols, rc.nDataSymbols+rc.adapationRate)
		rc.nRepairSymbols = utils.MaxUint(MinRSRepairSymbols, rc.nRepairSymbols-1)
	}

	rc.filterNDataSymbols()
}

func (rc *delaySensitiveRedundancyController) increaseRFAdditive() {
	//fmt.Println("Increase Additive")
	switch rc.scheme {
	case protocol.RLCFECScheme:
		if rc.windowStep < rc.adapationRate {
			rc.windowStep = MinRLCWindowStep
		} else {
			rc.windowStep = utils.MaxUint(MinRLCWindowStep, rc.windowStep-rc.adapationRate)
		}
	case protocol.XORFECScheme:
		if rc.nDataSymbols < rc.adapationRate {
			// prevent underflow
			rc.nDataSymbols = MinXORBlockSize
		} else {
			rc.nDataSymbols = utils.MaxUint(MinXORBlockSize, rc.nDataSymbols-rc.adapationRate)
		}
	case protocol.ReedSolomonFECScheme:
		if rc.nDataSymbols < rc.adapationRate {
			// prevent underflow
			rc.nDataSymbols = MinRSNSourceSymbols
		} else {
			rc.nDataSymbols = utils.MaxUint(MinRSNSourceSymbols, rc.nDataSymbols-rc.adapationRate)
		}
		rc.nRepairSymbols = utils.MinUint(MaxRSRepairSymbols, rc.nRepairSymbols+1)
	}
	utils.Debugf("\n DataSymbol: %d, RepairSym: %d", rc.GetNumberOfDataSymbols(), rc.GetNumberOfRepairSymbols())
	rc.filterNDataSymbols()
}

func (rc *delaySensitiveRedundancyController) decreaseRFMultiplicative() {
	//fmt.Println("Decrease Multiplicative")
	switch rc.scheme {
	case protocol.RLCFECScheme:
		rc.windowStep = utils.MinUint(MaxRLCWindowStep, rc.windowStep*2)
	case protocol.XORFECScheme:
		rc.nDataSymbols = utils.MinUint(MaxXORBlockSize, rc.nDataSymbols*2)
	case protocol.ReedSolomonFECScheme:
		rc.nDataSymbols = utils.MinUint(MaxRSNSourceSymbols, rc.nDataSymbols*2)
		rc.nRepairSymbols = utils.MaxUint(MinRSRepairSymbols, rc.nRepairSymbols/2)
	}

	rc.filterNDataSymbols()
}

func (rc *delaySensitiveRedundancyController) increaseRFMultiplicative() {
	//fmt.Println("Increase Multiplicative")
	switch rc.scheme {
	case protocol.RLCFECScheme:
		rc.windowStep = utils.MaxUint(MinRLCWindowStep, rc.windowStep/2)
	case protocol.XORFECScheme:
		rc.nDataSymbols = utils.MaxUint(MinXORBlockSize, rc.nDataSymbols/2)
	case protocol.ReedSolomonFECScheme:
		rc.nDataSymbols = utils.MaxUint(MinRSNSourceSymbols, rc.nDataSymbols/2)
		rc.nRepairSymbols = utils.MinUint(MaxRSRepairSymbols, rc.nRepairSymbols*2)
	}

	//rc.filterNDataSymbols()
}

func (rc *delaySensitiveRedundancyController) onCount() {
	rc.lrWindowSize++

	if rc.lrWindowSize < LRRMaxWindowSize {
		// not yet gathered enough information
		return
	}

	rc.lrWindowSize = 0

	if rc.recoveries == 0 {
		// this means we've filled the window with only losses
		// treat this the same way we treat a shifted optimum when having a
		// very high LR delta
		rc.losses = 0
		rc.nLRRWindowsWithoutChange = 0

		rc.adapationRate = MaxAdapationRate
		rc.increaseRFMultiplicative()
	} else if rc.losses == 0 {
		// we've had no losses
		// thus we can opportunistically decrease RF somewhat
		// HOWEVER only additively, as we don't want to move too far away from
		// a possible optimum
		rc.recoveries = 0
		rc.nLRRWindowsWithoutChange = 0

		rc.adapationRate = MinAdapationRate
		rc.decreaseRFAdditive()
	} else {
		lrRatio := float64(rc.losses) / float64(rc.recoveries)
		delta := lrRatio - rc.weightedLRRatio

		//fmt.Println("losses", rc.losses)
		//fmt.Println("recoveries", rc.recoveries)
		//fmt.Println("ratio", lrRatio)
		//fmt.Println("delta", delta)

		rc.weightedLRRatio = utils.Lerp(rc.weightedLRRatio, lrRatio, LRLerpFactor)
		rc.losses = 0
		rc.recoveries = 0

		if rc.adapationRate > MinAdapationRate {
			// for every full window, we try to lower the adapation rate
			rc.adapationRate--
		}

		if delta < -LRRAssumeNewOptimumDelta {
			// large negative change in LRR
			// adjust through multiplicative decrease of RF to quickly find new
			// smaller optimum
			// do not change the adaptation rate here, as a lower optimum means
			// we'll have to be "careful" not to move to fast into the wrong direction
			rc.decreaseRFMultiplicative()
			rc.nLRRWindowsWithoutChange = 0
		} else if delta < -LRMinDelta {
			// minor negative change of RF
			rc.decreaseRFAdditive()
			rc.nLRRWindowsWithoutChange = 0
		} else if delta > LRRAssumeNewOptimumDelta {
			// large positive change in LRR or more losses than recoveries
			// multiplicative increase of RF and reset adaptation rate to quickly
			// find new optimum
			rc.adapationRate = MaxAdapationRate
			rc.increaseRFMultiplicative()
			rc.nLRRWindowsWithoutChange = 0
		} else if delta > LRMinDelta {
			// minor positive change of RF
			rc.increaseRFAdditive()
			rc.nLRRWindowsWithoutChange = 0
		} else {
			rc.nLRRWindowsWithoutChange++
		}

		if rc.nLRRWindowsWithoutChange > LRRMaxWindowsWithoutChange {
			// no change since n windows were filled
			// always decrease RF here
			rc.decreaseRFAdditive()
		}
	}
}

func (rc *delaySensitiveRedundancyController) countLoss() {
	rc.losses++
}

func (rc *delaySensitiveRedundancyController) countRecovery() {
	rc.recoveries++
}

func (rc *delaySensitiveRedundancyController) InsertMeasurement(p protocol.PathID, oSender *congestion.OliaSender, rttStats congestion.RTTStats, sentPacketHandler ackhandler.SentPacketHandler) {
	if !rc.useRTT {
		return
	}

	rtt := rttStats.SmoothedRTT()

	if rtt < rc.lowestRTT {
		rc.lowestRTT = rtt
		rc.lowestRTTPath = p
		return
	}

	deltaRTT := rtt - rc.lowestRTT

	if rc.lowestRTTPath == p && deltaRTT > rttStats.MeanDeviation() {
		// only care if the rtt increased on the path with the lowest overall
		// rtt, this assumes usage of some MP scheduler that is focused on low
		// delays
		// RTT increased, opportunistically increase RF
		rc.lowestRTT = rtt
		rc.increaseRFAdditive()
	}
}

// is called whenever a packet is sent
func (rc *delaySensitiveRedundancyController) OnPacketSent(pn protocol.PacketNumber, hasFECFrame bool) {
}

// is called whenever a packet is lost
func (rc *delaySensitiveRedundancyController) OnPacketLost(pn protocol.PacketNumber) {
	rc.countLoss()
	rc.onCount()
}

// is called whenever a packet is received
func (rc *delaySensitiveRedundancyController) OnPacketReceived(pn protocol.PacketNumber, recovered bool) {
	if recovered {
		rc.countRecovery()
		rc.onCount()
	}
}

// returns the number of data symbols that should compose a single FEC Group
func (rc *delaySensitiveRedundancyController) GetNumberOfDataSymbols() uint {
	logger.ExpLogInsertFECConfig(rc.nDataSymbols, rc.nRepairSymbols, rc.GetWindowStepSize(protocol.InitialPathID))
	return rc.nDataSymbols
}

// returns the maximum number of repair symbols that should be generated for a single FEC Group
func (rc *delaySensitiveRedundancyController) GetNumberOfRepairSymbols() uint {
	return rc.nRepairSymbols
}

// returns the number of blocks that must be interleaved for block FEC Schemes
func (rc *delaySensitiveRedundancyController) GetNumberOfInterleavedBlocks() uint {
	return rc.nInterleavedBlocks
}

// returns the window step size for convolutional FEC Schemes
func (rc *delaySensitiveRedundancyController) GetWindowStepSize(p protocol.PathID) uint {
	return rc.windowStep
}
