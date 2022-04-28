package sedpf

import (
	"fmt"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

type PathInfo struct {
	arrivals     []float64
	arrivalModel []Gaussian
	bandwidth    float64
	Z            Gaussian
	nextInsert   int
	windowFilled int
}

var Paths map[protocol.PathID]PathInfo = make(map[protocol.PathID]PathInfo)

func NewPathInfo() PathInfo {
	return PathInfo{
		arrivals:     make([]float64, protocol.SEDPF_WINDOW_SIZE),
		arrivalModel: make([]Gaussian, protocol.SEDPF_WINDOW_SIZE),
		nextInsert:   0,
		windowFilled: 0,
	}
}

func (p *PathInfo) UpdateZ() {
	p.Z = MaxPartitionMBT(p.arrivalModel)
}

func (p PathInfo) ReceivedEnoughMeasurements() bool {
	return p.windowFilled >= protocol.SEDPF_WINDOW_SIZE
}

func (p PathInfo) ExpectedArrivalDelta(size protocol.ByteCount) float64 {
	return float64(size*8) / p.bandwidth
}

func (p PathInfo) ExpectedArrivalNext(size protocol.ByteCount) float64 {
	return p.Z.Mean + p.ExpectedArrivalDelta(size)
}

func (p *PathInfo) InsertMeasurement(delay, rate float64) {
	delay *= 1000 // delay in milliseconds

	if delay <= 0 || rate <= 0 {
		fmt.Println("skipped InsertMeasurement")
		return
	}

	p.bandwidth = rate
	p.arrivals[p.nextInsert] = delay
	p.arrivalModel[p.nextInsert] = NewGaussianFromSeries(p.arrivals)

	if !p.ReceivedEnoughMeasurements() {
		p.windowFilled++
	}

	p.nextInsert = (p.nextInsert + 1) % protocol.SEDPF_WINDOW_SIZE
}

func AddPath(pathID protocol.PathID) {
	utils.Infof("adding path %d to S-EDPF scheduler", pathID)

	p := NewPathInfo()
	Paths[pathID] = p
}

func InsertMeasurement(pathID protocol.PathID, delay, rate float64) {
	path, ok := Paths[pathID]

	if !ok {
		return
	}

	path.InsertMeasurement(delay, rate)

	Paths[pathID] = path
}
