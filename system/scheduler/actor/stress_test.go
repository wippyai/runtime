package actor

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// RandomSleepHandler simulates IO with random sleep
type RandomSleepHandler struct {
	minSleep time.Duration
	maxSleep time.Duration
}

func (h *RandomSleepHandler) Handle(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	sleep := h.minSleep + time.Duration(rand.Int63n(int64(h.maxSleep-h.minSleep))) //nolint:gosec // weak random is fine for test jitter
	time.Sleep(sleep)
	receiver.CompleteYield(tag, sleep.Nanoseconds(), nil)
	return nil
}

// CPUWorkHandler simulates CPU work
type CPUWorkHandler struct {
	iterations int
}

func (h *CPUWorkHandler) Handle(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	sum := 0
	for i := 0; i < h.iterations; i++ {
		sum += i * i
	}
	receiver.CompleteYield(tag, sum, nil)
	return nil
}

// StressConfig defines stress test parameters
type StressConfig struct {
	Name          string
	Workers       int
	Processes     int
	StepsPerProc  int
	HandlerType   string // "sleep", "cpu", "instant"
	MaxProcessors int64
}

func runStressTest(_ *testing.T, cfg StressConfig) StressResult {
	registry := scheduler.NewRegistry()

	var handler dispatcher.Handler
	switch cfg.HandlerType {
	case "sleep":
		handler = &RandomSleepHandler{minSleep: 100 * time.Microsecond, maxSleep: 1 * time.Millisecond}
	case "cpu":
		handler = &CPUWorkHandler{iterations: 1000}
	default:
		handler = &InstantHandler{}
	}
	registry.Register(1, handler)

	var completed atomic.Int64
	var errCount atomic.Int64

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ relay.PID, result *apiruntime.Result) {
			if result.Error != nil {
				errCount.Add(1)
			} else {
				completed.Add(1)
			}
		},
	}

	opts := []Option{
		WithWorkers(cfg.Workers),
		WithQueueSize(cfg.Processes * 2),
		WithLifecycle(lc),
	}
	if cfg.MaxProcessors > 0 {
		opts = append(opts, WithMaxProcesses(cfg.MaxProcessors))
	}

	sched := NewScheduler(registry, opts...)

	var wg sync.WaitGroup

	sched.Start()

	start := time.Now()
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	for i := 0; i < cfg.Processes; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			proc := &RandomYieldProcess{}
			pid := relay.PID{UniqID: fmt.Sprintf("stress-%d", id)}
			_, err := sched.Submit(context.Background(), pid, proc, "", testInput(cfg.StepsPerProc))
			if errors.Is(err, process.ErrMaxProcessesExceeded) {
				errCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Wait for all processes to complete
	deadline := time.Now().Add(30 * time.Second)
	for completed.Load()+errCount.Load() < int64(cfg.Processes) && time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
	}

	elapsed := time.Since(start)

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	sched.Stop(context.Background())

	stats := sched.Stats()

	return StressResult{
		Config:       cfg,
		Duration:     elapsed,
		Completed:    completed.Load(),
		Errors:       errCount.Load(),
		OpsPerSec:    float64(completed.Load()) / elapsed.Seconds(),
		StepsPerSec:  float64(stats["executed"]) / elapsed.Seconds(),
		HeapAllocMB:  float64(memAfter.HeapAlloc-memBefore.HeapAlloc) / 1024 / 1024,
		TotalAllocMB: float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024,
		NumGC:        memAfter.NumGC - memBefore.NumGC,
		Stats:        stats,
		WorkerStats:  sched.WorkerStats(),
	}
}

type StressResult struct {
	Config       StressConfig
	Duration     time.Duration
	Completed    int64
	Errors       int64
	OpsPerSec    float64
	StepsPerSec  float64
	HeapAllocMB  float64
	TotalAllocMB float64
	NumGC        uint32
	Stats        map[string]uint64
	WorkerStats  []map[string]uint64
}

