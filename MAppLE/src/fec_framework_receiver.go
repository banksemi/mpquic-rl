package quic

import (
	"bytes"
	"errors"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/logger"
	"log"
	"time"
)

/**
 *	Receives payloads and repair symbols for a block (FEC Group) and gives it to the FEC scheme. FEC scheme returns :
 * 		- One or more Recovered FEC Source payloads (recovered packets)
 */

//		+----------------------+
//		|     Application      |
//		+----------------------+
//		^
//		|
//		|(6) ADUs
//		|
//		+----------------------+                           +----------------+
//		|    FEC Framework     |                           |                |
//		|                      |<--------------------------|   FEC Scheme   |
//		|(2)Extract FEC Payload|(5) Recovered QUIC packets |                |
//		|   IDs and pass IDs,  |                           |(4) FEC Decoding|
//		|   payloads & symbols |-------------------------->|                |
//		|   to FEC scheme      |(3) Explicit Source FEC    |                |
//		+----------------------+    Payload IDs            +----------------+
//		^             ^         Source payloads (FEC-protected packets)
//		|             |         Repair symbols
//		|             |
//		|             |
//		|Source       |Repair FEC Frames
//		|packets      |
//		|             |
//		+-- -- -- -- -- -- -- -+
//		|    QUIC Processing	 |
//		+-- -- -- -- -- -- -- -+
//		^
//		|(1) QUIC packets containing FEC source data and repair symbols (in FEC Frames)
//		|
//		+----------------------+
//		|   Transport Layer    |
//		|     (e.g., UDP)      |
//		+----------------------+
//
//		Figure 5: Receiver Operation with RTP Repair Flows

// TODO: give a maximum number of buffers and remove older buffers if full (FIFO ?), to avoid memory explosion

type FECFrameworkReceiver struct {
	fecGroupsBuffer  *fecGroupsBuffer
	waitingFECFrames map[protocol.FECBlockNumber][]map[protocol.FecFrameOffset]*wire.FECFrame // fec frames that only contain parts of a FEC payload and that wait for the next parts access: [fecPayloadID][repairSymbol][Offset]
	recoveredPackets chan *receivedPacket
	session          *session // the session that uses this handler (TODO: find another way than passing the session directly to get the version number, perspective and remoteAddress informations
	doRecovery       bool     // Debug parameter: if false, the recovered packets won't be used by the session, like if it has not been recovered
	fecScheme        fec.BlockFECScheme
}

func NewFECFrameworkReceiver(s *session, fecScheme fec.BlockFECScheme) *FECFrameworkReceiver {
	buffer := newFecGroupsBuffer(200)
	return &FECFrameworkReceiver{
		fecGroupsBuffer: buffer,
		// TODO: find a good value for the packets buffer size rather than 1000
		waitingFECFrames: make(map[protocol.FECBlockNumber][]map[protocol.FecFrameOffset]*wire.FECFrame),
		session:          s,
		recoveredPackets: s.recoveredPackets,
		doRecovery:       true,
		fecScheme:        fecScheme,
	}
}

func (f *FECFrameworkReceiver) handlePacket(data []byte, header *wire.Header) {
	if !header.FECFlag {
		return
	}
	fecBlockNumber := header.FECPayloadID.GetBlockNumber()
	_, ok := f.fecGroupsBuffer.fecGroups[fecBlockNumber]
	if !ok {
		group := fec.NewFECGroup(fecBlockNumber, f.session.version)
		group.AddPacket(data, header)
		f.fecGroupsBuffer.addFECGroup(group)
		logger.ExpLogInsertFECEvent(fecBlockNumber, "started")
	}
	f.fecGroupsBuffer.addPacketInFECGroup(data, header)

	for bn := fecBlockNumber - 4; bn <= fecBlockNumber; bn++ {
		f.updateStateForSomeFECGroup(bn)
	}
}

// Recovers a packet from this FEC group if possible. If a packet has been recovered or if this FEC group is useless (there is no missing packet in the buffer),
// the buffer of this FEC group will be removed
func (f *FECFrameworkReceiver) updateStateForSomeFECGroup(fecBlockNumber protocol.FECBlockNumber) error {
	group, ok := f.fecGroupsBuffer.fecGroups[fecBlockNumber]
	if !ok || len(group.RepairSymbols) == 0 {
		return nil
	}
	if group.TotalNumberOfPackets > 0 && group.CurrentNumberOfPackets() == group.TotalNumberOfPackets {
		delete(f.fecGroupsBuffer.fecGroups, fecBlockNumber)
		return nil
	}
	if f.fecScheme.CanRecoverPackets(group, f.session.GetLargestRcvdPacketNumber()) {
		// if there is one remaining packet to receive, we can find it thanks to the FEC payload and the already received packets
		recoveredPackets, err := f.fecScheme.RecoverPackets(group)
		if err != nil {
			return err
		}
		if len(recoveredPackets) == 0 {
			return errors.New("the fec scheme hasn't recovered any packet although it indicated that it could")
		}
		if len(recoveredPackets) > 0 {
			log.Printf("recovered %d packets !", len(recoveredPackets))
			logger.ExpLogInsertFECEvent(fecBlockNumber, "recovered")
		}
		for _, packet := range recoveredPackets {
			f.parseAndSendRecoveredPacket(packet)
		}
		delete(f.fecGroupsBuffer.fecGroups, fecBlockNumber)
	}
	return nil
}

