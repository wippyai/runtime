package adaptive

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/system/scheduler/pool/adaptive/worksim"
)

type simResult struct {
	scaleUps       int
	scaleDowns     int
	probeSuccesses int
	probeFails     int
	totalOps       int64
	avgWorkers     float64
	finalWorkers   int32
	maxWorkers     int32
	minWorkers     int32
	maxActive      int32
}

func runSimulation(t *testing.T, cfg ControllerConfig, workload *worksim.Workload, duration time.Duration) simResult {
	t.Helper()

	c := newController(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var (
		workers    atomic.Int32
		busyCount  atomic.Int32
		queueLen   atomic.Int32
		opsCount   atomic.Int64
		workerWg   sync.WaitGroup
		workerDone = make(chan struct{})
	)

	workers.Store(int32(cfg.MinWorkers))

	result := simResult{
		minWorkers: int32(cfg.MaxWorkers),
	}

	startWorker := func() {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case <-workerDone:
					return
				default:
				}

				currentWorkers := workers.Load()
				currentBusy := busyCount.Load()
				if currentBusy >= currentWorkers {
					time.Sleep(time.Millisecond)
					continue
				}

				busyCount.Add(1)
				err := workload.Work(ctx)
				busyCount.Add(-1)

				if err == nil {
					opsCount.Add(1)
				}
			}
		}()
	}

	for i := 0; i < cfg.MinWorkers; i++ {
		startWorker()
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if busyCount.Load() >= workers.Load() {
				queueLen.Store(10)
			} else {
				queueLen.Store(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	ticker := time.NewTicker(cfg.ControlInterval)
	defer ticker.Stop()

	var workerSamples int64
	var workerSum int64

	for {
		select {
		case <-ctx.Done():
			close(workerDone)
			workerWg.Wait()

			result.finalWorkers = workers.Load()
			result.totalOps = opsCount.Load()
			result.maxActive = workload.MaxActive()
			if workerSamples > 0 {
				result.avgWorkers = float64(workerSum) / float64(workerSamples)
			}
			return result

		case now := <-ticker.C:
			currentWorkers := workers.Load()
			currentBusy := busyCount.Load()
			currentQueue := int(queueLen.Load())
			currentOps := opsCount.Load()

			workerSamples++
			workerSum += int64(currentWorkers)
			if currentWorkers > result.maxWorkers {
				result.maxWorkers = currentWorkers
			}
			if currentWorkers < result.minWorkers {
				result.minWorkers = currentWorkers
			}

			d, target := c.tick(now, currentOps, currentWorkers, currentBusy, currentQueue)

			switch d {
			case scaleNone:
				// No action
			case scaleUp:
				toAdd := target
				if currentWorkers+toAdd > int32(cfg.MaxWorkers) {
					toAdd = int32(cfg.MaxWorkers) - currentWorkers
				}
				for i := int32(0); i < toAdd; i++ {
					workers.Add(1)
					startWorker()
				}
				if toAdd > 0 {
					result.scaleUps++
				}
			case probeSuccess:
				result.probeSuccesses++
			case probeFail:
				toRemove := target
				if currentWorkers-toRemove < int32(cfg.MinWorkers) {
					toRemove = currentWorkers - int32(cfg.MinWorkers)
				}
				workers.Add(-toRemove)
				if toRemove > 0 {
					result.probeFails++
				}
			case scaleDown:
				if target >= int32(cfg.MinWorkers) && target < currentWorkers {
					diff := currentWorkers - target
					workers.Store(target)
					result.scaleDowns += int(diff)
				}
			}
		}
	}
}

func TestControllerWorksim_Bottleneck1(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetBottleneck(1)
	workload.SetLatency(5 * time.Millisecond)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 4

	result := runSimulation(t, cfg, workload, 3*time.Second)

	t.Logf("Bottleneck=1: workers=%d (max=%d, avg=%.1f), ops=%d, maxActive=%d, scaleUps=%d, probeFails=%d, successes=%d",
		result.finalWorkers, result.maxWorkers, result.avgWorkers, result.totalOps, result.maxActive,
		result.scaleUps, result.probeFails, result.probeSuccesses)

	if result.avgWorkers > 3 {
		t.Errorf("Expected avg workers <= 3 for bottleneck=1, got %.1f", result.avgWorkers)
	}
	if result.maxActive > 1 {
		t.Errorf("Expected maxActive=1, got %d", result.maxActive)
	}
}

func TestControllerWorksim_Bottleneck2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetBottleneck(2)
	workload.SetLatency(5 * time.Millisecond)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 4

	result := runSimulation(t, cfg, workload, 3*time.Second)

	t.Logf("Bottleneck=2: workers=%d (max=%d, avg=%.1f), ops=%d, maxActive=%d, scaleUps=%d, probeFails=%d, successes=%d",
		result.finalWorkers, result.maxWorkers, result.avgWorkers, result.totalOps, result.maxActive,
		result.scaleUps, result.probeFails, result.probeSuccesses)

	if result.avgWorkers > 4 {
		t.Errorf("Expected avg workers <= 4 for bottleneck=2, got %.1f", result.avgWorkers)
	}
	if result.maxActive > 2 {
		t.Errorf("Expected maxActive=2, got %d", result.maxActive)
	}
}

func TestControllerWorksim_Bottleneck4(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetBottleneck(4)
	workload.SetLatency(5 * time.Millisecond)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 4

	result := runSimulation(t, cfg, workload, 3*time.Second)

	t.Logf("Bottleneck=4: workers=%d (max=%d, avg=%.1f), ops=%d, maxActive=%d, scaleUps=%d, probeFails=%d, successes=%d",
		result.finalWorkers, result.maxWorkers, result.avgWorkers, result.totalOps, result.maxActive,
		result.scaleUps, result.probeFails, result.probeSuccesses)

	if result.avgWorkers > 6 {
		t.Errorf("Expected avg workers <= 6 for bottleneck=4, got %.1f", result.avgWorkers)
	}
	if result.maxActive > 4 {
		t.Errorf("Expected maxActive=4, got %d", result.maxActive)
	}
}

func TestControllerWorksim_IOBound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetLatency(10 * time.Millisecond)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 4

	result := runSimulation(t, cfg, workload, 3*time.Second)

	t.Logf("I/O-bound: workers=%d (max=%d, avg=%.1f), ops=%d, maxActive=%d, scaleUps=%d, probeFails=%d, successes=%d",
		result.finalWorkers, result.maxWorkers, result.avgWorkers, result.totalOps, result.maxActive,
		result.scaleUps, result.probeFails, result.probeSuccesses)

	// I/O-bound should scale up
	if result.scaleUps == 0 {
		t.Errorf("Expected scale-ups for I/O-bound workload")
	}
}

