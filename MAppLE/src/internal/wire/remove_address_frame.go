package wire

import (
	"bytes"

	"github.com/lucas-clemente/quic-go/internal/protocol"
)

// A RemoveAddressFrame in QUIC
type RemoveAddressFrame struct {
	AddrID protocol.AddressID
}

//Write writes a RemoveAddress frame
func (f *RemoveAddressFrame) Write(b *bytes.Buffer, version protocol.VersionNumber) error {
	b.WriteByte(0x11)
	b.WriteByte(byte(f.AddrID))
	return nil
}

// MinLength of a written frame
func (f *RemoveAddressFrame) MinLength(version protocol.VersionNumber) (protocol.ByteCount, error) {
	return 1 + 1, nil
}

// ParseRemoveAddressFrame parses a REMOVE_ADDRESS frame
func ParseRemoveAddressFrame(r *bytes.Reader, version protocol.VersionNumber) (*RemoveAddressFrame, error) {
	frame := &RemoveAddressFrame{}

	// read the TypeByte
	if _, err := r.ReadByte(); err != nil {
		return nil, err
	}
	aid, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	frame.AddrID = protocol.AddressID(aid)
	return frame, nil
}
