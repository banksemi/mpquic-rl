package fec

import (
	"fmt"
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

type remainingBytesRedundancyController struct {
	remainingBytes    map[protocol.PathID]uint
	maxRemainingBytes uint
	sumRemainingBytes uint
}

var _ RedundancyController = &remainingBytesRedundancyController{}

func NewRemainingBytesRedundancyController() RedundancyController {
	fmt.Println("using remaining bytes redundancy controller")
	return &remainingBytesRedundancyController{
		remainingBytes: make(map[protocol.PathID]uint),
	}
}

func (rc *remainingBytesRedundancyController) InsertMeasurement(p protocol.PathID, oSender *congestion.OliaSender, rttStats congestion.RTTStats, sentPacketHandler ackhandler.SentPacketHandler) {
	currentCwin := oSender.GetCongestionWindow()
	currentBytesInFlight := sentPacketHandler.GetBytesInFlight()

	var remainingBytes uint
	if currentCwin > currentBytesInFlight {
		remainingBytes = uint(currentCwin - currentBytesInFlight)
	}

	rc.remainingBytes[p] = remainingBytes
	rc.maxRemainingBytes = utils.MaxUint(
		rc.remainingBytes[1],
		rc.remainingBytes[2],
	)
	rc.sumRemainingBytes = rc.remainingBytes[1] + rc.remainingBytes[2]
}

// is called whenever a packet is sent
func (rc *remainingBytesRedundancyController) OnPacketSent(pn protocol.PacketNumber, hasFECFrame bool) {
}

// is called whenever a packet is lost
func (rc *remainingBytesRedundancyController) OnPacketLost(pn protocol.PacketNumber) {
}

// is called whenever a packet is received
func (rc *remainingBytesRedundancyController) OnPacketReceived(pn protocol.PacketNumber, recovered bool) {
}

// returns the number of data symbols that should compose a single FEC Group
func (rc *remainingBytesRedundancyController) GetNumberOfDataSymbols() uint {
	n := rc.sumRemainingBytes / uint(protocol.MaxReceivePacketSize)
	return n
}

// returns the maximum number of repair symbols that should be generated for a single FEC Group
func (rc *remainingBytesRedundancyController) GetNumberOfRepairSymbols() uint {
	return 1
}

// returns the number of blocks that must be interleaved for block FEC Schemes
func (rc *remainingBytesRedundancyController) GetNumberOfInterleavedBlocks() uint {
	return 1
}

// returns the window step size for convolutional FEC Schemes
func (rc *remainingBytesRedundancyController) GetWindowStepSize(p protocol.PathID) uint {
	return 1
}