func TestControllerWorksim_HighLatencyLowBottleneck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetBottleneck(2)
	workload.SetLatency(50 * time.Millisecond)
	workload.SetJitter(0.3)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 100 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 4

	result := runSimulation(t, cfg, workload, 4*time.Second)

	t.Logf("HighLatency+Bottleneck=2: workers=%d (max=%d, avg=%.1f), ops=%d, maxActive=%d, scaleUps=%d, probeFails=%d, successes=%d",
		result.finalWorkers, result.maxWorkers, result.avgWorkers, result.totalOps, result.maxActive,
		result.scaleUps, result.probeFails, result.probeSuccesses)

	if result.avgWorkers > 5 {
		t.Errorf("Expected avg workers <= 5 for bottleneck=2 with high latency, got %.1f", result.avgWorkers)
	}
	if result.maxActive > 2 {
		t.Errorf("Expected maxActive=2, got %d", result.maxActive)
	}
}

func TestControllerWorksim_DynamicBottleneck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetLatency(5 * time.Millisecond)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 3

	c := newController(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	var (
		workers   atomic.Int32
		busyCount atomic.Int32
		queueLen  atomic.Int32
		opsCount  atomic.Int64
		workerWg  sync.WaitGroup
		workerMu  sync.Mutex
	)

	workers.Store(1)

	var phase1MaxActive, phase2MaxActive int32
	var phase1Workers, phase2Workers int32

	startWorker := func() {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				currentWorkers := workers.Load()
				currentBusy := busyCount.Load()
				if currentBusy >= currentWorkers {
					time.Sleep(time.Millisecond)
					continue
				}

				busyCount.Add(1)
				err := workload.Work(ctx)
				busyCount.Add(-1)

				if err == nil {
					opsCount.Add(1)
				}
			}
		}()
	}

	startWorker()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if busyCount.Load() >= workers.Load() {
				queueLen.Store(10)
			} else {
				queueLen.Store(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	go func() {
		workload.SetBottleneck(1)
		time.Sleep(3 * time.Second)
		phase1MaxActive = workload.MaxActive()
		phase1Workers = workers.Load()

		workload.ResetMetrics()
		workload.SetBottleneck(0)
	}()

	ticker := time.NewTicker(cfg.ControlInterval)
	defer ticker.Stop()

	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			workerWg.Wait()
			phase2MaxActive = workload.MaxActive()
			phase2Workers = workers.Load()

			t.Logf("Dynamic bottleneck: Phase1(bottleneck=1): workers=%d, maxActive=%d | Phase2(no bottleneck): workers=%d, maxActive=%d",
				phase1Workers, phase1MaxActive, phase2Workers, phase2MaxActive)

			// Phase1 should be constrained by bottleneck
			if phase1MaxActive > 2 {
				t.Errorf("Phase1: maxActive=%d should be constrained by bottleneck=1", phase1MaxActive)
			}
			return

		case now := <-ticker.C:
			currentWorkers := workers.Load()
			currentBusy := busyCount.Load()
			currentQueue := int(queueLen.Load())
			currentOps := opsCount.Load()

			d, target := c.tick(now, currentOps, currentWorkers, currentBusy, currentQueue)

			workerMu.Lock()
			switch d {
			case scaleNone, probeSuccess:
				// No action
			case scaleUp:
				toAdd := target
				if currentWorkers+toAdd > int32(cfg.MaxWorkers) {
					toAdd = int32(cfg.MaxWorkers) - currentWorkers
				}
				for i := int32(0); i < toAdd; i++ {
					workers.Add(1)
					startWorker()
				}
			case probeFail:
				toRemove := target
				if currentWorkers-toRemove < int32(cfg.MinWorkers) {
					toRemove = currentWorkers - int32(cfg.MinWorkers)
				}
				workers.Add(-toRemove)
			case scaleDown:
				if target >= int32(cfg.MinWorkers) && target < currentWorkers {
					workers.Store(target)
				}
			}
			workerMu.Unlock()

			elapsed := time.Since(start)
			if elapsed.Milliseconds()%1000 < cfg.ControlInterval.Milliseconds() {
				t.Logf("  t=%.1fs: workers=%d, busy=%d, ops=%d, maxActive=%d",
					elapsed.Seconds(), workers.Load(), currentBusy, currentOps, workload.MaxActive())
			}
		}
	}
}