// Transforms the recoveredPacket and sends it to the session, like a normal received packet
func (f *FECFrameworkReceiver) parseAndSendRecoveredPacket(recoveredPacket []byte) {
	// Handle the doRecovery debug parameter
	if !f.doRecovery {
		return
	}
	// TODO: maybe try to find a cleaner way to re-parse the bytes into a receivedPacket...
	r := bytes.NewReader(recoveredPacket)

	var header *wire.Header
	var err error
	if f.session.perspective == protocol.PerspectiveServer {
		header, err = wire.ParseHeaderSentByClient(r)
	} else {
		header, err = wire.ParseHeaderSentByServer(r, f.session.version)
	}
	if err == nil {
		header.Raw = recoveredPacket[:len(recoveredPacket)-r.Len()]
		rp := &receivedPacket{
			f.session.RemoteAddr(),
			header,
			recoveredPacket[len(recoveredPacket)-r.Len():],
			time.Now(),
			nil,
			true,
		}

		f.recoveredPackets <- rp
	} else {
		log.Printf("Error when parsing header of recovered packet : %s", err.Error())
	}
}

// adds the FEC frame in the waiting FEC frames and handles its payload directly if it can recover the full payload
// for the FEC group of the specified FEC frame
func (f *FECFrameworkReceiver) handleFECFrame(frame *wire.FECFrame) {
	// Copying FEC Frame data
	newData := make([]byte, len(frame.Data))
	copy(newData, frame.Data)
	frame.Data = newData
	f.putWaitingFrame(frame)
	symbol, numberOfPacketsInFECGroup, numberOfRepairSymbols := f.getRepairSymbolForSomeFECGroup(frame)
	if symbol != nil {
		f.handleRepairSymbol(symbol, numberOfPacketsInFECGroup, numberOfRepairSymbols)
	}
}

// pre: the payload argument must be a full packet payload
// addFECGroup the complete FEC Payload in a buffer in the fecGroupsMap, adds the waitingPackets of this FEC group in the buffer
// and recovers a packet if it can
func (f *FECFrameworkReceiver) handleRepairSymbol(symbol *fec.RepairSymbol, numberOfPacketsInFECGroup int, numberOfRepairSymbols int) {
	group, ok := f.fecGroupsBuffer.fecGroups[symbol.FECBlockNumber]
	if !ok {
		group = fec.NewFECGroup(symbol.FECBlockNumber, f.session.version)
		group.AddRepairSymbol(symbol)
		f.fecGroupsBuffer.addFECGroup(group)
	} else {
		group.AddRepairSymbol(symbol)
	}
	group.TotalNumberOfPackets = numberOfPacketsInFECGroup
	group.TotalNumberOfRepairSymbols = numberOfRepairSymbols
	if ok || numberOfPacketsInFECGroup == 1 {
		// recover packet if possible, remove useless buffers
		f.updateStateForSomeFECGroup(symbol.FECBlockNumber)
	}
}

// TODO: waitingFrames should be an array of frames sorted by offset, absent frames should be nil in the array

