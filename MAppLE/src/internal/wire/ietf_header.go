package wire

import (
	"bytes"
	"fmt"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/qerr"
)

// parseHeader parses the header.
func parseHeader(b *bytes.Reader, packetSentBy protocol.Perspective) (*Header, error) {
	typeByte, err := b.ReadByte()
	if err != nil {
		return nil, err
	}
	if typeByte&0x80 > 0 {
		return parseLongHeader(b, packetSentBy, typeByte)
	}
	return parseShortHeader(b, typeByte)
}

func parseLongHeader(b *bytes.Reader, sentBy protocol.Perspective, typeByte byte) (*Header, error) {
	connID, err := utils.BigEndian.ReadUint64(b)
	if err != nil {
		return nil, err
	}
	pn, err := utils.BigEndian.ReadUint32(b)
	if err != nil {
		return nil, err
	}
	v, err := utils.BigEndian.ReadUint32(b)
	if err != nil {
		return nil, err
	}
	packetType := protocol.PacketType(typeByte & 0x7f)
	if sentBy == protocol.PerspectiveClient && (packetType != protocol.PacketTypeInitial && packetType != protocol.PacketTypeHandshake && packetType != protocol.PacketType0RTT) {
		if packetType == protocol.PacketTypeVersionNegotiation {
			return nil, qerr.Error(qerr.InvalidVersionNegotiationPacket, "sent by the client")
		}
		return nil, qerr.Error(qerr.InvalidPacketHeader, fmt.Sprintf("Received packet with invalid packet type: %d", packetType))
	}
	if sentBy == protocol.PerspectiveServer && (packetType != protocol.PacketTypeVersionNegotiation && packetType != protocol.PacketTypeRetry && packetType != protocol.PacketTypeHandshake) {
		return nil, qerr.Error(qerr.InvalidPacketHeader, fmt.Sprintf("Received packet with invalid packet type: %d", packetType))
	}
	fecFlag := typeByte&0x40 > 0
	h := &Header{
		Type:            packetType,
		IsLongHeader:    true,
		ConnectionID:    protocol.ConnectionID(connID),
		PacketNumber:    protocol.PacketNumber(pn),
		PacketNumberLen: protocol.PacketNumberLen4,
		Version:         protocol.VersionNumber(v),
		FECFlag:         fecFlag,
	}
	if h.Type == protocol.PacketTypeVersionNegotiation {
		if b.Len() == 0 {
			return nil, qerr.Error(qerr.InvalidVersionNegotiationPacket, "empty version list")
		}
		h.SupportedVersions = make([]protocol.VersionNumber, b.Len()/4)
		for i := 0; b.Len() > 0; i++ {
			v, err := utils.BigEndian.ReadUint32(b)
			if err != nil {
				return nil, qerr.InvalidVersionNegotiationPacket
			}
			h.SupportedVersions[i] = protocol.VersionNumber(v)
		}
	}

	if h.FECFlag {
		fpid, err := utils.BigEndian.ReadUint32(b)
		if err != nil {
			return nil, err
		}

		h.FECPayloadID = protocol.FECPayloadID(fpid)

		if err != nil {
			return nil, err
		}
	}

	return h, nil
}

func parseShortHeader(b *bytes.Reader, typeByte byte) (*Header, error) {
	hasConnID := typeByte&0x40 > 0
	var connID uint64
	if hasConnID {
		var err error
		connID, err = utils.BigEndian.ReadUint64(b)
		if err != nil {
			return nil, err
		}
	}
	hasPathID := typeByte&0x10 > 0
	var pathID uint8
	if hasPathID {
		var err error
		pathID, err = b.ReadByte()
		if err != nil {
			return nil, err
		}
	}
	pnLen := 1 << ((typeByte & 0x3) - 1)
	pn, err := utils.BigEndian.ReadUintN(b, uint8(pnLen))
	if err != nil {
		return nil, err
	}

	fecFlag := typeByte&0x10 > 1
	var fpid uint32
	if fecFlag {
		fpid, err = utils.BigEndian.ReadUint32(b)
		if err != nil {
			return nil, err
		}
	}

	return &Header{
		KeyPhase:         int(typeByte&0x20) >> 5,
		OmitConnectionID: !hasConnID,
		FECFlag:          fecFlag,
		ConnectionID:     protocol.ConnectionID(connID),
		PathID:           protocol.PathID(pathID),
		PacketNumber:     protocol.PacketNumber(pn),
		PacketNumberLen:  protocol.PacketNumberLen(pnLen),
		FECPayloadID:     protocol.FECPayloadID(fpid),
	}, nil
}