func TestControllerWorksim_LowLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetLatency(1 * time.Millisecond)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 4

	result := runSimulation(t, cfg, workload, 3*time.Second)

	t.Logf("LowLatency: workers=%d (max=%d, avg=%.1f), ops=%d, maxActive=%d, scaleUps=%d, probeFails=%d, successes=%d",
		result.finalWorkers, result.maxWorkers, result.avgWorkers, result.totalOps, result.maxActive,
		result.scaleUps, result.probeFails, result.probeSuccesses)

	// Low latency should trigger scale-up attempts
	if result.scaleUps == 0 {
		t.Errorf("Expected scale-ups for low-latency workload")
	}
}

func TestControllerWorksim_GradualBottleneck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load simulation in short mode")
	}
	workload := worksim.New()
	workload.SetLatency(5 * time.Millisecond)

	cfg := DefaultControllerConfig(16)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 3

	c := newController(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Second)
	defer cancel()

	var (
		workers   atomic.Int32
		busyCount atomic.Int32
		queueLen  atomic.Int32
		opsCount  atomic.Int64
		workerWg  sync.WaitGroup
	)

	workers.Store(1)

	startWorker := func() {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				currentWorkers := workers.Load()
				currentBusy := busyCount.Load()
				if currentBusy >= currentWorkers {
					time.Sleep(time.Millisecond)
					continue
				}

				busyCount.Add(1)
				err := workload.Work(ctx)
				busyCount.Add(-1)

				if err == nil {
					opsCount.Add(1)
				}
			}
		}()
	}

	startWorker()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if busyCount.Load() >= workers.Load() {
				queueLen.Store(10)
			} else {
				queueLen.Store(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	type phase struct {
		bottleneck int
		workers    int32
		maxActive  int32
	}
	var phases []phase
	var phasesMu sync.Mutex

	go func() {
		bottlenecks := []int{8, 4, 2, 1}
		for i, b := range bottlenecks {
			workload.SetBottleneck(b)
			time.Sleep(3 * time.Second)
			phasesMu.Lock()
			phases = append(phases, phase{
				bottleneck: b,
				workers:    workers.Load(),
				maxActive:  workload.MaxActive(),
			})
			phasesMu.Unlock()
			if i < len(bottlenecks)-1 {
				workload.ResetMetrics()
			}
		}
	}()

	ticker := time.NewTicker(cfg.ControlInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			workerWg.Wait()

			phasesMu.Lock()
			phasesCopy := make([]phase, len(phases))
			copy(phasesCopy, phases)
			phasesMu.Unlock()

			t.Log("Gradual bottleneck decrease:")
			for _, p := range phasesCopy {
				t.Logf("  bottleneck=%d: workers=%d, maxActive=%d", p.bottleneck, p.workers, p.maxActive)
			}

			finalMaxActive := workload.MaxActive()
			t.Logf("Final maxActive after all phases: %d", finalMaxActive)

			// Verify all phases completed
			if len(phasesCopy) < 4 {
				t.Errorf("Expected 4 phases, got %d", len(phasesCopy))
				return
			}

			// Final phase (bottleneck=1) should show constrained activity
			// maxActive may include overlap from transitions, so allow margin
			finalPhase := phasesCopy[len(phasesCopy)-1]
			if finalPhase.maxActive > 4 {
				t.Errorf("Final phase bottleneck=1: maxActive=%d is too high", finalPhase.maxActive)
			}
			return

		case now := <-ticker.C:
			currentWorkers := workers.Load()
			currentBusy := busyCount.Load()
			currentQueue := int(queueLen.Load())
			currentOps := opsCount.Load()

			d, target := c.tick(now, currentOps, currentWorkers, currentBusy, currentQueue)

			switch d {
			case scaleNone, probeSuccess:
				// No action
			case scaleUp:
				toAdd := target
				if currentWorkers+toAdd > int32(cfg.MaxWorkers) {
					toAdd = int32(cfg.MaxWorkers) - currentWorkers
				}
				for i := int32(0); i < toAdd; i++ {
					workers.Add(1)
					startWorker()
				}
			case probeFail:
				toRemove := target
				if currentWorkers-toRemove < int32(cfg.MinWorkers) {
					toRemove = currentWorkers - int32(cfg.MinWorkers)
				}
				workers.Add(-toRemove)
			case scaleDown:
				if target >= int32(cfg.MinWorkers) && target < currentWorkers {
					workers.Store(target)
				}
			}
		}
	}
}
