package quic

import (
	"fmt"
	"os"
	"time"
	"math"
	"github.com/lucas-clemente/quic-go/ackhandler"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"math/rand"

	"bitbucket.com/marcmolla/gorl/agents"
	"bitbucket.com/marcmolla/gorl/types"
	"gonum.org/v1/gonum/mat"
)

const banditAlpha = 0.75
const banditDimension = 6

type scheduler struct {
	// XXX Currently round-robin based, inspired from MPTCP scheduler
	quotas map[protocol.PathID]uint
	// Selected scheduler
	SchedulerName string
	// Is training?
	Training bool
	// Training Agent
	TrainingAgent agents.TrainingAgent
	// Normal Agent
	Agent agents.Agent
    
	// Cached state for training
	cachedState		types.Vector
	cachedPathID	protocol.PathID

	AllowedCongestion int

	// async updated reward
	record	uint64
	episoderecord uint64
	statevector [6000]types.Vector
	packetvector [6000]uint64
	//rewardvector [6000]types.Output
	actionvector [6000]int
	recordDuration [6000]types.Output
	lastfiretime time.Time
	zz [6000]time.Time
	waiting    uint64

	// linUCB
	fe uint64
	se uint64
	MAaF [banditDimension][banditDimension]float64
    MAaS [banditDimension][banditDimension]float64
    MbaF [banditDimension]float64
	MbaS [banditDimension]float64
	featureone [6000]float64
	featuretwo [6000]float64
	featurethree [6000]float64
	featurefour [6000]float64
	featurefive [6000]float64
	featuresix [6000]float64
	// Retrans cache
	retrans				map[protocol.PathID] uint64

	// Write experiences
	DumpExp				bool
	DumpPath			string
	dumpAgent			experienceAgent

	// Reinforcement
	rlmemories 			map[protocol.PathID]*RLMemory
	nmBandwidth			*networkMonitor
}

func (sch *scheduler) setup() {
	sch.nmBandwidth = &networkMonitor{}
	sch.nmBandwidth.setup(50)
	sch.rlmemories = make(map[protocol.PathID]*RLMemory)

	sch.quotas = make(map[protocol.PathID]uint)
	sch.retrans = make(map[protocol.PathID]uint64)
	sch.waiting = 0

	//Read lin to buffer
	file, err := os.Open("/App/output/lin")
	if err != nil {
    	panic(err)
	}

	for i := 0; i < banditDimension; i++ {
		for j := 0; j < banditDimension; j++ {
			fmt.Fscanln(file, &sch.MAaF[i][j])
		}
	}
	for i := 0; i < banditDimension; i++ {
		for j := 0; j < banditDimension; j++ {
			fmt.Fscanln(file, &sch.MAaS[i][j])
		}
	}
	for i := 0; i < banditDimension; i++ {
		fmt.Fscanln(file, &sch.MbaF[i])
	}
	for i := 0; i < banditDimension; i++ {
		fmt.Fscanln(file, &sch.MbaS[i])
	}
	file.Close()

	//TODO: expose to config
	sch.DumpPath = "/tmp/"
	sch.dumpAgent.Setup()

	sch.cachedState = types.Vector{-1, -1}
	if sch.SchedulerName == "dqnAgent" {
		if sch.Training {
			sch.TrainingAgent = GetTrainingAgent("", "", "", 0.)
		} else {
			sch.Agent = GetAgent("", "")
		}
	}
}


func (sch *scheduler) getRetransmission(s *session) (hasRetransmission bool, retransmitPacket *ackhandler.Packet, pth *path) {
	// check for retransmissions first
	for {
		// TODO add ability to reinject on another path
		// XXX We need to check on ALL paths if any packet should be first retransmitted
		s.pathsLock.RLock()
	retransmitLoop:
		for _, pthTmp := range s.paths {
			retransmitPacket = pthTmp.sentPacketHandler.DequeuePacketForRetransmission()
			if retransmitPacket != nil {
				pth = pthTmp
				break retransmitLoop
			}
		}
		s.pathsLock.RUnlock()
		if retransmitPacket == nil {
			break
		}
		hasRetransmission = true

		if retransmitPacket.EncryptionLevel != protocol.EncryptionForwardSecure {
			if s.handshakeComplete {
				// Don't retransmit handshake packets when the handshake is complete
				continue
			}
			utils.Debugf("\tDequeueing handshake retransmission for packet 0x%x", retransmitPacket.PacketNumber)
			return
		}
		utils.Debugf("\tDequeueing retransmission of packet 0x%x from path %d", retransmitPacket.PacketNumber, pth.pathID)
		// resend the frames that were in the packet
		for _, frame := range retransmitPacket.GetFramesForRetransmission() {
			switch f := frame.(type) {
			case *wire.StreamFrame:
				s.streamFramer.AddFrameForRetransmission(f)
			case *wire.WindowUpdateFrame:
				// only retransmit WindowUpdates if the stream is not yet closed and the we haven't sent another WindowUpdate with a higher ByteOffset for the stream
				// XXX Should it be adapted to multiple paths?
				currentOffset, err := s.flowControlManager.GetReceiveWindow(f.StreamID)
				if err == nil && f.ByteOffset >= currentOffset {
					s.packer.QueueControlFrame(f, pth)
				}
			case *wire.PathsFrame:
				// Schedule a new PATHS frame to send
				s.schedulePathsFrame()
			default:
				s.packer.QueueControlFrame(frame, pth)
			}
		}
	}
	return
}