func (r StressResult) String() string {
	var workerDist string
	for i, ws := range r.WorkerStats {
		if i > 0 {
			workerDist += ", "
		}
		workerDist += fmt.Sprintf("w%d:%d", i, ws["executed"])
	}

	return fmt.Sprintf(`
%s (%d workers):
  Duration:     %v
  Completed:    %d / %d (errors: %d)
  Throughput:   %.0f procs/sec, %.0f steps/sec
  Memory:       heap +%.2f MB, total +%.2f MB, GC runs: %d
  Executed:     %d total
  Worker dist:  [%s]
`,
		r.Config.Name, r.Config.Workers,
		r.Duration,
		r.Completed, r.Config.Processes, r.Errors,
		r.OpsPerSec, r.StepsPerSec,
		r.HeapAllocMB, r.TotalAllocMB, r.NumGC,
		r.Stats["executed"],
		workerDist,
	)
}

func TestStress10kProcesses4Workers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	configs := []StressConfig{
		{Name: "10k instant", Workers: 4, Processes: 10000, StepsPerProc: 5, HandlerType: "instant"},
		{Name: "10k CPU", Workers: 4, Processes: 10000, StepsPerProc: 5, HandlerType: "cpu"},
	}

	for _, cfg := range configs {
		result := runStressTest(t, cfg)
		t.Log(result.String())

		if result.Completed != int64(cfg.Processes) {
			t.Errorf("%s: expected %d completed, got %d", cfg.Name, cfg.Processes, result.Completed)
		}
	}
}

func TestStress10kProcesses32Workers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	configs := []StressConfig{
		{Name: "10k instant", Workers: 32, Processes: 10000, StepsPerProc: 5, HandlerType: "instant"},
		{Name: "10k CPU", Workers: 32, Processes: 10000, StepsPerProc: 5, HandlerType: "cpu"},
	}

	for _, cfg := range configs {
		result := runStressTest(t, cfg)
		t.Log(result.String())

		if result.Completed != int64(cfg.Processes) {
			t.Errorf("%s: expected %d completed, got %d", cfg.Name, cfg.Processes, result.Completed)
		}
	}
}

func TestStressIOBound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	configs := []StressConfig{
		{Name: "1k sleep 4w", Workers: 4, Processes: 1000, StepsPerProc: 3, HandlerType: "sleep"},
		{Name: "1k sleep 32w", Workers: 32, Processes: 1000, StepsPerProc: 3, HandlerType: "sleep"},
	}

	for _, cfg := range configs {
		result := runStressTest(t, cfg)
		t.Log(result.String())

		if result.Completed != int64(cfg.Processes) {
			t.Errorf("%s: expected %d completed, got %d", cfg.Name, cfg.Processes, result.Completed)
		}
	}
}

func TestStressMaxProcessorsLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cfg := StressConfig{
		Name:          "10k with 1k limit",
		Workers:       4,
		Processes:     10000,
		StepsPerProc:  5,
		HandlerType:   "instant",
		MaxProcessors: 1000,
	}

	result := runStressTest(t, cfg)
	t.Log(result.String())

	// Some should fail due to limit
	if result.Errors == 0 {
		t.Log("Note: no errors, limit may not have been hit due to fast completion")
	}
	t.Logf("Completed: %d, Errors: %d", result.Completed, result.Errors)
}

func TestStressWorkerBalance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	configs := []StressConfig{
		{Name: "balance", Workers: 8, Processes: 10000, StepsPerProc: 5, HandlerType: "cpu"},
	}

	for _, cfg := range configs {
		result := runStressTest(t, cfg)
		t.Log(result.String())

		// Check work distribution
		var minExec, maxExec uint64 = ^uint64(0), 0
		for _, ws := range result.WorkerStats {
			exec := ws["executed"]
			if exec < minExec {
				minExec = exec
			}
			if exec > maxExec {
				maxExec = exec
			}
		}

		balance := float64(minExec) / float64(maxExec)
		t.Logf("%s balance: %.2f (min=%d, max=%d)", cfg.Name, balance, minExec, maxExec)

		// Work stealing should have better balance
		if balance < 0.5 {
			t.Logf("Warning: poor balance for %s", cfg.Name)
		}
	}
}
