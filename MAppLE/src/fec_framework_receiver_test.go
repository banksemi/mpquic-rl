package quic

import (
	"bytes"
	"fmt"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"net"
)

func getFECPayloadID(blockNumber protocol.FECBlockNumber, offset uint8) protocol.FECPayloadID {
	return (protocol.FECPayloadID(blockNumber) << 8) + protocol.FECPayloadID(offset)
}

var _ = Describe("FEC Handler", func() {
	var (
		unpacker                           *packetUnpacker
		buf                                *bytes.Buffer
		framework                          *FECFrameworkReceiver
		sess                               *session
		f1, f2, f3, f4                     *wire.StreamFrame
		fec1, fec2, fec3                   *wire.FECFrame
		hdr1, hdr2, hdr3, hdr4             *wire.Header
		hdrBin1, hdrBin2, hdrBin3, hdrBin4 []byte
		data1, data2, data3, data4         []byte
		fecGroupNumber                     protocol.FECBlockNumber
		packetNumber                       protocol.PacketNumber
		fecGroup                           *fec.FECBlock
		fecScheme                          fec.BlockFECScheme
		pathID                             protocol.PathID
	)

	setData := func(p []byte, hdrBin []byte, pn protocol.PacketNumber) []byte {
		d, _ := unpacker.aead.(*mockAEAD).Seal(nil, p, pn, hdrBin)
		return d
	}
	for versionMap, versionNameMap := range map[protocol.VersionNumber]string{ /*versionIETFFrames: "IETF QUIC",*/ protocol.Version39: "gQUIC"} {
		version := versionMap
		versionName := versionNameMap
		Context(fmt.Sprintf("for %s", versionName), func() {

			BeforeEach(func() {
				pathID = protocol.InitialPathID
				fecScheme = &fec.XORFECScheme{}
				fecGroupNumber = 55
				unpacker = &packetUnpacker{aead: &mockAEAD{}}
				unpacker.aead.(*mockAEAD).encLevelOpen = protocol.EncryptionForwardSecure
				fec1 = &wire.FECFrame{
					FECBlockNumber:        fecGroupNumber,
					Offset:                0,
					NumberOfRepairSymbols: 1,
					NumberOfPackets:       1,
					Data:                  []byte("foobar1"),
					DataLength:            7,
				}
				fec2 = &wire.FECFrame{
					FECBlockNumber: fecGroupNumber,
					Offset:         1,
					FinBit:         true,
					Data:           []byte("foobar1.2"),
					DataLength:     7,
				}
				fec3 = &wire.FECFrame{
					FECBlockNumber: fecGroupNumber + 1,
					Offset:         4,
					Data:           []byte("foobar2"),
					DataLength:     7,
				}
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
				Eventually(areSessionsRunning).Should(BeFalse())
				addr := &net.UDPAddr{IP: net.IPv4(192, 168, 100, 200), Port: 1337}
				packetConn := &mockPacketConn{
					addr:         &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
					dataReadFrom: addr,
				}
				pconnMgr := &pconnManager{}
				pconnMgr.setup(packetConn, addr, newMockNetWatcher)

				sess = &session{version: version, perspective: protocol.PerspectiveClient, recoveredPackets: make(chan *receivedPacket, 10), paths: make(map[protocol.PathID]*path)}
				c := &conn{
					currentAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 1234},
					pconn:       packetConn,
				}
				sess.paths[0] = &path{conn: c}
				framework = NewFECFrameworkReceiver(sess, fecScheme)
				fecGroup = fec.NewFECGroup(fecGroupNumber, version)
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
				hdr3 = &wire.Header{
					PacketNumber:    packetNumber + 2,
					PacketNumberLen: 1,
					FECPayloadID:    getFECPayloadID(fecGroupNumber+1, 0),
					FECFlag:         true,
					ConnectionID:    0xcafe,
				}
				hdr4 = &wire.Header{
					PacketNumber:    packetNumber + 3,
					PacketNumberLen: 1,
					FECPayloadID:    getFECPayloadID(fecGroupNumber+2, 0),
					FECFlag:         true,
					ConnectionID:    0xcafe,
				}
				b := &bytes.Buffer{}
				hdr1.Write(b, protocol.PerspectiveServer, version)
				hdrBin1 = b.Bytes()

				b = &bytes.Buffer{}
				hdr2.Write(b, protocol.PerspectiveServer, version)
				hdrBin2 = b.Bytes()

				b = &bytes.Buffer{}
				hdr3.Write(b, protocol.PerspectiveServer, version)
				hdrBin3 = b.Bytes()

				b = &bytes.Buffer{}
				hdr4.Write(b, protocol.PerspectiveServer, version)
				hdrBin4 = b.Bytes()

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
				data3 = append(hdrBin3, setData(buf.Bytes(), hdrBin3, packetNumber+2)...)

				buf = &bytes.Buffer{}
				err = f4.Write(buf, 0)
				Expect(err).ToNot(HaveOccurred())
				data4 = append(hdrBin4, setData(buf.Bytes(), hdrBin4, packetNumber+2)...)

				fecGroup.AddPacket(data1, hdr1)
				fecGroup.AddPacket(data2, hdr2)
				fecGroup.PrepareToSend()
			})

			It("creates a new receiver framework", func() {
				fw := NewFECFrameworkReceiver(sess, &fec.XORFECScheme{})
				Expect(fw).ToNot(BeNil())
				Expect(fw.fecScheme).To(Equal(&fec.XORFECScheme{}))
				Expect(fw.session).To(Equal(sess))
			})

			It("handles a FEC-protected packet", func() {
				_, ok := framework.fecGroupsBuffer.fecGroups[fecGroupNumber]
				Expect(ok).To(BeFalse())
				framework.handlePacket(data1, hdr1)
				group, ok := framework.fecGroupsBuffer.fecGroups[fecGroupNumber]
				Expect(ok).To(BeTrue())
				Expect(group.CurrentNumberOfPackets()).To(Equal(1))
				Expect(group.HasPacket(hdr1.PacketNumber, pathID)).To(BeTrue())
			})

			It("handles multiple FEC-protected packets of the same FEC group", func() {
				framework.handlePacket(data1, hdr1)
				framework.handlePacket(data2, hdr2)
				group, ok := framework.fecGroupsBuffer.fecGroups[fecGroupNumber]
				Expect(ok).To(BeTrue())
				Expect(group.CurrentNumberOfPackets()).To(Equal(2))
				Expect(group.HasPacket(packetNumber, pathID)).To(BeTrue())
				Expect(group.HasPacket(packetNumber+1, pathID)).To(BeTrue())
			})

			Context("Handling FEC frames", func() {
				var (
					frame0Symbol0 *wire.FECFrame
					frame1Symbol0 *wire.FECFrame
					frame0Symbol1 *wire.FECFrame
					frame1Symbol1 *wire.FECFrame
					frame0Symbol2 *wire.FECFrame
					frame1Symbol2 *wire.FECFrame
					frame2Symbol2 *wire.FECFrame
				)
				BeforeEach(func() {
					frame0Symbol0 = &wire.FECFrame{
						FECBlockNumber:        fecGroupNumber,
						Offset:                0,
						RepairSymbolNumber:    0,
						NumberOfPackets:       2,
						NumberOfRepairSymbols: 3,
						Data:                  []byte("frame0Symbol0"),
					}

					frame1Symbol0 = &wire.FECFrame{
						FECBlockNumber:     fecGroupNumber,
						Offset:             1,
						RepairSymbolNumber: 0,
						Data:               []byte("frame0Symbol1"),
						FinBit:             true,
					}

					frame0Symbol1 = &wire.FECFrame{
						FECBlockNumber:     fecGroupNumber,
						Offset:             0,
						RepairSymbolNumber: 1,
						Data:               []byte("frame0Symbol1"),
					}

					frame1Symbol1 = &wire.FECFrame{
						FECBlockNumber:     fecGroupNumber,
						Offset:             1,
						RepairSymbolNumber: 1,
						Data:               []byte("frame1Symbol1"),
						FinBit:             true,
					}

					frame0Symbol2 = &wire.FECFrame{
						FECBlockNumber:     fecGroupNumber,
						Offset:             0,
						RepairSymbolNumber: 2,
						Data:               []byte("frame0Symbol2"),
					}

					frame1Symbol2 = &wire.FECFrame{
						FECBlockNumber:     fecGroupNumber,
						Offset:             1,
						RepairSymbolNumber: 2,
						Data:               []byte("frame1Symbol2"),
					}

					frame2Symbol2 = &wire.FECFrame{
						FECBlockNumber:     fecGroupNumber,
						Offset:             2,
						RepairSymbolNumber: 2,
						Data:               []byte("frame2Symbol2"),
						FinBit:             true,
					}
				})

				It("Handles one FEC frame", func() {
					framework.handleFECFrame(fec1)
					Expect(framework.waitingFECFrames[fecGroupNumber]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber][0][0]).To(Equal(fec1))
				})

				It("Handles one FEC frame that contains a whole Repair Symbol (Offset 0 and FinBit set)", func() {
					fec1.FinBit = true       // the frame contains a whole symbol
					fec1.NumberOfPackets = 2 // ensure nothing will be recovered after handling the symbol
					framework.handleFECFrame(fec1)
					Expect(framework.waitingFECFrames[fecGroupNumber]).To(HaveLen(0))
					fg := framework.fecGroupsBuffer.fecGroups[fecGroupNumber]
					Expect(fg).ToNot(BeNil())
					Expect(fg.RepairSymbols).ToNot(BeEmpty())
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[0].Data).To(Equal(fec1.Data))
				})

				It("Handles two FEC frames that contains a part of Repair Symbol of a different FEC Group", func() {
					framework.handleFECFrame(fec2)
					framework.handleFECFrame(fec3)
					Expect(framework.waitingFECFrames[fecGroupNumber][0]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber+1][0]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber][0][fec2.Offset]).To(Equal(fec2))
					Expect(framework.waitingFECFrames[fecGroupNumber+1][0][fec3.Offset]).To(Equal(fec3))
				})

				It("Handles two FEC frame that, when put together, contain a whole Repair Symbol", func() {
					framework.handleFECFrame(frame0Symbol0)
					Expect(framework.waitingFECFrames[fecGroupNumber]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol0.RepairSymbolNumber][frame0Symbol0.Offset]).To(Equal(frame0Symbol0))
					framework.handleFECFrame(frame1Symbol0)
					Expect(framework.waitingFECFrames[fecGroupNumber][frame1Symbol0.RepairSymbolNumber]).To(HaveLen(0))
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[frame0Symbol0.RepairSymbolNumber].Data).To(Equal(append(frame0Symbol0.Data, frame1Symbol0.Data...)))
				})

				It("Handles two FEC frames that, when put together, contain only a part of Repair Symbol (the last frame for the symbol has not been received)", func() {
					framework.handleFECFrame(frame0Symbol2)
					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol2.RepairSymbolNumber]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol2.RepairSymbolNumber][frame0Symbol2.Offset]).To(Equal(frame0Symbol2))
					framework.handleFECFrame(frame1Symbol2)
					Expect(framework.waitingFECFrames[fecGroupNumber][frame1Symbol2.RepairSymbolNumber]).To(HaveLen(2))
					Expect(framework.waitingFECFrames[fecGroupNumber][frame1Symbol2.RepairSymbolNumber][frame0Symbol2.Offset]).To(Equal(frame0Symbol2))
					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol2.RepairSymbolNumber][frame1Symbol2.Offset]).To(Equal(frame1Symbol2))
				})

				It("Handles two FEC frames that, when put together, contain only a part of Repair Symbol (there is a missing frame between these two)", func() {
					framework.handleFECFrame(frame0Symbol2)
					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol2.RepairSymbolNumber]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol2.RepairSymbolNumber][frame0Symbol2.Offset]).To(Equal(frame0Symbol2))
					framework.handleFECFrame(frame2Symbol2)
					Expect(framework.waitingFECFrames[fecGroupNumber][frame2Symbol2.RepairSymbolNumber]).To(HaveLen(2))
					Expect(framework.waitingFECFrames[fecGroupNumber][frame2Symbol2.RepairSymbolNumber][frame0Symbol2.Offset]).To(Equal(frame0Symbol2))
					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol2.RepairSymbolNumber][frame2Symbol2.Offset]).To(Equal(frame2Symbol2))
				})

				It("Handles two duplicated FEC frames", func() {
					framework.handleFECFrame(fec1)
					Expect(framework.waitingFECFrames[fecGroupNumber]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber][0][fec1.Offset]).To(Equal(fec1))
					framework.handleFECFrame(fec1)
					Expect(framework.waitingFECFrames[fecGroupNumber]).To(HaveLen(1))
					Expect(framework.waitingFECFrames[fecGroupNumber][0][fec1.Offset]).To(Equal(fec1))
				})

				It("Handles multiple FEC Frames for multiple symbols of the same FEC Group", func() {
					framework.handleFECFrame(frame0Symbol2)
					framework.handleFECFrame(frame2Symbol2)
					framework.handleFECFrame(frame1Symbol2)

					framework.handleFECFrame(frame0Symbol0)
					framework.handleFECFrame(frame1Symbol0)

					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol2.RepairSymbolNumber]).To(HaveLen(0))
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[0].SymbolNumber).To(Equal(byte(2)))
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[0].Data).To(Equal(append(frame0Symbol2.Data, append(frame1Symbol2.Data, frame2Symbol2.Data...)...)))

					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol0.RepairSymbolNumber]).To(HaveLen(0))
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[1].SymbolNumber).To(Equal(byte(0)))
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[1].Data).To(Equal(append(frame0Symbol0.Data, frame1Symbol0.Data...)))

					framework.handleFECFrame(frame0Symbol1)
					framework.handleFECFrame(frame1Symbol1)

					Expect(framework.waitingFECFrames[fecGroupNumber][frame0Symbol1.RepairSymbolNumber]).To(HaveLen(0))
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[2].SymbolNumber).To(Equal(byte(1)))
					Expect(framework.fecGroupsBuffer.fecGroups[fecGroupNumber].RepairSymbols[2].Data).To(Equal(append(frame0Symbol1.Data, frame1Symbol1.Data...)))

				})

			})
			It("handles multiple FEC-protected packets of different FEC groups", func() {
				framework.handlePacket(data1, hdr1)
				framework.handlePacket(data3, hdr3)
				group, ok := framework.fecGroupsBuffer.fecGroups[fecGroupNumber]
				Expect(ok).To(BeTrue())
				Expect(group.CurrentNumberOfPackets()).To(Equal(1))
				Expect(group.HasPacket(packetNumber, pathID)).To(BeTrue())

				group, ok = framework.fecGroupsBuffer.fecGroups[fecGroupNumber+1]
				Expect(ok).To(BeTrue())
				Expect(group.CurrentNumberOfPackets()).To(Equal(1))
				Expect(group.HasPacket(packetNumber+2, pathID)).To(BeTrue())
			})

			It("Recovers a packet correctly after handling a packet that completes the FEC group", func() {
				symbols, err := fecScheme.GetRepairSymbols(fecGroup, 1, fecGroup.FECBlockNumber)
				symbols[0].FECBlockNumber = fecGroup.FECBlockNumber
				Expect(err).ToNot(HaveOccurred())
				framework.handleRepairSymbol(symbols[0], fecGroup.TotalNumberOfPackets, 1)
				framework.handlePacket(data1, hdr1)
				select {
				case packet := <-framework.recoveredPackets:
					packet.header.Raw = nil
					Expect(packet.header).To(Equal(hdr2))
					Expect(packet.data).To(Equal(data2[len(hdrBin2):]))
				default:
					Fail("no packet has been recovered")
				}
			})

			It("Recovers a packet correctly after handling a FEC Frame containing the full payload with a FEC Group of 1 packet", func() {
				fecGroup = fec.NewFECGroup(fecGroupNumber, version)
				fecGroup.AddPacket(data1, hdr1)
				fecGroup.PrepareToSend()
				symbols, err := fecScheme.GetRepairSymbols(fecGroup, 1, fecGroup.FECBlockNumber)
				Expect(err).ToNot(HaveOccurred())
				frame := &wire.FECFrame{
					FECBlockNumber:  fecGroupNumber,
					Offset:          0,
					FinBit:          true,
					Data:            symbols[0].Data,
					DataLength:      protocol.FecFrameLength(len(symbols[0].Data)),
					NumberOfPackets: 1,
				}
				framework.handleFECFrame(frame)
				select {
				case packet := <-framework.recoveredPackets:
					packet.header.Raw = nil
					Expect(packet.header).To(Equal(hdr1))
					Expect(packet.data).To(Equal(data1[len(hdrBin1):]))
				default:
					Fail("no packet has been recovered")
				}
			})

			It("Recovers a packet correctly after handling a packet that completes the FEC group with a FEC Group of size > 2", func() {
				hdr3.FECFlag = true
				hdr3.FECPayloadID = getFECPayloadID(fecGroupNumber, 2)
				fecGroup.AddPacket(data3, hdr3)
				err := fecGroup.PrepareToSend()
				Expect(err).ToNot(HaveOccurred())
				symbols, err := fecScheme.GetRepairSymbols(fecGroup, 1, fecGroup.FECBlockNumber)
				symbols[0].FECBlockNumber = fecGroup.FECBlockNumber
				Expect(err).ToNot(HaveOccurred())
				framework.handleRepairSymbol(symbols[0], fecGroup.TotalNumberOfPackets, 1)
				framework.handlePacket(data1, hdr1)
				framework.handlePacket(data3, hdr3)
				select {
				case packet := <-framework.recoveredPackets:
					packet.header.Raw = nil
					Expect(packet.header).To(Equal(hdr2))
					Expect(packet.data).To(Equal(data2[len(hdrBin2):]))
				default:
					Fail("no packet has been recovered")
				}
			})

			It("Recovers a packet correctly after handling the missing RepairSymbol for a recoverable FEC Group", func() {
				framework.handlePacket(data1, hdr1)
				symbols, err := fecScheme.GetRepairSymbols(fecGroup, 1, fecGroup.FECBlockNumber)
				Expect(err).ToNot(HaveOccurred())
				symbols[0].FECBlockNumber = fecGroup.FECBlockNumber
				framework.handleRepairSymbol(symbols[0], fecGroup.TotalNumberOfPackets, 1)
				select {
				case packet := <-framework.recoveredPackets:
					packet.header.Raw = nil
					Expect(packet.header).To(Equal(hdr2))
					Expect(packet.data).To(Equal(data2[len(hdrBin2):]))
				default:
					Fail("no packet has been recovered")
				}
			})

			It("Recovers no packet after handling the missing RepairSymbol for complete FEC Group (no packet needs to be recovered)", func() {
				framework.handlePacket(data1, hdr1)
				framework.handlePacket(data2, hdr2)
				framework.handleRepairSymbol(&fec.RepairSymbol{FECBlockNumber: fecGroupNumber, Data: []byte("randomRepairSymbol")}, fecGroup.TotalNumberOfPackets, 1)
				select {
				case <-framework.recoveredPackets:
					Fail("a packet has been recovered")
				default:

				}
			})

			It("pushes many FEC Groups in the framework and checks that it is handled correctly", func() {
				framework.fecGroupsBuffer.maxSize = 2
				framework.handlePacket(data1, hdr1)
				framework.handlePacket(data3, hdr3)
				framework.handlePacket(data4, hdr4)

				fecGroups := framework.fecGroupsBuffer.fecGroups

				Expect(fecGroups).To(HaveLen(2))

				g, ok := fecGroups[fecGroupNumber]
				Expect(ok).To(BeFalse())

				g, ok = fecGroups[fecGroupNumber+1]
				Expect(ok).To(BeTrue())
				Expect(g.CurrentNumberOfPackets()).To(Equal(1))
				Expect(g.HasPacket(hdr3.PacketNumber, pathID)).To(BeTrue())

				g, ok = fecGroups[fecGroupNumber+2]
				Expect(ok).To(BeTrue())
				Expect(g.CurrentNumberOfPackets()).To(Equal(1))
				Expect(g.HasPacket(hdr4.PacketNumber, pathID)).To(BeTrue())
			})

		})

	}

})
