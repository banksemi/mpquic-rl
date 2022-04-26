package utils

import "sync/atomic"

// An AtomicBool is an atomic bool
type AtomicUint64 struct {
	v uint64
}

// Set sets the value
func (a *AtomicUint64) Set(value uint64) {
	atomic.StoreUint64(&a.v, value)
}

// Get gets the value
func (a *AtomicUint64) Get() uint64 {
	return atomic.LoadUint64(&a.v)
}

func (a *AtomicUint64) Increment(delta uint64) {
	atomic.AddUint64(&a.v, delta)
}
func (a *AtomicUint64) Decrement(delta uint64) {
	atomic.AddUint64(&a.v, ^uint64(delta-1))
}
