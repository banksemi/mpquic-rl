package quic

import (
	"time"
	"runtime"
	
	"github.com/lucas-clemente/quic-go/internal/protocol"
    "github.com/gammazero/deque"
	"gorgonia.org/tensor"

	"github.com/aunum/goro/pkg/v1/layer"
	"github.com/aunum/gold/pkg/v1/agent/deepq"
	"github.com/aunum/gold/pkg/v1/common/require"
	"github.com/aunum/gold/pkg/v1/common"
	envv1 "github.com/aunum/gold/pkg/v1/env"
	agentv1 "github.com/aunum/gold/pkg/v1/agent"
	modelv1 "github.com/aunum/goro/pkg/v1/model"

	goldlog "github.com/aunum/log"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/ackhandler"

	"github.com/aunum/gold/pkg/v1/common/num"
	g "gorgonia.org/gorgonia"
)
const StateShapeInPath int = 3
const StateShapeSession int = 3
const StateShape int = StateShapeInPath * 2 + StateShapeSession

var Hyperparameters = &deepq.Hyperparameters{
	Epsilon:           common.DefaultDecaySchedule(),
	Gamma:             0.5,
	UpdateTargetSteps: 10,
	BufferSize:        10e6,
}

// DefaultFCLayerBuilder is a default fully connected layer builder.
var DefaultFCLayerBuilder = func(x, y *modelv1.Input) []layer.Config {
	return []layer.Config{
		layer.FC{Input: x.Squeeze()[0], Output: 24},
		layer.FC{Input: 24, Output: 24},
		layer.FC{Input: 24, Output: 24},
		layer.FC{Input: 24, Output: y.Squeeze()[0], Activation: layer.Linear},
	}
}

// DefaultPolicyConfig are the default hyperparameters for a policy.
var DefaultPolicyConfig = &deepq.PolicyConfig{
	Loss:         modelv1.MSE,
	Optimizer:    g.NewAdamSolver(g.WithLearnRate(0.0005)),
	LayerBuilder: DefaultFCLayerBuilder,
	BatchSize:    20,
	Track:        true,
}

// DefaultAgentConfig is the default config for a dqn agent.
var DefaultAgentConfig = &deepq.AgentConfig{
	Hyperparameters: Hyperparameters,
	PolicyConfig:    DefaultPolicyConfig,
	Base:            agentv1.NewBase("DeepQ"),
	StateShape:		 []int{1, StateShape},
	ActionShape:	 []int{1, 7},
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

	SegmentNumber int

	// Data size, not retransmission
	DataLen protocol.ByteCount

	MaxOffset protocol.ByteCount

	SendTime time.Time
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
		SegmentNumber: -1,
		DataLen: 0,
		SendTime: time.Now(),
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
var last_chunk int = -1;
func SetupRL() {
	runtime.GOMAXPROCS(4)
	newagent, err := deepq.NewAgent(DefaultAgentConfig)
	agent = newagent
	require.NoError(err)
	go SetupThreadRL()
	goldlog.Infof("쓰레드 실행 명령")
}

func (sch *scheduler) receivedACKForRL(s *session, ackFrame *wire.AckFrame) {
	var pathID = ackFrame.PathID;
	
	// var largetstack = ackFrame.LargestAcked
	// var lowestack = ackFrame.LowestAcked

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

	//goldlog.Infof("	수신 [%d] [계산:%d] %d - %d %d", pathID, ack, lowestack, largetstack, ackFrame.AckRanges)

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

		// Add datalen to received chunk size
		chunk_finished, duration := GetChunkManager().receivePacket(s, FrontData)

		if (chunk_finished) {
			rtt := time.Since(FrontData.SendTime) / 2

			goldlog.Infof("[청크 마무리 단계1] %s - %s - %s", duration, rtt, ackFrame.DelayTime)
			// Eliminates the effect of RTT
			// Why RTT and not one way delay? - HTTP Request (1 owd) + Sent ACK (1 owd)
			duration -= rtt

			// Eliminates the effect of artificial ACK delay on the client
			duration -= ackFrame.DelayTime
			goldlog.Infof("[청크 마무리 단계2] %s", duration)
			


			// Reward
			var outcome *envv1.Outcome = new(envv1.Outcome)
			outcome.Action = last_action

			// The state changed due to the action must be entered
			outcome.Observation = sch.getRLState(s, FrontData.SegmentNumber)

			outcome.Reward = float32(1000 - duration.Milliseconds())
			outcome.Done = true

			// Store event to replay buffer
			event := deepq.NewEvent(last_state, outcome.Action, outcome)
			agent.Remember(event)
			goldlog.Infof("[청크 마무리] 기존 액션 %d 결과 %f 스테이트 %d -> %d", last_action, outcome.Reward, last_state, outcome.Observation)
		}
	}
}

