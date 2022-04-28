package quic

import (
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

type FECFramer struct {
	transmissionQueue []*fec.RepairSymbol

	currentOffsetInSymbol protocol.ByteCount

	version protocol.VersionNumber

	currentFrameOffset int
	FECSender
}

type FECSender interface {
	onHasFECData()
}

func newFECFramer(sender FECSender, version protocol.VersionNumber) *FECFramer {
	return &FECFramer{
		FECSender: sender,
		version:   version,
	}
}

// Pops some FEC Frames from the transmission buffer if there is space enough and returns a tuple (res, takenPayload)
// with takenPayload the payload size taken by these frames
func (f *FECFramer) maybePopFECFrames(maxBytes protocol.ByteCount) (res []*wire.FECFrame, takenPayload protocol.ByteCount, error error) {
	defer func() {
		if len(f.transmissionQueue) > 0 {
			f.onHasFECData()
		}
	}()
	takenPayload = 0
	if maxBytes > 0 && len(f.transmissionQueue) > 0 {
		frame, takenPayload := f.popFECFrame(maxBytes)
		return []*wire.FECFrame{frame}, takenPayload, nil
	}

	return
}

func (f *FECFramer) popFECFrame(maxBytes protocol.ByteCount) (*wire.FECFrame, protocol.ByteCount) {
	if len(f.transmissionQueue) == 0 {
		return nil, 0
	}
	symbol := f.transmissionQueue[0]
	tempFrame := wire.FECFrame{Offset: protocol.FecFrameOffset(f.currentOffsetInSymbol), RepairSymbolNumber: symbol.SymbolNumber}
	fecFrameHeaderLength, _ := tempFrame.MinLength(f.version)

	if maxBytes <= fecFrameHeaderLength {
		return nil, 0
	}

	maxBytes -= fecFrameHeaderLength
	remainingData := symbol.Data[f.currentOffsetInSymbol:]
	lenDataToSend := utils.Min(int(maxBytes), len(remainingData))
	dataToSend := remainingData[:lenDataToSend]

	frame := &wire.FECFrame{
		FECSchemeSpecific:     symbol.FECSchemeSpecific,
		FECBlockNumber:        symbol.FECBlockNumber,
		Offset:                protocol.FecFrameOffset(f.currentFrameOffset),
		RepairSymbolNumber:    symbol.SymbolNumber,
		DataLength:            protocol.FecFrameLength(len(dataToSend)),
		Data:                  dataToSend,
		NumberOfPackets:       byte(symbol.NumberOfPackets),
		NumberOfRepairSymbols: byte(symbol.NumberOfRepairSymbols),
		Convolutional:         symbol.Convolutional,
		EncodingSymbolID:      symbol.EncodingSymbolID,
	}

	f.currentOffsetInSymbol += protocol.ByteCount(lenDataToSend)

	if lenDataToSend == len(remainingData) {
		// We've read the symbol until the end
		f.currentOffsetInSymbol = 0
		f.currentFrameOffset = 0
		f.transmissionQueue = f.transmissionQueue[1:]
		frame.FinBit = true
	} else {
		f.currentFrameOffset++
	}
	ml, _ := frame.MinLength(f.version)
	return frame, ml + protocol.ByteCount(len(frame.Data))
}

//TODO: make this thread safe (channels)
func (f *FECFramer) popRepairSymbol() (retVal *fec.RepairSymbol) {
	retVal, f.transmissionQueue = f.transmissionQueue[0], f.transmissionQueue[1:]
	return
}
func (f *FECFramer) pushRepairSymbol(symbol *fec.RepairSymbol) {
	f.transmissionQueue = append(f.transmissionQueue, symbol)
	f.onHasFECData()
}

func (f *FECFramer) pushRepairSymbols(symbols []*fec.RepairSymbol) {
	f.transmissionQueue = append(f.transmissionQueue, symbols...)
}

func (f *FECFramer) hasFECDataToSend() bool {
	return len(f.transmissionQueue) > 0
}
