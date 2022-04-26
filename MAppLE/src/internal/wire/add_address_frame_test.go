package wire

import (
	"bytes"
	"net"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AddAddressFrame", func() {
	Context("when parsing", func() {
		It("accepts valid frame with IPv4 and a port field", func() {
			b := bytes.NewReader([]byte{0x10, 0x14, 0x01, 0x01, 0x02, 0x03, 0x04, 0x13, 0x37})
			frame, err := ParseAddAddressFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.AddrID).To(Equal(protocol.AddressID(0x01)))
			Expect(frame.Addr.String()).To(Equal("1.2.3.4:4919")) // 0x1337 = 4919
		})

		It("accepts valid frame with IPv4 without port field", func() {
			b := bytes.NewReader([]byte{0x10, 0x04, 0x01, 0x01, 0x02, 0x03, 0x04})
			frame, err := ParseAddAddressFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.AddrID).To(Equal(protocol.AddressID(0x01)))
			Expect(frame.Addr.String()).To(Equal("1.2.3.4:0"))
		})

		It("accepts valid frame with IPv6 and a port field", func() {
			b := bytes.NewReader([]byte{0x10, 0x16, 0x01, 0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe, 0xd1, 0xde, 0xca, 0x5e, 0xb1, 0x6b, 0x00, 0xb5, 0x13, 0x37})
			frame, err := ParseAddAddressFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.AddrID).To(Equal(protocol.AddressID(0x01)))
			Expect(frame.Addr.String()).To(Equal("[dead:beef:cafe:babe:d1de:ca5e:b16b:b5]:4919")) // 0x1337 = 4919
			Expect(frame.Backup).To(BeFalse())
		})

		It("accepts valid frame with backup bit, IPv6 and a port field", func() {
			b := bytes.NewReader([]byte{0x10, 0x36, 0x01, 0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe, 0xd1, 0xde, 0xca, 0x5e, 0xb1, 0x6b, 0x00, 0xb5, 0x13, 0x37})
			frame, err := ParseAddAddressFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.AddrID).To(Equal(protocol.AddressID(0x01)))
			Expect(frame.Addr.String()).To(Equal("[dead:beef:cafe:babe:d1de:ca5e:b16b:b5]:4919")) // 0x1337 = 4919
			Expect(frame.Backup).To(BeTrue())
		})

		It("accepts valid frame with IPv6 and no port field", func() {
			b := bytes.NewReader([]byte{0x10, 0x06, 0x01, 0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe, 0xd1, 0xde, 0xca, 0x5e, 0xb1, 0x6b, 0x00, 0xb5})
			frame, err := ParseAddAddressFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.AddrID).To(Equal(protocol.AddressID(0x01)))
			Expect(frame.Addr.String()).To(Equal("[dead:beef:cafe:babe:d1de:ca5e:b16b:b5]:0")) // 0x1337 = 4919
			Expect(frame.Backup).To(BeFalse())
		})

		It("accepts valid frame with backup bit, IPv6 and no port field", func() {
			b := bytes.NewReader([]byte{0x10, 0x26, 0x01, 0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe, 0xd1, 0xde, 0xca, 0x5e, 0xb1, 0x6b, 0x00, 0xb5})
			frame, err := ParseAddAddressFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.AddrID).To(Equal(protocol.AddressID(0x01)))
			Expect(frame.Addr.String()).To(Equal("[dead:beef:cafe:babe:d1de:ca5e:b16b:b5]:0")) // 0x1337 = 4919
			Expect(frame.Backup).To(BeTrue())
		})

		It("errors on EOFs", func() {
			_, err := ParseAddAddressFrame(bytes.NewReader(nil), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})

		It("errors with missing frame payload", func() {
			_, err := ParseAddAddressFrame(bytes.NewReader([]byte{0x10}), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})

		It("errors with missing address ID", func() {
			_, err := ParseAddAddressFrame(bytes.NewReader([]byte{0x10, 0x04}), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})

		It("errors with unknown IP version", func() {
			_, err := ParseAddAddressFrame(bytes.NewReader([]byte{0x10, 0x17, 0x01, 0x01, 0x02, 0x03, 0x04, 0x13, 0x37}), protocol.VersionWhatever)
			Expect(err).To(MatchError(ErrUnknownIPVersion))
		})

		It("errors with missing IPv4 bytes", func() {
			_, err := ParseAddAddressFrame(bytes.NewReader([]byte{0x10, 0x04, 0x01, 0x01, 0x02, 0x03}), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})

		It("errors with missing IPv6 bytes", func() {
			_, err := ParseAddAddressFrame(bytes.NewReader([]byte{0x10, 0x06, 0x01, 0x01, 0x02, 0x03}), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})

		It("errors with missing port if announced", func() {
			_, err := ParseAddAddressFrame(bytes.NewReader([]byte{0x10, 0x14, 0x01, 0x01, 0x02, 0x03, 0x04}), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when writing", func() {
		It("writes a simple IPv4 frame with port", func() {
			b := &bytes.Buffer{}
			frame := AddAddressFrame{AddrID: protocol.AddressID(4), Addr: net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 0x1337}}
			frame.Write(b, protocol.VersionWhatever)
			Expect(b.Bytes()).To(Equal([]byte{0x10, 0x14, 0x04, 0x01, 0x02, 0x03, 0x04, 0x13, 0x37}))
		})

		It("writes a simple IPv4 frame without port", func() {
			b := &bytes.Buffer{}
			frame := AddAddressFrame{AddrID: protocol.AddressID(4), Addr: net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}}
			frame.Write(b, protocol.VersionWhatever)
			Expect(b.Bytes()).To(Equal([]byte{0x10, 0x04, 0x04, 0x01, 0x02, 0x03, 0x04}))
		})

		It("writes a simple IPv6 frame with port", func() {
			b := &bytes.Buffer{}
			frame := AddAddressFrame{AddrID: protocol.AddressID(4), Addr: net.UDPAddr{IP: net.ParseIP("dead:beef:cafe:babe:d1de:ca5e:b16b:00b5"), Port: 0x1337}}
			frame.Write(b, protocol.VersionWhatever)
			Expect(b.Bytes()).To(Equal([]byte{0x10, 0x16, 0x04, 0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe, 0xd1, 0xde, 0xca, 0x5e, 0xb1, 0x6b, 0x00, 0xb5, 0x13, 0x37}))
		})

		It("writes a simple IPv6 frame without port", func() {
			b := &bytes.Buffer{}
			frame := AddAddressFrame{AddrID: protocol.AddressID(4), Addr: net.UDPAddr{IP: net.ParseIP("dead:beef:cafe:babe:d1de:ca5e:b16b:00b5")}}
			frame.Write(b, protocol.VersionWhatever)
			Expect(b.Bytes()).To(Equal([]byte{0x10, 0x06, 0x04, 0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe, 0xd1, 0xde, 0xca, 0x5e, 0xb1, 0x6b, 0x00, 0xb5}))
		})

		It("has the correct min length with IPv4 and port", func() {
			frame := AddAddressFrame{Addr: net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 0x1337}}
			Expect(frame.MinLength(protocol.VersionWhatever)).To(Equal(protocol.ByteCount(9)))
		})

		It("has the correct min length with IPv4 without port", func() {
			frame := AddAddressFrame{Addr: net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 0}}
			Expect(frame.MinLength(protocol.VersionWhatever)).To(Equal(protocol.ByteCount(7)))
		})

		It("has the correct min length with IPv6 and port", func() {
			frame := AddAddressFrame{Addr: net.UDPAddr{IP: net.ParseIP("dead:beef:cafe:babe:d1de:ca5e:b16b:00b5"), Port: 0x1337}}
			Expect(frame.MinLength(protocol.VersionWhatever)).To(Equal(protocol.ByteCount(21)))
		})

		It("has the correct min length with IPv6 without port", func() {
			frame := AddAddressFrame{Addr: net.UDPAddr{IP: net.ParseIP("dead:beef:cafe:babe:d1de:ca5e:b16b:00b5"), Port: 0}}
			Expect(frame.MinLength(protocol.VersionWhatever)).To(Equal(protocol.ByteCount(19)))
		})
	})
})
