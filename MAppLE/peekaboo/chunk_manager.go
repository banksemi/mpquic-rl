package quic

import (
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
			goldlog.Infof("%d 번 청크 결과 %d / %d (수신함 %d)", cm.segmentNumber, cm.chunks[cm.segmentNumber].sendBytes, cm.chunks[cm.segmentNumber].contentBytes, cm.chunks[cm.segmentNumber].receiveBytes)
		}
		cm.segmentNumber, _ = strconv.Atoi(matches[1])
		if (cm.chunks[cm.segmentNumber] == nil) {
			cm.chunks[cm.segmentNumber] = &chunkObject{
				sendBytes: 0, 
				receiveBytes: 0,
				contentBytes: protocol.ByteCount(size),
				startTime: time.Now(),
			}
		}
		goldlog.Infof("청크 생성 %d",cm.segmentNumber,  cm.chunks[cm.segmentNumber])
	}
}

func (cm *chunkManager) sendPacket(f *wire.StreamFrame) {
	// goldlog.Infof("%d, %d + %d 전송", f.StreamID, f.Offset, f.DataLen())

	if (f.StreamID == 3) {
		return
	}
	if (cm.chunks[cm.segmentNumber] != nil) {
		// Stream index mapping
		cm.chunks_from_stream_id[f.StreamID] = cm.chunks[cm.segmentNumber]

		if (f.Offset >= cm.chunks[cm.segmentNumber].sendBytes) {
			// Retransmitted packets are not considered
			cm.chunks[cm.segmentNumber].sendBytes += f.DataLen()
		}
	}
}

func (cm *chunkManager) receivePacket(f *wire.AckFrame) {
	// goldlog.Infof("수신", f)
	// cm.chunks_from_stream_id[f.StreamID].receiveBytes += f.DataLen()
}