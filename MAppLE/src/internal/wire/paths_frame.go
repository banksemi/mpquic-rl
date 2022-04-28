package wire

import (
	"bytes"
	"errors"
	"sort"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

var (
	ErrPathsNumber = errors.New("PathsFrame: number of paths advertised and # of paths do not match")
)

// PathInfoSection of a PATHS frame
type PathInfoSection struct {
	AddrID protocol.AddressID
	RTT    time.Duration
}

// A PathsFrame in QUIC
type PathsFrame struct {
	ActivePaths protocol.PathID
	PathInfos   map[protocol.PathID]PathInfoSection
}

func (f *PathsFrame) Write(b *bytes.Buffer, version protocol.VersionNumber) error {
	typeByte := uint8(0x12)
	b.WriteByte(typeByte)
	b.WriteByte(uint8(f.ActivePaths))

	if int(f.ActivePaths)+1 != len(f.PathInfos) {
		return ErrPathsNumber
	}

	// Sort the keys for deterministic output
	var keys []int
	for k := range f.PathInfos {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)

	for k := range keys {
		pathID := protocol.PathID(k)
		pathInfo := f.PathInfos[pathID]
		b.WriteByte(uint8(pathID))
		b.WriteByte(uint8(pathInfo.AddrID))
		// FIXME RTT in 2 bytes instead of 4
		utils.BigEndian.WriteUfloat16(b, uint64(pathInfo.RTT/time.Microsecond))
	}

	return nil
}

// ParsePathsFrame parses a PATHS frame
func ParsePathsFrame(r *bytes.Reader, version protocol.VersionNumber) (*PathsFrame, error) {
	frame := &PathsFrame{PathInfos: make(map[protocol.PathID]PathInfoSection)}

	// read the TypeByte
	_, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	activePaths, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	frame.ActivePaths = protocol.PathID(activePaths)

	for i := 0; i <= int(frame.ActivePaths); i++ {
		pathID, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		remoteAddr, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		remoteRTT, err := utils.BigEndian.ReadUfloat16(r)
		if err != nil {
			return nil, err
		}
		frame.PathInfos[protocol.PathID(pathID)] = PathInfoSection{
			AddrID: protocol.AddressID(remoteAddr),
			RTT:    time.Duration(remoteRTT) * time.Microsecond,
		}
	}

	return frame, nil
}

// MinLength of a PATHS frame
func (f *PathsFrame) MinLength(version protocol.VersionNumber) (protocol.ByteCount, error) {
	length := 1 + 1 + (4 * (f.ActivePaths + 1))
	return protocol.ByteCount(length), nil
}