func (sch *scheduler) selectPathRoundRobin(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	if sch.quotas == nil {
		sch.setup()
	}

	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// TODO cope with decreasing number of paths (needed?)
	var selectedPath *path
	var lowerQuota, currentQuota uint
	var ok bool

	// Max possible value for lowerQuota at the beginning
	lowerQuota = ^uint(0)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		// If this path is potentially failed, do no consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
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

func (sch *scheduler) selectPathLowLatency(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	utils.Debugf("selectPathLowLatency")
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			utils.Debugf("Only initial path and sending not allowed without retransmission")
			utils.Debugf("SCH RTT - NIL")
			return nil
		}
		utils.Debugf("Only initial path and sending is allowed or has retransmission")
		utils.Debugf("SCH RTT - InitialPath")
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				utils.Debugf("has ret, has stream ret and sRTT == 0")
				utils.Debugf("SCH RTT - Selecting %d by low quota", pathID)
				return pth
			}
		}
	}

	var selectedPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	selectedPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			utils.Debugf("Discarding %d - no has ret and sending is not allowed ", pathID)
			continue pathLoop
		}

		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			utils.Debugf("Discarding %d - potentially failed", pathID)
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
			continue pathLoop
		}

		currentRTT = pth.rttStats.SmoothedRTT()

		// Prefer staying single-path if not blocked by current path
		// Don't consider this sample if the smoothed RTT is 0
		if lowerRTT != 0 && currentRTT == 0 {
			utils.Debugf("Discarding %d - currentRTT == 0 and lowerRTT != 0 ", pathID)
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
				utils.Debugf("Discarding %d - higher quota ", pathID)
				continue pathLoop
			}
		}

		if currentRTT != 0 && lowerRTT != 0 && selectedPath != nil && currentRTT >= lowerRTT {
			utils.Debugf("Discarding %d - higher SRTT ", pathID)
			continue pathLoop
		}

		// Update
		lowerRTT = currentRTT
		selectedPath = pth
		selectedPathID = pathID
	}
	utils.Debugf("SCH RTT - Selecting %d by low RTT: %f", selectedPathID, lowerRTT)
	return selectedPath
}

func (sch *scheduler) selectBLEST(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var bestPath *path
	var secondBestPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	var secondLowerRTT time.Duration
	bestPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
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
			lowerQuota, _ := sch.quotas[bestPathID]
			if bestPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT >= lowerRTT {
			if (secondLowerRTT == 0 || currentRTT < secondLowerRTT) && pth.SendingAllowed() {
				// Update second best available path
				secondLowerRTT = currentRTT
				secondBestPath = pth
			}
			if currentRTT != 0 && lowerRTT != 0 && bestPath != nil {
				continue pathLoop
			}
		}

		// Update
		lowerRTT = currentRTT
		bestPath = pth
		bestPathID = pathID
	}

	if bestPath == nil {
		if secondBestPath != nil {
			return secondBestPath
		}
		return nil
	}

	if hasRetransmission || bestPath.SendingAllowed() {
		return bestPath
	}

	if secondBestPath == nil {
		return nil
	}
	cwndBest := uint64(bestPath.sentPacketHandler.GetCongestionWindow())
	FirstCo := uint64(protocol.DefaultTCPMSS) * uint64(secondLowerRTT) * (cwndBest*2*uint64(lowerRTT) + uint64(secondLowerRTT) - uint64(lowerRTT))
	BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
	SecondCo := 2 * 1 * uint64(lowerRTT) * uint64(lowerRTT) * (uint64(BSend) - (uint64(secondBestPath.sentPacketHandler.GetBytesInFlight())+uint64(protocol.DefaultTCPMSS)))

	if (FirstCo > SecondCo) {
		return nil		
	} else {
		return secondBestPath
	}
}

