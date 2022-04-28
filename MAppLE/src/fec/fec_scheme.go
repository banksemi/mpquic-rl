package fec

import (
	"errors"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"runtime"
	"unsafe"
)

type FECScheme interface {
}

type BlockFECScheme interface {
	FECScheme
	GetRepairSymbols(fecGroup FECContainer, numberOfSymbols uint, id protocol.FECBlockNumber) ([]*RepairSymbol, error)
	RecoverPackets(fecGroup *FECBlock) ([][]byte, error)
	CanRecoverPackets(fecGroup *FECBlock, largestRcvdPacketNumber protocol.PacketNumber) bool
}

type ConvolutionalFECScheme interface {
	FECScheme
	GetRepairSymbols(fecGroup FECContainer, numberOfSymbols uint, id protocol.FECEncodingSymbolID) ([]*RepairSymbol, error)
	CanRecoverPackets() bool
	RecoverPackets(ignoreBelow protocol.FECEncodingSymbolID) ([][]byte, error)
	AddEquation(*Equation, []protocol.FECEncodingSymbolID, []*Variable)
	// returns true if the variable was an unknown of the current system (and thus could help to solve it), false otherwise
	AddKnownVariable(*Variable) bool
}

func FlushCurrentSymbols(f FECScheme, fc FECContainer, numberOfRepairSymbols uint) ([]*RepairSymbol, error) {
	if fs, ok := f.(ConvolutionalFECScheme); ok {
		window := fc.(*FECWindow)
		return fs.GetRepairSymbols(window, numberOfRepairSymbols, window.currentIndex)
	} else if fs, ok := f.(BlockFECScheme); ok {
		group := fc.(*FECBlock)
		return fs.GetRepairSymbols(group, numberOfRepairSymbols, group.FECBlockNumber)
	}
	return nil, nil
}

type XORFECScheme struct {
	isConvolutional bool
	dontOptimize    bool
}

var _ BlockFECScheme = &XORFECScheme{}

var XORFECSchemeCannotRecoverPacket = errors.New("XORFECScheme: cannot recover packet")
var XORFECSchemeCannotGetRepairSymbol = errors.New("XORFECScheme: cannot get repair symbol")
var XORFECSchemeTooMuchSymbolsNeeded = errors.New("XORFECScheme: cannot generate enough repair symbols")

func (f *XORFECScheme) CanRecoverPackets(group *FECBlock, largestRcvdPacketNumber protocol.PacketNumber) bool {
	// delay restores until there is at least a gap in the received packets
	return largestRcvdPacketNumber-1 > group.highestPacketNumber &&
		group.CurrentNumberOfPackets() == group.TotalNumberOfPackets-1 &&
		len(group.RepairSymbols) == 1
}

func (f *XORFECScheme) RecoverPackets(group *FECBlock) ([][]byte, error) {
	// XXX ugly, Ugly, UGLY hack
	// want to bypass the packet number check here
	if !f.CanRecoverPackets(group, protocol.PacketNumber(1000000000)) {
		return nil, XORFECSchemeCannotRecoverPacket
	}
	current := group.RepairSymbols[0].Data
	for _, p := range group.GetPackets() {
		if p != nil {
			current = f.XOR(current, p)
		}
	}
	return [][]byte{current}, nil
}

func (f *XORFECScheme) GetRepairSymbols(group FECContainer, numberOfSymbols uint, _ protocol.FECBlockNumber) ([]*RepairSymbol, error) {
	packets := group.GetPackets()
	max := 0
	for _, p := range group.GetPackets() {
		if max < len(p) {
			max = len(p)
		}
	}

	if len(packets) == 0 {
		return nil, XORFECSchemeCannotGetRepairSymbol
	}
	if numberOfSymbols > 1 {
		return nil, XORFECSchemeTooMuchSymbolsNeeded
	}
	var current []byte
	first := true
	for _, p := range packets {
		if p != nil {
			if first {
				current = p
				first = false
			} else {
				current = f.XOR(current, p)
			}
		}
	}
	return []*RepairSymbol{{
		Data:          current,
		Convolutional: f.Convolutional(),
	}}, nil
}

func (f *XORFECScheme) XOR(a, b []byte) []byte {
	if !f.dontOptimize && supportsUnaligned {
		return fastXORBytes(a, b)
	} else {
		return slowXOR(a, b)
	}
}

func slowXOR(a []byte, b []byte) []byte {
	var retVal []byte
	if len(a) >= len(b) {
		retVal = make([]byte, protocol.MaxReceivePacketSize)[:len(a)]
	} else {
		retVal = make([]byte, protocol.MaxReceivePacketSize)[:len(b)]
	}
	for i := 0; i < len(retVal); i++ {
		if i >= len(a) {
			retVal[i] = b[i]
		} else if i >= len(b) {
			retVal[i] = a[i]
		} else {
			retVal[i] = a[i] ^ b[i]
		}
	}
	return retVal
}

const wordSize = int(unsafe.Sizeof(uintptr(0)))
const supportsUnaligned = runtime.GOARCH == "386" || runtime.GOARCH == "amd64" || runtime.GOARCH == "ppc64" || runtime.GOARCH == "ppc64le" || runtime.GOARCH == "s390x"

// fastXORBytes xors in bulk. It only works on architectures that
// support unaligned read/writes.
func fastXORBytes(a, b []byte) []byte {
	n := len(a)
	nMax := len(b)
	if len(b) < n {
		n = len(b)
		nMax = len(a)
	}
	if n == 0 {
		return nil
	}
	dst := make([]byte, nMax)
	w := n / wordSize
	if w > 0 {
		dw := *(*[]uintptr)(unsafe.Pointer(&dst))
		aw := *(*[]uintptr)(unsafe.Pointer(&a))
		bw := *(*[]uintptr)(unsafe.Pointer(&b))
		// xor all the full words
		for i := 0; i < w; i++ {
			dw[i] = aw[i] ^ bw[i]
		}
	}
	// xor the remaining bytes
	for i := (n - n%wordSize); i < n; i++ {
		dst[i] = a[i] ^ b[i]
	}
	for i := n; i < nMax; i++ {
		if len(b) > len(a) {
			dst[i] = b[i]
		} else {
			dst[i] = a[i]
		}
	}
	return dst
}

func (f *XORFECScheme) Convolutional() bool {
	return f.isConvolutional
}
