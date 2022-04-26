package protocol

// A StreamID in QUIC
type FecFrameLength uint32
type FecFrameOffset uint8

type FECPayloadID uint64

const MaxNumberOfFecPackets uint32 = 32
const MaxFECWindowSize = ^uint8(0)

const DEBUG_RETRANSMIT_UNRELIABLE bool = false

const MAX_RECOVERED_PACKETS_IN_ONE_ROW = 32

const PROTECT_RELIABLE_STREAM_FRAMES = true
const SEND_RECOVERED_FRAMES_WHEN_RECOVERED = true
const USE_FAST_RETRANSMIT = true

var FILE_CONTAINING_CWIN = "cwin_evolution"

const APPLY_CONGESTION_CONTROL_ON_UNRELIABLE_STREAMS = true

const APPLY_CONGESTION_CONTROL = true

const USE_FEC_DEDICATED_PATH = true

type FECSchemeID = byte

const XORFECScheme FECSchemeID = 0
const ReedSolomonFECScheme FECSchemeID = 1
const RLCFECScheme FECSchemeID = 2

var (
	NumberOfFecPackets           uint32 = 4
	NumberOfRepairSymbols        uint8  = 1
	NumberOfInterleavedFECGroups uint   = 1
	ConvolutionalWindowSize      uint8  = 20
	ConvolutionalStepSize        int    = 6
)

var Total = make(map[string]uint64)

type FECBlockNumber uint32
type FECEncodingSymbolID uint32

func NewBlockRepairFECPayloadID(fecSchemeSpecific uint32, blockNumber FECBlockNumber, symbolNumber uint8) FECPayloadID {
	return FECPayloadID(uint64(fecSchemeSpecific)<<32 + uint64(blockNumber<<8) + uint64(symbolNumber))
}

func NewBlockSourceFECPayloadID(blockNumber FECBlockNumber, symbolOffset uint8) FECPayloadID {
	return NewBlockRepairFECPayloadID(0, blockNumber, symbolOffset)
}

func NewConvolutionalRepairFECPayloadID(fecSchemeSpecific uint32, esID FECEncodingSymbolID) FECPayloadID {
	return FECPayloadID(uint64(fecSchemeSpecific)<<32 + uint64(esID))
}

func NewConvolutionalSourceFECPayloadID(esID FECEncodingSymbolID) FECPayloadID {
	return FECPayloadID(uint64(esID))
}

func (f FECPayloadID) GetFECSchemeSpecific() uint32 {
	return uint32(f >> 32)
}

func (f FECPayloadID) GetBlockNumber() FECBlockNumber {
	return FECBlockNumber(uint32(f) >> 8)
}

func (f FECPayloadID) GetBlockOffset() uint8 {
	return uint8(f)
}

func (f FECPayloadID) GetBlockSymbolNumber() uint8 {
	return uint8(f)
}

func (f FECPayloadID) GetConvolutionalEncodingSymbolID() FECEncodingSymbolID {
	return FECEncodingSymbolID(uint32(f))
}
