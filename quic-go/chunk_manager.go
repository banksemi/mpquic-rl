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

type ackObject struct {
	offset int
	maxoffset int
}

type chunkObject struct {
	deque *Deque
	contentBytes protocol.ByteCount
	sendBytes protocol.ByteCount
	receiveBytes protocol.ByteCount
	startTime time.Time
	exploration              bool
	finished bool
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
			segmentNumber: -1,
		}
	}
	return cm_instance
}
func (cm *chunkManager) receivedChunk(segmentNumber int) int{
	// 1. 청크 모든 큐에 속함
	var deque *Deque = cm.chunks[segmentNumber].deque

	for {
		if (deque.Len() >= 2) {
			chunk1 := deque.At(0).(*ackObject)
			chunk2 := deque.At(1).(*ackObject)
			if (chunk1.maxoffset == chunk2.offset) {
				// Pop data
				chunk2.offset = chunk1.offset
				deque.PopFront()
			} else {
				break
			}
		} else {
			break
		}
	}	
	if (deque.Len() == 0) {
		return 0
	} else {
		chunk1 := deque.At(0).(*ackObject)
		if (chunk1.offset == 0) {
			return chunk1.maxoffset
		} else {
			return 0
		}
	}
}
func (cm *chunkManager) AddPartChunk(segmentNumber int, offset int, size int) {
	// goldlog.Infof("%s %d 청크 부분 전달 %d ~ %d", time.Now(), segmentNumber, offset, offset + size);
	var deque *Deque = cm.chunks[segmentNumber].deque
	for i := 0; i < deque.Len(); i++ {
		for {
			if (size == 0) {
				return
			}
			chunk := deque.At(i).(*ackObject)
			if (chunk.maxoffset <= offset) { // 앞에 떨어져있거나, 완전하게 붙어있는건 생각 X
				break
			}
			if (offset < chunk.offset) { // 시작점이 다음 청크보다 앞에 있으면
				if (offset+size < chunk.offset) { // 끝점이 시작점보다 앞에 있으면
					// Add chunk offset, size
					deque.Insert(i, &ackObject{offset: offset, maxoffset: offset+size})
					return
				}
				if (offset+size == chunk.offset) { // ACK 끝점과 지금 청크의 시작점이 같으면
					chunk.offset = offset // 시작점 위치 변경
					return
				}
				if (offset+size > chunk.offset) { // offset을 넘어섰으면
					chunk.offset = offset // 시작점 위치 변경
					continue // 한번 더 생각
				}
			}
			if (offset == chunk.offset) { // 시작점이 같으면
				if (offset+size < chunk.maxoffset) { // ACK 끝점이 최대값 안에 있으면
					return // 그냥 끝
				}
				if (offset+size == chunk.maxoffset) { // ACK 끝점이 최대값과 같으면
					return // 그냥 끝
				}
				if (offset+size > chunk.maxoffset) { // ACK 끝점이 최대값을 넘어섰으면
					move := chunk.maxoffset - offset
					offset += move
					size -= move
					// ACK 끝점으로 이동한 상태
					break // 다음 Chunk로 넘기기
				}
			}
			if (offset > chunk.offset && chunk.maxoffset < offset) { // 시작점이 안에 있으면
				
				if (offset+size < chunk.maxoffset) { // ACK 끝점이 최대값 안에 있으면
					return // 그냥 끝
				}
				if (offset+size == chunk.maxoffset) { // ACK 끝점이 최대값과 같으면
					return // 그냥 끝
				}
				if (offset+size > chunk.maxoffset) { // ACK 끝점이 최대값을 넘어섰으면
					move := chunk.maxoffset - offset
					offset += move
					size -= move
					// ACK 끝점으로 이동한 상태
					break // 다음 Chunk로 넘기기

				}
			} else {
				break // 이런 경우 그냥 다음 청크를 봐야함
			}
		}
	}
	if (size > 0) {
		deque.PushBack(&ackObject{offset: offset, maxoffset: offset+size})
	}
}
func (cm *chunkManager) ServeHTTP(w http.ResponseWriter, r *http.Request, size int64) {
	bs := r.Header.Get("Buffer-Current-Size")
	buffer_size, _ := strconv.Atoi(bs)

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
		cm.mutex.Lock()
		cm.segmentNumber, _ = strconv.Atoi(matches[1])
		if (cm.chunks[cm.segmentNumber] == nil) {
			exploration := false
			if (buffer_size >= 10) {
				exploration = true
			}

			cm.chunks[cm.segmentNumber]  = &chunkObject{
				sendBytes: 0, 
				receiveBytes: 0,
				deque: &Deque{},
				contentBytes: protocol.ByteCount(size),
				startTime: time.Now(),
				exploration: exploration,
				finished: false,
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
	// goldlog.Infof("%s [전송 %d] %d + %d", time.Now(), cm.segmentNumber, f.Offset, f.DataLen())
	if (cm.chunks[cm.segmentNumber] != nil) {
		// Stream index mapping
		cm.chunks_from_stream_id[f.StreamID] = cm.chunks[cm.segmentNumber]


		event.MaxOffset = maxOffset
		event.Offset = f.Offset
		event.Size = f.DataLen()

		if (maxOffset >= cm.chunks[cm.segmentNumber].sendBytes) {
			// goldlog.Infof("%s [전송 %d]  업데이트 %d", time.Now(), cm.segmentNumber, maxOffset)

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

func (cm *chunkManager) receivePacket(s *session, event *RLEvent) (bool, protocol.ByteCount, time.Duration) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if (event.SegmentNumber != -1) {
		// goldlog.Infof("\t%s [수신 %d] (packet %d) %d (include %d)", time.Now(), event.SegmentNumber, event.PacketNumber, event.MaxOffset, event.DataLen)
		
		cm.AddPartChunk(event.SegmentNumber, int(event.Offset), int(event.Size))
		if (event.MaxOffset > cm.chunks[event.SegmentNumber].receiveBytes) {
			cm.chunks[event.SegmentNumber].receiveBytes = protocol.ByteCount(cm.receivedChunk(event.SegmentNumber)) // event.MaxOffset
		}
		// goldlog.Infof("%s %d 청크 %d 상태", time.Now(), event.SegmentNumber, cm.receivedChunk(event.SegmentNumber));
		if (!cm.chunks[event.SegmentNumber].finished) {
			if (cm.chunks[event.SegmentNumber].contentBytes == cm.chunks[event.SegmentNumber].receiveBytes) {
				cm.chunks[event.SegmentNumber].finished = true
				if (cm.chunks[event.SegmentNumber].exploration == false) {
					goldlog.Infof("[청크 마무리 단계0]")

				}
				return true, cm.chunks[event.SegmentNumber].contentBytes, time.Since(cm.chunks[event.SegmentNumber].startTime)
			}
		}
	}
	return false, 0, time.Duration(0)
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
func (cm *chunkManager) getElapsedTime() time.Duration {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if (cm.chunks[cm.segmentNumber] != nil) { 
		return time.Since(cm.chunks[cm.segmentNumber].startTime)
	} else {
		return 0
	}
}


func (cm *chunkManager) getExploration() bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if (cm.chunks[cm.segmentNumber] != nil) { 
		return cm.chunks[cm.segmentNumber].exploration
	} else {
		return false
	}
}