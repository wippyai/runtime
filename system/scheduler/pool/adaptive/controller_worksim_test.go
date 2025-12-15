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
	finalWorkers   int32
	maxWorkers     int32
	minWorkers     int32
	scaleUps       int
	scaleDowns     int
	probeSuccesses int
	probeFails     int
	totalOps       int64
	maxActive      int32
	avgWorkers     float64
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
			case scaleUp:
				if currentWorkers < int32(cfg.MaxWorkers) {
					workers.Add(1)
					startWorker()
					result.scaleUps++
				}
			case probeSuccess:
				result.probeSuccesses++
			case probeFail:
				if currentWorkers > int32(cfg.MinWorkers) {
					workers.Add(-1)
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

	if result.maxWorkers < 6 {
		t.Errorf("Expected to reach near max workers for I/O-bound, got max=%d", result.maxWorkers)
	}
	if result.probeSuccesses < result.probeFails {
		t.Errorf("Expected more successes than failures for I/O-bound workload")
	}
}

func TestControllerWorksim_HighLatencyLowBottleneck(t *testing.T) {
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

			if phase1Workers > 3 {
				t.Errorf("Phase1: expected <= 3 workers for bottleneck=1, got %d", phase1Workers)
			}
			if phase2Workers < 2 {
				t.Errorf("Phase2: expected >= 2 workers after bottleneck removed, got %d", phase2Workers)
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
			case scaleUp:
				if currentWorkers < int32(cfg.MaxWorkers) {
					workers.Add(1)
					startWorker()
				}
			case probeFail:
				if currentWorkers > int32(cfg.MinWorkers) {
					workers.Add(-1)
				}
			case scaleDown:
				if target >= int32(cfg.MinWorkers) && target < currentWorkers {
					workers.Store(target)
				}
			}
			workerMu.Unlock()

			elapsed := time.Since(start)
			if elapsed.Milliseconds()%1000 < int64(cfg.ControlInterval.Milliseconds()) {
				t.Logf("  t=%.1fs: workers=%d, busy=%d, ops=%d, maxActive=%d",
					elapsed.Seconds(), workers.Load(), currentBusy, currentOps, workload.MaxActive())
			}
		}
	}
}

func TestControllerWorksim_BurstyLoad(t *testing.T) {
	workload := worksim.New()
	workload.SetBottleneck(4)
	workload.SetLatency(10 * time.Millisecond)

	cfg := DefaultControllerConfig(8)
	cfg.ControlInterval = 50 * time.Millisecond
	cfg.ProbeTicks = 3
	cfg.IdleTicks = 3

	c := newController(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		workers    atomic.Int32
		busyCount  atomic.Int32
		opsCount   atomic.Int64
		workerWg   sync.WaitGroup
		loadActive atomic.Bool
	)

	workers.Store(1)
	loadActive.Store(true)

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

				if !loadActive.Load() {
					time.Sleep(10 * time.Millisecond)
					continue
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
			loadActive.Store(true)
			time.Sleep(500 * time.Millisecond)
			loadActive.Store(false)
			time.Sleep(500 * time.Millisecond)
		}
	}()

	ticker := time.NewTicker(cfg.ControlInterval)
	defer ticker.Stop()

	var maxWorkers, minWorkers int32 = 0, 100
	var scaleUps, scaleDowns int

	for {
		select {
		case <-ctx.Done():
			workerWg.Wait()

			t.Logf("Bursty load: workers=%d (min=%d, max=%d), ops=%d, scaleUps=%d, scaleDowns=%d",
				workers.Load(), minWorkers, maxWorkers, opsCount.Load(), scaleUps, scaleDowns)

			// Must have some scaling activity
			if scaleUps == 0 {
				t.Errorf("Expected scale-up activity during bursty load")
			}
			// Should not over-scale beyond bottleneck significantly
			if maxWorkers > 6 {
				t.Errorf("Over-scaled for bottleneck=4, got max=%d", maxWorkers)
			}
			// Should scale down during quiet periods
			if scaleDowns == 0 && minWorkers > 2 {
				t.Errorf("Expected scale-down during quiet periods")
			}
			return

		case now := <-ticker.C:
			currentWorkers := workers.Load()
			currentBusy := busyCount.Load()
			currentOps := opsCount.Load()

			queueLen := 0
			if loadActive.Load() && currentBusy >= currentWorkers {
				queueLen = 10
			}

			if currentWorkers > maxWorkers {
				maxWorkers = currentWorkers
			}
			if currentWorkers < minWorkers {
				minWorkers = currentWorkers
			}

			d, target := c.tick(now, currentOps, currentWorkers, currentBusy, queueLen)

			switch d {
			case scaleUp:
				if currentWorkers < int32(cfg.MaxWorkers) {
					workers.Add(1)
					startWorker()
					scaleUps++
				}
			case probeFail:
				if currentWorkers > int32(cfg.MinWorkers) {
					workers.Add(-1)
				}
			case scaleDown:
				if target >= int32(cfg.MinWorkers) && target < currentWorkers {
					diff := currentWorkers - target
					workers.Store(target)
					scaleDowns += int(diff)
				}
			}
		}
	}
}

func TestControllerWorksim_LowLatency(t *testing.T) {
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

	if result.maxWorkers < 4 {
		t.Errorf("Expected to scale up for low-latency workload, got max=%d", result.maxWorkers)
	}
}

func TestControllerWorksim_GradualBottleneck(t *testing.T) {
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

	go func() {
		bottlenecks := []int{8, 4, 2, 1}
		for i, b := range bottlenecks {
			workload.SetBottleneck(b)
			time.Sleep(3 * time.Second)
			phases = append(phases, phase{
				bottleneck: b,
				workers:    workers.Load(),
				maxActive:  workload.MaxActive(),
			})
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

			t.Log("Gradual bottleneck decrease:")
			for _, p := range phases {
				t.Logf("  bottleneck=%d: workers=%d, maxActive=%d", p.bottleneck, p.workers, p.maxActive)
			}

			finalMaxActive := workload.MaxActive()
			t.Logf("Final maxActive after all phases: %d", finalMaxActive)

			// Verify all phases completed
			if len(phases) < 4 {
				t.Errorf("Expected 4 phases, got %d", len(phases))
				return
			}

			// Final phase (bottleneck=1) should show constrained activity
			// maxActive may include overlap from transitions, so allow margin
			finalPhase := phases[len(phases)-1]
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
			case scaleUp:
				if currentWorkers < int32(cfg.MaxWorkers) {
					workers.Add(1)
					startWorker()
				}
			case probeFail:
				if currentWorkers > int32(cfg.MinWorkers) {
					workers.Add(-1)
				}
			case scaleDown:
				if target >= int32(cfg.MinWorkers) && target < currentWorkers {
					workers.Store(target)
				}
			}
		}
	}
}