func (sch *scheduler) selectECF(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var bestPath *path
	var secondBestPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	var secondLowerRTT time.Duration
	bestPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if !hasRetransmission && !pth.SendingAllowed() {
			continue pathLoop
		}

		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
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
			lowerQuota, _ := sch.quotas[bestPathID]
			if bestPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT >= lowerRTT {
			if (secondLowerRTT == 0 || currentRTT < secondLowerRTT) && pth.SendingAllowed() {
				// Update second best available path
				secondLowerRTT = currentRTT
				secondBestPath = pth
			}
			if currentRTT != 0 && lowerRTT != 0 && bestPath != nil {
				continue pathLoop
			}
		}

		// Update
		lowerRTT = currentRTT
		bestPath = pth
		bestPathID = pathID
	}

	if bestPath == nil {
		if secondBestPath != nil {
			return secondBestPath
		}
		return nil
	}

	if hasRetransmission || bestPath.SendingAllowed() {
		return bestPath
	}

	if secondBestPath == nil {
		return nil
	}

	var queueSize uint64
	getQueueSize := func(s *stream) (bool, error) {
		if s != nil {
			queueSize = queueSize + uint64(s.lenOfDataForWriting())
		}
		return true, nil
	}
	s.streamsMap.Iterate(getQueueSize)

	cwndBest := uint64(bestPath.sentPacketHandler.GetCongestionWindow())
	cwndSecond := uint64(secondBestPath.sentPacketHandler.GetCongestionWindow())
	deviationBest := uint64(bestPath.rttStats.MeanDeviation())
	deviationSecond := uint64(secondBestPath.rttStats.MeanDeviation())

	delta := deviationBest
	if deviationBest < deviationSecond {
		delta = deviationSecond
	}
	xBest := queueSize
	if queueSize < cwndBest {
		xBest = cwndBest
	}

	lhs := uint64(lowerRTT) * (xBest + cwndBest)
	rhs := cwndBest * (uint64(secondLowerRTT) + delta)
	if (lhs * 4) < ((rhs * 4) + sch.waiting*rhs){
		xSecond := queueSize
		if queueSize < cwndSecond {
			xSecond = cwndSecond
		}
		lhsSecond := uint64(secondLowerRTT) * xSecond
		rhsSecond := cwndSecond * (2*uint64(lowerRTT) + delta)
		if (lhsSecond > rhsSecond) {
				sch.waiting = 1
			    return nil
		} 
	} else {
		sch.waiting = 0
	}

	return secondBestPath
}

