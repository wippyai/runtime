// Package worksim provides workload simulation for testing adaptive pool behavior.
//
// The simulator allows dynamic control of:
//   - Bottleneck: max concurrent workers that can make progress (simulates lock contention, CPU cores)
//   - Latency: base processing time per operation
//   - Jitter: random variation in latency (0.0-1.0 ratio)
package worksim

import (
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// Workload simulates controllable work patterns.
// Safe for concurrent use.
type Workload struct {
	sem        chan struct{}
	bottleneck int
	latencyNs  atomic.Int64
	completed  atomic.Int64
	mu         sync.Mutex
	jitterPct  atomic.Uint32
	active     atomic.Int32
	waiting    atomic.Int32
	maxActive  atomic.Int32
}

// New creates a workload simulator.
// Initial state: no bottleneck, zero latency.
func New() *Workload {
	return &Workload{}
}

// SetBottleneck sets max concurrent workers that can make progress.
// n=0 means unlimited (no bottleneck).
// Workers beyond this limit block until others complete.
func (w *Workload) SetBottleneck(n int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.bottleneck = n
	if n > 0 {
		// Create new semaphore with n tokens
		w.sem = make(chan struct{}, n)
		for i := 0; i < n; i++ {
			w.sem <- struct{}{}
		}
	} else {
		w.sem = nil
	}
}

// SetLatency sets base processing time per operation.
func (w *Workload) SetLatency(d time.Duration) {
	w.latencyNs.Store(int64(d))
}

// SetJitter sets random variation in latency as a ratio (0.0-1.0).
// Actual latency = base * (1 + random(-jitter, +jitter))
func (w *Workload) SetJitter(ratio float32) {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	w.jitterPct.Store(uint32(ratio * 100))
}

// Completed returns total completed operations.
func (w *Workload) Completed() int64 {
	return w.completed.Load()
}

// Active returns currently executing operations.
func (w *Workload) Active() int32 {
	return w.active.Load()
}

// Waiting returns operations blocked on bottleneck.
func (w *Workload) Waiting() int32 {
	return w.waiting.Load()
}

// MaxActive returns peak concurrent active operations observed.
func (w *Workload) MaxActive() int32 {
	return w.maxActive.Load()
}

// ResetMetrics clears all metrics.
func (w *Workload) ResetMetrics() {
	w.completed.Store(0)
	w.maxActive.Store(0)
}

// Work simulates doing work. Blocks according to bottleneck and latency settings.
// Returns context error if canceled while waiting.
func (w *Workload) Work(ctx context.Context) error {
	// Get semaphore reference under lock (atomic read of channel)
	w.mu.Lock()
	sem := w.sem
	w.mu.Unlock()

	// Bottleneck simulation: acquire token
	if sem != nil {
		w.waiting.Add(1)
		select {
		case <-sem: // acquire token
			w.waiting.Add(-1)
		case <-ctx.Done():
			w.waiting.Add(-1)
			return ctx.Err()
		}
		defer func() { sem <- struct{}{} }() // release token
	}

	// Track active
	active := w.active.Add(1)
	defer w.active.Add(-1)

	// Update high water mark
	for {
		currentMax := w.maxActive.Load()
		if active <= currentMax {
			break
		}
		if w.maxActive.CompareAndSwap(currentMax, active) {
			break
		}
	}

	// Latency simulation
	latencyNs := w.latencyNs.Load()
	if latencyNs > 0 {
		jitterPct := w.jitterPct.Load()
		if jitterPct > 0 {
			jitter := float64(jitterPct) / 100.0
			//nolint:gosec // G404: weak random acceptable for simulation
			factor := 1.0 + (rand.Float64()*2-1)*jitter
			latencyNs = int64(float64(latencyNs) * factor)
		}

		if latencyNs > 0 {
			timer := time.NewTimer(time.Duration(latencyNs))
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
	}

	w.completed.Add(1)
	return nil
}
