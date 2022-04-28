package wire

import (
	"bytes"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PathsFrame", func() {
	Context("when parsing", func() {
		It("accepts sample frame", func() {
			b := bytes.NewReader([]byte{0x12, 0x2, 0x0, 0x0, 0x13, 0x37, 0x1, 0x0, 0x13, 0x37, 0x2, 0x1, 0x8, 0x97})
			frame, err := ParsePathsFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.ActivePaths).To(Equal(protocol.PathID(0x2)))
			Expect(len(frame.PathInfos)).To(Equal(3))
			Expect(frame.PathInfos[0].AddrID).To(Equal(protocol.AddressID(0)))
			Expect(frame.PathInfos[1].AddrID).To(Equal(protocol.AddressID(0)))
			Expect(frame.PathInfos[2].AddrID).To(Equal(protocol.AddressID(1)))
		})

		It("errors on EOFs", func() {
			_, err := ParsePathsFrame(bytes.NewReader(nil), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when writing", func() {
		It("writes a sample frame", func() {
			b := &bytes.Buffer{}
			frame := PathsFrame{
				ActivePaths: protocol.PathID(0x2),
				PathInfos:   make(map[protocol.PathID]PathInfoSection),
			}
			frame.PathInfos[0] = PathInfoSection{
				AddrID: protocol.AddressID(0x0),
				RTT:    time.Duration(1) * time.Millisecond,
			}
			frame.PathInfos[1] = PathInfoSection{
				AddrID: protocol.AddressID(0x0),
				RTT:    time.Duration(1) * time.Millisecond,
			}
			frame.PathInfos[2] = PathInfoSection{
				AddrID: protocol.AddressID(0x1),
				RTT:    time.Duration(2) * time.Millisecond,
			}
			err := frame.Write(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(b.Bytes()).To(Equal([]byte{0x12, 0x2, 0x0, 0x0, 3, 232, 0x1, 0x0, 3, 232, 0x2, 0x1, 7, 208}))
		})

		It("errors if the PathsInfos are not matching ActivePaths", func() {
			b := &bytes.Buffer{}
			frame := PathsFrame{
				ActivePaths: protocol.PathID(0x2),
				PathInfos:   make(map[protocol.PathID]PathInfoSection),
			}
			frame.PathInfos[0] = PathInfoSection{
				AddrID: protocol.AddressID(0x0),
				RTT:    time.Duration(1) * time.Millisecond,
			}
			err := frame.Write(b, protocol.VersionWhatever)
			Expect(err).To(MatchError(ErrPathsNumber))
		})

		It("has the correct min length", func() {
			frame := PathsFrame{
				ActivePaths: protocol.PathID(0x2),
				PathInfos:   make(map[protocol.PathID]PathInfoSection),
			}
			frame.PathInfos[0] = PathInfoSection{
				AddrID: protocol.AddressID(0x0),
				RTT:    time.Duration(1) * time.Millisecond,
			}
			frame.PathInfos[1] = PathInfoSection{
				AddrID: protocol.AddressID(0x0),
				RTT:    time.Duration(1) * time.Millisecond,
			}
			frame.PathInfos[2] = PathInfoSection{
				AddrID: protocol.AddressID(0x1),
				RTT:    time.Duration(2) * time.Millisecond,
			}
			Expect(frame.MinLength(0)).To(Equal(protocol.ByteCount(14)))
		})
	})
})
