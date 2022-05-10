package quic

import (
	"time"
	
	"github.com/lucas-clemente/quic-go/internal/protocol"
    "github.com/gammazero/deque"
	"gorgonia.org/tensor"

	"github.com/aunum/gold/pkg/v1/agent/deepq"
	"github.com/aunum/gold/pkg/v1/common/require"
	envv1 "github.com/aunum/gold/pkg/v1/env"

	goldlog "github.com/aunum/log"
	"github.com/lucas-clemente/quic-go/internal/wire"

)

type RLMemory struct {
	*deque.Deque
}

type RLEvent struct {
	// Action that was taken.
	PathID protocol.PathID
	PacketNumber protocol.PacketNumber

	// State by which the action was taken.
	State *tensor.Dense
}

func RLNewMemory() *RLMemory {
	return &RLMemory{
		Deque: &deque.Deque{},
	}
}

// NewEvent returns a new event
func RLNewEvent(pathID protocol.PathID, packetnumber protocol.PacketNumber, state *tensor.Dense) *RLEvent {
	return &RLEvent{
		PathID:  pathID,
		PacketNumber:  packetnumber,
		State:   state,
	}
}

var agent *deepq.Agent;
func SetupRL() {
	s, err := envv1.NewLocalServer(envv1.GymServerConfig)
	require.NoError(err)
	defer s.Close()

	env, err := s.Make("CartPole-v0",
		envv1.WithNormalizer(envv1.NewExpandDimsNormalizer(0)),
	)
	require.NoError(err)

	agent, err = deepq.NewAgent(deepq.DefaultAgentConfig, env)
	require.NoError(err)

	// episodes = agent.MakeEpisodes(1000)
	// timesteps:= episode.Steps(1000)
}

func (sch *scheduler) receivedACKForRL(paths map[protocol.PathID]*path, ackFrame *wire.AckFrame) {
	var pathID = ackFrame.PathID;
	
	var largetstack = ackFrame.LargestAcked
	var lowestack = ackFrame.LowestAcked
	var ack = ackFrame.LargestAcked
	// Calculation of ACK number received without loss
	if (len(ackFrame.AckRanges) > 0) {
		ack = ackFrame.AckRanges[len(ackFrame.AckRanges)-1].Last
	}

	// If this value is empty, the reinforcement learning scheduler has not been used.
	// Todo: Add code to check which scheduler is being used
	if (sch.rlmemories[pathID] == nil) {
		return
	}

	goldlog.Infof("	수신 [%d] [계산:%d] %d - %d %d", pathID, ack, lowestack, largetstack, ackFrame.AckRanges)

	// Repeat for all saved reinforcement learning events	
	for {
		// If there are no more items
		if (sch.rlmemories[pathID].Len() == 0) {
			break;
		}

		var FrontData = sch.rlmemories[pathID].Front().(*RLEvent)
		if (FrontData.PacketNumber > ack) {
			break;
		}

		// Pop data
		sch.rlmemories[pathID].PopFront()

		sch.nmBandwidth.push(1)

		// Reward
		var outcome *envv1.Outcome = new(envv1.Outcome)
		if (pathID == 1) {
			outcome.Action = 0
		}
		if (pathID == 2) {
			outcome.Action = 1
		}
		//utcome.Action = int(uint8(pathID) - uint8(1))
		outcome.Reward = float32(sch.nmBandwidth.getSum())
		outcome.Done = false
		outcome.Observation = sch.getRLState(paths)	// The state changed due to the action must be entered

		goldlog.Infof("	읽기 [%d] %d %f %d -> %d", pathID, FrontData.PacketNumber, outcome.Reward, FrontData.State, outcome.Observation)
		
		// Store event to replay buffer
		event := deepq.NewEvent(FrontData.State, outcome.Action, outcome)
		agent.Remember(event)
		agent.Learn()
	}
}

func (sch *scheduler) getRLState(paths map[protocol.PathID]*path) (state *tensor.Dense) {
	var features [4]float32;
	for pathID, pth := range paths {
		if (pathID == protocol.InitialPathID) { 
			continue;
		}
		
		// Only two paths are used except for the initial path
		if (pathID >= 3) {
			continue;
		}

		// Feature extraction of path
		rtt := float32(pth.rttStats.SmoothedRTT().Milliseconds())
		cwnd :=  float32(pth.GetCongestionWindow())
		features[(pathID-1)*2+0] = rtt;
		features[(pathID-1)*2+1] = cwnd;
	}

	// Set state vactor
	state = tensor.New(tensor.WithShape(1,4), tensor.WithBacking([]float32{features[0],features[1], features[2], features[3]}))

	// return state
	return
}

func (sch *scheduler) storeStateAction(s sessionI, pathID protocol.PathID, packetNumber protocol.PacketNumber) {
	if (sch.rlmemories[pathID] == nil) {
		sch.rlmemories[pathID] = RLNewMemory()
	}

	// Set state vactor
	state := sch.getRLState(s.Paths())

	event := RLNewEvent(pathID, packetNumber, state)
	sch.rlmemories[pathID].PushBack(event)

	goldlog.Infof("전송 [%d] %d", pathID, packetNumber)
}

func (sch *scheduler) selectPathReinforcementLearning(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
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

	// timesteps:= episode.Steps(env.MaxSteps())
	

	// goldlog.Infof("State: %s", x)
	// goldlog.Infof("Path count: %d Select %d Action %d", len(paths), selectedPathID, action)

	// Set state vactor
	state := sch.getRLState(s.Paths())

	// Perform Action
	action, _ := agent.Action(state)


	return selectedPath
	if (action == 0) {
		return paths[1]
	}
	if (action == 1) {
		return paths[2]
	}
	goldlog.Infof("Path count: %d Select %d Action %d", len(paths), selectedPathID, action)
	goldlog.Infof("Error")
	return selectedPath
}
