package quic

import (
	"math"
	"sort"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)


func EstimateDelivery (congestionControl string, datalength protocol.ByteCount, paths map[protocol.PathID]*path) int{
	RTTList:= []int {}
	for id, path := range paths{
		pathRTT := int(path.rttStats.SmoothedRTT().Milliseconds())
		//if id==0 || pathRTT==0{
			//continue
		//}
		utils.Debugf("\nEstimateDelivery: pathid %d RTT %d", id, pathRTT)
		for i:=1; i<100; i++ {
			RTTList = append(RTTList , i*pathRTT)
		}
	}
	sort.Ints(RTTList)

	return estimateReno(datalength, paths, RTTList)
}

func estimateReno(datalength protocol.ByteCount, paths map[protocol.PathID]*path, RTTList []int) int{
	for {
		for i:=0; i<len(RTTList);i++{
			var total_sent protocol.ByteCount
			for id, path := range paths{
				if path.rttStats.SmoothedRTT() == 0 || id==0 {
					continue
				}
				round := uint(RTTList[i]/int(path.rttStats.SmoothedRTT().Milliseconds()))
				estimatedCongestionWindow, slowStartThreshold := estimateCwndIdle(path)

				var send_estimation protocol.ByteCount
				if estimatedCongestionWindow<<(round-1) < slowStartThreshold{
					send_estimation =protocol.ByteCount(estimatedCongestionWindow*(1<<round -1))*protocol.MaxPacketSize
				}else{
					slowstart_round := uint(math.Ceil(math.Log2(float64(slowStartThreshold)/float64(estimatedCongestionWindow))))
					avoidance_round :=protocol.PacketNumber( round - slowstart_round)
					slowstart_sent :=protocol.ByteCount(estimatedCongestionWindow*(1<<slowstart_round -1)) * protocol.MaxPacketSize
					avoidance_sent := protocol.ByteCount(((avoidance_round -1 + 2*slowStartThreshold)*(avoidance_round)/2)) * protocol.MaxPacketSize
					send_estimation = slowstart_sent + avoidance_sent

				}
				total_sent += send_estimation
				utils.Debugf("\nEstimateReno: id %d RTTList[i] %d round %d currentCongestionWindow %d  slowStartThreshold %d send_estimation %d total_sent %d datalength %d ", id,RTTList[i], round, estimatedCongestionWindow, slowStartThreshold, send_estimation, total_sent, datalength )

			}
			if total_sent >= datalength || total_sent == 0{
				lastPacketArrivalTime := 0
				var lastPath protocol.PathID
				for id, path := range paths{
					pathRTT := path.rttStats.SmoothedRTT()
					if pathRTT== 0 || id==0 {
						continue
					}
					lastOfPath := (2*RTTList[i]/int(pathRTT.Milliseconds()) -1)*int(pathRTT.Milliseconds())/2
					if lastOfPath > lastPacketArrivalTime{
						lastPacketArrivalTime = lastOfPath
						lastPath = id
					}
				}
				utils.Debugf("\n EstimateReno:RTTList[i] %d lastPacketArrivalTime %d from path %d", RTTList[i], lastPacketArrivalTime, lastPath)
				return lastPacketArrivalTime
			}
		}

	}
}

func estimateCwndIdle(path *path) (protocol.PacketNumber, protocol.PacketNumber){
	rto := path.sentPacketHandler.ComputeRTOTimeout()
	delta := time.Since(path.sentPacketHandler.GetLastSendTime())
	cwnd := protocol.PacketNumber(path.GetCongestionWindow()/protocol.DefaultTCPMSS)
	restart_cwnd := utils.MinPacketNumber(protocol.InitialCongestionWindow, cwnd);

	//utils.Debugf("\npath.GetCongestionWindow() %d delta %d rto %d",path.GetCongestionWindow(), delta.Milliseconds(), rto.Milliseconds())
	estimatedCwnd := utils.MaxPacketNumber(restart_cwnd, cwnd>>protocol.PacketNumber(delta/rto))
	estimatedSlowstartThreshold := utils.MaxPacketNumber(utils.MaxPacketNumber(protocol.InitialCongestionWindow, path.sentPacketHandler.GetSendAlgorithm().SlowstartThreshold()),  cwnd*3/4)
	return estimatedCwnd, estimatedSlowstartThreshold
}
