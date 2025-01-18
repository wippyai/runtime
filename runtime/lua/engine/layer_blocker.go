package engine

import "sync/atomic"

type LayerBlocker struct {
	count             atomic.Int32
	notify            chan<- Layer
	layer             Layer
	notifiedBlocked   atomic.Bool // true if we've notified parent we're blocked in this cycle
	notifiedUnblocked atomic.Bool // true if we've notified parent we're unblocked in this cycle
}

func NewLayerBlocker(layer Layer, notify chan<- Layer) *LayerBlocker {
	return &LayerBlocker{
		layer:  layer,
		notify: notify,
	}
}

func (b *LayerBlocker) Add() {
	b.count.Add(1)
}

func (b *LayerBlocker) Done() {
	b.count.Add(-1)

	// If we were notified as blocked and haven't sent unblock notification yet
	if b.notifiedBlocked.Load() && !b.notifiedUnblocked.Load() {
		select {
		case b.notify <- b.layer:
		default:
			// must never happen
		}
		b.notifiedUnblocked.Store(true)
		b.notifiedBlocked.Store(false)
	}
}

func (b *LayerBlocker) FlushState() {
	if b.IsBlocked() {
		// Only notify if:
		// 1. We haven't sent blocked notification yet AND
		// 2. We haven't sent any unblock notifications (meaning these are fresh blocks)
		if !b.notifiedBlocked.Load() && !b.notifiedUnblocked.Load() {
			select {
			case b.notify <- b.layer:
			default:
				// must never happen
			}
			b.notifiedBlocked.Store(true)
			b.notifiedUnblocked.Store(false)
		}
	} else {
		// Reset both flags when not blocked
		b.notifiedBlocked.Store(false)
		b.notifiedUnblocked.Store(false)
	}
}

func (b *LayerBlocker) IsBlocked() bool {
	return b.count.Load() > 0
}