func (sch *scheduler) selectPathLowBandit(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var bestPath *path
	var secondBestPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	var secondLowerRTT time.Duration
	bestPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
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
			lowerQuota, _ := sch.quotas[bestPathID]
			if bestPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT >= lowerRTT {
			if (secondLowerRTT == 0 || currentRTT < secondLowerRTT) && pth.SendingAllowed() {
				// Update second best available path
				secondLowerRTT = currentRTT
				secondBestPath = pth
			}
			if currentRTT != 0 && lowerRTT != 0 && bestPath != nil {
				continue pathLoop
			}
		}

		// Update
		lowerRTT = currentRTT
		bestPath = pth
		bestPathID = pathID

	}
	
	//Get reward and Update Aa, ba
	if bestPath != nil && secondBestPath != nil {
		for sch.episoderecord < sch.record {
			// Get reward
			cureNum := uint64(0)
			curereward := float64(0)
			if sch.actionvector[sch.episoderecord] == 0 {
				cureNum = uint64(bestPath.sentPacketHandler.GetLeastUnacked() - 1)
			} else {
				cureNum = uint64(secondBestPath.sentPacketHandler.GetLeastUnacked() - 1)
			}
			if sch.packetvector[sch.episoderecord] <= cureNum {
				curereward = float64(protocol.DefaultTCPMSS)/float64(time.Since(sch.zz[sch.episoderecord]))
			} else {
				break
			}
			//Update Aa, ba
			feature := mat.NewDense(banditDimension, 1, nil)
			feature.Set(0, 0, sch.featureone[sch.episoderecord])
			feature.Set(1, 0, sch.featuretwo[sch.episoderecord])
			feature.Set(2, 0, sch.featurethree[sch.episoderecord])
			feature.Set(3, 0, sch.featurefour[sch.episoderecord])
			feature.Set(4, 0, sch.featurefive[sch.episoderecord])
			feature.Set(5, 0, sch.featuresix[sch.episoderecord])

			if sch.actionvector[sch.episoderecord] == 0 {
				rewardMul := mat.NewDense(banditDimension, 1, nil)
				rewardMul.Scale(curereward, feature)
				baF := mat.NewDense(banditDimension, 1, nil)
				for i := 0; i < banditDimension; i++ {
					baF.Set(i, 0, sch.MbaF[i])
				}
				baF.Add(baF, rewardMul)
				for i := 0; i < banditDimension; i++ {
					sch.MbaF[i] = baF.At(i, 0)
				}
				featureMul := mat.NewDense(banditDimension, banditDimension, nil)
				featureMul.Product(feature, feature.T())
				AaF := mat.NewDense(banditDimension, banditDimension, nil)
				for i := 0; i < banditDimension; i++ {
					for j := 0; j < banditDimension; j++ {
						AaF.Set(i, j, sch.MAaF[i][j])
					}
				}
				AaF.Add(AaF, featureMul)
				for i := 0; i < banditDimension; i++ {
					for j := 0; j < banditDimension; j++ {
						sch.MAaF[i][j] = AaF.At(i, j)
					}
				}
				sch.fe += 1
			} else {
				rewardMul := mat.NewDense(banditDimension, 1, nil)
				rewardMul.Scale(curereward, feature)
				baS := mat.NewDense(banditDimension, 1, nil)
				for i := 0; i < banditDimension; i++ {
					baS.Set(i, 0, sch.MbaS[i])
				}
				baS.Add(baS, rewardMul)
				for i := 0; i < banditDimension; i++ {
					sch.MbaS[i] = baS.At(i, 0)
				}
				featureMul := mat.NewDense(banditDimension, banditDimension, nil)
				featureMul.Product(feature, feature.T())
				AaS := mat.NewDense(banditDimension, banditDimension, nil)
				for i := 0; i < banditDimension; i++ {
					for j := 0; j < banditDimension; j++ {
						AaS.Set(i, j, sch.MAaS[i][j])
					}
				}
				AaS.Add(AaS, featureMul)
				for i := 0; i < banditDimension; i++ {
					for j := 0; j < banditDimension; j++ {
						sch.MAaS[i][j] = AaS.At(i, j)
					}
				}
				sch.se += 1
			}
			//Update pointer
			sch.episoderecord += 1
		}
	}

	if bestPath == nil {
	 	if secondBestPath != nil {
	 		return secondBestPath
		}
		if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
			return s.paths[protocol.InitialPathID]
	    }else{
	  		return nil
		}
	}
	if bestPath.SendingAllowed() {
		sch.waiting = 0
		return bestPath
	}
	if secondBestPath == nil {
		if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
			return s.paths[protocol.InitialPathID]
	    }else{
	  		return nil
		}
	}

	if hasRetransmission && secondBestPath.SendingAllowed() {
		return secondBestPath
	}
	if hasRetransmission {
		return s.paths[protocol.InitialPathID]
	}

	if sch.waiting == 1 {
		return nil
	} else {
		// Migrate from buffer to local variables
		AaF := mat.NewDense(banditDimension, banditDimension, nil)
		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				AaF.Set(i, j, sch.MAaF[i][j])
			}
		}
		AaS := mat.NewDense(banditDimension, banditDimension, nil)
		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				AaS.Set(i, j, sch.MAaS[i][j])
			}
		}
		baF := mat.NewDense(banditDimension, 1, nil)
		for i := 0; i < banditDimension; i++ {
			baF.Set(i, 0, sch.MbaF[i])
		}
		baS := mat.NewDense(banditDimension, 1, nil)
		for i := 0; i < banditDimension; i++ {
			baS.Set(i, 0, sch.MbaS[i])
		}

		//Features
		cwndBest := float64(bestPath.sentPacketHandler.GetCongestionWindow())
		cwndSecond := float64(secondBestPath.sentPacketHandler.GetCongestionWindow())
		BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
		inflightf := float64(bestPath.sentPacketHandler.GetBytesInFlight())
		inflights := float64(secondBestPath.sentPacketHandler.GetBytesInFlight())
		llowerRTT := bestPath.rttStats.LatestRTT()
		lsecondLowerRTT := secondBestPath.rttStats.LatestRTT()
		feature := mat.NewDense(banditDimension, 1, nil)
		if 0 < float64(lsecondLowerRTT) && 0 < float64(llowerRTT) {
			feature.Set(0, 0, cwndBest/float64(llowerRTT))
			feature.Set(2, 0, float64(BSend)/float64(llowerRTT))
			feature.Set(4, 0, inflightf/float64(llowerRTT))
			feature.Set(1, 0, inflights/float64(lsecondLowerRTT))
			feature.Set(3, 0, float64(BSend)/float64(lsecondLowerRTT))
			feature.Set(5, 0, cwndSecond/float64(lsecondLowerRTT))
		} else {
			feature.Set(0, 0, 0)
			feature.Set(2, 0, 0)
			feature.Set(4, 0, 0)
			feature.Set(1, 0, 0)
			feature.Set(3, 0, 0)
			feature.Set(5, 0, 0)
		}
		
		//Buffer feature for latter update
		sch.featureone[sch.record] = feature.At(0, 0)
		sch.featuretwo[sch.record] = feature.At(1, 0)
		sch.featurethree[sch.record] = feature.At(2, 0)
		sch.featurefour[sch.record] = feature.At(3, 0)
		sch.featurefive[sch.record] = feature.At(4, 0)
		sch.featuresix[sch.record] = feature.At(5, 0)

		//Obtain theta
		AaIF := mat.NewDense(banditDimension, banditDimension, nil)
		AaIF.Inverse(AaF)
		thetaF := mat.NewDense(banditDimension, 1, nil)
		thetaF.Product(AaIF, baF)

		AaIS := mat.NewDense(banditDimension, banditDimension, nil)
		AaIS.Inverse(AaS)
		thetaS := mat.NewDense(banditDimension, 1, nil)
		thetaS.Product(AaIS, baS)

		//Obtain bandit value
		thetaFPro := mat.NewDense(1, 1, nil)
		thetaFPro.Product(thetaF.T(), feature)
		featureFProOne := mat.NewDense(1, banditDimension, nil)
		featureFProOne.Product(feature.T(), AaIF)
		featureFProTwo := mat.NewDense(1, 1, nil)
		featureFProTwo.Product(featureFProOne, feature)

		thetaSPro := mat.NewDense(1, 1, nil)
		thetaSPro.Product(thetaS.T(), feature)
		featureSProOne := mat.NewDense(1, banditDimension, nil)
		featureSProOne.Product(feature.T(), AaIS)
		featureSProTwo := mat.NewDense(1, 1, nil)
		featureSProTwo.Product(featureSProOne, feature)

		//Make decision based on bandit value
		if (thetaSPro.At(0, 0) + banditAlpha*math.Sqrt(featureSProTwo.At(0, 0))) < (thetaFPro.At(0, 0) + banditAlpha*math.Sqrt(featureFProTwo.At(0, 0))) {
			sch.waiting = 1
			sch.zz[sch.record] = time.Now()
			sch.actionvector[sch.record] = 0
			sch.packetvector[sch.record] = bestPath.sentPacketHandler.GetLastPackets() + 1
			sch.record += 1
			return nil
		} else {
			sch.waiting = 0
			sch.zz[sch.record] = time.Now()
			sch.actionvector[sch.record] = 1
			sch.packetvector[sch.record] = secondBestPath.sentPacketHandler.GetLastPackets() + 1 
			sch.record += 1
			return secondBestPath
		}

	}

}

