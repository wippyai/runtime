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
	var ops int64

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
	var ops int64

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
	var ops int64

	// Build up baseline with stable throughput (1000 ops/s)
	for i := 0; i < 10; i++ {
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

	// During probe: throughput drops significantly (degradation)
	// Multiple ticks with lower throughput and worse queue
	for i := 0; i < 3; i++ {
		now = now.Add(100 * time.Millisecond)
		ops += 50                         // 500 ops/s - 50% drop
		d, _ = c.tick(now, ops, 2, 2, 10) // queue got worse
	}

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
	// With additive decrease, we remove 1 at a time: 4 -> 3
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
			// Additive decrease: target = workers - 1 = 3 (keeping busy+1=2 headroom)
			if target != 3 {
				t.Errorf("expected target 3 (additive decrease), got %d", target)
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
	var ops int64
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
		case scaleNone:
			// No action
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
	var ops int64
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

		d, target := c.tick(now, ops, workers, busy, queueLen)

		switch d {
		case scaleNone, scaleDown:
			// No action
		case scaleUp:
			// Multiplicative scale-up: target = workers to add
			workers += target
			scaleUps++
		case probeSuccess:
			probeSuccesses++
		case probeFail:
			// target = workers to remove
			workers -= target
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

	// I/O-bound workload should scale up (more successes than fails indicates scaling worked)
	if scaleUps == 0 {
		t.Errorf("Expected at least one scale-up for I/O-bound workload")
	}
}
