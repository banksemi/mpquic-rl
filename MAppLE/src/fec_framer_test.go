package quic

import (
	. "github.com/onsi/ginkgo"
	//. "github.com/onsi/gomega"
	//"github.com/lucas-clemente/quic-go/internal/protocol"
	//"github.com/lucas-clemente/quic-go/fec"
	//"github.com/lucas-clemente/quic-go/internal/wire"
)

var _ = Describe("FEC Framer", func() {
	//var (
	//	framer *FECFramer
	//	g1, g2 *fec.FECGroup
	//	queue  []*fec.FECGroup
	//	symbol1, symbol2, symbol3	*fec.RepairSymbol
	//)
	//
	//Context("for gQUIC", func(){
	//
	//	BeforeEach(func() {
	//		g1 = fec.NewFECGroup(42, protocol.VersionWhatever)
	//		g2 = fec.NewFECGroup(43, protocol.VersionWhatever)
	//		framer = newFECFramer()
	//		queue = []*fec.FECGroup{g1, g2}
	//		symbol1 = &fec.RepairSymbol{
	//			FECBlockNumber: 42,
	//			Data: []byte("foobarfoobar"),						// length 12
	//		}
	//		symbol2 = &fec.RepairSymbol{
	//			FECBlockNumber: 		42,
	//			Data: 				[]byte("barfoobarfoo__"),	// length 14
	//			SymbolNumber: 1,
	//		}
	//		symbol3 = &fec.RepairSymbol{
	//			FECBlockNumber: 43,
	//			Data: []byte("foo2bar2"),								// length 8
	//		}
	//
	//		g1.AddRepairSymbol(symbol1)
	//		g1.AddRepairSymbol(symbol2)
	//		g2.AddRepairSymbol(symbol3)
	//	})
	//
	//
	//	It("pushes FEC groups in FEC framer", func() {
	//		Expect(framer.transmissionQueue).To(HaveLen(0))
	//		framer.pushFECGroup(g1)
	//		framer.pushFECGroup(g2)
	//		Expect(framer.transmissionQueue).To(Equal(queue))
	//		framer.transmissionQueue = []*fec.FECGroup{}
	//		Expect(framer.transmissionQueue).To(HaveLen(0))
	//		framer.pushFECGroups(queue)
	//		Expect(framer.transmissionQueue).To(Equal(queue))
	//	})
	//
	//	It("pops FEC groups in FEC Framer", func() {
	//		framer.transmissionQueue = queue
	//		Expect(framer.popFECGroup()).To(Equal(g1))
	//		Expect(framer.popFECGroup()).To(Equal(g2))
	//	})
	//
	//	It("tries to pop FEC frames while the FEC framer is empty", func() {
	//		popped, takenPayload, err := framer.maybePopFECFrames(100000)
	//		Expect(err).ToNot(HaveOccurred())
	//		Expect(popped).To(BeNil())
	//		Expect(takenPayload).To(BeZero())
	//	})
	//
	//	It("tries to pop FEC frames while the max capacity is too low to pop anything", func() {
	//		framer.transmissionQueue = queue
	//		popped, takenPayload, err := framer.maybePopFECFrames(0)
	//		Expect(err).ToNot(HaveOccurred())
	//		Expect(popped).To(BeNil())
	//		Expect(takenPayload).To(BeZero())
	//	})
	//
	//
	//	It("tries to pop FEC frames while the max capacity is too low to pop all the frames at once", func() {
	//		framer.transmissionQueue = queue
	//		frame := &wire.FECFrame{}
	//		minLength := frame.Length(versionGQUICFrames)
	//
	//		// first frame
	//		popped, takenPayload, err := framer.maybePopFECFrames(minLength + 8)
	//		Expect(err).ToNot(HaveOccurred())
	//		Expect(popped).To(HaveLen(1))
	//		Expect(takenPayload).To(Equal(minLength+8))
	//		frame = popped[0]
	//		Expect(frame.Offset).To(Equal(protocol.FecFrameOffset(0)))
	//		Expect(frame.RepairSymbolNumber).To(BeZero())
	//		Expect(frame.Data).To(HaveLen(8))
	//
	//		// second frame
	//		minLength -= 2	// The second frame should have two bytes less header
	//		popped, takenPayload, err = framer.maybePopFECFrames(minLength + 3)
	//		Expect(err).ToNot(HaveOccurred())
	//		Expect(popped).To(HaveLen(1))
	//		Expect(takenPayload).To(Equal(minLength+3))
	//		frame = popped[0]
	//		Expect(frame.Offset).To(Equal(protocol.FecFrameOffset(1)))
	//		Expect(frame.RepairSymbolNumber).To(BeZero())		// We should still be at symbol 0
	//		Expect(frame.Data).To(HaveLen(3))
	//
	//		// third frame
	//		popped, takenPayload, err = framer.maybePopFECFrames(minLength + 1 + minLength + 10)
	//		Expect(err).ToNot(HaveOccurred())
	//		Expect(popped).To(HaveLen(2))
	//		Expect(takenPayload).To(Equal(minLength + 1 + minLength + 10))
	//		frame1 := popped[0]
	//		Expect(frame1.Offset).To(Equal(protocol.FecFrameOffset(2)))
	//		Expect(frame1.RepairSymbolNumber).To(BeZero())		// We should still be at symbol 0
	//		Expect(frame1.Data).To(HaveLen(1))
	//		frame2 := popped[1]
	//		Expect(frame2.Offset).To(Equal(protocol.FecFrameOffset(0)))
	//		Expect(frame2.RepairSymbolNumber).To(Equal(byte(1)))		// We are at symbol 1
	//		Expect(frame2.Data).To(HaveLen(10))
	//
	//	})
	//})

})
