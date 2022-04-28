package quic

import (
	"github.com/lucas-clemente/quic-go/congestion"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"math"
)

type lossBasedScheduler struct {
	roundRobinPaths          []protocol.PathID
	isInRoundRobin           []bool
	indexRoundRobin          int
	punishedPaths            map[protocol.PathID]uint
	maxNumberOfRepairSymbols uint
	quotas                   map[protocol.PathID]uint
}

var _ schedulerInterface = &lossBasedScheduler{}

func newLossBasedScheduler(quotas map[protocol.PathID]uint, maxNumberOfRepairSymbols uint) *lossBasedScheduler {
	return &lossBasedScheduler{
		roundRobinPaths:          nil,
		indexRoundRobin:          -1,
		quotas:                   quotas,
		isInRoundRobin:           make([]bool, 255),
		punishedPaths:            make(map[protocol.PathID]uint),
		maxNumberOfRepairSymbols: maxNumberOfRepairSymbols,
	}
}

// pre: the index must be in the array
func removePathAtIndexFrom(idx int, slice []protocol.PathID) []protocol.PathID {
	if len(slice) <= 1 {
		return slice[:0]
	}
	for i := idx; i < len(slice)-1; i++ {
		slice[idx] = slice[idx+1]
	}
	return slice[:len(slice)-1]
}

func removePathFrom(path protocol.PathID, slice []protocol.PathID) []protocol.PathID {
	for i := 0; i < len(slice); i++ {
		if slice[i] == path {
			slice = removePathAtIndexFrom(i, slice)
			return slice
		}
	}
	return slice
}

func (sch *lossBasedScheduler) selectPath(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, hasFECFrame bool, fromPth *path) *path {

	getEstimatedLossRate := func(packetsBetweenTwoLosses, currentPacketsSinceLastLost float64) float64 {
		if packetsBetweenTwoLosses == -1 {
			return 0.0
		}
		return 1.0 / math.Max(packetsBetweenTwoLosses, currentPacketsSinceLastLost) // MPTCP LAMPS
	}

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
	var lowerLossRate float64 = 2
	var currentLossRate float64 = 2
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
			if sch.isInRoundRobin[pathID] {
				sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
				sch.isInRoundRobin[pathID] = false
			}
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			if sch.isInRoundRobin[pathID] {
				sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
				sch.isInRoundRobin[pathID] = false
			}
			continue pathLoop
		}

		if !considerBackup && pth.backup.Get() {
			if sch.isInRoundRobin[pathID] {
				sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
				sch.isInRoundRobin[pathID] = false
			}
			continue pathLoop
		}

		// At least one non-backup path is active and did not faced RTO
		if !pth.facedRTO.Get() {
			needBackup = false
		}

		// It the preferred path never faced RTO, and this one did, then ignore it
		if selectedPath != nil && !selectedPath.facedRTO.Get() && pth.facedRTO.Get() {
			if sch.isInRoundRobin[pathID] {
				sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
				sch.isInRoundRobin[pathID] = false
			}
			continue
		}

		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			if sch.isInRoundRobin[pathID] {
				sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
				sch.isInRoundRobin[pathID] = false
			}
			continue pathLoop
		}
		pathRTT := pth.rttStats.SmoothedRTT()
		oSender := s.PathManager().oliaSenders[pth.pathID]
		currentLossRate = getEstimatedLossRate(oSender.GetSmoothedBytesBetweenTwoLosses(), float64(oSender.GetCurrentPacketsSinceLastLoss())) //pth.rttStats.SmoothedRTT()
		currentBurstLength := oSender.GetEstimatedBurstLength()

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerLossRate != 2 && pathRTT == 0 {
			if sch.isInRoundRobin[pathID] {
				sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
				sch.isInRoundRobin[pathID] = false
			}
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
				if sch.isInRoundRobin[pathID] {
					sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
					sch.isInRoundRobin[pathID] = false
				}
				continue pathLoop
			}
		}

		if currentBurstLength <= 1.3*float64(sch.maxNumberOfRepairSymbols) {
			if !sch.isInRoundRobin[pth.pathID] {
				sch.isInRoundRobin[pth.pathID] = true
				sch.roundRobinPaths = append(sch.roundRobinPaths, pth.pathID)
			}
		}

		if pathRTT != 0 && lowerLossRate != 2 && selectedPath != nil && currentLossRate >= lowerLossRate {
			continue pathLoop
		}

		// Update
		lowerLossRate = currentLossRate
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

	pm := s.PathManager()
	//for _, pathID := range sch.roundRobinPaths {
	//	if sender, hasSender := pm.oliaSenders[pathID]; hasSender {
	//		lossRate := getEstimatedLossRate(sender.GetSmoothedBytesBetweenTwoLosses(), float64(sender.GetCurrentPacketsSinceLastLoss()))
	//		if lossRate > 2*currentLossRate {
	//			sch.roundRobinPaths = removePathFrom(pathID, sch.roundRobinPaths)
	//		}
	//	}
	//}

	if len(sch.roundRobinPaths) > 1 {
		for {
			sch.indexRoundRobin = (sch.indexRoundRobin + 1) % len(sch.roundRobinPaths)
			pthRR := sch.roundRobinPaths[sch.indexRoundRobin]
			if sender, hasSender := pm.oliaSenders[pthRR]; hasSender {
				punishedNumber := sch.punishedPaths[pthRR]
				lossRate := getEstimatedLossRate(sender.GetSmoothedBytesBetweenTwoLosses(), float64(sender.GetCurrentPacketsSinceLastLoss()))
				burstLength := sender.GetEstimatedBurstLength()
				if punishedNumber%5 == 4 {
					sch.punishedPaths[pthRR]++
					return paths[sch.roundRobinPaths[sch.indexRoundRobin]]
				} else if lossRate <= 2*currentLossRate && burstLength <= float64(sch.maxNumberOfRepairSymbols) {
					sch.punishedPaths[pthRR] = 0
					return paths[pthRR]
				} else {
					sch.punishedPaths[pthRR]++
				}
			}
		}
	} else if len(sch.roundRobinPaths) == 1 {
		sch.indexRoundRobin = 0
		return paths[sch.roundRobinPaths[0]]
	}

	return selectedPath
}
