package utils

import "github.com/lucas-clemente/quic-go/internal/protocol"

type PacketsRingBuffer interface {
	// returns the index on which to get the packet
	AddPacket([]byte) int
	Get(int) []byte
	IsFull() bool
	CurrentSize() int
	GetAll() [][]byte
}

type packetsRingBuffer struct {
	array       [protocol.MaxFECWindowSize][]byte
	startIndex  int
	currentSize int
	maxSize     int
}

var _ PacketsRingBuffer = &packetsRingBuffer{}

func NewPacketsRingBuffer(maxSize uint8) *packetsRingBuffer {
	return &packetsRingBuffer{
		maxSize: int(maxSize),
	}
}

func (b *packetsRingBuffer) AddPacket(packet []byte) int {
	if b.currentSize == b.maxSize {
		// make some space for the new packet: discard the oldest packet
		b.startIndex = (b.startIndex + 1) % len(b.array)
		b.currentSize--
	}
	index := (b.startIndex + b.currentSize) % len(b.array)
	b.array[index] = packet
	b.currentSize++
	return index
}

func (b *packetsRingBuffer) Get(i int) []byte {
	return b.array[(b.startIndex+i)%len(b.array)]
}

func (b *packetsRingBuffer) IsFull() bool {
	return b.currentSize == b.maxSize
}

func (b *packetsRingBuffer) CurrentSize() int {
	return b.currentSize
}

func (b *packetsRingBuffer) GetAll() [][]byte {
	retVal := make([][]byte, b.currentSize)
	for i := b.startIndex; i < Min(len(b.array), b.startIndex+b.currentSize); i++ {
		retVal[i-b.startIndex] = b.array[i]
	}
	if b.startIndex+b.currentSize > len(b.array) {
		for i := 0; i < (b.startIndex+b.currentSize)%len(b.array); i++ {
			retVal[len(b.array)-b.startIndex+i] = b.array[i]
		}
	}
	return retVal
}
