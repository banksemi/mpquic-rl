package fec

import (
	"github.com/lucas-clemente/quic-go/internal/protocol"
)

type FECScheduler interface {
	//GetNextFECPayload() *wire.RepairSymbol
	// called to indicate that a RepairSymbol for a particular FEC Group has been sent and should thus be replaced by a new one in the scheduler
	SentFECBlock(protocol.FECBlockNumber)
	GetNextFECBlockNumber() protocol.FECBlockNumber
	GetNextFECGroup() *FECBlock
	GetNextFECGroupOffset() byte
	SetRedundancyController(controller RedundancyController)
}

type RoundRobinScheduler struct {
	fecGroups             []*FECBlock
	maxFECBlockNumber     protocol.FECBlockNumber
	size                  uint
	offset                uint
	nextFECBlockNumber    protocol.FECBlockNumber
	fecBlockNumberToIndex map[protocol.FECBlockNumber]uint
	version               protocol.VersionNumber
	redundancyController  RedundancyController
}

var _ FECScheduler = &RoundRobinScheduler{}

func NewRoundRobinScheduler(redundancyController RedundancyController, version protocol.VersionNumber) *RoundRobinScheduler {
	return &RoundRobinScheduler{
		redundancyController:  redundancyController,
		fecGroups:             make([]*FECBlock, redundancyController.GetNumberOfInterleavedBlocks()),
		size:                  0,
		offset:                0,
		nextFECBlockNumber:    0,
		maxFECBlockNumber:     (1 << 24) - 1,
		fecBlockNumberToIndex: make(map[protocol.FECBlockNumber]uint),
		version:               version,
	}
}

func (s *RoundRobinScheduler) SentFECBlock(group protocol.FECBlockNumber) {
	index, ok := s.fecBlockNumberToIndex[group]
	if ok {
		delete(s.fecBlockNumberToIndex, group)
		// fill the hole created
		s.putNewFECGroupAtIndex(index)
	}
}

// post: the FECBlock of the returned FECBlockNumber can be added at least one more packet
func (s *RoundRobinScheduler) GetNextFECBlockNumber() protocol.FECBlockNumber {
	if s.size < s.redundancyController.GetNumberOfInterleavedBlocks() {
		return s.nextFECBlockNumber
	}
	return s.fecGroups[s.offset].FECBlockNumber
}

// post: the returned RepairSymbol can be added at least one more packet
func (s *RoundRobinScheduler) GetNextFECGroup() *FECBlock {
	if s.size < s.redundancyController.GetNumberOfInterleavedBlocks() && s.offset == s.size {
		s.putNewFECGroupAtIndex(s.size)
		s.size++
		retVal := s.fecGroups[s.offset]
		s.offset = (s.offset + 1) % s.redundancyController.GetNumberOfInterleavedBlocks()
		//s.offset = (s.offset + 1) % s.size
		return retVal
	} else {
		retVal := s.fecGroups[s.offset]
		s.offset = (s.offset + 1) % s.redundancyController.GetNumberOfInterleavedBlocks()
		//s.offset = (s.offset + 1) % s.size
		return retVal
	}
}

func (s *RoundRobinScheduler) GetNextFECGroupOffset() byte {
	if s.size < s.redundancyController.GetNumberOfInterleavedBlocks() && s.offset == s.size {
		return 0
	}
	return byte(s.fecGroups[s.offset].CurrentNumberOfPackets())
}

func (s *RoundRobinScheduler) SetRedundancyController(c RedundancyController) {
	s.redundancyController = c
}

func (s *RoundRobinScheduler) getNextFECBlockNumberAndThenIncrementIt() protocol.FECBlockNumber {
	retVal := s.nextFECBlockNumber
	s.nextFECBlockNumber = s.nextFECBlockNumber + 1%s.maxFECBlockNumber
	return retVal
}

func (s *RoundRobinScheduler) putNewFECGroupAtIndex(index uint) {
	fecGroupNumber := s.getNextFECBlockNumberAndThenIncrementIt()
	if len(s.fecGroups) <= int(index) {
		s.fecGroups = append(s.fecGroups, make([]*FECBlock, int(index)-len(s.fecGroups)+1)...)
	}
	s.fecGroups[index] = NewFECGroup(fecGroupNumber, s.version)
	s.fecBlockNumberToIndex[fecGroupNumber] = index
}
