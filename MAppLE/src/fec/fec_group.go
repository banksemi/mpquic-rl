package fec

import (
	"errors"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

//TODO: maybe the RepairSymbol should have its number in the structure

type FECBlock struct {
	FECBlockNumber             protocol.FECBlockNumber
	RepairSymbols              []*RepairSymbol
	nSentRepairSymbols         uint8
	framesOffset               uint8
	offsetInSymbol             protocol.ByteCount
	packets                    [][]byte
	packetIndexes              map[protocol.PathID]map[protocol.PacketNumber]int
	version                    protocol.VersionNumber
	TotalNumberOfPackets       int
	TotalNumberOfRepairSymbols int
	highestPacketNumber        protocol.PacketNumber
}

var _ FECContainer = &FECBlock{}

func NewFECGroup(fecBlockNumber protocol.FECBlockNumber, version protocol.VersionNumber) *FECBlock {
	return &FECBlock{
		FECBlockNumber: fecBlockNumber,
		packetIndexes:  make(map[protocol.PathID]map[protocol.PacketNumber]int),
		packets:        make([][]byte, 0),
		version:        version,
	}
}

var FECGroupPacketAddedToFullFECGroup = errors.New("FECBlockNumber: A packet has been added to an already full FEC Payload")

func (f *FECBlock) ShouldBeSent(c RedundancyController, p protocol.PathID) bool {
	return uint32(len(f.packets)) >= uint32(c.GetNumberOfDataSymbols())
}

func (f *FECBlock) AddPacket(packet []byte, hdr *wire.Header) {
	if _, ok := f.packetIndexes[hdr.PathID]; !ok {
		f.packetIndexes[hdr.PathID] = make(map[protocol.PacketNumber]int)
	} else if _, ok := f.packetIndexes[hdr.PathID][hdr.PacketNumber]; ok {
		return
	}

	if f.highestPacketNumber < hdr.PacketNumber {
		f.highestPacketNumber = hdr.PacketNumber
	}

	packetCopy := make([]byte, protocol.MaxReceivePacketSize)[:len(packet)]
	copy(packetCopy, packet)
	fpid := hdr.FECPayloadID
	if fpid.GetBlockOffset() >= byte(len(f.packets)) {
		delta := int(fpid.GetBlockOffset()) - len(f.packets)
		for i := 0; i <= delta; i++ {
			f.packets = append(f.packets, nil)
		}
	}
	f.packets[fpid.GetBlockOffset()] = packetCopy
	f.packetIndexes[hdr.PathID][hdr.PacketNumber] = int(fpid.GetBlockOffset())
	return
}

// Must be called before sending the group
func (f *FECBlock) PrepareToSend() error {
	f.TotalNumberOfPackets = len(f.packets)
	f.TotalNumberOfRepairSymbols = len(f.RepairSymbols)

	for _, r := range f.RepairSymbols {
		r.NumberOfPackets = uint8(f.TotalNumberOfPackets)
		r.NumberOfRepairSymbols = uint8(f.TotalNumberOfRepairSymbols)

	}
	return nil
}

func (f *FECBlock) AddRepairSymbol(symbol *RepairSymbol) error {
	f.RepairSymbols = append(f.RepairSymbols, symbol)
	return nil
}

func (f *FECBlock) HasPacket(packetNumber protocol.PacketNumber, pathID protocol.PathID) bool {
	if _, ok := f.packetIndexes[pathID]; ok {
		_, ok = f.packetIndexes[pathID][packetNumber]
		return ok
	}
	return false
}

func (f *FECBlock) GetPacketOffset(packetNumber protocol.PacketNumber, pathID protocol.PathID) byte {
	return byte(f.packetIndexes[pathID][packetNumber])
}

func (f *FECBlock) HasFECDataToSend() bool {
	return int(f.nSentRepairSymbols) < len(f.RepairSymbols)
}

func (f *FECBlock) GetFECFrame(maxBytes protocol.ByteCount) (*wire.FECFrame, error) {
	if f.nSentRepairSymbols == uint8(len(f.RepairSymbols)) || len(f.packets) == 0 {
		return nil, nil
	}
	symbol := f.RepairSymbols[f.nSentRepairSymbols]
	tempFrame := wire.FECFrame{Offset: protocol.FecFrameOffset(f.framesOffset), RepairSymbolNumber: f.nSentRepairSymbols}
	fecFrameHeaderLength, _ := tempFrame.MinLength(f.version)
	if maxBytes <= fecFrameHeaderLength {
		return nil, nil
	}
	maxBytes -= fecFrameHeaderLength
	remainingData := symbol.Data[f.offsetInSymbol:]
	lenDataToSend := utils.Min(int(maxBytes), len(remainingData))
	dataToSend := remainingData[:lenDataToSend]
	frame := &wire.FECFrame{
		FECBlockNumber:        f.FECBlockNumber,
		Offset:                protocol.FecFrameOffset(f.framesOffset),
		RepairSymbolNumber:    f.nSentRepairSymbols,
		DataLength:            protocol.FecFrameLength(len(dataToSend)),
		Data:                  dataToSend,
		NumberOfPackets:       byte(len(f.packets)),
		NumberOfRepairSymbols: byte(len(f.RepairSymbols)),
	}
	f.offsetInSymbol += protocol.ByteCount(lenDataToSend)
	if lenDataToSend == len(remainingData) {
		// We've read the symbol until the end
		f.nSentRepairSymbols++
		f.offsetInSymbol = 0
		f.framesOffset = 0
		frame.FinBit = true
	} else {
		f.framesOffset++
	}
	return frame, nil
}

func (f *FECBlock) GetFECFrames(maxBytes protocol.ByteCount) ([]*wire.FECFrame, protocol.ByteCount, error) {
	var frames []*wire.FECFrame
	var totalBytes protocol.ByteCount = 0
	// for ; maxBytes > 0; {
	frame, err := f.GetFECFrame(maxBytes)
	if err != nil {
		return nil, 0, err
	}
	if frame == nil {
		return frames, totalBytes, nil
	}
	frameHeaderLen, _ := frame.MinLength(f.version)
	frameLen := protocol.ByteCount(len(frame.Data)) + frameHeaderLen
	maxBytes -= frameLen
	totalBytes += frameLen
	frames = append(frames, frame)
	// }
	return frames, totalBytes, nil
}

func (f *FECBlock) CurrentNumberOfPackets() int {
	retVal := 0
	for _, packets := range f.packetIndexes {
		retVal += len(packets)
	}
	return retVal
}

func (f *FECBlock) GetRepairSymbols() []*RepairSymbol {
	return f.RepairSymbols
}

func (f *FECBlock) SetRepairSymbols(symbols []*RepairSymbol) {
	for _, s := range symbols {
		s.FECBlockNumber = f.FECBlockNumber
		s.NumberOfRepairSymbols = uint8(len(symbols))
		s.NumberOfPackets = uint8(f.CurrentNumberOfPackets())
	}
	f.RepairSymbols = symbols
}

func (f *FECBlock) GetPackets() [][]byte {
	retVal := make([][]byte, len(f.packets))
	for _, pathPackets := range f.packetIndexes {
		for _, idx := range pathPackets {
			retVal[idx] = f.packets[idx]
		}
	}
	return retVal

}
