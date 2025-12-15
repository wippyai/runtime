package adaptive

import (
	"testing"
	"time"
)

func BenchmarkControllerTick(b *testing.B) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	c := newController(cfg)

	now := time.Now()
	var ops int64

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		now = now.Add(50 * time.Millisecond)
		ops += 100
		c.tick(now, ops, 4, 3, 5)
	}
}

func BenchmarkControllerProbeEvaluate(b *testing.B) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()
	var ops int64

	// Trigger probe
	now = now.Add(100 * time.Millisecond)
	ops = 1000
	c.tick(now, ops, 1, 1, 10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Reset probe state
		c.probing = true
		c.probeStart = now

		c.baselineTput = 1000
		c.workersBefore = 1

		evalTime := now.Add(200 * time.Millisecond)
		c.tick(evalTime, ops+2000, 2, 2, 5)
	}
}