func (sch *scheduler) selectPathPeek(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	// FIXME Only works at the beginning... Cope with new paths during the connection
	if hasRetransmission && hasStreamRetransmission && fromPth.rttStats.SmoothedRTT() == 0 {
		// Is there any other path with a lower number of packet sent?
		currentQuota := sch.quotas[fromPth.pathID]
		for pathID, pth := range s.paths {
			if pathID == protocol.InitialPathID || pathID == fromPth.pathID {
				continue
			}
			// The congestion window was checked when duplicating the packet
			if sch.quotas[pathID] < currentQuota {
				return pth
			}
		}
	}

	var bestPath *path
	var secondBestPath *path
	var lowerRTT time.Duration
	var currentRTT time.Duration
	var secondLowerRTT time.Duration
	bestPathID := protocol.PathID(255)

pathLoop:
	for pathID, pth := range s.paths {
		// If this path is potentially failed, do not consider it for sending
		if pth.potentiallyFailed.Get() {
			continue pathLoop
		}

		// XXX Prevent using initial pathID if multiple paths
		if pathID == protocol.InitialPathID {
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
			lowerQuota, _ := sch.quotas[bestPathID]
			if bestPath != nil && currentQuota > lowerQuota {
				continue pathLoop
			}
		}

		if currentRTT >= lowerRTT {
			if (secondLowerRTT == 0 || currentRTT < secondLowerRTT) && pth.SendingAllowed() {
				// Update second best available path
				secondLowerRTT = currentRTT
				secondBestPath = pth
			}
			if currentRTT != 0 && lowerRTT != 0 && bestPath != nil {
				continue pathLoop
			}
		}

		// Update
		lowerRTT = currentRTT
		bestPath = pth
		bestPathID = pathID

	}	
	
	if bestPath == nil {
	 	if secondBestPath != nil {
	 		return secondBestPath
		}
		if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
			return s.paths[protocol.InitialPathID]
	    }else{
	  		return nil
		}
	}
	if bestPath.SendingAllowed() {
		sch.waiting = 0
		return bestPath
	}
	if secondBestPath == nil {
		if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
			return s.paths[protocol.InitialPathID]
	    }else{
	  		return nil
		}
	}

	if hasRetransmission && secondBestPath.SendingAllowed() {
		return secondBestPath
	}
	if hasRetransmission {
		return s.paths[protocol.InitialPathID]
	}

	if sch.waiting == 1 {
		return nil
	} else {
		// Migrate from buffer to local variables
		AaF := mat.NewDense(banditDimension, banditDimension, nil)
		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				AaF.Set(i, j, sch.MAaF[i][j])
			}
		}
		AaS := mat.NewDense(banditDimension, banditDimension, nil)
		for i := 0; i < banditDimension; i++ {
			for j := 0; j < banditDimension; j++ {
				AaS.Set(i, j, sch.MAaS[i][j])
			}
		}
		baF := mat.NewDense(banditDimension, 1, nil)
		for i := 0; i < banditDimension; i++ {
			baF.Set(i, 0, sch.MbaF[i])
		}
		baS := mat.NewDense(banditDimension, 1, nil)
		for i := 0; i < banditDimension; i++ {
			baS.Set(i, 0, sch.MbaS[i])
		}

		//Features
		cwndBest := float64(bestPath.sentPacketHandler.GetCongestionWindow())
		cwndSecond := float64(secondBestPath.sentPacketHandler.GetCongestionWindow())
		BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
		inflightf := float64(bestPath.sentPacketHandler.GetBytesInFlight())
		inflights := float64(secondBestPath.sentPacketHandler.GetBytesInFlight())
		llowerRTT := bestPath.rttStats.LatestRTT()
		lsecondLowerRTT := secondBestPath.rttStats.LatestRTT()
		feature := mat.NewDense(banditDimension, 1, nil)
		if 0 < float64(lsecondLowerRTT) && 0 < float64(llowerRTT) {
			feature.Set(0, 0, cwndBest/float64(llowerRTT))
			feature.Set(2, 0, float64(BSend)/float64(llowerRTT))
			feature.Set(4, 0, inflightf/float64(llowerRTT))
			feature.Set(1, 0, inflights/float64(lsecondLowerRTT))
			feature.Set(3, 0, float64(BSend)/float64(lsecondLowerRTT))
			feature.Set(5, 0, cwndSecond/float64(lsecondLowerRTT))
		} else {
			feature.Set(0, 0, 0)
			feature.Set(2, 0, 0)
			feature.Set(4, 0, 0)
			feature.Set(1, 0, 0)
			feature.Set(3, 0, 0)
			feature.Set(5, 0, 0)
		}
		
		//Obtain theta
		AaIF := mat.NewDense(banditDimension, banditDimension, nil)
		AaIF.Inverse(AaF)
		thetaF := mat.NewDense(banditDimension, 1, nil)
		thetaF.Product(AaIF, baF)

		AaIS := mat.NewDense(banditDimension, banditDimension, nil)
		AaIS.Inverse(AaS)
		thetaS := mat.NewDense(banditDimension, 1, nil)
		thetaS.Product(AaIS, baS)

		//Obtain bandit value
		thetaFPro := mat.NewDense(1, 1, nil)
		thetaFPro.Product(thetaF.T(), feature)

		thetaSPro := mat.NewDense(1, 1, nil)
		thetaSPro.Product(thetaS.T(), feature)

		//Make decision based on bandit value and stochastic value
		if thetaSPro.At(0, 0) < thetaFPro.At(0, 0) {
			if rand.Intn(100) < 70 {
				sch.waiting = 1
				return nil
			} else {
				sch.waiting = 0
				return secondBestPath
			}			
		} else {
			if rand.Intn(100) < 90 {
				sch.waiting = 0
				return secondBestPath
			} else {
				sch.waiting = 1
				return nil
			}	
		}
	}

}

