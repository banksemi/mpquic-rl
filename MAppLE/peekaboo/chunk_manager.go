package quic

import (
	"sync"
	"time"
	"regexp"
    "strconv"
	"net/http"

	goldlog "github.com/aunum/log"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
)

type chunkObject struct {
	contentBytes protocol.ByteCount
	sendBytes protocol.ByteCount
	receiveBytes protocol.ByteCount
	startTime time.Time
}

type chunkManager struct {
	chunks			         map[int] *chunkObject
	chunks_from_stream_id    map[protocol.StreamID] *chunkObject
	segmentNumber            int
	mutex sync.RWMutex
}

var cm_instance *chunkManager = nil;
func GetChunkManager() *chunkManager{
	if (cm_instance == nil) {
		cm_instance = &chunkManager{
			chunks: make(map[int]*chunkObject),
			chunks_from_stream_id: make(map[protocol.StreamID]*chunkObject),
			segmentNumber : -1,
		}
	}
	return cm_instance
}

func (cm *chunkManager) ServeHTTP(w http.ResponseWriter, r *http.Request, size int64) {
	re, _ := regexp.Compile("chunk-stream[0-9]+-([0-9]+).m4s")
	reqPath := r.URL.Path

	matches := re.FindStringSubmatch(reqPath)

	if (len(matches) > 0) {
		if (cm.chunks[cm.segmentNumber] != nil) {
			if (cm.chunks[cm.segmentNumber].sendBytes != cm.chunks[cm.segmentNumber].contentBytes) {
				goldlog.Infof("에러")
			}
			if (cm.chunks[cm.segmentNumber].sendBytes != cm.chunks[cm.segmentNumber].receiveBytes) {
				goldlog.Infof("에러3")
			}
			goldlog.Infof("%d 번 청크 결과 %d / %d (수신함 %d)", cm.segmentNumber, cm.chunks[cm.segmentNumber].sendBytes, cm.chunks[cm.segmentNumber].contentBytes, cm.chunks[cm.segmentNumber].receiveBytes)
		}
		cm.segmentNumber, _ = strconv.Atoi(matches[1])
		cm.mutex.Lock()
		if (cm.chunks[cm.segmentNumber] == nil) {
			cm.chunks[cm.segmentNumber]  = &chunkObject{
				sendBytes: 0, 
				receiveBytes: 0,
				contentBytes: protocol.ByteCount(size),
				startTime: time.Now(),
			}
		}
		cm.mutex.Unlock()
		goldlog.Infof("청크 생성 %d",cm.segmentNumber, cm.chunks[cm.segmentNumber])
	}
}

func (cm *chunkManager) sendPacket(f *wire.StreamFrame, event *RLEvent) {
	maxOffset := f.Offset + f.DataLen()
	if (f.StreamID == 3) {
		return
	}
	goldlog.Infof("%s [전송 %d] %d + %d", time.Now(), cm.segmentNumber, f.Offset, f.DataLen())
	if (cm.chunks[cm.segmentNumber] != nil) {
		// Stream index mapping
		cm.chunks_from_stream_id[f.StreamID] = cm.chunks[cm.segmentNumber]


		event.MaxOffset = maxOffset

		if (maxOffset >= cm.chunks[cm.segmentNumber].sendBytes) {
			goldlog.Infof("%s [전송 %d]  업데이트 %d", time.Now(), cm.segmentNumber, maxOffset)

			// Retransmitted packets are not considered
			cm.chunks[cm.segmentNumber].sendBytes = maxOffset
			event.DataLen += f.DataLen() 
			if (event.SegmentNumber == -1 || event.SegmentNumber == cm.segmentNumber) {
				event.SegmentNumber = cm.segmentNumber
			} else {
				// goldlog.Infof("에러")
				// 한개의 패킷에,,, 두개의 서로 다른 새로운 바이트 할당?
				goldlog.Infof("에러2")
			}
		}
	}
}

func (cm *chunkManager) receivePacket(event *RLEvent) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if (event.SegmentNumber != -1) {
		// goldlog.Infof("\t%s [수신 %d] (packet %d) %d (include %d)", time.Now(), event.SegmentNumber, event.PacketNumber, event.MaxOffset, event.DataLen)
		if (event.MaxOffset > cm.chunks[event.SegmentNumber].receiveBytes) {
			cm.chunks[event.SegmentNumber].receiveBytes = event.MaxOffset
		}
	}
}

func (cm *chunkManager) remainBytesByClient(segmentNumber int) protocol.ByteCount {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if (cm.chunks[segmentNumber] != nil) { 
		return cm.chunks[segmentNumber].contentBytes - cm.chunks[segmentNumber].receiveBytes
	} else {
		return 1234
	}
}

func (cm *chunkManager) remainBytesByServer(segmentNumber int) protocol.ByteCount {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if (cm.chunks[segmentNumber] != nil) { 
		return cm.chunks[segmentNumber].contentBytes - cm.chunks[segmentNumber].sendBytes
	} else {
		return 1234
	}
}