func (sch *scheduler) getRLState(s *session, segmentNumber int) (state *tensor.Dense) {
	var features [StateShape]float32;
	i := 0
	for _, pth := range s.paths {
		if (pth.pathID == protocol.InitialPathID) { 
			continue;
		}
		
		// Only two paths are used except for the initial path
		if (pth.pathID == 2) {
			continue;
		}
		if (pth.pathID == 1) {
			i = 1
		}
		if (pth.pathID == 3) {
			i = 2
		}
		
		// Feature extraction of path
		rtt := float32(pth.rttStats.SmoothedRTT().Milliseconds())
		cwnd :=  float32(pth.sentPacketHandler.GetCongestionWindow())
		inflight := float32(pth.sentPacketHandler.GetBytesInFlight())
		features[(i-1)*StateShapeInPath+0] = rtt / 100;
		features[(i-1)*StateShapeInPath+1] = cwnd / 100000;
		features[(i-1)*StateShapeInPath+2] = inflight / 100000;
	}
	sid := getHTTPStreamID(s)
	// Set state vactor

	cm := GetChunkManager()
	f, _ := s.flowControlManager.SendWindowSize(sid)
	state = tensor.New(
		tensor.WithShape(agent.StateShape...), 
		tensor.WithBacking([]float32{
			features[0], 
			features[1], 
			features[2], 
			features[3], 
			features[4], 
			features[5], 
			float32(f) / 100000,
			float32(cm.remainBytesByServer(segmentNumber)) / 100000,
			float32(cm.remainBytesByClient(segmentNumber)) / 100000,
		}))

	// return state
	return
}

func (sch *scheduler) storeStateAction(s *session, pathID protocol.PathID, pkt *ackhandler.Packet) {
	var packetNumber protocol.PacketNumber = pkt.PacketNumber
	if (sch.rlmemories[pathID] == nil) {
		sch.rlmemories[pathID] = RLNewMemory()
	}

	// Set state vactor
	state := sch.getRLState(s, GetChunkManager().segmentNumber)

	event := RLNewEvent(pathID, packetNumber, state)

	for _, frame := range pkt.Frames {
		switch f := frame.(type) {
		case *wire.StreamFrame:
			cm := GetChunkManager()
			cm.sendPacket(f, event) // Update event object
		}
	}
	
	sch.rlmemories[pathID].PushBack(event)


	// goldlog.Infof("%s 전송 [%d] %d", time.Now(), pathID, packetNumber)
}
func getHTTPStreamID(s *session) protocol.StreamID {
	var sid protocol.StreamID = protocol.StreamID(0)
	s.streamsMap.Iterate(func(str *stream) (bool, error) {
		id := str.StreamID()
		if (id != 1 && !(str.shouldSendFin() || str.finished())) {
			if (id != 3) {
				sid = id
			}
			// return false, nil
		}
		return true, nil
	})
	return sid
}
func (sch *scheduler) selectPathReinforcementLearning(s *session, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path) *path {
	utils.Debugf("selectPathReinforcementLearning")
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

	// If all paths are not available
	var dontsendpacket = true
	for _, pth := range s.paths {
		// Don't block path usage if we retransmit, even on another path
		if hasRetransmission || pth.SendingAllowed() {
			dontsendpacket = false
		}
	}
	if (dontsendpacket == true) {
		return nil
	}
	cm := GetChunkManager()
	if (cm.segmentNumber != last_chunk) {
		// Set state vactor
		state := sch.getRLState(s, cm.segmentNumber)

		// Perform Action
		action, _ := agent.Action(state)

		sch.nmBandwidth.clear()
		last_chunk = cm.segmentNumber
		last_action = action
		last_state = state
		last_scheduling_time = time.Now()
	}

	if (time.Since(last_scheduling_time).Milliseconds() > 50) {
		// Set state vactor
		state := sch.getRLState(s, cm.segmentNumber)

		// Perform Action
		action, _ := agent.Action(state)

		// Reward
		if (last_state != nil) {
			var outcome *envv1.Outcome = new(envv1.Outcome)
			outcome.Action = last_action
			outcome.Reward = float32(sch.nmBandwidth.getSum())
			outcome.Done = false
			outcome.Observation = state // The state changed due to the action must be entered

			// Check stream data for sending
			var remain_data protocol.ByteCount = 0
			var streami int = 0
			s.streamsMap.Iterate(func(str *stream) (bool, error) {
				id := str.StreamID()
				if (id != 1 && !(str.shouldSendFin() || str.finished())) {
					remain_data += str.lenOfDataForWriting()
					if (id != 3) {
						streami += 1
					}
					// return false, nil
				}
				return true, nil
			})

			// Store event to replay buffer in sending
			if (streami >= 1) {
				event := deepq.NewEvent(last_state, outcome.Action, outcome)
				agent.Remember(event)
				goldlog.Infof("딜레이: %s", time.Since(last_scheduling_time).Milliseconds())
				goldlog.Infof("남은 바이트 %d, 기존 액션 %d 결과 %f 스트림 %d,스테이트 %d -> %d", remain_data, last_action, outcome.Reward, streami, last_action, outcome.Observation)
			}
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
	if (last_action == 5) {
		return s.paths[1] // only fast
	}
	if (last_action == 6) {
		return selectedPath
	}
	if num.RandF32(0.0, 1.0) < split_p1 { 
		return s.paths[1]
	} else {
		return s.paths[3]
	}
	return nil
}
