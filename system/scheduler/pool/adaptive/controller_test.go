package adaptive

import (
	"testing"
	"time"
)

func TestControllerScaleUp(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()
	var ops int64 = 0

	// Tick 1: low load, no action
	d, _ := c.tick(now, ops, 1, 0, 0)
	if d != scaleNone {
		t.Errorf("expected none, got %d", d)
	}

	// Tick 2: pressure (all busy + queue)
	now = now.Add(100 * time.Millisecond)
	ops = 100
	d, _ = c.tick(now, ops, 1, 1, 5)
	if d != scaleUp {
		t.Errorf("expected scaleUp, got %d", d)
	}

	if !c.probing {
		t.Error("expected probing state")
	}
}

func TestControllerProbeSuccess(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()
	var ops int64 = 0

	// Build up baseline EMA with multiple ticks at 1000 ops/s
	for i := 0; i < 5; i++ {
		now = now.Add(100 * time.Millisecond)
		ops += 100
		c.tick(now, ops, 1, 0, 0)
	}

	// Start probe (saturated: all busy + queue)
	now = now.Add(100 * time.Millisecond)
	ops += 100
	d, _ := c.tick(now, ops, 1, 1, 5)
	if d != scaleUp {
		t.Errorf("expected scaleUp to start probe, got %d", d)
	}

	// During probe: much higher throughput (2000 ops/s) to show improvement
	// Multiple ticks let EMA converge
	for i := 0; i < 3; i++ {
		now = now.Add(100 * time.Millisecond)
		ops += 200
		d, _ = c.tick(now, ops, 2, 2, 3)
	}

	if d != probeSuccess {
		t.Errorf("expected probeSuccess, got %d", d)
	}
}

func TestControllerProbeFail(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()

	// Tick 1: establish baseline (need elapsed > 50ms)
	now = now.Add(100 * time.Millisecond)
	d, _ := c.tick(now, 100, 1, 1, 5)
	if d != scaleUp {
		t.Errorf("expected scaleUp to start probe, got %d", d)
	}

	// Wait probe duration (probeTicks * interval = 200ms)
	now = now.Add(200 * time.Millisecond)

	// Poor improvement: 1000 ops/s baseline -> 1100 ops/s = 10% improvement
	d, _ = c.tick(now, 100+220, 2, 2, 3)
	if d != probeFail {
		t.Errorf("expected probeFail, got %d", d)
	}
}

func TestControllerScaleDown(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.IdleTicks = 3
	c := newController(cfg)

	now := time.Now()
	var ops int64 = 1000

	// Simulate 4 workers, only 1 busy, no queue
	for i := 0; i < 3; i++ {
		now = now.Add(100 * time.Millisecond)
		ops += 10
		d, target := c.tick(now, ops, 4, 1, 0)
		if i < 2 {
			if d != scaleNone {
				t.Errorf("tick %d: expected none, got %d", i, d)
			}
		} else {
			if d != scaleDown {
				t.Errorf("tick %d: expected scaleDown, got %d", i, d)
			}
			if target != 2 {
				t.Errorf("expected target 2, got %d", target)
			}
		}
	}
}

func TestControllerBottleneckSimulation(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()
	var ops int64 = 0
	workers := int32(1)

	opsPerInterval := int64(10) // 100 ops/s constant (bottleneck)

	scaleUps := 0
	probeFails := 0
	probeSuccesses := 0

	for i := 0; i < 50; i++ {
		now = now.Add(100 * time.Millisecond)
		ops += opsPerInterval

		busy := workers
		queueLen := 10

		d, target := c.tick(now, ops, workers, busy, queueLen)

		switch d {
		case scaleUp:
			workers++
			scaleUps++
		case probeSuccess:
			probeSuccesses++
		case probeFail:
			workers--
			probeFails++
		case scaleDown:
			workers = target
		}

		if workers < 1 {
			workers = 1
		}
		if workers > 8 {
			workers = 8
		}
	}

	t.Logf("Bottleneck=1: final workers=%d, scaleUps=%d, probeFails=%d, successes=%d",
		workers, scaleUps, probeFails, probeSuccesses)

	if probeFails == 0 && scaleUps > 2 {
		t.Errorf("Expected probe failures when bottleneck prevents improvement")
	}
}

func TestControllerIOBoundSimulation(t *testing.T) {
	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 2
	c := newController(cfg)

	now := time.Now()
	var ops int64 = 0
	workers := int32(1)

	scaleUps := 0
	probeSuccesses := 0
	probeFails := 0

	for i := 0; i < 50; i++ {
		now = now.Add(100 * time.Millisecond)

		// Throughput scales linearly with workers (I/O bound)
		opsPerWorker := int64(10)
		ops += opsPerWorker * int64(workers)

		busy := workers
		queueLen := 10

		d, _ := c.tick(now, ops, workers, busy, queueLen)

		switch d {
		case scaleUp:
			workers++
			scaleUps++
		case probeSuccess:
			probeSuccesses++
		case probeFail:
			workers--
			probeFails++
		}

		if workers < 1 {
			workers = 1
		}
		if workers > 8 {
			workers = 8
		}
	}

	t.Logf("I/O-bound: final workers=%d, scaleUps=%d, successes=%d, fails=%d",
		workers, scaleUps, probeSuccesses, probeFails)

	if workers < 6 {
		t.Errorf("Expected to scale up for I/O-bound workload, got %d workers", workers)
	}
}
