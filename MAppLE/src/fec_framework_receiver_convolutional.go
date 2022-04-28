package quic

import (
	"bytes"
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

type waitingRepairSymbolsBuffer struct {
	buffer []protocol.FECPayloadID
	index  int
}

func newWaitingRepairSymbolsBuffer(size int) waitingRepairSymbolsBuffer {
	return waitingRepairSymbolsBuffer{
		buffer: make([]protocol.FECPayloadID, size),
		index:  0,
	}
}

func (rb waitingRepairSymbolsBuffer) insert(payloadID protocol.FECPayloadID) {
	index := (rb.index + 1) % len(rb.buffer)
	rb.buffer[index] = payloadID
}

type FECFrameworkReceiverConvolutional struct {
	variablesBuffer      []*fec.Variable
	equations            map[protocol.FECPayloadID]*fec.Equation
	waitingFECFrames     map[protocol.FECPayloadID][]map[protocol.FecFrameOffset]*wire.FECFrame // fec frames that only contain parts of a FEC payload and that wait for the next parts access: [fecPayloadID][repairSymbol][Offset]
	waitingRepairSymbols waitingRepairSymbolsBuffer
	recoveredPackets     chan *receivedPacket
	session              *session // the session that uses this handler (TODO: find another way than passing the session directly to get the version number, perspective and remoteAddress informations
	doRecovery           bool     // Debug parameter: if false, the recovered packets won't be used by the session, like if it has not been recovered
	fecScheme            fec.ConvolutionalFECScheme
	numberOfVarsInBuffer int
	highestVarID         protocol.FECEncodingSymbolID
	highestRemoved       protocol.FECEncodingSymbolID
}

func NewFECFrameworkReceiverConvolutional(s *session, fecScheme fec.ConvolutionalFECScheme) *FECFrameworkReceiverConvolutional {
	buffer := make([]*fec.Variable, 500)
	waitingBuffer := newWaitingRepairSymbolsBuffer(protocol.SIOD_MaxSchedulingWindow * 10)
	return &FECFrameworkReceiverConvolutional{
		variablesBuffer: buffer,
		// TODO: find a good value for the packets buffer size rather than 1000
		waitingFECFrames:     make(map[protocol.FECPayloadID][]map[protocol.FecFrameOffset]*wire.FECFrame),
		waitingRepairSymbols: waitingBuffer,
		equations:            make(map[protocol.FECPayloadID]*fec.Equation),
		session:              s,
		recoveredPackets:     s.recoveredPackets,
		doRecovery:           true,
		fecScheme:            fecScheme,
	}
}

func (f *FECFrameworkReceiverConvolutional) canUseESID(esid protocol.FECEncodingSymbolID) bool {
	return f.highestVarID+1 < esid
}

func (f *FECFrameworkReceiverConvolutional) addToVariablesBuffer(data []byte, header *wire.Header) bool {
	index := header.GetFECPayloadID() % protocol.FECPayloadID(len(f.variablesBuffer))
	esid := header.FECPayloadID.GetConvolutionalEncodingSymbolID()
	if f.variablesBuffer[index] != nil { // XXX
		id := f.variablesBuffer[index].ID
		if id >= esid {
			return false
		}
		delete(f.equations, header.FECPayloadID)
		if id > f.highestRemoved {
			f.highestRemoved = id
		}
		f.numberOfVarsInBuffer--
	}
	dataCpy := make([]byte, len(data))
	copy(dataCpy, data)
	variable := &fec.Variable{
		Packet: dataCpy,
		ID:     esid,
	}
	f.variablesBuffer[index] = variable
	if f.highestVarID < variable.ID {
		f.highestVarID = variable.ID
	}
	logger.ExpLogInsertFECEvent(protocol.FECBlockNumber(esid), "added")
	f.numberOfVarsInBuffer++
	return true
}

func (f *FECFrameworkReceiverConvolutional) getFromVariablesBuffer(id protocol.FECEncodingSymbolID) *fec.Variable {
	index := id % protocol.FECEncodingSymbolID(len(f.variablesBuffer))
	candidate := f.variablesBuffer[index]
	if candidate != nil && candidate.ID == id {
		return candidate
	}
	return nil
}

func (f *FECFrameworkReceiverConvolutional) handlePacket(data []byte, header *wire.Header) {
	if !header.FECFlag {
		return
	}

	if f.addToVariablesBuffer(data, header) {
		if f.fecScheme.AddKnownVariable(f.getFromVariablesBuffer(header.GetFECPayloadID().GetConvolutionalEncodingSymbolID())) {
			f.updateStateForSomeEncodingSymbolID(header.GetFECPayloadID().GetConvolutionalEncodingSymbolID())
		}
	}

	for i, waitingRepairSymbol := range f.waitingRepairSymbols.buffer {
		fecPayloadID := waitingRepairSymbol
		if fecPayloadID == 0 {
			continue
		}

		esid := f.equations[fecPayloadID].ID()
		if f.canUseESID(esid) {
			f.applyRepairSymbol(fecPayloadID)
			f.waitingRepairSymbols.buffer[i] = 0
			break
		}
	}
}

// Recovers a packet from this FEC group if possible. If a packet has been recovered or if this FEC group is useless (there is no missing packet in the buffer),
// the buffer of this FEC group will be removed
func (f *FECFrameworkReceiverConvolutional) updateStateForSomeEncodingSymbolID(fecSymbolID protocol.FECEncodingSymbolID) error {
	if f.fecScheme.CanRecoverPackets() {
		recoveredPackets, err := f.fecScheme.RecoverPackets(f.highestRemoved + 1)
		if err != nil {
			return err
		}
		if len(recoveredPackets) > 0 {
			log.Printf("recovered %d packets !", len(recoveredPackets))
		}

		for _, packet := range recoveredPackets {
			f.parseAndSendRecoveredPacket(packet)
			logger.ExpLogInsertFECEvent(protocol.FECBlockNumber(fecSymbolID), "recovered")
		}

	}

	return nil
}

// Transforms the recoveredPacket and sends it to the session, like a normal received packet
func (f *FECFrameworkReceiverConvolutional) parseAndSendRecoveredPacket(recoveredPacket []byte) {
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
func (f *FECFrameworkReceiverConvolutional) handleFECFrame(frame *wire.FECFrame) {
	// Copying FEC Frame data
	newData := make([]byte, len(frame.Data))
	copy(newData, frame.Data)
	frame.Data = newData
	f.putWaitingFrame(frame)
	fecPayloadID := frame.GetFECPayloadID()
	symbol := f.getRepairSymbolForSomeFECGroup(frame)
	if symbol != nil {
		f.handleRepairSymbol(symbol, fecPayloadID)
	}
}

// pre: the payload argument must be a full packet payload
// addFECGroup the complete FEC Payload in a buffer in the fecGroupsMap, adds the waitingPackets of this FEC group in the buffer
// and recovers a packet if it can
func (f *FECFrameworkReceiverConvolutional) handleRepairSymbol(symbol *fec.RepairSymbol, fecPayloadID protocol.FECPayloadID) {
	_, ok := f.equations[fecPayloadID]

	if !ok {
		equation := fec.NewEquation(symbol)
		f.equations[fecPayloadID] = equation

		// Only utilize this symbols if we know the source symbols it encodes
		if f.canUseESID(equation.ID()) {
			f.applyRepairSymbol(fecPayloadID)
		} else {
			f.waitingRepairSymbols.insert(fecPayloadID)
		}
	}
}

func (f *FECFrameworkReceiverConvolutional) applyRepairSymbol(fecPayloadID protocol.FECPayloadID) {
	esid := fecPayloadID.GetConvolutionalEncodingSymbolID()
	equation := f.equations[fecPayloadID]

	if f.highestVarID < equation.ID() {
		f.highestVarID = equation.ID()
	}

	begin, end := equation.Bounds()

	var unknownVariables []protocol.FECEncodingSymbolID
	for i := begin; i <= end; i++ {
		if f.getFromVariablesBuffer(i) == nil {
			// the variable is unknown
			unknownVariables = append(unknownVariables, i)
		}
	}

	if len(unknownVariables) > 0 {
		f.fecScheme.AddEquation(equation, unknownVariables, f.variablesBuffer)
		f.updateStateForSomeEncodingSymbolID(esid)
	}

}

// TODO: waitingFrames should be an array of frames sorted by offset, absent frames should be nil in the array

// Looks in the waitingFECFrames and returns the full payload for the specified FEC group and removes the frames from the waitingFrames if all the frames are present.
// First return value: Returns nil if all the frames for this FEC group are not present in the waitingFrames
// Second return value: the number of packets concerned by this FEC Group if the return symbol has the number 0. Returns -1 otherwise.
// Third return value: the number of repair symbols concerned by this FEC Group if the return symbol has the number 0. Returns -1 otherwise.
func (f *FECFrameworkReceiverConvolutional) getRepairSymbolForSomeFECGroup(receivedFrame *wire.FECFrame) *fec.RepairSymbol {
	fecPayloadID := receivedFrame.GetFECPayloadID()
	waitingFrames, ok := f.waitingFECFrames[fecPayloadID]
	if !ok || len(waitingFrames) == 0 {
		// there are no waiting frames
		return nil
	}
	if len(waitingFrames) <= int(receivedFrame.Offset) {
		delta := int(receivedFrame.Offset) - len(waitingFrames)
		for i := 0; i <= delta; i++ {
			waitingFrames = append(waitingFrames, make(map[protocol.FecFrameOffset]*wire.FECFrame))
		}
		f.waitingFECFrames[fecPayloadID] = waitingFrames
	}
	waitingFramesForSymbol := waitingFrames[receivedFrame.RepairSymbolNumber]
	if len(waitingFramesForSymbol) == 0 {
		// there are no waiting frames
		return nil
	}
	if len(waitingFramesForSymbol) == 1 {
		if !receivedFrame.FinBit || receivedFrame.Offset != 0 {
			// there is only one waiting (which is the receivedFrame) frame which does not contain a full symbol, so obviously we cannot return symbols
			return nil
		} else {
			// there is only one FEC Frame, which contains a full symbol and has the FinBit set so return the symbol
			delete(f.waitingFECFrames, fecPayloadID)
			symbol := &fec.RepairSymbol{
				FECSchemeSpecific:     receivedFrame.FECSchemeSpecific,
				EncodingSymbolID:      receivedFrame.EncodingSymbolID,
				SymbolNumber:          receivedFrame.RepairSymbolNumber,
				Data:                  receivedFrame.Data,
				NumberOfPackets:       receivedFrame.NumberOfPackets,
				NumberOfRepairSymbols: receivedFrame.NumberOfRepairSymbols,
			}
			return symbol
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
		return nil
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
		f.waitingFECFrames[fecPayloadID][receivedFrame.RepairSymbolNumber] = nil
		//delete(f.waitingFECFrames[fecPayloadID], receivedFrame.RepairSymbolNumber)
		var nPackets, nRepairSymbols byte
		if orderedFrames[0] != nil {
			nPackets = orderedFrames[0].NumberOfPackets
			nRepairSymbols = orderedFrames[0].NumberOfRepairSymbols
		}
		return &fec.RepairSymbol{
			FECSchemeSpecific:     receivedFrame.FECSchemeSpecific,
			EncodingSymbolID:      receivedFrame.EncodingSymbolID,
			Data:                  payloadData,
			SymbolNumber:          receivedFrame.RepairSymbolNumber,
			NumberOfPackets:       nPackets,
			NumberOfRepairSymbols: nRepairSymbols,
		}
	}
}

func (f *FECFrameworkReceiverConvolutional) putWaitingFrame(frame *wire.FECFrame) {
	// add frame if not already present
	waitingFrames := f.waitingFECFrames[frame.GetFECPayloadID()]

	// ensure to have place for this repair symbol
	if len(waitingFrames) <= int(frame.RepairSymbolNumber) {
		delta := int(frame.RepairSymbolNumber) - len(waitingFrames)
		for i := 0; i <= delta; i++ {
			waitingFrames = append(waitingFrames, make(map[protocol.FecFrameOffset]*wire.FECFrame))
		}
		f.waitingFECFrames[frame.GetFECPayloadID()] = waitingFrames
	}

	waitingFramesForSymbol := waitingFrames[frame.RepairSymbolNumber]
	if _, ok := waitingFramesForSymbol[frame.Offset]; !ok {
		waitingFramesForSymbol[frame.Offset] = frame
	}
}

type buffer struct {
	head      *bufferNode // FIFO queue which will be used to remove old fecBuffers which has not been normally removed (never used, maybe because of the loss of more than one packet)
	tail      *bufferNode
	size      uint
	maxSize   uint
	fecGroups map[protocol.FECPayloadID]*fec.FECBlock
}

type bufferNode struct {
	fecGroup     protocol.FECBlockNumber
	fecPayloadID protocol.FECPayloadID
	next         *bufferNode
}

func newBuffer(maxSize uint) *buffer {
	return &buffer{
		nil,
		nil,
		0,
		maxSize,
		make(map[protocol.FECPayloadID]*fec.FECBlock),
	}
}

func (b *buffer) addFECGroup(group *fec.FECBlock, payloadID protocol.FECPayloadID) {
	number := group.FECBlockNumber
	if b.size == b.maxSize {
		toRemove := b.head
		b.head = b.head.next
		delete(b.fecGroups, toRemove.fecPayloadID)
		b.size--
	}
	newNode := &bufferNode{number, payloadID, nil}
	if b.size == 0 {
		b.tail = newNode
		b.head = newNode
	} else {
		b.tail.next = newNode
		b.tail = newNode
	}
	b.fecGroups[payloadID] = group
	b.size++
}

// TODO: should return true if it has been added, and false if not.
func (b *buffer) addPacketInFECGroup(packet []byte, header *wire.Header, id protocol.FECPayloadID) (err error) {
	group, ok := b.fecGroups[id]
	if ok {
		group.AddPacket(packet, header)
	}
	return
}

type byFECPayloadID []*fec.Equation

func (a byFECPayloadID) Len() int           { return len(a) }
func (a byFECPayloadID) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byFECPayloadID) Less(i, j int) bool { return a[i].ID() < a[j].ID() }
