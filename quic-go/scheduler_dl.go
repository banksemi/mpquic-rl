package quic

import (
	"io/ioutil"
	"bitbucket.com/marcmolla/gorl/agents"
	"bitbucket.com/marcmolla/gorl"
	"time"
	"bitbucket.com/marcmolla/gorl/types"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"errors"
	"fmt"
)

func GetAgent(weightsFile string, specFile string) agents.Agent{
	var spec []byte
	var err error
	if specFile != ""{
		spec, err = ioutil.ReadFile(specFile)
		if err != nil{
			panic(err)
		}
	}
	agent := gorl.GetNormalInstance(string(spec))
	if weightsFile != ""{
		err = agent.LoadWeights(weightsFile)
		if err != nil{
			panic(err)
		}
	}
	return agent
}

func GetTrainingAgent(weightsFile string, specFile string, outputPath string, epsilon float64) agents.TrainingAgent{
	var spec []byte
	var err error
	if specFile != "" {
		spec, err = ioutil.ReadFile(specFile)
		if err != nil {
			panic(err)
		}
	}

	agent := gorl.GetTrainingInstance(string(spec), outputPath, float32(epsilon))
	if weightsFile != ""{
		err = agent.LoadWeights(weightsFile)
		if err != nil{
			panic(err)
		}
	}
	return agent
}

func NormalizeTimes(stat time.Duration) types.Output{
	return types.Output(stat.Nanoseconds()) / types.Output(time.Millisecond.Nanoseconds()*150)
}

func RewardFinalGoodput(sch *scheduler, s *session, duration time.Duration, _ time.Duration) types.Output {
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)
	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)

	for pathID, path := range s.paths{
		if pathID != protocol.InitialPathID{
			packetNumber[pathID], retransNumber[pathID], _ = path.sentPacketHandler.GetStatistics()
			// Ordering paths
			if firstPath == protocol.PathID(255){
				firstPath = pathID
			}else{
				if pathID < firstPath{
					secondPath = firstPath
					firstPath = pathID
				}else{
					secondPath = pathID
				}
			}
		}
	}

	sentPackets := types.Output(packetNumber[firstPath]+packetNumber[secondPath])* types.Output(protocol.DefaultTCPMSS)
	retransPackets := types.Output(retransNumber[firstPath]+retransNumber[secondPath])* types.Output(protocol.DefaultTCPMSS)

	elapsedtime := types.Output(duration)
	partialReward := ((sentPackets - retransPackets)) / 1024/1024 / elapsedtime
	//partialReward = types.Output(-100)

	return partialReward
}

