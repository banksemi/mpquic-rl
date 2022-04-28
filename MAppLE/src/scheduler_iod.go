package quic

import (
	"log"
	"math"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/sedpf"
)

type IOD_Strategy int

const SEDPFBasedIOD IOD_Strategy = 0
const LowRTTBasedIOD IOD_Strategy = 1
const OpportunisticIOD IOD_Strategy = 2

type scheduledPacketInfo struct {
	path   *path
	packet *packedPacket
}

func packPacket(pth *path, s sessionI, sch *scheduler, hasStreamRetransmission bool) (*packedPacket, bool, error) {
	packer := s.GetPacker()
	fecFramer := s.GetFECFramer()
	streamFramer := s.GetStreamFramer()

	hasFECFrame := len(fecFramer.transmissionQueue) > 0

	// packet packing starts here
	var ack *wire.AckFrame

	rcv := pth.GetRecoveredFrame()
	if rcv != nil {
		packer.QueueControlFrame(rcv, pth)
	} else if !hasFECFrame {
		ack = pth.GetAckFrame()
		if ack != nil {
			packer.QueueControlFrame(ack, pth)
		}
	}

	if ack != nil || hasStreamRetransmission {
		swf := pth.sentPacketHandler.GetStopWaitingFrame(hasStreamRetransmission)
		if swf != nil {
			packer.QueueControlFrame(swf, pth)
		}
		hasStreamRetransmission = false
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

	packet, err := sch.preparePacketForSending(s, pth, hasFECFrame)
	return packet, hasStreamRetransmission, err
}

func (sched *SEDPFScheduler) SelectPathsAndOrder(s sessionI, hasRetransmission bool, hasStreamRetransmission bool, fromPth *path, sch *scheduler) ([]scheduledPacketInfo, error) {
	scheduleFast := make([]scheduledPacketInfo, 0, protocol.SIOD_MaxSchedulingWindow)
	scheduleSlow := make([]scheduledPacketInfo, 0, protocol.SIOD_MaxSchedulingWindow)

	var fastPath, slowPath *path

	disableIOD := false

	// select path with latest arrival time for FECFrame packets
	switch sched.iodStrategy {
	case SEDPFBasedIOD:
		// S-EDPF
		fastPath = sched.SelectPath(s, hasRetransmission, hasStreamRetransmission, false, fromPth, sch)
		// inverse S-EDPF
		slowPath = sched.SelectFECPathArrivalBased(s)
	case LowRTTBasedIOD:
		// LowRTT
		fastPath = sched.SelectPathLowRTT(s, hasRetransmission, hasStreamRetransmission, false, fromPth, sch)
		// inverse LowRTT
		slowPath = sched.SelectFECPathLatencyBased(s)
	case OpportunisticIOD:
		// LowRTT
		fastPath = sched.SelectPathLowRTT(s, hasRetransmission, hasStreamRetransmission, false, fromPth, sch)
		// inverse LowRTT
		slowPath = sched.SelectFECPathLatencyBased(s)
		if fastPath != nil && slowPath != nil && fastPath != slowPath && fastPath.GetWindowedLossRatio() > slowPath.GetWindowedLossRatio() {
			// actually the fast path has a much worse loss rate than the
			// slow path
			// regular IOD would be harmful, so fall back to LowRTT here
			disableIOD = true
			utils.Debugf("\n Disable O-IOD: \n fastPath RTT %d loss %f \n slowPath RTT %d loss %f", fastPath.rttStats.SmoothedRTT(), fastPath.GetWindowedLossRatio(), slowPath.rttStats.SmoothedRTT(), slowPath.GetWindowedLossRatio())
		} else if fastPath != nil && slowPath != nil && fastPath != slowPath {
			utils.Debugf("\n Enable O-IOD: \n fastPath RTT %d loss %f \n slowPath RTT %d loss %f", fastPath.rttStats.SmoothedRTT(), fastPath.GetWindowedLossRatio(), slowPath.rttStats.SmoothedRTT(), slowPath.GetWindowedLossRatio())
		}
	}

	fecFramer := s.GetFECFramer()

	if fastPath == nil && slowPath == nil {
		// no free path
		return scheduleFast, nil
	} else if fastPath == nil || fastPath == slowPath {
		// only slow path available OR both paths are the same
		packet, _, err := packPacket(fastPath, s, sch, hasStreamRetransmission)
		if err != nil {
			return nil, err
		}
		return append(scheduleFast, scheduledPacketInfo{
			packet: packet,
			path:   fastPath,
		}), nil
	} else if slowPath == nil {
		// only fast path available
		packet, _, err := packPacket(fastPath, s, sch, hasStreamRetransmission)
		if err != nil {
			return nil, err
		}
		return append(scheduleSlow, scheduledPacketInfo{
			packet: packet,
			path:   fastPath,
		}), nil
	} else if disableIOD {
		// IOD behaviour disabled
		// transmit uncoded over fast and coded over slow but don't perform
		// out-of-order sending
		// (basically fall back to LowRTT)

		// use fast path by default
		path := fastPath
		schedule := scheduleFast

		//hasFECFrame := len(fecFramer.transmissionQueue) > 0
		// maybe do something else?
		// transmit FEC over more lossy path? but it would be the fast one...
		// maybe swap paths if difference is REALLY large?
		//if hasFECFrame {
		//	path = fastPath
		//	schedule = scheduleFast
		//}

		// only ever transmit a single packet
		packet, _, err := packPacket(path, s, sch, hasStreamRetransmission)
		if err != nil {
			return nil, err
		}
		return append(schedule, scheduledPacketInfo{
			packet: packet,
			path:   path,
		}), nil
	}

	// gather the congestion windows of all paths
	bytesSent := make(map[*path]protocol.ByteCount)

	for _, p := range s.Paths() {
		bytesSent[p] = 0
	}

	// how many packets we can send on the fast path before sending one on the slower path makes sense
	var overhang uint64
	switch sched.iodStrategy {
	case SEDPFBasedIOD:
		arrFast := sedpf.Paths[fastPath.pathID].ExpectedArrivalDelta(protocol.MaxPacketSize)
		arrSlow := sedpf.Paths[slowPath.pathID].ExpectedArrivalDelta(protocol.MaxPacketSize)
		overhang = uint64(math.Ceil(arrSlow / arrFast))
	case OpportunisticIOD:
		fallthrough
	case LowRTTBasedIOD:
		rttFast := uint64(fastPath.rttStats.SmoothedRTT())
		rttSlow := uint64(slowPath.rttStats.SmoothedRTT())
		if rttFast != 0 {
			overhang = uint64(math.Ceil(float64(rttSlow) / float64(rttFast)))
		} else {
			// prevent divide by zero
			overhang = 1
		}
	}

	sentSlow := false
	sentFEC := false

	// schedule and prepare packets to send
	for i := 0; i < protocol.SIOD_MaxSchedulingWindow; i++ {
		hasFECFrame := len(fecFramer.transmissionQueue) > 0

		if sentFEC && !hasFECFrame {
			// Reed-Solomon FEC sends the coded symbols as a continuous group
			// we ensure that this group is sent in full, only stopping once we
			// encounter a non-coded packet again
			break
		}
		sentFEC = hasFECFrame

		// packet packing starts here
		var pth *path
		if hasFECFrame {
			// for packets with FEC frames
			if bytesSent[slowPath] < slowPath.GetCongestionWindowFree() {
				// prefer usage of slower path
				pth = slowPath
			} else if bytesSent[fastPath] < fastPath.GetCongestionWindowFree() {
				// send on fast path if necessary
				pth = fastPath
			} else {
				break
			}
		} else {
			// regular packets
			if bytesSent[fastPath] < fastPath.GetCongestionWindowFree() && overhang > 0 {
				// send n packets on fast path
				pth = fastPath
				overhang--
			} else if !sentSlow && !sentFEC && bytesSent[slowPath] < slowPath.GetCongestionWindowFree() {
				// send one (and only one) packet in the slow path
				// only if we have not used this path for transmission of FEC data
				pth = slowPath
				sentSlow = true
			} else {
				break
			}
		}

		packet, newHasStreamRetransmission, err := packPacket(pth, s, sch, hasStreamRetransmission)
		hasStreamRetransmission = newHasStreamRetransmission

		if err != nil {
			return nil, err
		} else if packet == nil {
			// no packet was prepared
			// sending buffer is most likely empty and no control frames are due
			break
		}

		scheduled := scheduledPacketInfo{
			path:   pth,
			packet: packet,
		}

		switch pth {
		case slowPath:
			if hasFECFrame {
				// only prepend FEC packets when on slow path
				// if they are on fast path, slow is unusable
				// in those cases we don't want to limit performance
				scheduleSlow = append([]scheduledPacketInfo{scheduled}, scheduleSlow...)
			} else {
				// append regular packets to slow path
				scheduleSlow = append(scheduleSlow, scheduled)
			}
		case fastPath:
			// append regular packets to fast path
			scheduleFast = append(scheduleFast, scheduled)
		}

		// we only subtract the packet size from the available space in the
		// congestion window after making the check
		// this matches the behaviour of the SendingAllowed() function in other
		// schedulers, where paths can be used as long as there is some space
		// left in the window
		bytesSent[pth] += protocol.ByteCount(len(packet.raw))

		if sentSlow || hasFECFrame && pth == fastPath {
			break
		}
	}

	// return scheduled packets such that the ones for the slow path are sent
	// first
	return append(scheduleSlow, scheduleFast...), nil
}
