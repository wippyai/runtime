package engine

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Example subsystem commands - in real code these would be in separate packages

const (
	CmdComplete CommandID = 0
	CmdYield    CommandID = 1
	CmdSleep    CommandID = 10
)

type Complete struct{ Result any }

func (Complete) CmdID() CommandID { return CmdComplete }

type YieldCmd struct{}

func (YieldCmd) CmdID() CommandID { return CmdYield }

type Sleep struct{ Duration time.Duration }

func (Sleep) CmdID() CommandID { return CmdSleep }

// Example handlers

func CompleteHandler() Handler {
	return HandlerFunc(func(cmd Command, proc *Processor) {
		c := cmd.(Complete)
		proc.Complete(c.Result, nil)
	})
}

func YieldHandler() Handler {
	return HandlerFunc(func(cmd Command, proc *Processor) {
		proc.Complete(nil, nil)
	})
}

func SleepHandler() Handler {
	return HandlerFunc(func(cmd Command, proc *Processor) {
		s := cmd.(Sleep)
		go func() {
			time.Sleep(s.Duration)
			proc.Complete(nil, nil)
		}()
	})
}

// Example process - counts to N with yields (zero-alloc)

type CounterProcess struct {
	target  int
	current int
	ctx     context.Context
}

func (p *CounterProcess) Start(ctx context.Context, input any) error {
	p.ctx = ctx
	p.target = input.(int)
	return nil
}

func (p *CounterProcess) Step(results *YieldResults) (StepResult, error) {
	if p.current >= p.target {
		var r StepResult
		r.Status = StepDone
		r.YieldsBuf[0] = Complete{Result: p.current}
		r.YieldCount = 1
		return r, nil
	}

	p.current++
	var r StepResult
	r.Status = StepContinue
	r.YieldsBuf[0] = YieldCmd{}
	r.YieldCount = 1
	return r, nil
}

func (p *CounterProcess) Send(pkg *Package) error {
	return nil
}

// Example process - sleeps then completes (zero-alloc)

type SleepProcess struct {
	duration time.Duration
	slept    bool
	ctx      context.Context
}

func (p *SleepProcess) Start(ctx context.Context, input any) error {
	p.ctx = ctx
	p.duration = input.(time.Duration)
	return nil
}

func (p *SleepProcess) Step(results *YieldResults) (StepResult, error) {
	if !p.slept {
		p.slept = true
		var r StepResult
		r.Status = StepContinue
		r.YieldsBuf[0] = Sleep{Duration: p.duration}
		r.YieldCount = 1
		return r, nil
	}

	var r StepResult
	r.Status = StepDone
	r.YieldsBuf[0] = Complete{Result: "done"}
	r.YieldCount = 1
	return r, nil
}

func (p *SleepProcess) Send(pkg *Package) error {
	return nil
}

// Tests

func TestDeque(t *testing.T) {
	d := NewDeque(8)

	// Push 5 items
	for i := 0; i < 5; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	if d.Len() != 5 {
		t.Fatalf("expected len 5, got %d", d.Len())
	}

	// Pop returns LIFO (last in, first out)
	p := d.Pop()
	if p.ID != 4 {
		t.Fatalf("expected ID 4, got %d", p.ID)
	}

	// Steal returns FIFO (first in, first out)
	p = d.Steal()
	if p.ID != 0 {
		t.Fatalf("expected ID 0, got %d", p.ID)
	}

	if d.Len() != 3 {
		t.Fatalf("expected len 3, got %d", d.Len())
	}
}

func TestDequeGrow(t *testing.T) {
	d := NewDeque(4)

	// Push more than capacity
	for i := 0; i < 10; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	if d.Len() != 10 {
		t.Fatalf("expected len 10, got %d", d.Len())
	}

	// Verify all items are retrievable
	for i := 9; i >= 0; i-- {
		p := d.Pop()
		if p == nil || p.ID != uint64(i) {
			t.Fatalf("expected ID %d, got %v", i, p)
		}
	}
}

