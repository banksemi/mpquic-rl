package wire

import (
	"bytes"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("RemoveAddressFrame", func() {
	Context("when parsing", func() {
		It("accepts sample frame", func() {
			b := bytes.NewReader([]byte{0x11, 0x07})
			frame, err := ParseRemoveAddressFrame(b, protocol.VersionWhatever)
			Expect(err).ToNot(HaveOccurred())
			Expect(frame.AddrID).To(Equal(protocol.AddressID(0x07)))
		})

		It("errors on EOFs", func() {
			data := []byte{0x11}
			_, err := ParseRemoveAddressFrame(bytes.NewReader(data), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
			data = []byte{}
			_, err = ParseRemoveAddressFrame(bytes.NewReader(data), protocol.VersionWhatever)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("when writing", func() {
		It("writes a sample frame", func() {
			b := &bytes.Buffer{}
			frame := RemoveAddressFrame{AddrID: 0x07}
			frame.Write(b, protocol.VersionWhatever)
			Expect(b.Bytes()).To(Equal([]byte{0x11, 0x07}))
		})

		It("has the correct min length", func() {
			frame := RemoveAddressFrame{AddrID: 2}
			Expect(frame.MinLength(protocol.VersionWhatever)).To(Equal(protocol.ByteCount(2)))
		})
	})
})
