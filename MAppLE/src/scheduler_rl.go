package quic

import (
	"time"
	"runtime"
	
	"github.com/lucas-clemente/quic-go/internal/protocol"
    "github.com/gammazero/deque"
	"gorgonia.org/tensor"

	"github.com/aunum/gold/pkg/v1/agent/deepq"
	"github.com/aunum/gold/pkg/v1/common/require"
	envv1 "github.com/aunum/gold/pkg/v1/env"
	agentv1 "github.com/aunum/gold/pkg/v1/agent"

	goldlog "github.com/aunum/log"
	"github.com/lucas-clemente/quic-go/internal/wire"

	"github.com/aunum/gold/pkg/v1/common/num"
)

const StateShape int = 6

// DefaultAgentConfig is the default config for a dqn agent.
var DefaultAgentConfig = &deepq.AgentConfig{
	Hyperparameters: deepq.DefaultHyperparameters,
	PolicyConfig:    deepq.DefaultPolicyConfig,
	Base:            agentv1.NewBase("DeepQ"),
	StateShape:		 []int{1, StateShape},
	ActionShape:	 []int{1, 5},
}

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
func SetupThreadRL() {
	for {
		time.Sleep(10 * time.Millisecond)
		// startTime2 := time.Now()
		// lock.Lock()
		// startTime1 := time.Now()
		agent.Learn()
		// time1 := time.Since(startTime1)
		// lock.Unlock()
		// time2 := time.Since(startTime2)
		// goldlog.Infof("학습 소요 시간 %s, Mutex Lock 시간 %s", time1, time2 - time1)
	}
}


var agent *deepq.Agent;
var last_action int = 0;
var last_scheduling_time time.Time = time.Now();
var last_state *tensor.Dense;
func SetupRL() {
	runtime.GOMAXPROCS(4)
	newagent, err := deepq.NewAgent(DefaultAgentConfig)
	agent = newagent
	require.NoError(err)
	go SetupThreadRL()
	goldlog.Infof("쓰레드 실행 명령")
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
		// event := deepq.NewEvent(FrontData.State, outcome.Action, outcome)
		// agent.Remember(event)
	}
}

func (sch *scheduler) getRLState(paths map[protocol.PathID]*path) (state *tensor.Dense) {
	var features [StateShape]float32;
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
		inflight := float32(pth.sentPacketHandler.GetBytesInFlight())
		features[(int(pathID)-1)*(StateShape/2)+0] = rtt / 100;
		features[(int(pathID)-1)*(StateShape/2)+1] = cwnd / 100000;
		features[(int(pathID)-1)*(StateShape/2)+2] = inflight / 100000;

	}

	// Set state vactor
	state = tensor.New(tensor.WithShape(agent.StateShape...), tensor.WithBacking([]float32{features[0],features[1], features[2], features[3], features[4], features[5]}))

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

	// If all paths are not available
	var dontsendpacket = true
	for _, pth := range paths {
		// Don't block path usage if we retransmit, even on another path
		if hasRetransmission || pth.SendingAllowed() {
			dontsendpacket = false
		}
	}
	if (dontsendpacket == true) {
		return nil
	}

	if (time.Since(last_scheduling_time).Milliseconds() > 50) {
		// Set state vactor
		state := sch.getRLState(s.Paths())

		// Perform Action
		action, _ := agent.Action(state)

		// Reward
		if (last_state != nil) {
			var outcome *envv1.Outcome = new(envv1.Outcome)
			outcome.Action = last_action
			outcome.Reward = float32(sch.nmBandwidth.getSum())
			outcome.Done = false
			outcome.Observation = last_state // The state changed due to the action must be entered

			// Store event to replay buffer
			event := deepq.NewEvent(state, outcome.Action, outcome)
			agent.Remember(event)
			goldlog.Infof("기존 액션 %d 결과 %f 스테이트 %d -> %d", last_action, outcome.Reward, outcome.Observation, state)
		}
		last_action = action
		last_state = state
		last_scheduling_time = time.Now()

	}

	// initial path
	if (selectedPathID == protocol.PathID(0)) {
		return selectedPath
	}
	var split_p1 float32 = 0.0
	if (last_action == 0) {
		split_p1 = 0.1
	}
	if (last_action == 1) {
		split_p1 = 0.25
	}
	if (last_action == 2) {
		split_p1 = 0.5
	}
	if (last_action == 3) {
		split_p1 = 0.75
	}
	if (last_action == 4) {
		split_p1 = 0.9
	}
	if num.RandF32(0.0, 1.0) < split_p1 { 
		return paths[1]
	} else {
		return paths[2]
	}
	return nil
}