func TestStealHalf(t *testing.T) {
	d := NewDeque(16)

	for i := 0; i < 8; i++ {
		d.Push(&Processor{ID: uint64(i)})
	}

	buf := make([]*Processor, 64)
	count := d.StealHalfInto(buf)
	if count != 4 {
		t.Fatalf("expected 4 stolen, got %d", count)
	}

	// Stolen should be oldest items (FIFO)
	for i := 0; i < count; i++ {
		if buf[i].ID != uint64(i) {
			t.Fatalf("stolen[%d] expected ID %d, got %d", i, i, buf[i].ID)
		}
	}

	if d.Len() != 4 {
		t.Fatalf("expected len 4, got %d", d.Len())
	}
}

func TestSchedulerCounter(t *testing.T) {
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())

	sched := NewScheduler(registry, 2)

	var completedCount atomic.Int32
	var results sync.Map

	sched.OnComplete(func(proc *Processor, result any, err error) {
		if err != nil {
			t.Errorf("process %d error: %v", proc.ID, err)
			return
		}
		if c, ok := result.(Complete); ok {
			results.Store(proc.ID, c.Result)
		}
		completedCount.Add(1)
	})

	sched.Start()
	defer sched.Stop()

	// Submit 10 counter processes
	for i := 0; i < 10; i++ {
		_, err := sched.Submit(context.Background(), &CounterProcess{}, 100)
		if err != nil {
			t.Fatalf("submit error: %v", err)
		}
	}

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for completedCount.Load() < 10 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if completedCount.Load() != 10 {
		t.Fatalf("expected 10 completed, got %d", completedCount.Load())
	}

	// Verify results
	results.Range(func(key, value any) bool {
		if value.(int) != 100 {
			t.Errorf("process %d result %v, expected 100", key, value)
		}
		return true
	})

	stats := sched.Stats()
	t.Logf("Stats: executed=%d stolen=%d", stats["executed"], stats["stolen"])
}

func TestSchedulerSleep(t *testing.T) {
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdSleep, SleepHandler())

	sched := NewScheduler(registry, 2)

	var completed atomic.Bool

	sched.OnComplete(func(proc *Processor, result any, err error) {
		if err != nil {
			t.Errorf("error: %v", err)
		}
		completed.Store(true)
	})

	sched.Start()
	defer sched.Stop()

	start := time.Now()
	_, err := sched.Submit(context.Background(), &SleepProcess{}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(2 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("completed too fast: %v", elapsed)
	}

	if !completed.Load() {
		t.Fatal("process did not complete")
	}
}

func TestWorkStealing(t *testing.T) {
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())

	sched := NewScheduler(registry, 4)

	var completed atomic.Int32

	sched.OnComplete(func(proc *Processor, result any, err error) {
		completed.Add(1)
	})

	sched.Start()
	defer sched.Stop()

	// Submit 100 processes
	for i := 0; i < 100; i++ {
		sched.Submit(context.Background(), &CounterProcess{}, 50)
	}

	// Wait
	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < 100 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if completed.Load() != 100 {
		t.Fatalf("expected 100 completed, got %d", completed.Load())
	}

	stats := sched.Stats()
	t.Logf("Work stealing stats: executed=%d stolen=%d", stats["executed"], stats["stolen"])

	// With 4 workers and 100 processes, we should see some stealing
	if stats["stolen"] == 0 {
		t.Log("Warning: no work stealing occurred (may be OK for small workloads)")
	}
}

func BenchmarkScheduler(b *testing.B) {
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())

	sched := NewScheduler(registry, runtime.GOMAXPROCS(0))

	var completed atomic.Int64

	sched.OnComplete(func(proc *Processor, result any, err error) {
		completed.Add(1)
	})

	sched.Start()
	defer sched.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		completed.Store(0)

		// Submit 1000 processes
		for j := 0; j < 1000; j++ {
			sched.Submit(context.Background(), &CounterProcess{}, 10)
		}

		// Wait
		for completed.Load() < 1000 {
			runtime.Gosched()
		}
	}
}
