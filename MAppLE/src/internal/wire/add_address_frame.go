package wire

import (
	"bytes"
	"errors"
	"net"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

var (
	// ErrUnknownIPVersion means the IP version field has not valid value
	ErrUnknownIPVersion = errors.New("AddAddressFrame: unknown IP version")
)

var (
	errInconsistentAddrIPVersion = errors.New("internal inconsistency: Addr does not match IP version")
)

// A AddAddressFrame in QUIC
type AddAddressFrame struct {
	AddrID protocol.AddressID
	Addr   net.UDPAddr
	Backup bool
}

func (f *AddAddressFrame) Write(b *bytes.Buffer, version protocol.VersionNumber) error {
	typeByte := uint8(0x10)
	b.WriteByte(typeByte)

	ipVers := utils.GetIPVersion(f.Addr.IP)

	flags := uint8(ipVers)
	if f.Addr.Port != 0 {
		flags |= 0x10
	}
	if f.Backup {
		flags |= 0x20
	}
	b.WriteByte(flags)

	b.WriteByte(byte(f.AddrID))

	switch ipVers {
	case 4:
		ip := f.Addr.IP.To4()
		if ip == nil {
			return errInconsistentAddrIPVersion
		}
		for i := 0; i < 4; i++ {
			b.WriteByte(ip[i])
		}
	case 6:
		ip := f.Addr.IP.To16()
		if ip == nil {
			return errInconsistentAddrIPVersion
		}
		for i := 0; i < 16; i++ {
			b.WriteByte(ip[i])
		}
	default:
		return ErrUnknownIPVersion
	}

	if f.Addr.Port != 0 {
		utils.BigEndian.WriteUint16(b, uint16(f.Addr.Port))
	}

	return nil
}

// ParseAddAddressFrame parses an ADD_ADDRESS frame
func ParseAddAddressFrame(r *bytes.Reader, version protocol.VersionNumber) (*AddAddressFrame, error) {
	frame := &AddAddressFrame{}

	// read the TypeByte
	_, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	b, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	frame.Backup = b&0x20 > 0
	hasPort := b&0x10 > 0
	ipVers := b & 0x0f

	addrID, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	frame.AddrID = protocol.AddressID(addrID)

	switch ipVers {
	case 4:
		var bs []byte
		for i := 0; i < 4; i++ {
			b, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			bs = append(bs, b)
		}
		frame.Addr.IP = net.IPv4(bs[0], bs[1], bs[2], bs[3])
	case 6:
		ip := make([]byte, 16)
		for i := 0; i < net.IPv6len; i++ {
			b, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			ip[i] = b
		}
		frame.Addr.IP = net.IP(ip)

	default:
		return nil, ErrUnknownIPVersion
	}

	if hasPort {
		port, err := utils.BigEndian.ReadUint16(r)
		if err != nil {
			return nil, err
		}

		frame.Addr.Port = int(port)
	}

	return frame, nil
}

// MinLength of the written frame
func (f *AddAddressFrame) MinLength(version protocol.VersionNumber) (protocol.ByteCount, error) {
	var portCount protocol.ByteCount
	if f.Addr.Port != 0 {
		portCount += 2
	}
	switch utils.GetIPVersion(f.Addr.IP) {
	case 4:
		return 1 + 1 + 1 + 4 + portCount, nil
	case 6:
		return 1 + 1 + 1 + 16 + portCount, nil
	default:
		return 0, ErrUnknownIPVersion
	}
}
