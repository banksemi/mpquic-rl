package quic

import (
	"math"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/sedpf"
)

type SEDPF_FECStrategy int

const LossBasedFECPath SEDPF_FECStrategy = 0
const RTTBasedFECPath SEDPF_FECStrategy = 1
const SEDPFBasedFECPath SEDPF_FECStrategy = 2
const ArrivalBasedFECPath SEDPF_FECStrategy = 3

type SEDPFScheduler struct {
	nextZUpdate time.Time
	fecStrategy SEDPF_FECStrategy
	iodStrategy IOD_Strategy
}

var SEDPFSchedulerSingleton SEDPFScheduler = SEDPFScheduler{
	nextZUpdate: time.Now(),
	fecStrategy: LossBasedFECPath,
	iodStrategy: SEDPFBasedIOD,
}

func (sched *SEDPFScheduler) updateZ() {
	for _, p := range sedpf.Paths {
		p.UpdateZ()
	}
}

func getEstimatedLossRate(packetsBetweenTwoLosses, currentPacketsSinceLastLost float64) float64 {
	if packetsBetweenTwoLosses == -1 {
		return 0.0
	}
	return 1.0 / math.Max(packetsBetweenTwoLosses, currentPacketsSinceLastLost) // MPTCP LAMPS
}

func (sched *SEDPFScheduler) SelectFECPathLossBased(s sessionI) *path {
	paths := s.Paths()

	var selectedPath *path
	var highestLossRate = math.Inf(-1)

	for pathID, pathInfo := range sedpf.Paths {
		if pathID == protocol.InitialPathID {
			continue
		}

		path := paths[pathID]

		if !path.SendingAllowed() {
			continue
		}

		if path.potentiallyFailed.Get() {
			continue
		}

		if !pathInfo.ReceivedEnoughMeasurements() {
			return path
		}

		oSender := s.PathManager().oliaSenders[pathID]
		currentLossRate := getEstimatedLossRate(oSender.GetSmoothedBytesBetweenTwoLosses(), float64(oSender.GetCurrentPacketsSinceLastLoss()))

		if currentLossRate > highestLossRate {
			highestLossRate = currentLossRate
			selectedPath = path
		}
	}

	return selectedPath
}

func (sched *SEDPFScheduler) SelectFECPathLatencyBased(s sessionI) *path {
	paths := s.Paths()

	var selectedPath *path
	var highestRTT time.Duration

	for pathID, pathInfo := range sedpf.Paths {
		if pathID == protocol.InitialPathID {
			continue
		}

		path := paths[pathID]

		if !path.SendingAllowed() {
			continue
		}

		if path.potentiallyFailed.Get() {
			continue
		}

		if !pathInfo.ReceivedEnoughMeasurements() {
			return path
		}

		rtt := path.rttStats.SmoothedRTT()

		if highestRTT == 0 || highestRTT < rtt {
			highestRTT = rtt
			selectedPath = path
		}
	}

	return selectedPath
}

func (sched *SEDPFScheduler) SelectFECPathArrivalBased(s sessionI) *path {
	paths := s.Paths()

	var maxEPath *path
	var maxE float64 = math.Inf(-1)

	for pathID, p := range sedpf.Paths {
		if pathID == protocol.InitialPathID {
			continue
		}

		path := paths[pathID]

		if !path.SendingAllowed() {
			continue
		}

		if path.potentiallyFailed.Get() {
			continue
		}

		if !p.ReceivedEnoughMeasurements() {
			return path
		}

		// XXX
		packetSize := protocol.MaxPacketSize

		e := math.Max(sedpf.Max(
			// XXX here I just expect to have two paths available,
			// this currently only holds true in the experiment setup
			sedpf.Paths[1].Z.Addf(p.ExpectedArrivalDelta(packetSize)),
			sedpf.Paths[2].Z.Addf(p.ExpectedArrivalDelta(packetSize)),
		).Mean, p.ExpectedArrivalNext(packetSize))

		//fmt.Printf("E %f on PATH %d\n", e, id)

		if e > maxE {
			maxE = e
			maxEPath = path
		}
	}

	return maxEPath
}

func (sched *SEDPFScheduler) SelectPathLowRTT(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path, sch *scheduler) *path {
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

func (sched *SEDPFScheduler) SelectPath(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path, sch *scheduler) *path {
	paths := s.Paths()

	if len(paths) < 2 {
		if !hasRetransmission && !paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		// scheduler does not know multiple paths yet, always return initial
		// path id
		return paths[protocol.InitialPathID]
	}

	var minE float64 = math.Inf(1)
	var minEPath *path

	for _, p := range sedpf.Paths {
		p.UpdateZ()
	}
	/*
		// update the Z_p values every 0.5 seconds
		// same frequency as the reference implementation right now
		if sched.nextZUpdate.Before(time.Now()) {
			sched.nextZUpdate = time.Now().Add(time.Millisecond * time.Duration(500))
			for _, p := range sedpf.Paths {
				p.UpdateZ()
			}
		}
	*/

	// S-EDPF sends FEC frames on the path with the highest loss rate
	// (erasure probability)
	if hasFECFrame && sched.fecStrategy != SEDPFBasedFECPath {
		switch sched.fecStrategy {
		case LossBasedFECPath:
			return sched.SelectFECPathLossBased(s)
		case RTTBasedFECPath:
			return sched.SelectFECPathLatencyBased(s)
		case ArrivalBasedFECPath:
			return sched.SelectFECPathArrivalBased(s)
		}
	}

pathsLoop:
	for id, p := range sedpf.Paths {
		path := paths[id]

		if id == protocol.InitialPathID {
			// do not use inital path id after scheduling started
			continue pathsLoop
		}

		if !hasRetransmission && !path.SendingAllowed() {
			continue pathsLoop
		}

		if path.potentiallyFailed.Get() {
			continue pathsLoop
		}

		if !p.ReceivedEnoughMeasurements() {
			// this path has not been used before
			// force using it once to receive a measurement for its properties
			return path
		}

		// XXX
		packetSize := protocol.MaxPacketSize

		e := math.Max(sedpf.Max(
			// XXX here I just expect to have two paths available,
			// this currently only holds true in the experiment setup
			sedpf.Paths[1].Z.Addf(p.ExpectedArrivalDelta(packetSize)),
			sedpf.Paths[2].Z.Addf(p.ExpectedArrivalDelta(packetSize)),
		).Mean, p.ExpectedArrivalNext(packetSize))

		//fmt.Printf("E %f on PATH %d\n", e, id)

		if e < minE {
			minE = e
			minEPath = path
		}
	}

	return minEPath
}