// Looks in the waitingFECFrames and returns the full payload for the specified FEC group and removes the frames from the waitingFrames if all the frames are present.
// First return value: Returns nil if all the frames for this FEC group are not present in the waitingFrames
// Second return value: the number of packets concerned by this FEC Group if the return symbol has the number 0. Returns -1 otherwise.
// Third return value: the number of repair symbols concerned by this FEC Group if the return symbol has the number 0. Returns -1 otherwise.
func (f *FECFrameworkReceiver) getRepairSymbolForSomeFECGroup(receivedFrame *wire.FECFrame) (*fec.RepairSymbol, int, int) {
	fecBlockNumber := receivedFrame.FECBlockNumber
	waitingFrames, ok := f.waitingFECFrames[fecBlockNumber]
	if !ok || len(waitingFrames) == 0 {
		// there are no waiting frames
		return nil, -1, -1
	}
	if len(waitingFrames) <= int(receivedFrame.Offset) {
		delta := int(receivedFrame.Offset) - len(waitingFrames)
		for i := 0; i <= delta; i++ {
			waitingFrames = append(waitingFrames, make(map[protocol.FecFrameOffset]*wire.FECFrame))
		}
		f.waitingFECFrames[fecBlockNumber] = waitingFrames
	}
	waitingFramesForSymbol := waitingFrames[receivedFrame.RepairSymbolNumber]
	if len(waitingFramesForSymbol) == 0 {
		// there are no waiting frames
		return nil, -1, -1
	}
	if len(waitingFramesForSymbol) == 1 {
		if !receivedFrame.FinBit || receivedFrame.Offset != 0 {
			// there is only one waiting (which is the receivedFrame) frame which does not contain a full symbol, so obviously we cannot return symbols
			return nil, -1, -1
		} else {
			// there is only one FEC Frame, which contains a full symbol and has the FinBit set so return the symbol
			delete(f.waitingFECFrames, fecBlockNumber)
			symbol := &fec.RepairSymbol{
				FECSchemeSpecific: receivedFrame.FECSchemeSpecific,
				FECBlockNumber:    receivedFrame.FECBlockNumber,
				SymbolNumber:      receivedFrame.RepairSymbolNumber,
				Data:              receivedFrame.Data,
			}
			return symbol, int(receivedFrame.NumberOfPackets), int(receivedFrame.NumberOfRepairSymbols)
		}
	}
	finBitFound := false
	var largestOffset protocol.FecFrameOffset = 0
	for _, frame := range waitingFramesForSymbol {
		if frame.FinBit {
			finBitFound = true
		}
		if frame.Offset > largestOffset {
			largestOffset = frame.Offset
		}
	}
	if !finBitFound || int(largestOffset) >= len(waitingFramesForSymbol) { // there is no packet with fin bit, or the largest offset in the waiting frames is greater than the number of waiting frames
		// all frames are not present in the waiting frames for the symbol, the payload is thus not complete
		return nil, -1, -1
	} else { // the frames are all present in the waitingFrames
		// the payload is complete, extract it
		orderedFrames := make([]*wire.FECFrame, len(waitingFramesForSymbol))
		for _, frame := range waitingFramesForSymbol {
			orderedFrames[frame.Offset] = frame
		}
		var payloadData []byte
		for _, frame := range orderedFrames {
			payloadData = append(payloadData, frame.Data...)
		}
		// remove the frames from waitingFECFrames, as the payload has been recovered
		f.waitingFECFrames[fecBlockNumber][receivedFrame.RepairSymbolNumber] = nil
		//delete(f.waitingFECFrames[fecBlockNumber], receivedFrame.RepairSymbolNumber)
		var nPackets, nRepairSymbols byte
		if orderedFrames[0] != nil {
			nPackets = orderedFrames[0].NumberOfPackets
			nRepairSymbols = orderedFrames[0].NumberOfRepairSymbols
		}
		return &fec.RepairSymbol{
			FECSchemeSpecific: receivedFrame.FECSchemeSpecific,
			FECBlockNumber:    fecBlockNumber,
			Data:              payloadData,
			SymbolNumber:      receivedFrame.RepairSymbolNumber,
		}, int(nPackets), int(nRepairSymbols)
	}
}

func (f *FECFrameworkReceiver) putWaitingFrame(frame *wire.FECFrame) {
	// add frame if not already present
	waitingFrames := f.waitingFECFrames[frame.FECBlockNumber]

	// ensure to have place for this repair symbol
	if len(waitingFrames) <= int(frame.RepairSymbolNumber) {
		delta := int(frame.RepairSymbolNumber) - len(waitingFrames)
		for i := 0; i <= delta; i++ {
			waitingFrames = append(waitingFrames, make(map[protocol.FecFrameOffset]*wire.FECFrame))
		}
		f.waitingFECFrames[frame.FECBlockNumber] = waitingFrames
	}

	waitingFramesForSymbol := waitingFrames[frame.RepairSymbolNumber]
	if _, ok := waitingFramesForSymbol[frame.Offset]; !ok {
		waitingFramesForSymbol[frame.Offset] = frame
	}
}

type fecGroupsBuffer struct {
	head      *node // FIFO queue which will be used to remove old fecBuffers which has not been normally removed (never used, maybe because of the loss of more than one packet)
	tail      *node
	size      uint
	maxSize   uint
	fecGroups map[protocol.FECBlockNumber]*fec.FECBlock
}

type node struct {
	fecBlockNumber protocol.FECBlockNumber
	next           *node
}

func newFecGroupsBuffer(maxSize uint) *fecGroupsBuffer {
	return &fecGroupsBuffer{
		nil,
		nil,
		0,
		maxSize,
		make(map[protocol.FECBlockNumber]*fec.FECBlock),
	}
}

func (b *fecGroupsBuffer) addFECGroup(group *fec.FECBlock) {
	number := group.FECBlockNumber
	if b.size == b.maxSize {
		toRemove := b.head
		b.head = b.head.next
		delete(b.fecGroups, toRemove.fecBlockNumber)
		b.size--
	}
	newNode := &node{number, nil}
	if b.size == 0 {
		b.tail = newNode
		b.head = newNode
	} else {
		b.tail.next = newNode
		b.tail = newNode
	}
	b.fecGroups[number] = group
	b.size++
}

// TODO: should return true if it has been added, and false if not.
func (b *fecGroupsBuffer) addPacketInFECGroup(packet []byte, header *wire.Header) (err error) {
	group, ok := b.fecGroups[header.FECPayloadID.GetBlockNumber()]
	if ok {
		group.AddPacket(packet, header)
	}
	return
}
