package wire

import (
	"bytes"
	"errors"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/qerr"
	"io"
)

// A FECFrame of FEC-QUIC
type FECFrame struct {
	EncodingSymbolID      protocol.FECEncodingSymbolID
	FECSchemeSpecific     uint32
	Convolutional         bool
	FECBlockNumber        protocol.FECBlockNumber
	FinBit                bool                    // true if this is the end of a FECFrame payload
	DataLength            protocol.FecFrameLength // length of the part of payload contained in this frame (should hold on 31 bits to put the FIN bit in it ?)
	RepairSymbolNumber    byte                    // number of the FEC symbol transiting in this FEC Frame for this particular FEC Group
	Offset                protocol.FecFrameOffset // offset number of a FECFrame (for big packets, FEC payloads could span across multiple FEC frames)
	NumberOfPackets       byte                    // represents the number of packets protected by the FEC payload of this frame
	NumberOfRepairSymbols byte                    // represents the total number of repair symbols for this FEC Group
	Data                  []byte                  // payload part of this frame
}

var _ Frame = &FECFrame{}

var (
	// We received a FEC_FRAME with a data length >= 2^15
	FECFrameTooHighDataLength = errors.New("FECFrame: DataLength should not be >= 2^15")
	FECFrameTooMuchData       = errors.New("FECFrame: should not contain >= 2^15 bytes of data")
	FECFrameTooHighFECGroup   = errors.New("FECFrame: FECBlockNumber should not be >= 2^48")
)

// ParseStreamFrame reads a stream frame. The type byte must not have been read yet.
func ParseFECFrame(r *bytes.Reader, version protocol.VersionNumber) (*FECFrame, error) {
	frame := &FECFrame{}
	_, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	finBitConvolutionalAndFrameLength, err := utils.BigEndian.ReadUint16(r)

	if err != nil {
		return nil, err
	}

	frame.FinBit = finBitConvolutionalAndFrameLength&1 == 1
	frame.Convolutional = finBitConvolutionalAndFrameLength&2 != 0
	frame.DataLength = protocol.FecFrameLength(finBitConvolutionalAndFrameLength >> 2)

	fpiddata, err := utils.BigEndian.ReadUint64(r)
	if err != nil {
		return nil, err
	}

	fpid := protocol.FECPayloadID(fpiddata)

	frame.FECSchemeSpecific = fpid.GetFECSchemeSpecific()
	if frame.Convolutional {
		frame.EncodingSymbolID = fpid.GetConvolutionalEncodingSymbolID()
	} else {
		frame.FECBlockNumber = fpid.GetBlockNumber()
		frame.RepairSymbolNumber = fpid.GetBlockSymbolNumber()
	}

	offset, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	frame.Offset = protocol.FecFrameOffset(offset)

	// if offset is 0, the first byte of the frame is the number of protected packets and the second byte is the number of repair symbols
	if frame.Offset == 0 {
		// read the number of protected packets
		// and the number of repair symbols for the FEC Group
		frame.NumberOfPackets, err = r.ReadByte()
		if err != nil {
			return nil, err
		}
		frame.NumberOfRepairSymbols, err = r.ReadByte()
		if err != nil {
			return nil, err
		}
	}

	if frame.DataLength != 0 {
		frame.Data = make([]byte, frame.DataLength)
		if _, err := io.ReadFull(r, frame.Data); err != nil {
			return nil, err
		}
	}

	if len(frame.Data) == 0 {
		return nil, qerr.EmptyFECFrame
	}
	return frame, nil
}

