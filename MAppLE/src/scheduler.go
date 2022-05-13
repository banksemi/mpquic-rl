package quic

import (
	"log"
	"time"

	"math/rand"

	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/fec"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"

	goldlog "github.com/aunum/log"
)

type schedulerInterface interface {
	selectPath(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path) *path
}

var _ schedulerInterface = &scheduler{}

type scheduler struct {
	// XXX Currently round-robin based, inspired from MPTCP scheduler
	quotas               map[protocol.PathID]uint
	lossRateScheduler    *lossBasedScheduler
	redundancyController fec.RedundancyController
	rlmemories 			map[protocol.PathID]*RLMemory
	nmBandwidth			*networkMonitor
}

func (sch *scheduler) setup() {
	sch.nmBandwidth = &networkMonitor{}
	sch.nmBandwidth.setup(50)
	sch.quotas = make(map[protocol.PathID]uint)
	sch.rlmemories = make(map[protocol.PathID]*RLMemory)
	if sch.redundancyController != nil {
		sch.lossRateScheduler = newLossBasedScheduler(sch.quotas, sch.redundancyController.GetNumberOfRepairSymbols())
	}
}

func (sch *scheduler) getRetransmission(s sessionI) (hasRetransmission bool, retransmitPacket *ackhandler.Packet, pth *path) {
	// check for retransmissions first
	for {
		// TODO add ability to reinject on another path
		// XXX We need to check on ALL paths if any packet should be first retransmitted
	retransmitLoop:
		for _, pthTmp := range s.Paths() {
			retransmitPacket = pthTmp.sentPacketHandler.DequeuePacketForRetransmission()
			// BUG
			// DequeuePacketForRetransmissions seems to return even Acked packets
			//retransmitPacket = nil
			if retransmitPacket != nil {
				pth = pthTmp
				break retransmitLoop
			}
		}
		if retransmitPacket == nil {
			break
		}
		hasRetransmission = true

		if retransmitPacket.EncryptionLevel != protocol.EncryptionForwardSecure {
			if s.IsHandshakeComplete() {
				// Don't retransmit handshake packets when the handshake is complete
				continue
			}
			utils.Debugf("\tDequeueing handshake retransmission for packet 0x%x", retransmitPacket.PacketNumber)
			return
		}
		utils.Debugf("\tDequeueing retransmission of packet 0x%x from path %d", retransmitPacket.PacketNumber, pth.pathID)
		goldlog.Infof("\tDequeueing retransmission of packet %d from path %d", retransmitPacket.PacketNumber, pth.pathID)
		// resend the frames that were in the packet
		for _, frame := range retransmitPacket.GetFramesForRetransmission() {
			// TODO: only retransmit WINDOW_UPDATEs if they actually enlarge the window
			switch f := frame.(type) {
			case *wire.StreamFrame:
				s.GetStreamFramer().AddFrameForRetransmission(f)
			case *wire.PathsFrame:
				// Schedule a new PATHS frame to send
				s.SchedulePathsFrame()
			case *wire.FECFrame:
			default:
				s.GetPacker().QueueControlFrame(frame, pth)
			}
		}
	}
	return
}

func (sch *scheduler) selectPathRoundRobin(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path) *path {
	if sch.quotas == nil {
		sch.setup()
	}

	paths := s.Paths()

	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(paths) <= 1 {
		if !hasRetransmission && !paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return paths[protocol.InitialPathID]
	}

	// TODO cope with decreasing number of paths (needed?)
	var selectedPath *path
	var lowerQuota, currentQuota uint
	var ok bool

	// Max possible value for lowerQuota at the beginning
	lowerQuota = ^uint(0)

pathLoop:
	for pathID, pth := range paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		// If this path is potentially failed, do no consider it for sending
		if pth.potentiallyFailed.Get() {
			// comment this line as it blocked some mininet experiments
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		/// use the FEC path if it exists
		if pth.fec.Get() {
			if hasFECFrame {
				return pth
			}
			continue
		}

		currentQuota, ok = sch.quotas[pathID]
		if !ok {
			sch.quotas[pathID] = 0
			currentQuota = 0
		}

		if currentQuota < lowerQuota {
			selectedPath = pth
			lowerQuota = currentQuota
		}
	}

	return selectedPath

}