func GetStateAndReward(sch *scheduler, s *session) (int, []*path){
	packetNumber := make(map[protocol.PathID]uint64)
	retransNumber := make(map[protocol.PathID]uint64)

	sRTT := make(map[protocol.PathID]time.Duration)
	cwnd := make(map[protocol.PathID]protocol.ByteCount)
	cwndlevel := make(map[protocol.PathID]types.Output)

	firstPath, secondPath := protocol.PathID(255), protocol.PathID(255)

	for pathID, path := range s.paths{
		if pathID != protocol.InitialPathID{
			packetNumber[pathID], retransNumber[pathID], _ = path.sentPacketHandler.GetStatistics()
			sRTT[pathID] = path.rttStats.SmoothedRTT()
			cwnd[pathID] = path.sentPacketHandler.GetCongestionWindow()
			cwndlevel[pathID] = types.Output(path.sentPacketHandler.GetBytesInFlight())/types.Output(cwnd[pathID])

			// Ordering paths
			if firstPath == protocol.PathID(255){
				firstPath = pathID
			}else{
				if pathID < firstPath{
					secondPath = firstPath
					firstPath = pathID
				}else{
					secondPath = pathID
				}
			}
		}
	}

	//packetNumberInitial, _, _ := s.paths[protocol.InitialPathID].sentPacketHandler.GetStatistics()

	//Penalize and fast-quit
	// if sch.Training{
	// 	if packetNumberInitial > 20 {
	// 		utils.Errorf("closing: zero tolerance")
	// 		sch.TrainingAgent.CloseEpisode(uint64(s.connectionID), -100, false)
	// 		s.closeLocal(errors.New("closing: zero tolerance"))
	// 	}
	// }
	
	//State
	BSend, _ := s.flowControlManager.SendWindowSize(protocol.StreamID(5))
	state := types.Vector{NormalizeTimes(sRTT[firstPath]), NormalizeTimes(sRTT[secondPath]),
	types.Output(cwnd[firstPath])/types.Output(protocol.DefaultTCPMSS)/300, types.Output(cwnd[secondPath])/types.Output(protocol.DefaultTCPMSS)/300, cwndlevel[firstPath], cwndlevel[secondPath], types.Output(BSend)/types.Output(protocol.DefaultTCPMSS)/300}
	
	//Action
	var action int
    if sch.Training{
		action = sch.TrainingAgent.GetAction(state)
	}else{
		action = sch.Agent.GetAction(state)
	}	

	//Write in state and action
	sch.statevector[sch.record] = state
	sch.actionvector[sch.record] = action

	//Partial Reward
	sentPackets := packetNumber[firstPath]+packetNumber[secondPath]
	retransPackets := retransNumber[firstPath]+retransNumber[secondPath]
	sch.packetvector[sch.record] = sentPackets - retransPackets

	partialReward := types.Output(0)
	elapsedtime := types.Output(0)
	buffertime := types.Output(0)
	sch.recordDuration[sch.record] = elapsedtime

	if sch.record == 0 {
		partialReward = types.Output(0)
		sch.episoderecord += 1
		if sch.Training{
			realstate := sch.statevector[sch.record]
			realaction := sch.actionvector[sch.record]
			sch.TrainingAgent.SaveStep(uint64(s.connectionID),partialReward, realstate, realaction)
		}else{
			if sch.DumpExp{
				sch.dumpAgent.AddStep(uint64(s.connectionID), []string{fmt.Sprint(sch.statevector[sch.record]), fmt.Sprint(sch.actionvector[sch.record])})
			}
		}
	} else {
		elapsedtime = types.Output(time.Since(sch.lastfiretime))
		sch.recordDuration[sch.record] = elapsedtime
		benchmark := sch.packetvector[sch.episoderecord - 1]
		if benchmark < (sentPackets - retransPackets) {
			for i:= uint64(0); i<(sentPackets - retransPackets - benchmark); i+=1{
				for z:= uint64(0); z < (sch.record - (sch.episoderecord - 1)); z+=1{
					buffertime +=  sch.recordDuration[sch.episoderecord + z] 
				}
				if sch.episoderecord == sch.record {
					partialReward = types.Output(sentPackets - retransPackets-benchmark - i) * types.Output(protocol.DefaultTCPMSS) /1024/1024 / buffertime
					buffertime = types.Output(0)
					if sch.Training{
						realstate := sch.statevector[sch.episoderecord]
						realaction := sch.actionvector[sch.episoderecord]
						sch.TrainingAgent.SaveStep(uint64(s.connectionID),partialReward, realstate, realaction)
					}else{
						if sch.DumpExp{
							sch.dumpAgent.AddStep(uint64(s.connectionID), []string{fmt.Sprint(sch.statevector[sch.episoderecord]), fmt.Sprint(sch.actionvector[sch.episoderecord])})
						}
					}
					sch.episoderecord += 1
					break
				} else {
					partialReward = types.Output(protocol.DefaultTCPMSS) /1024/1024 / buffertime
					buffertime = types.Output(0)
					if sch.Training{
						realstate := sch.statevector[sch.episoderecord]
						realaction := sch.actionvector[sch.episoderecord]
						sch.TrainingAgent.SaveStep(uint64(s.connectionID),partialReward, realstate, realaction)
					}else{
						if sch.DumpExp{
							sch.dumpAgent.AddStep(uint64(s.connectionID), []string{fmt.Sprint(sch.statevector[sch.episoderecord]), fmt.Sprint(sch.actionvector[sch.episoderecord])})
						}
					}
					sch.episoderecord += 1
				}
			}
		}
	}
	
	//Main pointer and fire time
	sch.record += 1
	sch.lastfiretime = time.Now()

	return action, []*path{s.paths[firstPath], s.paths[secondPath]}
}

func CheckAction(action int, state types.Vector, s *session, sch *scheduler){
	if action != 0{
		return
	}
	if state[4] < 1 || state[5] < 1 {
		// penalize not sending with one path allowed
		utils.Errorf("not sending with one path allowed")
		sch.TrainingAgent.CloseEpisode(uint64(s.connectionID), -100, false)
		s.closeLocal(errors.New("not sending with one path allowed"))
	}

}