//Writes a FECFrame
func (f FECFrame) Write(b *bytes.Buffer, version protocol.VersionNumber) error {
	if !version.UsesTLS() {
		b.WriteByte(0x0a)
	} else {
		b.WriteByte(0x0f)
	}
	if protocol.ByteCount(f.FECBlockNumber) >= protocol.MaxByteCount {
		return FECFrameTooHighFECGroup
	}

	var lengthFinBitAndConvolutional uint16 = 0
	if f.FinBit {
		lengthFinBitAndConvolutional = 1
	}

	if f.Convolutional {
		lengthFinBitAndConvolutional |= 2
	}

	if f.DataLength >= 1<<15 {
		return FECFrameTooHighDataLength
	}
	lengthFinBitAndConvolutional |= uint16(f.DataLength) << 2
	utils.BigEndian.WriteUint16(b, lengthFinBitAndConvolutional)

	var fpid protocol.FECPayloadID

	if f.Convolutional {
		fpid = protocol.NewConvolutionalRepairFECPayloadID(f.FECSchemeSpecific, f.EncodingSymbolID)
	} else {
		fpid = protocol.NewBlockRepairFECPayloadID(f.FECSchemeSpecific, protocol.FECBlockNumber(f.FECBlockNumber), f.RepairSymbolNumber)
	}

	utils.BigEndian.WriteUint64(b, uint64(fpid))

	b.WriteByte(uint8(f.Offset))
	if f.Offset == 0 {
		b.WriteByte(f.NumberOfPackets)
		b.WriteByte(f.NumberOfRepairSymbols)
	}
	b.Write(f.Data[:f.DataLength])
	return nil
}

func NewFrame(fecGroup protocol.FECBlockNumber, finBit bool, offset protocol.FecFrameOffset,
	numberOfRepairSymbols byte, data []byte) (*FECFrame, error) {
	if fecGroup >= 1<<24 {
		return nil, FECFrameTooHighFECGroup
	}
	if len(data) >= 1<<15 {
		return nil, FECFrameTooMuchData
	}
	return &FECFrame{
		FECBlockNumber:        fecGroup,
		FinBit:                finBit,
		Offset:                offset,
		DataLength:            protocol.FecFrameLength(len(data)),
		NumberOfRepairSymbols: numberOfRepairSymbols,
		Data:                  data,
	}, nil
}

// DataLen gives the length of data in bytes
func (f *FECFrame) DataLen() protocol.FecFrameLength {
	return protocol.FecFrameLength(len(f.Data))
}

func (f *FECFrame) MinLength(version protocol.VersionNumber) (protocol.ByteCount, error) {
	if !version.UsesTLS() {
		return f.lengthLegacy(version)
	}
	// TypeByte + FINBit & DataLength + FECPayloadID + Offset (+ NumberOfPackets + NumberOfRepairSymbols if offset == 0)
	var sizeNumberOfPacketsAndRepairSymbols protocol.ByteCount = 0
	if f.Offset == 0 {
		sizeNumberOfPacketsAndRepairSymbols = 2
	}
	//fecGroupLen := utils.VarIntLen(uint64(f.FECBlockNumber))
	fecPayloadIDLen := protocol.ByteCount(8)
	return 1 + 2 + fecPayloadIDLen + 1 + sizeNumberOfPacketsAndRepairSymbols /*+ protocol.ByteCount(f.DataLen())*/, nil
}

func (f *FECFrame) lengthLegacy(version protocol.VersionNumber) (protocol.ByteCount, error) {
	// TypeByte + FINBit & DataLength + FECPayloadID + Offset (+ NumberOfPackets + NumberOfRepairSymbols if offset == 0)
	var sizeNumberOfPacketsAndRepairSymbols byte = 0
	if f.Offset == 0 {
		sizeNumberOfPacketsAndRepairSymbols = 2
	}
	return protocol.ByteCount(1 + 2 + 8 + 1 + sizeNumberOfPacketsAndRepairSymbols) /*+ protocol.ByteCount(f.DataLen())*/, nil
}

func (f *FECFrame) GetFECPayloadID() protocol.FECPayloadID {
	if f.Convolutional {
		return protocol.NewConvolutionalRepairFECPayloadID(f.FECSchemeSpecific, f.EncodingSymbolID)
	} else {
		return protocol.NewBlockRepairFECPayloadID(f.FECSchemeSpecific, protocol.FECBlockNumber(f.FECBlockNumber), f.RepairSymbolNumber)
	}
}