func (sch *scheduler) selectPathLowLatency(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	paths := s.Paths()
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(paths) <= 1 {
		if !hasRetransmission && !paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var selectedPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	selectedPathID := protocol.PathID(255)

	considerBackup := false
	considerPf := false
	needBackup := true
	havePf := false

pathLoop:
	for pathID, pth := range paths {
		// If this path is potentially failed, do not consider it for sending
		if !considerPf && pth.potentiallyFailed.Get() {
			havePf = true
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		if !considerBackup && pth.backup.Get() {
			continue pathLoop
		}

		// At least one non-backup path is active and did not faced RTO
		if !pth.facedRTO.Get() {
			needBackup = false
		}

		// It the preferred path never faced RTO, and this one did, then ignore it
		if selectedPath != nil && !selectedPath.facedRTO.Get() && pth.facedRTO.Get() {
			continue
		}

		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		currentRTT = pth.rttStats.SmoothedRTT()

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerRTT != 0 && currentRTT == 0 {
			continue pathLoop
		}

		// Case if we have multiple paths unprobed
		if currentRTT == 0 {
			currentQuota, ok := sch.quotas[pathID]
			if !ok {
				sch.quotas[pathID] = 0
				currentQuota = 0
			}
			lowerQuota, _ := sch.quotas[selectedPathID]
			if selectedPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT != 0 && lowerRTT != 0 && selectedPath != nil && currentRTT >= lowerRTT {
			continue pathLoop
		}

		// Update
		lowerRTT = currentRTT
		selectedPath = pth
		selectedPathID = pathID
	}

	if !considerBackup && needBackup {
		// Restart decision, but consider backup paths also, even if an active path was selected
		// Because all current active paths might not be reliable...
		considerBackup = true
		goto pathLoop
	}

	if selectedPath == nil && considerBackup && havePf && !considerPf {
		// All paths are potentially failed... Try to resent!
		considerPf = true
		goto pathLoop
	}

	return selectedPath
}

func (sch *scheduler) selectPathHighestRemainingBytes(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path) *path {
	paths := s.Paths()
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(paths) <= 1 {
		if !hasRetransmission && !paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var selectedPath *path
	var higherRemainingBytes int64 = -1
	var currentRemainingBytes int64 = -1
	selectedPathID := protocol.PathID(255)

	considerBackup := false
	considerPf := false
	needBackup := true
	havePf := false

	mapRemainingBytes := make(map[protocol.PathID]int64)

	var totalRemainingBytes uint64 = 0
	var higherRTT time.Duration
	var selectedPathRTT time.Duration
pathLoop:
	for pathID, pth := range paths {
		// If this path is potentially failed, do not consider it for sending
		if !considerPf && pth.potentiallyFailed.Get() {
			havePf = true
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		if !considerBackup && pth.backup.Get() {
			continue pathLoop
		}

		// At least one non-backup path is active and did not faced RTO
		if !pth.facedRTO.Get() {
			needBackup = false
		}

		// It the preferred path never faced RTO, and this one did, then ignore it
		if selectedPath != nil && !selectedPath.facedRTO.Get() && pth.facedRTO.Get() {
			continue
		}

		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		oSender := s.PathManager().oliaSenders[pth.pathID]
		//TODO: use also the bytesinflight
		pathRTT := pth.rttStats.SmoothedRTT()
		currentCwin := oSender.GetCongestionWindow()
		currentBytesInFlight := pth.sentPacketHandler.GetBytesInFlight()
		if currentBytesInFlight >= currentCwin {
			currentRemainingBytes = 0
		} else {
			currentRemainingBytes = int64(currentCwin - currentBytesInFlight)
		}

		mapRemainingBytes[pathID] = currentRemainingBytes
		totalRemainingBytes += uint64(currentRemainingBytes)

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if higherRemainingBytes != -1 && pathRTT == 0 {
			continue pathLoop
		}

		// Case if we have multiple paths unprobed
		if pathRTT == 0 {
			currentQuota, ok := sch.quotas[pathID]
			if !ok {
				sch.quotas[pathID] = 0
				currentQuota = 0
			}
			lowerQuota, _ := sch.quotas[selectedPathID]
			if selectedPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if pathRTT != 0 && pathRTT > higherRTT {
			higherRTT = pathRTT
		}

		if pathRTT != 0 && higherRemainingBytes != -1 && selectedPath != nil && currentRemainingBytes <= higherRemainingBytes {
			continue pathLoop
		}

		// Update
		higherRemainingBytes = currentRemainingBytes
		selectedPath = pth
		selectedPathID = pathID
		selectedPathRTT = pathRTT
	}

	if !considerBackup && needBackup {
		// Restart decision, but consider backup paths also, even if an active path was selected
		// Because all current active paths might not be reliable...
		considerBackup = true
		goto pathLoop
	}

	if selectedPath == nil && considerBackup && havePf && !considerPf {
		// All paths are potentially failed... Try to resent!
		considerPf = true
		goto pathLoop
	}

	if totalRemainingBytes > 0 {
		// random pick
		randomThreshold := rand.Int63n(int64(totalRemainingBytes))
		for pathID, val := range mapRemainingBytes {
			if val >= randomThreshold {
				return paths[pathID]
			}
			randomThreshold -= val
		}
	}

	compensate := false
	if compensate && higherRTT > selectedPathRTT {
		// compensate to perceive equal RTTs at the receiver side (we assume OWD = RTT/2)
		time.Sleep(time.Duration((higherRTT.Nanoseconds() - selectedPathRTT.Nanoseconds()) / 2))
	}
	return selectedPath
}

func (sch *scheduler) selectPathLowestLossRate(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path) *path {

	paths := s.Paths()
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(paths) <= 1 {
		if !hasRetransmission && !paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return paths[protocol.InitialPathID]
	}

	hasOliaSender := false
	var oSender *congestion.OliaSender
	if fromPth != nil {
		oSender, hasOliaSender = s.PathManager().oliaSenders[fromPth.pathID]
	}
	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && (!hasOliaSender || oSender.GetSmoothedBytesBetweenTwoLosses() == 0) {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var selectedPath *path
	var lowerMetric float64 = 2
	var metric float64 = 2
	selectedPathID := protocol.PathID(255)

	considerBackup := false
	considerPf := false
	needBackup := true
	havePf := false

pathLoop:
	for pathID, pth := range paths {
		// If this path is potentially failed, do not consider it for sending
		if !considerPf && pth.potentiallyFailed.Get() {
			havePf = true
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		if !considerBackup && pth.backup.Get() {
			continue pathLoop
		}

		// At least one non-backup path is active and did not faced RTO
		if !pth.facedRTO.Get() {
			needBackup = false
		}

		// It the preferred path never faced RTO, and this one did, then ignore it
		if selectedPath != nil && !selectedPath.facedRTO.Get() && pth.facedRTO.Get() {
			continue
		}

		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}
		pathRTT := pth.rttStats.SmoothedRTT()
		oSender := s.PathManager().oliaSenders[pth.pathID]
		maxN1N2 := float64(oSender.GetSmoothedBytesBetweenTwoLosses())
		metric = ((maxN1N2 + 1) / (2 * (maxN1N2 - 1))) * float64(pth.rttStats.SmoothedRTT())
		//metric = getEstimatedLossRate(oSender.GetSmoothedBytesBetweenTwoLosses(), float64(oSender.GetCurrentPacketsSinceLastLoss()))

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerMetric != 2 && pathRTT == 0 {
			continue pathLoop
		}

		// Case if we have multiple paths unprobed
		if pathRTT == 0 {
			currentQuota, ok := sch.quotas[pathID]
			if !ok {
				sch.quotas[pathID] = 0
				currentQuota = 0
			}
			lowerQuota, _ := sch.quotas[selectedPathID]
			if selectedPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if pathRTT != 0 && lowerMetric != 2 && selectedPath != nil && metric >= lowerMetric {
			continue pathLoop
		}

		// Update
		lowerMetric = metric
		selectedPath = pth
		selectedPathID = pathID
	}

	if !considerBackup && needBackup {
		// Restart decision, but consider backup paths also, even if an active path was selected
		// Because all current active paths might not be reliable...
		considerBackup = true
		goto pathLoop
	}

	if selectedPath == nil && considerBackup && havePf && !considerPf {
		// All paths are potentially failed... Try to resent!
		considerPf = true
		goto pathLoop
	}

	return selectedPath
}

func (sch *scheduler) selectPathS_EDPF(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path) *path {
	return SEDPFSchedulerSingleton.SelectPath(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth, sch)
}

// Lock of s.paths must be held
func (sch *scheduler) selectPath(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path) *path {
	switch s.GetConfig().SchedulingScheme {
	case protocol.SchedRR:
		return sch.selectPathRoundRobin(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
	case protocol.SchedLossRate:
		//sch.lossRateScheduler.maxNumberOfRepairSymbols = sch.redundancyController.GetNumberOfRepairSymbols()
		//return sch.lossRateScheduler.selectPath(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
		return sch.selectPathLowestLossRate(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
	case protocol.SchedLowLatency:
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	case protocol.SchedHighestRemaining:
		return sch.selectPathHighestRemainingBytes(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
	case protocol.SchedReferenceS_EDPF:
		SEDPFSchedulerSingleton.fecStrategy = LossBasedFECPath
		return sch.selectPathS_EDPF(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
	case protocol.SchedRTTS_EDPF:
		SEDPFSchedulerSingleton.fecStrategy = RTTBasedFECPath
		return sch.selectPathS_EDPF(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
	case protocol.SchedSimpleS_EDPF:
		SEDPFSchedulerSingleton.fecStrategy = SEDPFBasedFECPath
		return sch.selectPathS_EDPF(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
	case protocol.SchedArrivalS_EDPF:
		SEDPFSchedulerSingleton.fecStrategy = ArrivalBasedFECPath
		return sch.selectPathS_EDPF(s, hasRetransmission, hasStreamRetransmission, hasFECFrame, fromPth)
	case protocol.SchedSingle:
		return s.Paths()[0]
	case protocol.SchedRL:
		return sch.selectPathReinforcementLearning(s, hasRetransmission, hasStreamRetransmission, fromPth)
	default:
		panic("unknown scheduler selected")
	}
}

func (sch *scheduler) preparePacketForSending(s sessionI, pth *path, allowFEC bool) (*packedPacket, error) {
	// add a retransmittable frame
	if pth.sentPacketHandler.ShouldSendRetransmittablePacket() {
		s.GetPacker().QueueControlFrame(&wire.PingFrame{}, pth)
	}

	fecPayloadIDOfPacket := s.GetFECFrameworkSender().GetNextSourceFECPayloadID()

	return s.GetPacker().PackPacket(pth, fecPayloadIDOfPacket, allowFEC)
}

func (sch *scheduler) performPreparedPacketSending(s sessionI, packet *packedPacket, windowUpdateFrames []wire.Frame, pth *path) (*ackhandler.Packet, bool, error) {
	if packet == nil {
		return nil, false, nil
	}

	if err := s.sendPackedPacket(packet, pth); err != nil {
		return nil, false, err
	}

	// send every window update twice
	for _, f := range windowUpdateFrames {
		s.GetPacker().QueueControlFrame(f, pth)
	}

	// Provide some logging if it is the last packet
	for _, frame := range packet.frames {
		switch frame := frame.(type) {
		case *wire.StreamFrame:
			if frame.FinBit {
				// Last packet to send on the stream, print stats
				utils.Infof("Info for stream %x of %x", frame.StreamID, s.GetConnectionID())
				for pathID, pth := range s.Paths() {
					sntPkts, sntRetrans, sntLost := pth.sentPacketHandler.GetStatistics()
					rcvPkts, recoveredPkts := pth.receivedPacketHandler.GetStatistics()
					utils.Infof("Path %x: sent %d retrans %d lost %d; rcv %d, recovered %d", pathID, sntPkts, sntRetrans, sntLost, rcvPkts, recoveredPkts)
				}
			}
		default:
		}
	}

	pkt := &ackhandler.Packet{
		PacketNumber:    packet.header.PacketNumber,
		Frames:          packet.frames,
		Length:          protocol.ByteCount(len(packet.raw)),
		EncryptionLevel: packet.encryptionLevel,
	}

	return pkt, true, nil
}

// Lock of s.paths must be free
func (sch *scheduler) ackRemainingPaths(s sessionI, totalWindowUpdates []wire.Frame) error {
	// Either we run out of data, or CWIN of usable paths are full
	// Send ACKs on paths not yet used, if needed. Either we have no data to send and
	// it will be a pure ACK, or we will have data in it, but the CWIN should then
	// not be an issue.
	// get WindowUpdate frames
	// this call triggers the flow controller to increase the flow control windows, if necessary
	windowUpdates := totalWindowUpdates
	if len(windowUpdates) == 0 {
		windowUpdates = s.getWindowUpdates(s.GetPeerBlocked())
	}
	packer := s.GetPacker()
	for _, pthTmp := range s.Paths() {
		ackTmp := pthTmp.GetAckFrame()
		for _, f := range windowUpdates {
			packer.QueueControlFrame(f, pthTmp)
		}
		if ackTmp != nil || len(windowUpdates) > 0 {
			if pthTmp.pathID == protocol.InitialPathID && ackTmp == nil {
				continue
			}
			swf := pthTmp.GetStopWaitingFrame(false)
			if swf != nil {
				packer.QueueControlFrame(swf, pthTmp)
			}
			packer.QueueControlFrame(ackTmp, pthTmp)
			// XXX (QDC) should we instead call PackPacket to provides WUFs?
			var packet *packedPacket
			var err error
			if ackTmp != nil {
				// Avoid internal error bug
				packet, err = packer.PackAckPacket(pthTmp)
			} else {
				fpid := s.GetFECFrameworkSender().GetNextSourceFECPayloadID()
				packet, err = packer.PackPacket(pthTmp, fpid, true)
			}
			if err != nil {
				return err
			}
			err = s.sendPackedPacket(packet, pthTmp)
			if err != nil {
				return err
			}
		}
	}
	s.SetPeerBlocked(false)
	return nil
}

func (sch *scheduler) sendLoopOutOfOrder(s sessionI, windowUpdates []wire.Frame) error {
	packer := s.GetPacker()
	streamFramer := s.GetStreamFramer()
	fecFramer := s.GetFECFramer()

	// Repeatedly try sending until we don't have any more data, or run out of the congestion window
	for {
		// We first check for retransmissions
		hasRetransmission, retransmitHandshakePacket, fromPth := sch.getRetransmission(s)
		// XXX There might still be some stream frames to be retransmitted
		hasStreamRetransmission := streamFramer.HasFramesForRetransmission()

		hasFECFrames := len(fecFramer.transmissionQueue) > 0

		{
			// If we have an handshake packet retransmission, do it directly
			if hasRetransmission && retransmitHandshakePacket != nil {
				// Select the path here
				// XXX on handshare retransmission, fall back on in-order path selection
				pth := sch.selectPath(s, hasRetransmission, hasStreamRetransmission, hasFECFrames, fromPth)

				stopWaitingFrame := pth.sentPacketHandler.GetStopWaitingFrame(true)
				packer.QueueControlFrame(stopWaitingFrame, pth)

				packet, err := packer.PackHandshakeRetransmission(retransmitHandshakePacket, pth)
				if err != nil {
					return err
				}
				if err = s.sendPackedPacket(packet, pth); err != nil {
					return err
				}
				continue
			}
		}

		// the scheduler selects the paths for the packets and their order
		var packetInfo []scheduledPacketInfo

		packetInfo, err := SEDPFSchedulerSingleton.SelectPathsAndOrder(s, hasRetransmission, hasStreamRetransmission, fromPth, sch)

		if err != nil {
			return err
		}

		if packetInfo == nil || len(packetInfo) == 0 {
			return sch.ackRemainingPaths(s, windowUpdates)
		}

		packetWindow := len(packetInfo)

		if packetWindow == 0 {
			windowUpdates = s.getWindowUpdates(false)
			return sch.ackRemainingPaths(s, windowUpdates)
		}

		var reachedEnd bool
		// iterate through the list of created packets once for each path
		// filter out only the packets to be sent on this path and transmit them
		// NOTE: using a goroutine DOES NOT give any benefits here
		for i := 0; i < packetWindow; i++ {
			packet := packetInfo[i].packet
			if packet == nil {
				reachedEnd = true
				continue
			}

			_, _, err := sch.performPreparedPacketSending(s, packet, windowUpdates, packetInfo[i].path)
			if err != nil {
				return err
			}
		}

		windowUpdates = nil
		if reachedEnd {
			// Sending buffer empty
			// Prevent sending empty packets
			return sch.ackRemainingPaths(s, windowUpdates)
		}

		// And try pinging on potentially failed paths
		if fromPth != nil && fromPth.potentiallyFailed.Get() {
			err := s.SendPing(fromPth)
			if err != nil {
				return err
			}
		}
	}
}

func (sch *scheduler) sendLoop(s sessionI, windowUpdates []wire.Frame) error {
	packer := s.GetPacker()
	streamFramer := s.GetStreamFramer()
	fecFramer := s.GetFECFramer()

	LockAgent()

	defer UnlockAgent()
	// Repeatedly try sending until we don't have any more data, or run out of the congestion window
	for {
		// We first check for retransmissions
		hasRetransmission, retransmitHandshakePacket, fromPth := sch.getRetransmission(s)
		// XXX There might still be some stream frames to be retransmitted
		hasStreamRetransmission := streamFramer.HasFramesForRetransmission()

		hasFECFrames := len(fecFramer.transmissionQueue) > 0

		// Select the path here
		pth := sch.selectPath(s, hasRetransmission, hasStreamRetransmission, hasFECFrames, fromPth)

		// XXX No more path available, should we have a new QUIC error message?
		if pth == nil {
			windowUpdates = s.getWindowUpdates(false)
			return sch.ackRemainingPaths(s, windowUpdates)
		}

		// If we have an handshake packet retransmission, do it directly
		if hasRetransmission && retransmitHandshakePacket != nil {
			stopWaitingFrame := pth.sentPacketHandler.GetStopWaitingFrame(true)
			packer.QueueControlFrame(stopWaitingFrame, pth)

			packet, err := packer.PackHandshakeRetransmission(retransmitHandshakePacket, pth)
			if err != nil {
				return err
			}
			if err = s.sendPackedPacket(packet, pth); err != nil {
				return err
			}
			continue
		}

		// XXX Some automatic ACK generation should be done someway
		var ack *wire.AckFrame
		var rcv *wire.RecoveredFrame

		ack = pth.GetAckFrame()
		if ack != nil {
			packer.QueueControlFrame(ack, pth)
		} else {
			rcv = pth.GetRecoveredFrame()
			if rcv != nil {
				packer.QueueControlFrame(rcv, pth)
			}
		}

		if ack != nil || hasStreamRetransmission {
			swf := pth.sentPacketHandler.GetStopWaitingFrame(hasStreamRetransmission)
			if swf != nil {
				packer.QueueControlFrame(swf, pth)
			}
		}

		if s.GetVersion() == protocol.VersionMP {
			// Also add ADD ADDRESS frames, if any
			for aaf := streamFramer.PopAddAddressFrame(); aaf != nil; aaf = streamFramer.PopAddAddressFrame() {
				log.Printf("ADDADDR: %s", aaf.Addr.String())
				packer.QueueControlFrame(aaf, pth)
			}

			// Also add REMOVE ADDRESS frames, if any
			for raf := streamFramer.PopRemoveAddressFrame(); raf != nil; raf = streamFramer.PopRemoveAddressFrame() {
				packer.QueueControlFrame(raf, pth)
			}

			// Also add PATHS frames, if any
			for pf := streamFramer.PopPathsFrame(); pf != nil; pf = streamFramer.PopPathsFrame() {
				packer.QueueControlFrame(pf, pth)
			}
		}
		packet, err := sch.preparePacketForSending(s, pth, true)
		if err != nil {
			return err
		}

		_, sent, err := sch.performPreparedPacketSending(s, packet, windowUpdates, pth)
		// Packet sent, so update its quota
		sch.quotas[pth.pathID]++

		if err != nil {
			return err
		}

		windowUpdates = nil
		if !sent {
			// Prevent sending empty packets
			return sch.ackRemainingPaths(s, windowUpdates)
		}

		if pth != nil {
			if (s.GetConfig().SchedulingScheme == protocol.SchedRL) {
				sch.storeStateAction(s, pth.pathID, packet.header.PacketNumber)
			}
			if (pth.lastpacketnumber + 1 != packet.header.PacketNumber) {
				pth.lastpacketnumber = packet.header.PacketNumber
				goldlog.Errorf("[Packet 번호 건너뜀] %d PacketNumber %d 이 존재하지 않음", pth.pathID, packet.header.PacketNumber - 1)
			} else {
				pth.lastpacketnumber = packet.header.PacketNumber
			}
		}
		
		// And try pinging on potentially failed paths
		if fromPth != nil && fromPth.potentiallyFailed.Get() {
			err = s.SendPing(fromPth)
			if err != nil {
				return err
			}
		}
	}
}

func (sch *scheduler) sendPacket(s sessionI) error {
	var pth *path

	// Update leastUnacked value of paths
	for _, pthTmp := range s.Paths() {
		pthTmp.SetLeastUnacked(pthTmp.sentPacketHandler.GetLeastUnacked())
	}

	packer := s.GetPacker()

	// Get MAX_DATA and MAX_STREAM_DATA frames
	// this call triggers the flow controller to increase the flow control windows, if necessary
	windowUpdates := s.getWindowUpdates(false)
	for _, f := range windowUpdates {
		packer.QueueControlFrame(f, pth)
	}

	// Perform this check now to avoid doing lot of time the computation
	for _, pthTmp := range s.Paths() {
		if pthTmp.sentPacketHandler.ComputeRTOTimeout() > time.Duration(500)*time.Millisecond {
			// Even if not
			pthTmp.facedRTO.Set(true)
		}
	}

	scheme := s.GetConfig().SchedulingScheme

	var iod bool
	if scheme == protocol.SchedS_IOD {
		iod = true
		SEDPFSchedulerSingleton.iodStrategy = SEDPFBasedIOD
	} else if scheme == protocol.SchedIOD {
		iod = true
		SEDPFSchedulerSingleton.iodStrategy = LowRTTBasedIOD
	} else if scheme == protocol.SchedO_IOD {
		iod = true
		SEDPFSchedulerSingleton.iodStrategy = OpportunisticIOD
	}

	if iod {
		return sch.sendLoopOutOfOrder(s, windowUpdates)
	} else {
		return sch.sendLoop(s, windowUpdates)
	}
}
