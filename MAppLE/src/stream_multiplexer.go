package quic

import (
	//	"bytes"
	//"github.com/lucas-clemente/quic-go/internal/protocol"
	//"fmt"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"encoding/binary"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"github.com/lucas-clemente/quic-go/logger"

	//"io"
	"container/list"
	"sync"
	"time"
)

//var multiplexingPolicy string = ""

type MessageQueue struct {
	mess_list *list.List
	// isSending bool
	mutex sync.RWMutex
}

var stream_queues map[protocol.StreamID]MessageQueue

func StartMultiplexer(quic_sess Session) {
	stream_queues = make(map[protocol.StreamID]MessageQueue)
	for {
		streamMap, err := quic_sess.GetStreamMap()
		if err != nil {
			return
		}
		streamMap.mutex.RLock()
		for stream_id, stream := range streamMap.streams {
			queue := stream_queues[stream_id]
			if queue.mess_list.Len() == 0 || stream.LenOfDataForWriting() > 0 {
				continue
			}
			queue_font := queue.mess_list.Front()
			message, _ := queue_font.Value.([]byte)

			//VUVA: estimate delivary time
			estimated_latency := EstimateDelivery("cubic-reno", protocol.ByteCount(len(message)) , quic_sess.GetPaths())
			if estimated_latency >0 {
				logger.ExpLogInsertDelayEstimation(int(binary.BigEndian.Uint32(message[0:4])),estimated_latency)
			}
			go stream.Write(message)

			queue.mutex.Lock()
			// send_queue.isSending = false
			queue.mess_list.Remove(queue_font)
			queue.mutex.Unlock()
		}
		streamMap.mutex.RUnlock()
		time.Sleep(time.Duration(100000) * time.Nanosecond) // Wait for 100 microsecond
	}
}

func createNewStream(quic_session Session) Stream {
	utils.Debugf("\n Opening new stream ...")
	stream, err := quic_session.OpenStreamSync()
	if err != nil {
		utils.Errorf("\nOpenStream: %s", err)
		return nil
	}
	stream_queues[stream.StreamID()] = MessageQueue{mess_list: list.New()}
	utils.Debugf("\n Stream %d created", stream.StreamID())
	return stream
}

func addDataToStream(stream Stream, data []byte) int {

	utils.Debugf("\n addDataToStream %d", stream.StreamID())
	/*
	 *        n, err := stream.Write(data)
	 *
	 *        if err != nil {
	 *                utils.Errorf("\nStreamWrite: %s", err)
	 *                return 0
	 *        }
	 *	return n
	 */
	//stream.Write(data)
	// TODO: check if the data is sent of not
	var stream_queue MessageQueue
	stream_queue = stream_queues[stream.StreamID()]
	stream_queue.mutex.Lock()
	stream_queue.mess_list.PushBack(data)
	stream_queue.mutex.Unlock()

	return 1
}

func AllStreamsAreEmpty(quic_sess Session) bool {

	streamMap, err := quic_sess.GetStreamMap()
	if err != nil {
		return false
	}
	streamMap.mutex.RLock()

	for stream_id, datastream := range streamMap.streams {
		if datastream.LenOfDataForWriting() > 0 || stream_queues[stream_id].mess_list.Len() > 0 {
			streamMap.mutex.RUnlock()
			return false
		}
	}

	streamMap.mutex.RUnlock()
	return true
}

func CloseAllOutgoingStream(quic_session Session) {

	streamMap, err := quic_session.GetStreamMap()
	if err != nil {
		return
	}

	for _, datastream := range streamMap.streams {
		if datastream.StreamID()%2 == 1 {
			datastream.Close()
		}
	}
}

func MultiplexData(session Session, multiplexingPolicy string, data []byte) bool {
	var selected_stream Stream
	switch multiplexingPolicy {
	case "parallel":
		selected_stream = policyParallel(session)
	case "oneStream":
		selected_stream = policyOneStream(session)
	}

	if selected_stream != nil {
		addDataToStream(selected_stream, data)
		//defer selected_stream.Close()
		return true
	} else {
		return false
	}
}

func policyParallel(quic_sess Session) Stream {
	streamMap, err := quic_sess.GetStreamMap()
	if err != nil {
		return nil
	}
	streamMap.mutex.RLock()

	for id, datastream := range streamMap.streams {
		if datastream.LenOfDataForWriting() == 0 && datastream.StreamID()%2 == 1 && datastream.StreamID() > 1 {
			utils.Debugf("\n policyParallel found avaiable stream %d id %d ", id, datastream.StreamID())
			streamMap.mutex.RUnlock()
			return datastream
		}
	}

	streamMap.mutex.RUnlock()

	return createNewStream(quic_sess)

}

func policyOneStream(quic_sess Session) Stream {

	streamMap, err := quic_sess.GetStreamMap()
	if err != nil {
		return nil
	}

	streamMap.mutex.RLock()
	for id, datastream := range streamMap.streams {
		if datastream.StreamID()%2 == 1 && datastream.StreamID() >= 1 {
			utils.Debugf("\n vuva: id %d stream %d", id, datastream.StreamID())
			streamMap.mutex.RUnlock()
			return datastream
		}
	}
	streamMap.mutex.RUnlock()
	return createNewStream(quic_sess)

}
