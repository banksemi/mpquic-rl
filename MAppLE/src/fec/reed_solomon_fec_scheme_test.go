package fec

import (
	"bytes"
	"github.com/klauspost/reedsolomon"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Reed Solomon FEC Scheme", func() {
	var (
		packet1, packet2, packet3                               []byte
		normalizedPacket1, normalizedPacket2, normalizedPacket3 []byte // packet1, 2 and 3 with the same size (padded with 0)
		fecGroup                                                *FECBlock
		fecGroupToRecover                                       *FECBlock
		fecScheme                                               BlockFECScheme
	)

	for _, versionLoop := range []protocol.VersionNumber{versionGQUIC, versionIETFQUIC} {
		version := versionLoop
		Context("Reed Solomon FEC Scheme", func() {
			BeforeEach(func() {
				packet1 = []byte{0xDE, 0xAD, 0xBE, 0xEF}
				packet2 = []byte{0xCA, 0xFE}
				packet3 = []byte{0x01, 0x23, 0x45, 0x67, 0x89}
				normalizedPacket1 = append(packet1, 0)
				normalizedPacket2 = append(packet2, bytes.Repeat([]byte{0}, 3)...)
				normalizedPacket3 = packet3
				packets := [][]byte{normalizedPacket1, normalizedPacket2, normalizedPacket3, make([]byte, 5), make([]byte, 5)}[:]
				enc, _ := reedsolomon.New(3, 2)
				enc.Encode(packets) // will place the repair symbols in indices 3 and 4 of packets

				repairSymbols := []*RepairSymbol{
					{
						FECBlockNumber: 42,
						Data:           packets[3],
						SymbolNumber:   0,
					},
					{
						FECBlockNumber: 42,
						Data:           packets[4],
						SymbolNumber:   1,
					},
					{}}
				fecGroup = &FECBlock{
					FECBlockNumber:       42,
					packets:              [][]byte{packet1, packet2, packet3},
					packetIndexes:        map[protocol.PathID]map[protocol.PacketNumber]int{0: {1: 0, 2: 1, 3: 2}},
					TotalNumberOfPackets: 3,
				}
				fecGroup.PrepareToSend()
				fecScheme, _ = NewReedSolomonFECScheme()
				fecGroupToRecover = NewFECGroup(42, version)
				fecGroupToRecover.AddPacket(packet3, &wire.Header{PacketNumber: 3, FECPayloadID: 2})
				fecGroupToRecover.AddRepairSymbol(repairSymbols[0])
				fecGroupToRecover.AddRepairSymbol(repairSymbols[1])
				fecGroupToRecover.TotalNumberOfPackets = 3
				fecGroupToRecover.TotalNumberOfRepairSymbols = 2
				fecGroupToRecover.TotalNumberOfPackets = fecGroup.TotalNumberOfPackets
			})

			It("successfully creates a ReedSolomonFECScheme", func() {
				scheme, err := NewReedSolomonFECScheme()
				Expect(err).ToNot(HaveOccurred())
				Expect(scheme).To(Equal(&ReedSolomonFECScheme{
					schemes:        make(map[[2]uint]reedsolomon.Encoder),
					performCaching: true}))
			})

			It("states that it cannot generate repair symbols with 0 packets / repair symbols", func() {
				fecGroup := NewFECGroup(42, version)
				symbols, err := fecScheme.GetRepairSymbols(fecGroup, 4, fecGroup.FECBlockNumber)
				Expect(err).To(HaveOccurred())
				Expect(symbols).To(BeNil())
			})

			It("states that it cannot recover packets when there are no repair symbols", func() {
				fecGroup := NewFECGroup(42, version)
				packets, err := fecScheme.RecoverPackets(fecGroup)
				Expect(err).To(HaveOccurred())
				Expect(packets).To(BeNil())
			})

			It("states that it cannot recover packets when the total number of packets is zero", func() {
				fecGroup := NewFECGroup(42, version)
				fecGroup.AddRepairSymbol(&RepairSymbol{})
				fecGroup.TotalNumberOfRepairSymbols = 1
				packets, err := fecScheme.RecoverPackets(fecGroup)
				Expect(err).To(HaveOccurred())
				Expect(packets).To(BeNil())
			})

			It("states that it cannot recover packets when there are not enough received data", func() {
				fecGroup := NewFECGroup(42, version)
				fecGroup.AddRepairSymbol(&RepairSymbol{Data: []byte{0, 0, 0, 0, 0}})
				fecGroup.TotalNumberOfRepairSymbols = 2
				fecGroup.TotalNumberOfPackets = 3
				fecGroup.AddPacket([]byte{0, 0, 0, 0, 0}, &wire.Header{})
				packets, err := fecScheme.RecoverPackets(fecGroup)
				Expect(err).To(HaveOccurred())
				Expect(packets).To(BeNil())
			})

			It("successfully generates Repair Symbols", func() {
				packets := [][]byte{normalizedPacket1, normalizedPacket2, normalizedPacket3, make([]byte, 5), make([]byte, 5)}[:]
				enc, err := reedsolomon.New(3, 2)
				err = enc.Encode(packets) // will place the repair symbols in indices 3 and 4 of packets
				Expect(err).ToNot(HaveOccurred())
				symbols, err := fecScheme.GetRepairSymbols(fecGroup, 2, fecGroup.FECBlockNumber)
				Expect(err).ToNot(HaveOccurred())
				Expect(symbols).To(HaveLen(2))
				Expect(symbols[0].Data).To(Equal(packets[3]))
				Expect(symbols[1].Data).To(Equal(packets[4]))
			})

			It("successfully repairs lost packets", func() {
				recoveredPackets, err := fecScheme.RecoverPackets(fecGroupToRecover)
				Expect(err).ToNot(HaveOccurred())
				Expect(recoveredPackets).To(HaveLen(2))
				Expect(recoveredPackets[0]).To(Equal(normalizedPacket1))
				Expect(recoveredPackets[1]).To(Equal(normalizedPacket2))
			})

			It("states that it cannot recover packets when there is no packet to recover", func() {
				fecGroup := NewFECGroup(42, version)
				fecGroup.AddRepairSymbol(&RepairSymbol{})
				fecGroup.TotalNumberOfRepairSymbols = 1
				Expect(fecScheme.CanRecoverPackets(fecGroup)).To(BeFalse())
			})

			It("states that it cannot recover packets when not enough packets have been received", func() {
				fecGroup := NewFECGroup(42, version)
				fecGroup.AddRepairSymbol(&RepairSymbol{SymbolNumber: 0})
				fecGroup.AddPacket([]byte{}, &wire.Header{})
				fecGroup.TotalNumberOfRepairSymbols = 2
				fecGroup.TotalNumberOfPackets = 3
				Expect(fecScheme.CanRecoverPackets(fecGroup)).To(BeFalse())
			})

			It("states that it can recover packets when enough packets have been received", func() {
				Expect(fecScheme.CanRecoverPackets(fecGroupToRecover)).To(BeTrue())
				fecGroupToRecover.AddPacket(packet2, &wire.Header{PacketNumber: 2, FECPayloadID: 1})
				Expect(fecScheme.CanRecoverPackets(fecGroupToRecover)).To(BeTrue())
			})
		})
	}

})
