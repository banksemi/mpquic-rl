package quic

import (
	"fmt"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"strings"
)

// values from QUIC-FEC example
const (
	NumberOfFecPackets           uint = 8
	NumberOfRepairSymbols        uint = 1
	NumberOfInterleavedFECGroups uint = 1
	ConvolutionalWindowSize      uint = 20
	ConvolutionalStepSize        uint = 6
)

func defaultConstantRC() fec.RedundancyController {
	return fec.NewConstantRedundancyController(NumberOfFecPackets, NumberOfRepairSymbols, NumberOfInterleavedFECGroups, ConvolutionalStepSize)
}

func defaultConstantRCConvolutional() fec.RedundancyController {
	return fec.NewConstantRedundancyController(ConvolutionalWindowSize, NumberOfRepairSymbols, NumberOfInterleavedFECGroups, ConvolutionalStepSize)
}

func defaultAverageRC() fec.RedundancyController {
	return fec.NewAverageRedundancyController(uint8(NumberOfFecPackets), uint8(NumberOfRepairSymbols))
}

func FECConfigXOR(sourceSymbols uint, repairSymbols uint, stepSize uint) (protocol.FECSchemeID, fec.RedundancyController) {
	rc := fec.NewConstantRedundancyController(sourceSymbols, repairSymbols, 1, stepSize)
	return XORFECScheme, rc
}

func FECConfigFromString(name string) (protocol.FECSchemeID, fec.RedundancyController) {
	fmt.Printf("selecting FEC scheme %s\n", name)

	switch strings.ToLower(name) {
	case "xor4-1":
		return FECConfigXOR(4, 1, 1)
	case "xor8-1":
		return FECConfigXOR(8, 1, 1)
	case "xor16-1":
		return FECConfigXOR(16, 1, 1)
	case "xor16-4":
		return FECConfigXOR(16, 1, 4)
	case "xor16-8":
		return FECConfigXOR(16, 1, 8)
	case "xor32-1":
		return FECConfigXOR(32, 1, 1)
	case "xor32-1-4":
		return FECConfigXOR(32, 4, 1)
	case "test":
		return FECConfigXOR(16, 1, 1)
	case "avg-xor":
		return XORFECScheme, defaultAverageRC()
	case "win-xor":
		return XORFECScheme, fec.NewWindowedRedundancyController()
	case "delay-xor":
		return XORFECScheme, fec.NewDelaySensitiveRedundancyController(protocol.XORFECScheme, false)
	case "delay2-xor":
		return XORFECScheme, fec.NewDelaySensitiveRedundancyController(protocol.XORFECScheme, true)
	case "dfec-xor":
		return XORFECScheme, fec.NewdFECRedundancyController()
	case "rb-xor":
		return XORFECScheme, fec.NewRemainingBytesRedundancyController()
	case "const-rlc":
		return RLCFECScheme, defaultConstantRCConvolutional()
	case "avg-rlc":
		return RLCFECScheme, defaultAverageRC()
	case "win-rlc":
		return RLCFECScheme, fec.NewWindowedRedundancyController()
	case "delay-rlc":
		return RLCFECScheme, fec.NewDelaySensitiveRedundancyController(protocol.RLCFECScheme, false)
	case "delay2-rlc":
		return RLCFECScheme, fec.NewDelaySensitiveRedundancyController(protocol.RLCFECScheme, true)
	case "rb-rlc":
		return RLCFECScheme, fec.NewRemainingBytesRedundancyController()
	case "const-rs":
		return ReedSolomonFECScheme, defaultConstantRC()
	case "avg-rs":
		return ReedSolomonFECScheme, defaultAverageRC()
	case "win-rs":
		return ReedSolomonFECScheme, fec.NewWindowedRedundancyController()
	case "delay-rs":
		return ReedSolomonFECScheme, fec.NewDelaySensitiveRedundancyController(protocol.ReedSolomonFECScheme, false)
	case "delay2-rs":
		return ReedSolomonFECScheme, fec.NewDelaySensitiveRedundancyController(protocol.ReedSolomonFECScheme, true)
	case "rb-rs":
		return ReedSolomonFECScheme, fec.NewRemainingBytesRedundancyController()
	}

	panic("invalid FEC scheme defined")
}
