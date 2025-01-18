package engine

import (
	"sync/atomic"
)

const (
	stateNormal    int32 = 0 // Not blocked
	stateBlocked   int32 = 1 // Blocked, not notified
	stateUnblocked int32 = 2 // Blocked and notified
)

type (
	Blocker struct {
		count  atomic.Int32
		state  int32
		notify chan<- LayerState
		layer  Layer
	}

	LayerState struct {
		Layer Layer
		Tasks int
	}

	Blockable interface {
		SetNotify(chan LayerState)
	}
)

func NewBlocker(layer Layer, notify chan<- LayerState) *Blocker {
	return &Blocker{
		layer:  layer,
		notify: notify,
	}
}

func (b *Blocker) Add() {
	b.count.Add(1)
}

func (b *Blocker) Done() {
	b.count.Add(-1)

	if atomic.CompareAndSwapInt32(&b.state, stateBlocked, stateUnblocked) {
		select {
		case b.notify <- LayerState{Layer: b.layer, Tasks: 0}:
		default:
			// must never happen
		}
	}
}

func (b *Blocker) FlushState() {
	if b.IsBlocked() {

		if atomic.CompareAndSwapInt32(&b.state, stateNormal, stateBlocked) || atomic.CompareAndSwapInt32(&b.state, stateUnblocked, stateBlocked) {
			select {
			case b.notify <- LayerState{Layer: b.layer, Tasks: int(b.count.Load())}:
			default:
				// must never happen
			}
		}
	} else {
		atomic.StoreInt32(&b.state, stateNormal)
	}
}

func (b *Blocker) IsBlocked() bool {
	return b.count.Load() > 0
}

func (b *Blocker) NumTasks() int {
	return int(b.count.Load())
}
