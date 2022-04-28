package congestion

import (
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

const scale uint = 10

const DEFAULT_SMOOTHING_FACTOR = 0.25
const NUMBER_OF_NEEDED_SAMPLES = 3

// Olia implements the olia algorithm from MPTCP
type Olia struct {
	// Total number of bytes acked two losses ago
	loss1 protocol.ByteCount
	// Total number of bytes acked at the last loss
	loss2 protocol.ByteCount
	// Current number of bytes acked
	loss3      protocol.ByteCount
	epsilonNum int
	epsilonDen uint32
	sndCwndCnt int
	// We need to keep a reference to all paths

	// added by michelfra: LAMPS
	n2                            int64 // number of packets successfully delivered after the last loss event
	n1                            int64 // number of packets successfully delivered between the two last loss events
	currentN2Samples              []int64
	currentBurstLengthSamples     []int64
	numberOfNeededSamples         int
	currentBurstLength            int
	smoothedAckedBetweenTwoLosses float64
	smoothedBurstLength           float64
	smoothingFactor               float64
}

func NewOlia(ackedBytes protocol.ByteCount) *Olia {
	o := &Olia{
		loss1:                         ackedBytes,
		loss2:                         ackedBytes,
		loss3:                         ackedBytes,
		epsilonNum:                    0,
		epsilonDen:                    1,
		sndCwndCnt:                    0,
		n2:                            -1,
		n1:                            -1,
		numberOfNeededSamples:         3,
		smoothedAckedBetweenTwoLosses: -1.0,
		smoothedBurstLength:           -1.0,
		smoothingFactor:               DEFAULT_SMOOTHING_FACTOR,
	}
	return o
}

func oliaScale(val uint64, scale uint) uint64 {
	return uint64(val) << scale
}

func (o *Olia) Reset() {
	o.loss1 = 0
	o.loss2 = 0
	o.loss3 = 0
	o.epsilonNum = 0
	o.epsilonDen = 1
	o.sndCwndCnt = 0

	// added by michelfra
	o.smoothedAckedBetweenTwoLosses = -1.0
	o.smoothedBurstLength = -1.0
}

func (o *Olia) SmoothedBytesBetweenLosses() protocol.ByteCount {
	return utils.MaxByteCount(o.loss3-o.loss2, o.loss2-o.loss1)
}

func (o *Olia) UpdateAckedSinceLastLoss(ackedBytes protocol.ByteCount) {
	o.loss3 += ackedBytes
	o.n2++
	if o.currentBurstLength > 0 {
		if o.smoothedBurstLength == -1 {
			o.currentBurstLengthSamples = append(o.currentBurstLengthSamples, int64(o.currentBurstLength))
			if len(o.currentBurstLengthSamples) == o.numberOfNeededSamples {
				o.smoothedBurstLength = average(o.currentBurstLengthSamples)
			}
		} else {
			o.smoothedBurstLength = (1.0-o.smoothingFactor)*float64(o.smoothedBurstLength) + o.smoothingFactor*float64(o.currentBurstLength)
		}
	}
	o.currentBurstLength = 0
}

func (o *Olia) OnPacketLost() {
	// check if it is a burst
	isBurst := o.loss3 == o.loss2

	// TODO should we add so many if check? Not done here
	o.loss1 = o.loss2
	o.loss2 = o.loss3

	// added by michelfra: update smoothed values
	// (1−α)×n1+α×sample_n1
	// o.loss2 is here equal to o.loss3
	packetsBetweenTwoLastLosses := o.n2
	o.n1 = packetsBetweenTwoLastLosses
	o.n2 = 0

	if !isBurst { // don't update the loss rate if we are on a burst, otherwise the loss rate will quickly converge towards 1%
		if o.smoothedAckedBetweenTwoLosses == -1 {
			o.currentN2Samples = append(o.currentN2Samples, packetsBetweenTwoLastLosses)
			if len(o.currentN2Samples) == o.numberOfNeededSamples {
				o.smoothedAckedBetweenTwoLosses = average(o.currentN2Samples)
			}
		} else {
			o.smoothedAckedBetweenTwoLosses = (1.0-o.smoothingFactor)*float64(o.smoothedAckedBetweenTwoLosses) + o.smoothingFactor*float64(packetsBetweenTwoLastLosses)
		}
	}
	o.currentBurstLength++
}

func (o *Olia) CongestionWindowAfterAck(currentCongestionWindow protocol.PacketNumber, rate protocol.ByteCount, cwndScaled uint64) protocol.PacketNumber {
	newCongestionWindow := currentCongestionWindow
	incDen := uint64(o.epsilonDen) * uint64(currentCongestionWindow) * uint64(rate)
	if incDen == 0 {
		incDen = 1
	}

	// calculate the increasing term, scaling is used to reduce the rounding effect
	if o.epsilonNum == -1 {
		if uint64(o.epsilonDen)*cwndScaled*cwndScaled < uint64(rate) {
			incNum := uint64(rate) - uint64(o.epsilonDen)*cwndScaled*cwndScaled
			o.sndCwndCnt -= int(oliaScale(incNum, scale) / uint64(incDen))
		} else {
			incNum := uint64(o.epsilonDen)*cwndScaled*cwndScaled - uint64(rate)
			o.sndCwndCnt += int(oliaScale(incNum, scale) / uint64(incDen))
		}
	} else {
		incNum := uint64(o.epsilonNum)*uint64(rate) + uint64(o.epsilonDen)*cwndScaled*cwndScaled
		o.sndCwndCnt += int(oliaScale(incNum, scale) / uint64(incDen))
	}

	if o.sndCwndCnt >= (1<<scale)-1 {
		newCongestionWindow++
		o.sndCwndCnt = 0
	} else if o.sndCwndCnt <= 0-(1<<scale)+1 {
		newCongestionWindow = utils.MaxPacketNumber(1, currentCongestionWindow-1)
		o.sndCwndCnt = 0
	}
	return newCongestionWindow
}

func average(slice []int64) float64 {
	retVal := float64(0)
	for _, i := range slice {
		retVal += float64(i)
	}
	retVal /= float64(len(slice))
	return retVal
}
