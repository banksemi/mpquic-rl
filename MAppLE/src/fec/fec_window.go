package fec

import (
	"errors"
	"fmt"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

type FECContainer interface {
	ShouldBeSent(RedundancyController, protocol.PathID) bool
	AddPacket([]byte, *wire.Header)
	PrepareToSend() error
	AddRepairSymbol(*RepairSymbol) error
	HasPacket(protocol.PacketNumber, protocol.PathID) bool
	GetPacketOffset(protocol.PacketNumber, protocol.PathID) byte
	CurrentNumberOfPackets() int
	SetRepairSymbols([]*RepairSymbol)
	GetRepairSymbols() []*RepairSymbol
	GetPackets() [][]byte
}

var _ FECContainer = &FECWindow{}

type FECWindow struct {
	WindowSize                 uint8
	RepairSymbols              []*RepairSymbol
	nSentRepairSymbols         uint8
	framesOffset               uint8
	offsetInSymbol             protocol.ByteCount
	packets                    utils.PacketsRingBuffer
	packetIndexes              map[protocol.PathID]map[protocol.PacketNumber]int
	version                    protocol.VersionNumber
	currentNumberOfPackets     int
	currentIndex               protocol.FECEncodingSymbolID
	lastSymbolSent             protocol.FECEncodingSymbolID
	TotalNumberOfPackets       int
	TotalNumberOfRepairSymbols int
}

func NewFECWindow(windowSize uint8, version protocol.VersionNumber) *FECWindow {
	return &FECWindow{
		WindowSize:    windowSize,
		packetIndexes: make(map[protocol.PathID]map[protocol.PacketNumber]int),
		packets:       utils.NewPacketsRingBuffer(windowSize),
		version:       version,
		currentIndex:  protocol.FECEncodingSymbolID(0),
	}
}

var FECWindowPacketAddedToFullFECWindow = errors.New("FECWindow: A packet has been added to an already full FEC Payload")

func (f *FECWindow) ShouldBeSent(c RedundancyController, p protocol.PathID) bool {
	return f.currentIndex-f.lastSymbolSent >= protocol.FECEncodingSymbolID(c.GetWindowStepSize(p))
}

func (f *FECWindow) AddPacket(packet []byte, hdr *wire.Header) {
	if hdr.GetFECPayloadID().GetConvolutionalEncodingSymbolID() != f.currentIndex+1 {
		panic(fmt.Sprintf("wrong fec encoding symbol id received: %d != %d, pid = %d", hdr.GetFECPayloadID().GetConvolutionalEncodingSymbolID(), f.currentIndex+1, hdr.PathID))
	}
	if _, ok := f.packetIndexes[hdr.PathID]; !ok {
		f.packetIndexes[hdr.PathID] = make(map[protocol.PacketNumber]int)
	} else if _, ok := f.packetIndexes[hdr.PathID][hdr.PacketNumber]; ok {
		return
	}
	packetCopy := make([]byte, protocol.MaxReceivePacketSize)[:len(packet)]
	copy(packetCopy, packet)
	f.packetIndexes[hdr.PathID][hdr.PacketNumber] = f.packets.AddPacket(packetCopy)
	f.currentNumberOfPackets = f.packets.CurrentSize()
	f.currentIndex++
	return
}

// Must be called before sending the group
func (f *FECWindow) PrepareToSend() error {
	f.lastSymbolSent = f.currentIndex
	return nil
}

func (f *FECWindow) AddRepairSymbol(symbol *RepairSymbol) error {
	f.RepairSymbols = append(f.RepairSymbols, symbol)
	return nil
}

func (f *FECWindow) HasPacket(packetNumber protocol.PacketNumber, pathID protocol.PathID) bool {
	_, ok := f.packetIndexes[pathID][packetNumber]
	return ok
}

func (f *FECWindow) GetPacketOffset(packetNumber protocol.PacketNumber, pathID protocol.PathID) byte {
	return byte(f.packetIndexes[pathID][packetNumber])
}

func (f *FECWindow) CurrentNumberOfPackets() int {
	return f.currentNumberOfPackets
}

func (f *FECWindow) SetRepairSymbols(symbols []*RepairSymbol) {
	for _, s := range symbols {
		s.EncodingSymbolID = f.currentIndex
		s.NumberOfRepairSymbols = uint8(len(symbols))
		s.NumberOfPackets = uint8(f.CurrentNumberOfPackets())
	}
	f.RepairSymbols = symbols
}

func (f *FECWindow) GetRepairSymbols() []*RepairSymbol {
	return f.RepairSymbols
}

func (f *FECWindow) GetPackets() [][]byte {
	return f.packets.GetAll()
}

func (f *FECWindow) HasSomethingToSend() bool {
	return f.currentIndex != f.lastSymbolSent
}

func (f *FECWindow) SetSize(s int) {
	newRingBuffer := utils.NewPacketsRingBuffer(uint8(s))
	for _, p := range f.packets.GetAll() {
		newRingBuffer.AddPacket(p)
	}
	f.packets = newRingBuffer
}