func (sch *scheduler) selectPathRandom(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}
	var availablePaths []protocol.PathID

	for pathID, pth := range s.paths{
		cong := float32(pth.sentPacketHandler.GetCongestionWindow())-float32(pth.sentPacketHandler.GetBytesInFlight())
		allowed := pth.SendingAllowed() || (cong <= 0 && float32(cong) >=  -float32(pth.sentPacketHandler.GetCongestionWindow()) * float32(sch.AllowedCongestion) * 0.01)

		if pathID != protocol.InitialPathID && (allowed || hasRetransmission){
		//if pathID != protocol.InitialPathID && (pth.SendingAllowed() || hasRetransmission){
			availablePaths = append(availablePaths, pathID)
		}
	}

	if len(availablePaths) == 0 {
		return nil
	}

	pathID := rand.Intn(len(availablePaths))
	utils.Debugf("Selecting path %d", pathID)
	return s.paths[availablePaths[pathID]]
}

func (sch *scheduler) selectFirstPath(s * session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}
	for pathID, pth := range s.paths {
		if pathID == protocol.PathID(1) && pth.SendingAllowed(){
			return pth
		}
	}

	return nil
}

func (sch *scheduler) selectPathDQNAgent(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Avoid using PathID 0 if there is more than 1 path
	if len(s.paths) <= 1 {
		if !hasRetransmission && !s.paths[protocol.InitialPathID].SendingAllowed() {
			return nil
		}
		return s.paths[protocol.InitialPathID]
	}

	if len(s.paths) == 2{
		for pathID, path := range s.paths{
			if pathID!=protocol.InitialPathID{
				utils.Debugf("Selecting path %d as unique path", pathID)
				return path
			}
		}
	}
	
	//Check for available paths
	var availablePaths  []protocol.PathID
	for pathID, path := range s.paths{
		if path.sentPacketHandler.SendingAllowed() && pathID != protocol.InitialPathID{
			availablePaths = append(availablePaths, pathID)
		}
	}

	if len(availablePaths) == 0{
		if s.paths[protocol.InitialPathID].SendingAllowed() || hasRetransmission{
			return s.paths[protocol.InitialPathID]
	    }else{
	  	return nil
		}
	}else if len(availablePaths) == 1{
		return s.paths[availablePaths[0]]
	}

	action, paths := GetStateAndReward(sch, s)

	if paths == nil {
		return s.paths[protocol.InitialPathID]
	}
	
	return paths[action]
}

