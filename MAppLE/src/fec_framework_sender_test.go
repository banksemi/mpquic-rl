package quic

import (
	"bytes"
	"fmt"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/mocks"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("FEC Handler", func() {
	var (
		unpacker         *packetUnpacker
		buf              *bytes.Buffer
		framework        *FECFrameworkSender
		sess             *session
		f1, f2, f3, f4   *wire.StreamFrame
		hdr1, hdr2       *wire.Header
		hdrBin1, hdrBin2 []byte
		data1, data2     []byte
		fecGroupNumber   protocol.FECBlockNumber
		packetNumber     protocol.PacketNumber
		fecGroup         *fec.FECBlock
		fecScheme        fec.FECScheme
		scheduler        *mocks.MockFECScheduler
		framer           *FECFramer
		pathID           protocol.PathID
	)

	setData := func(p []byte, hdrBin []byte, pn protocol.PacketNumber) []byte {
		d, _ := unpacker.aead.(*mockAEAD).Seal(nil, p, pn, hdrBin)
		return d
	}

	for versionMap, versionNameMap := range map[protocol.VersionNumber]string{protocol.Version39: "gQUIC"} {
		version := versionMap
		versionName := versionNameMap
		Context(fmt.Sprintf("for %s", versionName), func() {

			BeforeEach(func() {
				fecScheme = &fec.XORFECScheme{}
				scheduler = mocks.NewMockFECScheduler(mockCtrl)
				pathID = protocol.InitialPathID

				fecGroupNumber = 55
				unpacker = &packetUnpacker{aead: &mockAEAD{}}
				unpacker.aead.(*mockAEAD).encLevelOpen = protocol.EncryptionForwardSecure
				f1 = &wire.StreamFrame{
					StreamID:   7,
					Data:       []byte("foobarfoobar"), //TODO michelfra: with only "foobar", it throws "NullAEAD: ciphertext cannot be less than 12 bytes long"
					Unreliable: true,
				}
				f2 = &wire.StreamFrame{
					StreamID:   7,
					Data:       []byte("foobarfoobar2"), //TODO michelfra: with only "foobar", it throws "NullAEAD: ciphertext cannot be less than 12 bytes long"
					Unreliable: true,
				}
				f3 = &wire.StreamFrame{
					StreamID:   7,
					Data:       []byte("foobarfoobar3"), //TODO michelfra: with only "foobar", it throws "NullAEAD: ciphertext cannot be less than 12 bytes long"
					Unreliable: true,
				}
				f4 = &wire.StreamFrame{
					StreamID:   7,
					Data:       []byte("foobarfoobar3"), //TODO michelfra: with only "foobar", it throws "NullAEAD: ciphertext cannot be less than 12 bytes long"
					Unreliable: true,
				}
				sess = &session{version: version, perspective: protocol.PerspectiveClient, recoveredPackets: make(chan *receivedPacket, 10)}
				framer = newFECFramer(sess, sess.version)
				framework = NewFECFrameworkSender(fecScheme, scheduler, framer, fec.NewConstantRedundancyController(10, 1, 1, 1), version)
				fecGroup = fec.NewFECGroup(fecGroupNumber, version)
				//queue = []*wire.FECFrame{fec1, fec2}
				packetNumber = 10
				hdr1 = &wire.Header{
					PacketNumber:    packetNumber,
					PacketNumberLen: 1,
					FECPayloadID:    getFECPayloadID(fecGroupNumber, 0),
					FECFlag:         true,
					ConnectionID:    0xcafe,
				}
				hdr2 = &wire.Header{
					PacketNumber:    packetNumber + 1,
					PacketNumberLen: 1,
					FECPayloadID:    getFECPayloadID(fecGroupNumber, 1),
					FECFlag:         true,
					ConnectionID:    0xcafe,
				}
				b := &bytes.Buffer{}
				hdr1.Write(b, protocol.PerspectiveServer, version)
				hdrBin1 = b.Bytes()

				b = &bytes.Buffer{}
				hdr2.Write(b, protocol.PerspectiveServer, version)
				hdrBin2 = b.Bytes()

				buf = &bytes.Buffer{}
				err := f1.Write(buf, 0)
				Expect(err).ToNot(HaveOccurred())
				data1 = append(hdrBin1, setData(buf.Bytes(), hdrBin1, packetNumber)...)

				buf = &bytes.Buffer{}
				err = f2.Write(buf, 0)
				Expect(err).ToNot(HaveOccurred())
				data2 = append(hdrBin2, setData(buf.Bytes(), hdrBin2, packetNumber+1)...)

				buf = &bytes.Buffer{}
				err = f3.Write(buf, 0)
				Expect(err).ToNot(HaveOccurred())
				//data3 = append(hdrBin3, setData(buf.Bytes(), hdrBin3, packetNumber+2)...)

				buf = &bytes.Buffer{}
				err = f4.Write(buf, 0)
				Expect(err).ToNot(HaveOccurred())
				//data4 = append(hdrBin4, setData(buf.Bytes(), hdrBin4, packetNumber+2)...)

				fecGroup.AddPacket(data1, hdr1)
				fecGroup.AddPacket(data2, hdr2)
				fecGroup.PrepareToSend()
			})

			It("creates a new sender framework", func() {
				fw := NewFECFrameworkSender(fecScheme, scheduler, framer, fec.NewConstantRedundancyController(10, 1, 1, 1), version)
				Expect(fw).ToNot(BeNil())
				Expect(fw.fecScheme).To(Equal(fecScheme))
				Expect(fw.fecScheduler).To(Equal(scheduler))
				Expect(fw.fecFramer).To(Equal(framer))
			})

			Context("packets handling", func() {
				oldNumberOfFECPackets := protocol.NumberOfFecPackets
				BeforeEach(func() {
					protocol.NumberOfFecPackets = 2
				})

				AfterEach(func() {
					protocol.NumberOfFecPackets = oldNumberOfFECPackets
				})

				It("handles a packet correctly", func() {
					fg := fec.NewFECGroup(hdr1.FECPayloadID.GetBlockNumber(), version)
					scheduler.EXPECT().GetNextFECBlockNumber().Return(hdr1.FECPayloadID.GetBlockNumber())
					scheduler.EXPECT().GetNextFECGroupOffset().Return(hdr1.FECPayloadID.GetBlockOffset())
					scheduler.EXPECT().GetNextFECGroup().Return(fg)
					packet, err := framework.HandlePacketAndMaybePushRS(data1, hdr1)
					Expect(err).ToNot(HaveOccurred())
					Expect(packet).To(Equal(data1))
					Expect(fg.HasPacket(hdr1.PacketNumber, pathID)).To(BeTrue())
					Expect(fg.CurrentNumberOfPackets()).To(Equal(1))
				})

				It("handles a packet that will make the FEC Group to be sent (i.e. put in FECFramer)", func() {
					Expect(framer.transmissionQueue).To(HaveLen(0)) // ensure the FECFramer is empty
					fg := fec.NewFECGroup(hdr1.FECPayloadID.GetBlockNumber(), version)
					scheduler.EXPECT().GetNextFECBlockNumber().Return(hdr1.FECPayloadID.GetBlockNumber()).Times(2)
					scheduler.EXPECT().GetNextFECGroupOffset().Return(hdr1.FECPayloadID.GetBlockOffset())
					scheduler.EXPECT().GetNextFECGroup().Return(fg).Times(2)

					framework.redundancyController = fec.NewConstantRedundancyController(2, 1, 1, 1)
					packet, err := framework.HandlePacketAndMaybePushRS(data1, hdr1)
					Expect(err).ToNot(HaveOccurred())
					Expect(packet).To(Equal(data1))
					Expect(framer.transmissionQueue).To(HaveLen(0)) // ensure the FECFramer is still empty
					Expect(fg.RepairSymbols).To(BeEmpty())          // the FEC Group should still have no repair symbol

					scheduler.EXPECT().SentFECBlock(fg.FECBlockNumber) // the FEC Group should be sent after handling the packet 2
					scheduler.EXPECT().GetNextFECGroupOffset().Return(hdr2.FECPayloadID.GetBlockOffset())
					packet, err = framework.HandlePacketAndMaybePushRS(data2, hdr2)
					Expect(err).ToNot(HaveOccurred())
					Expect(packet).To(Equal(data2))
					Expect(fg.RepairSymbols).ToNot(BeEmpty())
					Expect(fg.TotalNumberOfPackets).To(Equal(2))
					Expect(framer.transmissionQueue).To(HaveLen(1)) // ensure the FECFramer is not empty anymore
					Expect(framer.transmissionQueue[0]).To(Equal(fg.RepairSymbols[0]))
				})

			})
		})
	}

})
