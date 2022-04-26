package fec

import (
	"bytes"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var versionIETFQUIC = protocol.VersionTLS
var versionGQUIC = protocol.Version39

var _ = Describe("FECBlockNumber", func() {
	var (
		packet1, packet2, packet3, packet4, packet5 []byte
		hdr1, hdr2, hdr3, hdr4, hdr5                *wire.Header
		fecGroup                                    *FECBlock
	)

	Context("for IETF QUIC", func() {
		Context("Adding packets", func() {

			BeforeEach(func() {
				packet1 = bytes.Repeat([]byte{1}, 42)
				packet2 = bytes.Repeat([]byte{2}, 43)
				packet3 = bytes.Repeat([]byte{3}, 41)
				packet4 = bytes.Repeat([]byte{4}, 48)
				packet5 = bytes.Repeat([]byte{5}, 42)
				hdr1 = &wire.Header{PacketNumber: 1, FECPayloadID: 0}
				hdr2 = &wire.Header{PacketNumber: 2, FECPayloadID: 1}
				hdr3 = &wire.Header{PacketNumber: 3, FECPayloadID: 2}
				hdr4 = &wire.Header{PacketNumber: 4, FECPayloadID: 3}
				hdr5 = &wire.Header{PacketNumber: 5, FECPayloadID: 4}
				fecGroup = NewFECGroup(42, versionIETFQUIC)
				fecGroup.RepairSymbols = append(fecGroup.RepairSymbols, &RepairSymbol{
					FECBlockNumber: 42,
					Data:           bytes.Repeat([]byte{6}, 48),
				})

			})

			It("adds packets successfully", func() {
				protocol.NumberOfFecPackets = 10
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
				fecGroup.AddPacket(packet1, hdr1)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet2, hdr2)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet3, hdr3)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet4, hdr4)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet5, hdr5)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.PrepareToSend()
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(5))
				Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(1))
			})
		})

		It("successfully adds repair symbols", func() {
			fecGroup = NewFECGroup(42, versionIETFQUIC)
			symbol1 := &RepairSymbol{
				FECBlockNumber: fecGroup.FECBlockNumber,
				Data:           bytes.Repeat([]byte{42}, 42),
			}
			symbol2 := &RepairSymbol{
				FECBlockNumber: fecGroup.FECBlockNumber,
				Data:           bytes.Repeat([]byte{43}, 43),
			}
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
			err := fecGroup.AddRepairSymbol(symbol1)
			Expect(err).ToNot(HaveOccurred())
			Expect(fecGroup.RepairSymbols).To(HaveLen(1))
			Expect(fecGroup.RepairSymbols).To(ContainElement(symbol1))
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
			err = fecGroup.AddRepairSymbol(symbol2)
			Expect(err).ToNot(HaveOccurred())
			Expect(fecGroup.RepairSymbols).To(HaveLen(2))
			Expect(fecGroup.RepairSymbols).To(ContainElement(symbol1))
			Expect(fecGroup.RepairSymbols).To(ContainElement(symbol2))
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
			fecGroup.PrepareToSend()
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(2))

		})

		It("returns the FECBlockOffset correctly", func() {
			fecGroup = NewFECGroup(42, versionIETFQUIC)
			fecGroup.AddPacket(packet1, hdr1)
			fecGroup.AddPacket(packet3, hdr3)
			fecGroup.AddPacket(packet5, hdr5)
			Expect(fecGroup.GetPacketOffset(hdr1.PacketNumber, 0)).To(Equal(hdr1.FECPayloadID.GetBlockOffset()))
			Expect(fecGroup.GetPacketOffset(hdr3.PacketNumber, 0)).To(Equal(hdr3.FECPayloadID.GetBlockOffset()))
			Expect(fecGroup.GetPacketOffset(hdr5.PacketNumber, 0)).To(Equal(hdr5.FECPayloadID.GetBlockOffset()))
		})

		Context("Building FEC Frames from Repair Symbols", func() {

			BeforeEach(func() {
				protocol.NumberOfFecPackets = 5
				fecGroup = NewFECGroup(42, versionIETFQUIC)
				fecGroup.RepairSymbols = append(fecGroup.RepairSymbols, &RepairSymbol{
					FECBlockNumber: 42,
					Data:           bytes.Repeat([]byte{6}, 48),
				})
				fecGroup.AddPacket(packet1, hdr1)
				fecGroup.AddPacket(packet2, hdr2)
				fecGroup.AddPacket(packet3, hdr3)
				fecGroup.AddPacket(packet4, hdr4)
				fecGroup.AddPacket(packet5, hdr5)
			})

		})
		It("indicates that it should be sent when the payload is full", func() {
			fecGroup = NewFECGroup(42, versionIETFQUIC)
			protocol.NumberOfFecPackets = 5
			Expect(fecGroup.HasPacket(1, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(2, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(3, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(0))
			fecGroup.AddPacket(packet1, hdr1)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(3, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(1))
			fecGroup.AddPacket(packet2, hdr2)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets(), 0).To(Equal(2))
			fecGroup.AddPacket(packet3, hdr3)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(3))
			fecGroup.AddPacket(packet4, hdr4)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets(), 0).To(Equal(4))
			fecGroup.AddPacket(packet5, hdr5)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(5, 0)).To(BeTrue())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(5))
			Expect(fecGroup.ShouldBeSent(NewConstantRedundancyController(5, 1, 1, 1))).To(BeTrue())
			fecGroup.AddPacket(packet5, hdr5)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(5, 0)).To(BeTrue())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(5))

			Expect(fecGroup.packets).To(ContainElement(packet1))
			Expect(fecGroup.packets).To(ContainElement(packet2))
			Expect(fecGroup.packets).To(ContainElement(packet3))
			Expect(fecGroup.packets).To(ContainElement(packet4))
			Expect(fecGroup.packets).To(ContainElement(packet5))
		})

	})

	Context("for gQUIC", func() {
		Context("Adding packets", func() {

			BeforeEach(func() {
				packet1 = bytes.Repeat([]byte{1}, 42)
				packet2 = bytes.Repeat([]byte{2}, 43)
				packet3 = bytes.Repeat([]byte{3}, 41)
				packet4 = bytes.Repeat([]byte{4}, 48)
				packet5 = bytes.Repeat([]byte{5}, 42)
				hdr1 = &wire.Header{PacketNumber: 1, FECPayloadID: 0}
				hdr2 = &wire.Header{PacketNumber: 2, FECPayloadID: 1}
				hdr3 = &wire.Header{PacketNumber: 3, FECPayloadID: 2}
				hdr4 = &wire.Header{PacketNumber: 4, FECPayloadID: 3}
				hdr5 = &wire.Header{PacketNumber: 5, FECPayloadID: 4}
				fecGroup = NewFECGroup(42, versionGQUIC)
				fecGroup.RepairSymbols = append(fecGroup.RepairSymbols, &RepairSymbol{
					FECBlockNumber: 42,
					Data:           bytes.Repeat([]byte{6}, 48),
				})

			})

			It("adds packets successfully", func() {
				protocol.NumberOfFecPackets = 10
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
				fecGroup.AddPacket(packet1, hdr1)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet2, hdr2)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet3, hdr3)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet4, hdr4)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.AddPacket(packet5, hdr5)
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(0))
				fecGroup.PrepareToSend()
				Expect(fecGroup.TotalNumberOfPackets).To(Equal(5))
				Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(1))
			})
		})

		It("successfully adds repair symbols", func() {
			fecGroup = NewFECGroup(42, versionGQUIC)
			symbol1 := &RepairSymbol{
				FECBlockNumber: fecGroup.FECBlockNumber,
				Data:           bytes.Repeat([]byte{42}, 42),
			}
			symbol2 := &RepairSymbol{
				FECBlockNumber: fecGroup.FECBlockNumber,
				Data:           bytes.Repeat([]byte{43}, 43),
			}
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
			err := fecGroup.AddRepairSymbol(symbol1)
			Expect(err).ToNot(HaveOccurred())
			Expect(fecGroup.RepairSymbols).To(HaveLen(1))
			Expect(fecGroup.RepairSymbols).To(ContainElement(symbol1))
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
			err = fecGroup.AddRepairSymbol(symbol2)
			Expect(err).ToNot(HaveOccurred())
			Expect(fecGroup.RepairSymbols).To(HaveLen(2))
			Expect(fecGroup.RepairSymbols).To(ContainElement(symbol1))
			Expect(fecGroup.RepairSymbols).To(ContainElement(symbol2))
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(0))
			fecGroup.PrepareToSend()
			Expect(fecGroup.TotalNumberOfRepairSymbols).To(Equal(2))

		})

		It("returns the FECBlockOffset correctly", func() {
			fecGroup = NewFECGroup(42, versionGQUIC)
			fecGroup.AddPacket(packet1, hdr1)
			fecGroup.AddPacket(packet3, hdr3)
			fecGroup.AddPacket(packet5, hdr5)
			Expect(fecGroup.GetPacketOffset(hdr1.PacketNumber, 0)).To(Equal(hdr1.FECPayloadID.GetBlockOffset()))
			Expect(fecGroup.GetPacketOffset(hdr3.PacketNumber, 0)).To(Equal(hdr3.FECPayloadID.GetBlockOffset()))
			Expect(fecGroup.GetPacketOffset(hdr5.PacketNumber, 0)).To(Equal(hdr5.FECPayloadID.GetBlockOffset()))
		})

		Context("Building FEC Frames from Repair Symbols", func() {

			BeforeEach(func() {
				protocol.NumberOfFecPackets = 5
				fecGroup = NewFECGroup(42, versionGQUIC)
				fecGroup.RepairSymbols = append(fecGroup.RepairSymbols, &RepairSymbol{
					FECBlockNumber: 42,
					Data:           bytes.Repeat([]byte{6}, 48),
				})
				fecGroup.AddPacket(packet1, hdr1)
				fecGroup.AddPacket(packet2, hdr2)
				fecGroup.AddPacket(packet3, hdr3)
				fecGroup.AddPacket(packet4, hdr4)
				fecGroup.AddPacket(packet5, hdr5)
			})

		})
		It("indicates that it should be sent when the payload is full", func() {
			fecGroup = NewFECGroup(42, versionGQUIC)
			protocol.NumberOfFecPackets = 5
			Expect(fecGroup.HasPacket(1, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(2, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(3, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(0))
			fecGroup.AddPacket(packet1, hdr1)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(3, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(1))
			fecGroup.AddPacket(packet2, hdr2)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(2))
			fecGroup.AddPacket(packet3, hdr3)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeFalse())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(3))
			fecGroup.AddPacket(packet4, hdr4)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(5, 0)).To(BeFalse())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(4))
			fecGroup.AddPacket(packet5, hdr5)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(5, 0)).To(BeTrue())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(5))
			Expect(fecGroup.ShouldBeSent(NewConstantRedundancyController(5, 1, 1, 1))).To(BeTrue())
			fecGroup.AddPacket(packet5, hdr5)
			Expect(fecGroup.HasPacket(1, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(2, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(3, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(4, 0)).To(BeTrue())
			Expect(fecGroup.HasPacket(5, 0)).To(BeTrue())
			Expect(fecGroup.CurrentNumberOfPackets()).To(Equal(5))

			Expect(fecGroup.packets).To(ContainElement(packet1))
			Expect(fecGroup.packets).To(ContainElement(packet2))
			Expect(fecGroup.packets).To(ContainElement(packet3))
			Expect(fecGroup.packets).To(ContainElement(packet4))
			Expect(fecGroup.packets).To(ContainElement(packet5))
		})

	})

})
