package fec

import (
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func XOR(a []byte, b []byte) []byte {
	var retVal []byte
	if len(a) >= len(b) {
		retVal = make([]byte, protocol.MaxReceivePacketSize)[:len(a)]
	} else {
		retVal = make([]byte, protocol.MaxReceivePacketSize)[:len(b)]
	}
	for i := 0; i < len(retVal); i++ {
		if i >= len(a) {
			retVal[i] = b[i]
		} else if i >= len(b) {
			retVal[i] = a[i]
		} else {
			retVal[i] = a[i] ^ b[i]
		}
	}
	return retVal
}

var _ = Describe("FEC Scheme", func() {
	var (
		packet1, packet2, packet3 []byte
		xoredpackets              []byte
		fecGroup                  *FECBlock
		fecGroupToRecover         *FECBlock
		repairSymbol              *RepairSymbol
		fecScheme                 BlockFECScheme
	)

	for _, versionLoop := range []protocol.VersionNumber{versionGQUIC, versionIETFQUIC} {
		version := versionLoop
		Context("XOR FEC Scheme", func() {
			BeforeEach(func() {
				packet1 = []byte{0xDE, 0xAD, 0xBE, 0xEF}
				packet2 = []byte{0xCA, 0xFE}
				packet3 = []byte{0x01, 0x23, 0x45, 0x67, 0x89}
				xoredpackets = []byte{
					0xDE ^ 0xCA ^ 0x01,
					0xAD ^ 0xFE ^ 0x23,
					0xBE ^ 0x45,
					0xEF ^ 0x67,
					0x89,
				}
				repairSymbol = &RepairSymbol{
					FECBlockNumber: 42,
					Data:           xoredpackets,
				}
				fecGroup = &FECBlock{
					FECBlockNumber:       42,
					packets:              [][]byte{packet1, packet2, packet3},
					packetIndexes:        map[protocol.PathID]map[protocol.PacketNumber]int{0: {1: 0, 2: 1, 3: 2}},
					TotalNumberOfPackets: 3,
				}
				fecGroup.PrepareToSend()
				fecScheme = &XORFECScheme{}
				fecGroupToRecover = NewFECGroup(42, version)
				fecGroupToRecover.AddPacket(packet1, &wire.Header{PacketNumber: 1, FECPayloadID: 0})
				fecGroupToRecover.AddPacket(packet3, &wire.Header{PacketNumber: 3, FECPayloadID: 2})
				fecGroupToRecover.AddRepairSymbol(repairSymbol)
				fecGroupToRecover.TotalNumberOfPackets = fecGroup.TotalNumberOfPackets
			})

			It("builds a repair symbol correctly", func() {
				repairSymbols, err := fecScheme.GetRepairSymbols(fecGroup, 1, 42)
				repairSymbols[0].FECBlockNumber = 42
				Expect(err).ToNot(HaveOccurred())
				Expect(repairSymbols).To(HaveLen(1))
				Expect(repairSymbols[0]).To(Equal(repairSymbol))
			})

			It("builds a repair symbol with only one packet", func() {
				fecGroupToRecover.packets = [][]byte{nil, nil, packet3}
				fecGroupToRecover.packetIndexes = map[protocol.PathID]map[protocol.PacketNumber]int{0: {3: 2}}
				fecGroupToRecover.TotalNumberOfPackets = 1
				repairSymbols, err := fecScheme.GetRepairSymbols(fecGroupToRecover, 1, 42)
				Expect(err).ToNot(HaveOccurred())
				Expect(repairSymbols).To(HaveLen(1))
				Expect(repairSymbols[0].Data).To(Equal(packet3))
			})

			It("indicates that it can recover packets", func() {
				Expect(fecScheme.CanRecoverPackets(fecGroupToRecover)).To(BeTrue())
			})

			It("indicates that it cannot recover packets", func() {
				fecGroupToRecover.packets = [][]byte{packet1}
				fecGroupToRecover.packetIndexes = map[protocol.PathID]map[protocol.PacketNumber]int{0: {1: 0}}
				Expect(fecScheme.CanRecoverPackets(fecGroupToRecover)).To(BeFalse())
				recovered, err := fecScheme.RecoverPackets(fecGroupToRecover)
				Expect(err).To(MatchError(XORFECSchemeCannotRecoverPacket))
				Expect(recovered).To(BeNil())
			})

			It("indicates that it cannot generate repair symbols", func() {
				fecGroupToRecover.packets = nil
				fecGroupToRecover.packetIndexes = nil
				repairSymbols, err := fecScheme.GetRepairSymbols(fecGroupToRecover, 1, 42)
				Expect(err).To(MatchError(XORFECSchemeCannotGetRepairSymbol))
				Expect(repairSymbols).To(BeNil())
			})

			It("recovers a lost packet successfully", func() {
				recoveredPackets, err := fecScheme.RecoverPackets(fecGroupToRecover)
				Expect(err).ToNot(HaveOccurred())
				Expect(recoveredPackets).To(HaveLen(1))
				Expect(recoveredPackets[0][:len(packet2)]).To(Equal(packet2))
			})
		})

		Context("XOR function", func() {
			var p1, p2, p3, p4, p1XORp2, p1XORp3, p1XORp4 []byte
			BeforeEach(func() {
				p1 = []byte{0xDE, 0xAD, 0xBE, 0xEF}
				p2 = []byte{0xCA, 0xFE}
				p3 = []byte{0xBA, 0xDD, 0xEC, 0xAF}
				p4 = nil
				p1XORp2 = []byte{0xDE ^ 0xCA, 0xAD ^ 0xFE, 0xBE, 0xEF}
				p1XORp3 = []byte{0xDE ^ 0xBA, 0xAD ^ 0xDD, 0xBE ^ 0xEC, 0xEF ^ 0xAF}
				p1XORp4 = p1

			})
			It("XORs correctly packets of the same size", func() {
				result := XOR(p1, p3)
				Expect(result).To(Equal(p1XORp3))
			})

			It("XORs correctly packets of different sizes (first bigger, second smaller)", func() {
				result := XOR(p1, p2)
				Expect(result).To(Equal(p1XORp2))
			})

			It("XORs correctly packets of different sizes (first bigger, second smaller)", func() {
				result := XOR(p2, p1)
				Expect(result).To(Equal(p1XORp2))
			})

			It("XORs correctly a packet with a nil/empty packet", func() {
				result := XOR(p1, p4)
				Expect(result).To(Equal(p1XORp4))
			})
		})
	}

})
