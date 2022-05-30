package quic

import (
	"time"
    "github.com/gammazero/deque"
)

type nmObject struct {
	value int
	time time.Time
}

type networkMonitor struct {
	Duration int
	Deque *deque.Deque
}

func (nm *networkMonitor) setup(duration int) {
	nm.Duration = duration
	nm.Deque = &deque.Deque{}
}

func (nm *networkMonitor) push(value int) {
	event := &nmObject{
		value:  value,
		time:  time.Now(),
	}
	nm.Deque.PushBack(event)
}

func (nm *networkMonitor) update() {
	for {
		// If there are no more items
		if (nm.Deque.Len() == 0) {
			break
		}

		var event = nm.Deque.Front().(*nmObject)
		var duration = time.Since(event.time).Milliseconds()
		if (duration > int64(nm.Duration)) {
			// Pop data
			nm.Deque.PopFront()
			continue
		}
		break
	}
}

func (nm *networkMonitor) getSum() (int) {
	nm.update()
	var sum = 0;
	for i := 0; i < nm.Deque.Len(); i++ {
        sum += nm.Deque.At(i).(*nmObject).value
    }
	return sum
}

func (nm *networkMonitor) clear() () {
	nm.Deque.Clear()
}