// Lock of s.paths must be held
func (sch *scheduler) selectPath(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	// XXX Currently round-robin
	if sch.SchedulerName == "rtt" {
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "random"{
		return sch.selectPathRoundRobin(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "lowband"{
		return sch.selectPathLowBandit(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "peek"{
		return sch.selectPathPeek(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "ecf"{
		return sch.selectECF(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "blest"{
		return sch.selectBLEST(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "dqnAgent" {
		return sch.selectPathDQNAgent(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "primary" {
		return sch.selectFirstPath(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}else if sch.SchedulerName == "rl" {
			return sch.selectPathReinforcementLearning(s, hasRetransmission, hasStreamRetransmission, fromPth)
		
	
	}else{
		panic("unknown scheduler selected")
		// Default, rtt
		return sch.selectPathLowLatency(s, hasRetransmission, hasStreamRetransmission, fromPth)
	}
	// return sch.selectPathRoundRobin(s, hasRetransmission, hasStreamRetransmission, fromPth)
}

// Lock of s.paths must be free (in case of log print)
func (sch *scheduler) performPacketSending(s *session, windowUpdateFrames []*wire.WindowUpdateFrame, pth *path) (*ackhandler.Packet, bool, error) {
	// add a retransmittable frame
	if pth.sentPacketHandler.ShouldSendRetransmittablePacket() {
		s.packer.QueueControlFrame(&wire.PingFrame{}, pth)
	}
	packet, err := s.packer.PackPacket(pth)
	if err != nil || packet == nil {
		return nil, false, err
	}
	if err = s.sendPackedPacket(packet, pth); err != nil {
		return nil, false, err
	}

	// send every window update twice
	for _, f := range windowUpdateFrames {
		s.packer.QueueControlFrame(f, pth)
	}

	// Packet sent, so update its quota
	sch.quotas[pth.pathID]++

	sRTT := make(map[protocol.PathID]time.Duration)

	// Provide some logging if it is the last packet
	for _, frame := range packet.frames {
		switch frame := frame.(type) {
		case *wire.StreamFrame:
			if frame.FinBit {
				// Last packet to send on the stream, print stats
				s.pathsLock.RLock()
				utils.Infof("Info for stream %x of %x", frame.StreamID, s.connectionID)
				for pathID, pth := range s.paths {
					sntPkts, sntRetrans, sntLost := pth.sentPacketHandler.GetStatistics()
					rcvPkts := pth.receivedPacketHandler.GetStatistics()
					utils.Infof("Path %x: sent %d retrans %d lost %d; rcv %d rtt %v", pathID, sntPkts, sntRetrans, sntLost, rcvPkts, pth.rttStats.SmoothedRTT())
					// TODO: Remove it
					utils.Infof("Congestion Window: %d", pth.sentPacketHandler.GetCongestionWindow())
					if sch.Training{
						sRTT[pathID] = pth.rttStats.SmoothedRTT()
					}
				}
				utils.Infof("Action: %d", sch.actionvector)
				utils.Infof("record: %d", sch.record)
				utils.Infof("epsidoe: %d", sch.episoderecord)
				utils.Infof("fe: %d", sch.fe)
				utils.Infof("se: %d", sch.se)
				if sch.Training && sch.SchedulerName == "dqnAgent"{
					duration := time.Since(s.sessionCreationTime)
					var maxRTT time.Duration
					for pathID := range sRTT{
						if sRTT[pathID] > maxRTT{
							maxRTT = sRTT[pathID]
						}
					}
					sch.TrainingAgent.CloseEpisode(uint64(s.connectionID), RewardFinalGoodput(sch, s, duration, maxRTT), false)
				}
				utils.Infof("Dump: %t, Training:%t, scheduler:%s", sch.DumpExp, sch.Training, sch.SchedulerName)
				if sch.DumpExp && !sch.Training && sch.SchedulerName == "dqnAgent"{
					utils.Infof("Closing episode %d", uint64(s.connectionID))
					sch.dumpAgent.CloseExperience(uint64(s.connectionID))
				}
				s.pathsLock.RUnlock()
				//Write lin parameters
				os.Remove("/App/output/lin")
				os.Create("/App/output/lin")
				file2, _ := os.OpenFile("/App/output/lin", os.O_WRONLY, 0600)
				for i := 0; i < banditDimension; i++ {
					for j := 0; j < banditDimension; j++ {
						fmt.Fprintf(file2, "%.8f\n", sch.MAaF[i][j])	
					}
				}
				for i := 0; i < banditDimension; i++ {
					for j := 0; j < banditDimension; j++ {
						fmt.Fprintf(file2, "%.8f\n", sch.MAaS[i][j])	
					}
				}
				for j := 0; j < banditDimension; j++ {
					fmt.Fprintf(file2, "%.8f\n", sch.MbaF[j])
				}
				for j := 0; j < banditDimension; j++ {
					fmt.Fprintf(file2, "%.8f\n", sch.MbaS[j])
				}
				file2.Close()
			}
		default:
		}
	}

	pkt := &ackhandler.Packet{
		PacketNumber:    packet.number,
		Frames:          packet.frames,
		Length:          protocol.ByteCount(len(packet.raw)),
		EncryptionLevel: packet.encryptionLevel,
	}

	return pkt, true, nil
}

// Lock of s.paths must be free
func (sch *scheduler) ackRemainingPaths(s *session, totalWindowUpdateFrames []*wire.WindowUpdateFrame) error {
	// Either we run out of data, or CWIN of usable paths are full
	// Send ACKs on paths not yet used, if needed. Either we have no data to send and
	// it will be a pure ACK, or we will have data in it, but the CWIN should then
	// not be an issue.
	s.pathsLock.RLock()
	defer s.pathsLock.RUnlock()
	// get WindowUpdate frames
	// this call triggers the flow controller to increase the flow control windows, if necessary
	windowUpdateFrames := totalWindowUpdateFrames
	if len(windowUpdateFrames) == 0 {
		windowUpdateFrames = s.getWindowUpdateFrames(s.peerBlocked)
	}
	for _, pthTmp := range s.paths {
		ackTmp := pthTmp.GetAckFrame()
		for _, wuf := range windowUpdateFrames {
			s.packer.QueueControlFrame(wuf, pthTmp)
		}
		if ackTmp != nil || len(windowUpdateFrames) > 0 {
			if pthTmp.pathID == protocol.InitialPathID && ackTmp == nil {
				continue
			}
			swf := pthTmp.GetStopWaitingFrame(false)
			if swf != nil {
				s.packer.QueueControlFrame(swf, pthTmp)
			}
			s.packer.QueueControlFrame(ackTmp, pthTmp)
			// XXX (QDC) should we instead call PackPacket to provides WUFs?
			var packet *packedPacket
			var err error
			if ackTmp != nil {
				// Avoid internal error bug
				packet, err = s.packer.PackAckPacket(pthTmp)
			} else {
				packet, err = s.packer.PackPacket(pthTmp)
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
	s.peerBlocked = false
	return nil
}

func (sch *scheduler) sendPacket(s *session) error {
	var pth *path

	// Update leastUnacked value of paths
	s.pathsLock.RLock()
	for _, pthTmp := range s.paths {
		pthTmp.SetLeastUnacked(pthTmp.sentPacketHandler.GetLeastUnacked())
	}
	s.pathsLock.RUnlock()

	// get WindowUpdate frames
	// this call triggers the flow controller to increase the flow control windows, if necessary
	windowUpdateFrames := s.getWindowUpdateFrames(false)
	for _, wuf := range windowUpdateFrames {
		s.packer.QueueControlFrame(wuf, pth)
	}

	// Repeatedly try sending until we don't have any more data, or run out of the congestion window
	for {
		// We first check for retransmissions
		hasRetransmission, retransmitHandshakePacket, fromPth := sch.getRetransmission(s)
		// XXX There might still be some stream frames to be retransmitted
		hasStreamRetransmission := s.streamFramer.HasFramesForRetransmission()

		// Select the path here
		s.pathsLock.RLock()
		pth = sch.selectPath(s, hasRetransmission, hasStreamRetransmission, fromPth)
		original_pth := pth
		s.pathsLock.RUnlock()

		// If an unavailable path is selected by agent, Initialize the path variable
		if (sch.SchedulerName == "rl") {
			if (pth != nil && !hasRetransmission && !pth.SendingAllowed()) {
				pth = nil
			}
		}

		// XXX No more path available, should we have a new QUIC error message?
		if pth == nil {
			windowUpdateFrames := s.getWindowUpdateFrames(false)
			return sch.ackRemainingPaths(s, windowUpdateFrames)
		}

		// If we have an handshake packet retransmission, do it directly
		if hasRetransmission && retransmitHandshakePacket != nil {
			s.packer.QueueControlFrame(pth.sentPacketHandler.GetStopWaitingFrame(true), pth)
			packet, err := s.packer.PackHandshakeRetransmission(retransmitHandshakePacket, pth)
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

		ack = pth.GetAckFrame()
		if ack != nil {
			s.packer.QueueControlFrame(ack, pth)
		}
		if ack != nil || hasStreamRetransmission {
			swf := pth.sentPacketHandler.GetStopWaitingFrame(hasStreamRetransmission)
			if swf != nil {
				s.packer.QueueControlFrame(swf, pth)
			}
		}

		// Also add CLOSE_PATH frames, if any
		for cpf := s.streamFramer.PopClosePathFrame(); cpf != nil; cpf = s.streamFramer.PopClosePathFrame() {
			s.packer.QueueControlFrame(cpf, pth)
		}

		// Also add ADD ADDRESS frames, if any
		for aaf := s.streamFramer.PopAddAddressFrame(); aaf != nil; aaf = s.streamFramer.PopAddAddressFrame() {
			s.packer.QueueControlFrame(aaf, pth)
		}

		// Also add PATHS frames, if any
		for pf := s.streamFramer.PopPathsFrame(); pf != nil; pf = s.streamFramer.PopPathsFrame() {
			s.packer.QueueControlFrame(pf, pth)
		}

		pkt, sent, err := sch.performPacketSending(s, windowUpdateFrames, pth)
		if err != nil {
			if err == ackhandler.ErrTooManyTrackedSentPackets{
				utils.Errorf("Closing episode")
				if sch.SchedulerName == "dqnAgent" && sch.Training{
					sch.TrainingAgent.CloseEpisode(uint64(s.connectionID), -100, false)
				}
			}
			return err
		}
		windowUpdateFrames = nil
		if !sent {
			// Prevent sending empty packets
			return sch.ackRemainingPaths(s, windowUpdateFrames)
		}
		if original_pth != nil {
			if (sch.SchedulerName == "rl") {
				sch.storeStateAction(s, original_pth.pathID, pkt)
			}
		}
		// Duplicate traffic when it was sent on an unknown performing path
		// FIXME adapt for new paths coming during the connection
		if pth.rttStats.SmoothedRTT() == 0 {
			currentQuota := sch.quotas[pth.pathID]
			// Was the packet duplicated on all potential paths?
		duplicateLoop:
			for pathID, tmpPth := range s.paths {
				if pathID == protocol.InitialPathID || pathID == pth.pathID {
					continue
				}
				if sch.quotas[pathID] < currentQuota && tmpPth.sentPacketHandler.SendingAllowed() {
					// Duplicate it
					pth.sentPacketHandler.DuplicatePacket(pkt)
					break duplicateLoop
				}
			}
		}

		// And try pinging on potentially failed paths
		if fromPth != nil && fromPth.potentiallyFailed.Get() {
			err = s.sendPing(fromPth)
			if err != nil {
				return err
			}
		}
	}
}
