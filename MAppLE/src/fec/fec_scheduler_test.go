package fec

import (
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("FEC Scheduler", func() {

	var (
		scheduler                                  *RoundRobinScheduler
		fecGroup1, fecGroup2, fecGroup3, fecGroup4 *FECBlock
	)
	Context("for IETF QUIC", func() {

		BeforeEach(func() {
			scheduler = NewRoundRobinScheduler(NewConstantRedundancyController(2, 1, 2, 1), versionIETFQUIC)
			fecGroup1 = NewFECGroup(0, versionIETFQUIC)
			fecGroup2 = NewFECGroup(1, versionIETFQUIC)
			fecGroup3 = NewFECGroup(2, versionIETFQUIC)
			fecGroup4 = NewFECGroup(3, versionIETFQUIC)
		})

		It("gives correctly the next FEC Payload", func() {
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
		})

		It("handles correctly the case of a senf FEC Payload", func() {
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			scheduler.SentFECBlock(0)
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup3))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			scheduler.SentFECBlock(2)
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup4))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
		})

		It("gives correctly the next FEC Group Number", func() {
			Expect(scheduler.GetNextFECBlockNumber()).To(Equal(protocol.FECBlockNumber(0)))
			scheduler.GetNextFECGroup()
			Expect(scheduler.GetNextFECBlockNumber()).To(Equal(protocol.FECBlockNumber(1)))
			scheduler.GetNextFECGroup()
			Expect(scheduler.GetNextFECBlockNumber()).To(Equal(protocol.FECBlockNumber(0)))
		})

		It("gives correctly the next FEC Group Offset", func() {
			scheduler = NewRoundRobinScheduler(NewConstantRedundancyController(1, 1, 1, 1), versionIETFQUIC)
			Expect(scheduler.GetNextFECGroupOffset()).To(Equal(byte(0)))
			scheduler.GetNextFECGroup().AddPacket([]byte{}, &wire.Header{})
			Expect(scheduler.GetNextFECGroupOffset()).To(Equal(byte(1)))
		})
	})

	Context("for gQUIC", func() {

		BeforeEach(func() {
			scheduler = NewRoundRobinScheduler(NewConstantRedundancyController(2, 1, 2, 1), versionGQUIC)
			fecGroup1 = NewFECGroup(0, versionGQUIC)
			fecGroup2 = NewFECGroup(1, versionGQUIC)
			fecGroup3 = NewFECGroup(2, versionGQUIC)
			fecGroup4 = NewFECGroup(3, versionGQUIC)
		})

		It("gives correctly the next FEC Payload", func() {
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
		})

		It("handles correctly the case of a senf FEC Payload", func() {
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup1))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			scheduler.SentFECBlock(0)
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup3))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
			scheduler.SentFECBlock(2)
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup4))
			Expect(scheduler.GetNextFECGroup()).To(Equal(fecGroup2))
		})

		It("gives correctly the next FEC Group Number", func() {
			Expect(scheduler.GetNextFECBlockNumber()).To(Equal(protocol.FECBlockNumber(0)))
			scheduler.GetNextFECGroup()
			Expect(scheduler.GetNextFECBlockNumber()).To(Equal(protocol.FECBlockNumber(1)))
			scheduler.GetNextFECGroup()
			Expect(scheduler.GetNextFECBlockNumber()).To(Equal(protocol.FECBlockNumber(0)))
		})

		It("gives correctly the next FEC Group Offset", func() {
			scheduler = NewRoundRobinScheduler(NewConstantRedundancyController(1, 1, 1, 1), versionGQUIC)
			Expect(scheduler.GetNextFECGroupOffset()).To(Equal(byte(0)))
			scheduler.GetNextFECGroup().AddPacket([]byte{}, &wire.Header{})
			Expect(scheduler.GetNextFECGroupOffset()).To(Equal(byte(1)))
		})
	})

})
