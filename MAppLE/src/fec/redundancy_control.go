package fec

import (
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

// The redundancy control will adapt the number of FEC Repair Symbols and
// the size of the FEC Group to the current conditions.

// Warning: it assumes that the packets are sent with packet numbers increasing by one.

const MAX_LOSS_BURST_LENGTH uint = 40
const SAMPLE_SIZE = 500

type RedundancyController interface {
	// is called when information about a path could be collected
	InsertMeasurement(p protocol.PathID, oSender *congestion.OliaSender, rttStats congestion.RTTStats, sentPacketHandler ackhandler.SentPacketHandler)
	// is called whenever a packet is sent
	OnPacketSent(pn protocol.PacketNumber, hasFECFrame bool)
	// is called whenever a packet is lost
	OnPacketLost(protocol.PacketNumber)
	// is called whenever a packet is received
	OnPacketReceived(pn protocol.PacketNumber, recovered bool)
	// returns the number of data symbols that should compose a single FEC Group
	GetNumberOfDataSymbols() uint
	// returns the maximum number of repair symbols that should be generated for a single FEC Group
	GetNumberOfRepairSymbols() uint
	// returns the number of blocks that must be interleaved for block FEC Schemes
	GetNumberOfInterleavedBlocks() uint
	// returns the window step size for convolutional FEC Schemes
	GetWindowStepSize(protocol.PathID) uint
}

type averageRedundancyController struct {
	initialPacketOfThisSample     protocol.PacketNumber
	packetsCounter                uint
	numberOfContiguousLostPackets uint
	lastLostPacketNumber          protocol.PacketNumber
	burstCounter                  map[uint]uint // TODO: evaluate performances vs array [MAX_LOSS_BURST_LENGTH]uint
	interLossDistanceCounter      map[uint]uint // TODO: evaluate performances vs array [SAMPLE_SIZE]uint

	meanBurstLength          uint
	meanInterLossDistance    uint
	maxNumberOfSourceSymbols uint8
	maxNumberOfRepairSymbols uint8
}

var _ RedundancyController = &averageRedundancyController{}

func NewAverageRedundancyController(maxNumberOfSourceSymbols uint8, maxNumberOfRepairSymbols uint8) RedundancyController {
	return &averageRedundancyController{
		burstCounter:             make(map[uint]uint),
		interLossDistanceCounter: make(map[uint]uint),
		meanBurstLength:          uint(maxNumberOfRepairSymbols),
		meanInterLossDistance:    uint(maxNumberOfSourceSymbols),
		maxNumberOfSourceSymbols: maxNumberOfSourceSymbols,
		maxNumberOfRepairSymbols: maxNumberOfRepairSymbols,
	}
}

func (rc *averageRedundancyController) InsertMeasurement(p protocol.PathID, oSender *congestion.OliaSender, rttStats congestion.RTTStats, sentPacketHandler ackhandler.SentPacketHandler) {
}

func (c *averageRedundancyController) OnPacketSent(pn protocol.PacketNumber, hasFECFrame bool) {}

func (c *averageRedundancyController) OnPacketLost(pn protocol.PacketNumber) {
	if c.packetsCounter == 0 {
		c.initialPacketOfThisSample = pn
	}
	if pn < c.initialPacketOfThisSample {
		return
	}
	if c.lastLostPacketNumber == pn-1 {
		// we continue the current burst
		c.numberOfContiguousLostPackets++
	} else {
		// we begin a new burst
		c.burstCounter[c.numberOfContiguousLostPackets]++
		c.interLossDistanceCounter[uint(pn-c.lastLostPacketNumber)]++
		c.numberOfContiguousLostPackets = 1
	}
	c.lastLostPacketNumber = pn
	c.incrementCounter()
}

func (c *averageRedundancyController) OnPacketReceived(pn protocol.PacketNumber, recovered bool) {
	if pn < c.initialPacketOfThisSample {
		return
	}
	if c.packetsCounter == 0 {
		c.initialPacketOfThisSample = pn
	}
	c.incrementCounter()
}

func (c *averageRedundancyController) GetNumberOfDataSymbols() uint {
	return uint(utils.MinUint64(uint64(c.meanInterLossDistance), uint64(c.maxNumberOfSourceSymbols)))
}

func (c *averageRedundancyController) GetNumberOfRepairSymbols() uint {
	return uint(utils.MinUint64(uint64(c.meanBurstLength), uint64(c.maxNumberOfRepairSymbols)))
}

func (c *averageRedundancyController) GetNumberOfInterleavedBlocks() uint {
	if c.maxNumberOfRepairSymbols == 1 {
		return c.meanBurstLength
	}
	return 1
}

func (c *averageRedundancyController) GetWindowStepSize(protocol.PathID) uint {
	return uint(utils.MaxUint64(2, uint64(c.meanInterLossDistance/c.meanBurstLength)))
}

func (c *averageRedundancyController) incrementCounter() {
	c.packetsCounter++
	if c.packetsCounter == SAMPLE_SIZE {
		c.computeEstimations()
		c.packetsCounter = 0
		c.burstCounter = make(map[uint]uint)
		c.interLossDistanceCounter = make(map[uint]uint)
	}
}

func (c *averageRedundancyController) computeEstimations() {
	var sumOccurrencesTimesBurstLength uint = 0
	var sumOccurrences uint = 0
	for burstLength, count := range c.burstCounter {
		sumOccurrencesTimesBurstLength += burstLength * count
		sumOccurrences += count
	}
	if sumOccurrences > 0 {
		c.meanBurstLength = movingAverage(c.meanBurstLength, sumOccurrencesTimesBurstLength/sumOccurrences, 0.5)
	} else {
		c.meanBurstLength = utils.MaxUint(1, movingAverage(c.meanBurstLength, 0, 0.5))
	}

	var sumOccurencesTimesILD uint = 0
	sumOccurrences = 0
	for ild, count := range c.interLossDistanceCounter {
		sumOccurencesTimesILD += ild * count
		sumOccurrences += count
	}
	if sumOccurrences > 0 {
		c.meanInterLossDistance = uint(utils.MinUint64(uint64(movingAverage(c.meanInterLossDistance, sumOccurencesTimesILD/sumOccurrences, 0.7)), uint64(c.maxNumberOfSourceSymbols)))
	} else {
		c.meanInterLossDistance = movingAverage(c.meanInterLossDistance, uint(c.maxNumberOfSourceSymbols), 0.7)
	}
}

func movingAverage(old uint, new uint, factor float64) uint {
	return uint(factor*float64(old) + (1-factor)*float64(new))
}