// writeHeader writes the Header.
func (h *Header) writeHeader(b *bytes.Buffer) error {
	if h.IsLongHeader {
		return h.writeLongHeader(b)
	}
	return h.writeShortHeader(b)
}

// TODO: add support for the key phase
func (h *Header) writeLongHeader(b *bytes.Buffer) error {
	var F byte = 0
	if h.FECFlag {
		F = 1 << 6
		b.WriteByte(byte(0x80|h.Type) | F)
	} else {
		F = 0xFF ^ (1 << 6) // flip the bits of F
		b.WriteByte(byte(0x80|h.Type) & F)
	}
	utils.BigEndian.WriteUint64(b, uint64(h.ConnectionID))
	utils.BigEndian.WriteUint32(b, uint32(h.PacketNumber))
	utils.BigEndian.WriteUint32(b, uint32(h.Version))
	if h.FECFlag {
		utils.BigEndian.WriteUint32(b, uint32(h.FECPayloadID))
	}
	return nil
}

func (h *Header) writeShortHeader(b *bytes.Buffer) error {
	typeByte := byte(h.KeyPhase << 5)
	if h.FECFlag {
		typeByte ^= 1 << 4
	}
	if !h.OmitConnectionID {
		typeByte ^= 0x40
	}
	if h.PathID != protocol.InitialPathID {
		typeByte ^= 0x10
	}
	switch h.PacketNumberLen {
	case protocol.PacketNumberLen1:
		typeByte ^= 0x1
	case protocol.PacketNumberLen2:
		typeByte ^= 0x2
	case protocol.PacketNumberLen4:
		typeByte ^= 0x3
	default:
		return fmt.Errorf("invalid packet number length: %d", h.PacketNumberLen)
	}
	b.WriteByte(typeByte)

	if !h.OmitConnectionID {
		utils.BigEndian.WriteUint64(b, uint64(h.ConnectionID))
	}
	if h.PathID != protocol.InitialPathID {
		b.WriteByte(uint8(h.PathID))
	}
	switch h.PacketNumberLen {
	case protocol.PacketNumberLen1:
		b.WriteByte(uint8(h.PacketNumber))
	case protocol.PacketNumberLen2:
		utils.BigEndian.WriteUint16(b, uint16(h.PacketNumber))
	case protocol.PacketNumberLen4:
		utils.BigEndian.WriteUint32(b, uint32(h.PacketNumber))
	}

	// write FEC Payload ID if needed
	if h.FECFlag {
		utils.BigEndian.WriteUint32(b, uint32(h.FECPayloadID))
	}

	return nil
}

// getHeaderLength gets the length of the Header in bytes.
func (h *Header) getHeaderLength() (protocol.ByteCount, error) {
	if h.IsLongHeader {
		var fecDataLength protocol.ByteCount = 0
		if h.FECFlag {
			fecDataLength = 4 // FECPayloadID (4 bytes)
		}
		return 1 + 8 + 4 + 4 + fecDataLength, nil
	}

	length := protocol.ByteCount(1) // type byte
	if !h.OmitConnectionID {
		length += 8
	}
	if h.PathID != protocol.InitialPathID {
		length++
	}
	if h.PacketNumberLen != protocol.PacketNumberLen1 && h.PacketNumberLen != protocol.PacketNumberLen2 && h.PacketNumberLen != protocol.PacketNumberLen4 {
		return 0, fmt.Errorf("invalid packet number length: %d", h.PacketNumberLen)
	}
	length += protocol.ByteCount(h.PacketNumberLen)
	if h.FECFlag {
		length += 4 // fpid length
	}
	return length, nil
}

func (h *Header) logHeader() {
	if h.IsLongHeader {
		utils.Debugf("   Long Header{Type: %s, ConnectionID: %#x, PacketNumber: %#x, Version: %s}", h.Type, h.ConnectionID, h.PacketNumber, h.Version)
	} else {
		connID := "(omitted)"
		if !h.OmitConnectionID {
			connID = fmt.Sprintf("%#x", h.ConnectionID)
		}
		utils.Debugf("   Short Header{ConnectionID: %s, PathID: %#x, PacketNumber: %#x, PacketNumberLen: %d, KeyPhase: %d}", connID, h.PathID, h.PacketNumber, h.PacketNumberLen, h.KeyPhase)
	}
}
