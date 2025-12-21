package actor

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// =============================================================================
// Test Process Types
// =============================================================================

// SimpleProcess completes after N steps with yields
type SimpleProcess struct {
	steps   int
	current int
}

func (p *SimpleProcess) Init(_ context.Context, _ string, input payload.Payloads) error {
	if len(input) > 0 {
		p.steps = input[0].Data().(int)
	}
	return nil
}

func (p *SimpleProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.current++
	if p.current > p.steps {
		out.Done(payload.New(p.current))
		return nil
	}
	out.Yield(YieldCmd{}, 0)
	out.Continue()
	return nil
}

func (p *SimpleProcess) Send(*relay.Package) error { return nil }
func (p *SimpleProcess) Close()                    {}

// IdleReceiverProcess waits for messages, counts them, exits after N
type IdleReceiverProcess struct {
	target   int
	received int
}

func (p *IdleReceiverProcess) Init(_ context.Context, _ string, input payload.Payloads) error {
	if len(input) > 0 {
		p.target = input[0].Data().(int)
	}
	return nil
}

func (p *IdleReceiverProcess) Step(events []process.Event, out *process.StepOutput) error {
	for _, e := range events {
		if e.Type == process.EventMessage {
			p.received++
		}
	}
	if p.received >= p.target {
		out.Done(payload.New(p.received))
		return nil
	}
	out.Idle()
	return nil
}

func (p *IdleReceiverProcess) Send(*relay.Package) error { return nil }
func (p *IdleReceiverProcess) Close()                    {}

// PingPongProcess sends a message and waits for reply, repeats N times
type PingPongProcess struct {
	self      pidapi.PID
	partner   pidapi.PID
	remaining int
	initiator bool
	started   bool
}

func (p *PingPongProcess) Init(_ context.Context, _ string, input payload.Payloads) error {
	if len(input) >= 3 {
		p.self = input[0].Data().(pidapi.PID)
		p.partner = input[1].Data().(pidapi.PID)
		p.remaining = input[2].Data().(int)
	}
	return nil
}

func (p *PingPongProcess) Step(events []process.Event, out *process.StepOutput) error {
	// Count received pings
	for _, e := range events {
		if e.Type == process.EventMessage {
			p.remaining--
		}
	}

	if p.remaining <= 0 {
		out.Done(nil)
		return nil
	}

	// Initiator starts the ping
	if p.initiator && !p.started {
		p.started = true
		out.Yield(BenchSendCmd{From: p.self, To: p.partner}, 0)
		out.Continue()
		return nil
	}

	// Reply to received message
	if len(events) > 0 {
		out.Yield(BenchSendCmd{From: p.self, To: p.partner}, 0)
		out.Continue()
		return nil
	}

	out.Idle()
	return nil
}

func (p *PingPongProcess) Send(*relay.Package) error { return nil }
func (p *PingPongProcess) Close()                    {}

// BenchSendCmd for benchmark send operations
type BenchSendCmd struct {
	From pidapi.PID
	To   pidapi.PID
}

func (BenchSendCmd) CmdID() dispatcher.CommandID { return 100 }

// =============================================================================
// Benchmark Helpers
// =============================================================================

type benchScheduler struct {
	*Scheduler
	registry  *scheduler.Registry
	completed atomic.Int64
}

func newBenchScheduler(workers int) *benchScheduler {
	bs := &benchScheduler{
		registry: scheduler.NewRegistry(),
	}

	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *apiruntime.Result) {
			bs.completed.Add(1)
		},
	}

	bs.Scheduler = NewScheduler(bs.registry, WithWorkers(workers), WithLifecycle(lc))
	bs.registry.Register(CmdYield, YieldHandler())
	bs.registry.Register(CmdComplete, CompleteHandler())

	return bs
}

func (bs *benchScheduler) registerSendHandler() {
	bs.registry.Register(100, dispatcher.HandlerFunc(func(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		sendCmd := cmd.(BenchSendCmd)
		msg := relay.AcquireMessage()
		msg.Topic = "ping"
		pkg := relay.NewMessagePackage(sendCmd.From, sendCmd.To, msg)
		err := bs.Send(pkg)
		receiver.CompleteYield(tag, nil, err)
		return nil
	}))
}

func (bs *benchScheduler) waitCompleted(n int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for bs.completed.Load() < n && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	return bs.completed.Load() >= n
}

func (bs *benchScheduler) reset() {
	bs.completed.Store(0)
}

// =============================================================================
// 1. PROCESS LIFECYCLE BENCHMARKS
// =============================================================================

// BenchmarkSubmitOnly measures raw Submit() latency without waiting for completion
func BenchmarkSubmitOnly(b *testing.B) {
	for _, workers := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("%dw", workers), func(b *testing.B) {
			bs := newBenchScheduler(workers)
			bs.Start()
			defer bs.Stop(context.Background())

			ctx := context.Background()
			input := payload.Payloads{payload.New(1)}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pid := pidapi.PID{UniqID: fmt.Sprintf("p%d", i)}
				_, _ = bs.Submit(ctx, pid, &SimpleProcess{}, "", input)
			}
			b.StopTimer()

			// Wait for all to complete before teardown
			bs.waitCompleted(int64(b.N), 30*time.Second)
		})
	}
}

// BenchmarkExecuteSync measures synchronous Execute (submit + wait)
func BenchmarkExecuteSync(b *testing.B) {
	for _, workers := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("%dw", workers), func(b *testing.B) {
			te := newTestExecutor(workers)
			te.Start()
			defer te.Stop()

			ctx := context.Background()
			input := payload.Payloads{payload.New(1)} // 1 step

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pid := pidapi.PID{UniqID: fmt.Sprintf("p%d", i)}
				_, _ = te.Execute(ctx, pid, &SimpleProcess{}, "", input)
			}
		})
	}
}

// BenchmarkBurstSubmit measures burst submission of many processes
func BenchmarkBurstSubmit(b *testing.B) {
	for _, burst := range []int{100, 1000, 10000, 100000} {
		b.Run(fmt.Sprintf("burst%d", burst), func(b *testing.B) {
			bs := newBenchScheduler(runtime.GOMAXPROCS(0))
			bs.Start()
			defer bs.Stop(context.Background())

			ctx := context.Background()
			input := payload.Payloads{payload.New(1)}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bs.reset()

				// Submit burst
				for j := 0; j < burst; j++ {
					pid := pidapi.PID{UniqID: fmt.Sprintf("p%d-%d", i, j)}
					_, _ = bs.Submit(ctx, pid, &SimpleProcess{}, "", input)
				}

				// Wait for all to complete
				bs.waitCompleted(int64(burst), 10*time.Second)
			}
		})
	}
}

// =============================================================================
// 2. MESSAGE SENDING BENCHMARKS
// =============================================================================

// BenchmarkSendToIdle measures Send() to an idle process (triggers wake)
func BenchmarkSendToIdle(b *testing.B) {
	for _, workers := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("%dw", workers), func(b *testing.B) {
			bs := newBenchScheduler(workers)
			bs.Start()
			defer bs.Stop(context.Background())

			// Create one receiver that expects many messages
			receiverPID := pidapi.PID{UniqID: "receiver"}
			receiver := &IdleReceiverProcess{target: b.N + 1000000} // won't complete
			_, _ = bs.Submit(context.Background(), receiverPID, receiver, "",
				payload.Payloads{payload.New(b.N + 1000000)})

			// Wait for receiver to go idle
			time.Sleep(10 * time.Millisecond)

			sender := pidapi.PID{UniqID: "sender"}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				msg := relay.AcquireMessage()
				msg.Topic = "test"
				pkg := relay.NewMessagePackage(sender, receiverPID, msg)
				_ = bs.Send(pkg)
			}
		})
	}
}

// BenchmarkSendParallel measures parallel Send() from multiple goroutines
func BenchmarkSendParallel(b *testing.B) {
	for _, receivers := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("%drecv", receivers), func(b *testing.B) {
			bs := newBenchScheduler(runtime.GOMAXPROCS(0))
			bs.Start()
			defer bs.Stop(context.Background())

			// Create receivers
			receiverPIDs := make([]pidapi.PID, receivers)
			for i := 0; i < receivers; i++ {
				pid := pidapi.PID{UniqID: fmt.Sprintf("recv%d", i)}
				receiverPIDs[i] = pid
				recv := &IdleReceiverProcess{target: 1000000000}
				_, _ = bs.Submit(context.Background(), pid, recv, "",
					payload.Payloads{payload.New(1000000000)})
			}

			time.Sleep(20 * time.Millisecond)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				sender := pidapi.PID{UniqID: "sender"}
				i := 0
				for pb.Next() {
					target := receiverPIDs[i%receivers]
					msg := relay.AcquireMessage()
					msg.Topic = "test"
					pkg := relay.NewMessagePackage(sender, target, msg)
					_ = bs.Send(pkg)
					i++
				}
			})
		})
	}
}

// =============================================================================
// 3. PING-PONG BENCHMARKS (realistic messaging patterns)
// =============================================================================

// BenchmarkPingPong measures message round-trip between two processes
func BenchmarkPingPong(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("%dw", workers), func(b *testing.B) {
			bs := newBenchScheduler(workers)
			bs.registerSendHandler()
			bs.Start()
			defer bs.Stop(context.Background())

			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bs.reset()

				pid1 := pidapi.PID{UniqID: fmt.Sprintf("a%d", i)}
				pid2 := pidapi.PID{UniqID: fmt.Sprintf("b%d", i)}

				// Each process does 50 round trips = 100 messages total
				rounds := 50

				proc1 := &PingPongProcess{self: pid1, partner: pid2, remaining: rounds, initiator: true}
				proc2 := &PingPongProcess{self: pid2, partner: pid1, remaining: rounds, initiator: false}

				_, _ = bs.Submit(ctx, pid1, proc1, "",
					payload.Payloads{payload.New(pid1), payload.New(pid2), payload.New(rounds)})
				_, _ = bs.Submit(ctx, pid2, proc2, "",
					payload.Payloads{payload.New(pid2), payload.New(pid1), payload.New(rounds)})

				bs.waitCompleted(2, 10*time.Second)
			}
		})
	}
}

// BenchmarkFanOut measures 1 sender -> N receivers pattern
func BenchmarkFanOut(b *testing.B) {
	for _, fanout := range []int{10, 100} {
		b.Run(fmt.Sprintf("1to%d", fanout), func(b *testing.B) {
			bs := newBenchScheduler(runtime.GOMAXPROCS(0))
			bs.registerSendHandler()
			bs.Start()
			defer bs.Stop(context.Background())

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bs.reset()

				// Create receivers
				receiverPIDs := make([]pidapi.PID, fanout)
				for j := 0; j < fanout; j++ {
					pid := pidapi.PID{UniqID: fmt.Sprintf("recv%d-%d", i, j)}
					receiverPIDs[j] = pid
					recv := &IdleReceiverProcess{target: 1}
					_, _ = bs.Submit(context.Background(), pid, recv, "",
						payload.Payloads{payload.New(1)})
				}

				// Wait for receivers to be ready
				time.Sleep(5 * time.Millisecond)

				// Send to all receivers
				sender := pidapi.PID{UniqID: fmt.Sprintf("sender%d", i)}
				for _, target := range receiverPIDs {
					msg := relay.AcquireMessage()
					msg.Topic = "fanout"
					pkg := relay.NewMessagePackage(sender, target, msg)
					_ = bs.Send(pkg)
				}

				bs.waitCompleted(int64(fanout), 10*time.Second)
			}
		})
	}
}

// =============================================================================
// 4. QUEUE CONTENTION BENCHMARKS
// =============================================================================

// BenchmarkGlobalQueuePush measures push contention
func BenchmarkGlobalQueuePush(b *testing.B) {
	q := NewQueue(1024)
	procs := make([]*Processor, 100)
	for i := range procs {
		procs[i] = &Processor{id: uint64(i)}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			q.Push(procs[i%len(procs)])
			i++
		}
	})
}

// BenchmarkGlobalQueueMixed measures mixed push/pop contention
func BenchmarkGlobalQueueMixed(b *testing.B) {
	q := NewQueue(1024)
	procs := make([]*Processor, 100)
	for i := range procs {
		procs[i] = &Processor{id: uint64(i)}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				q.Push(procs[i%len(procs)])
			} else {
				q.Pop()
			}
			i++
		}
	})
}

// BenchmarkDequeOperations measures local deque performance
func BenchmarkDequeOperations(b *testing.B) {
	b.Run("Push", func(b *testing.B) {
		d := NewDeque(256)
		proc := &Processor{id: 1}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			d.Push(proc)
			d.Pop()
		}
	})

	b.Run("PushPop", func(b *testing.B) {
		d := NewDeque(256)
		proc := &Processor{id: 1}
		for i := 0; i < 128; i++ {
			d.Push(proc)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			d.Pop()
			d.Push(proc)
		}
	})
}

// =============================================================================
// 5. SCALING BENCHMARKS
// =============================================================================

// BenchmarkWorkerScaling measures throughput vs worker count
func BenchmarkWorkerScaling(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8, 16, 32} {
		b.Run(fmt.Sprintf("%dw", workers), func(b *testing.B) {
			bs := newBenchScheduler(workers)
			bs.Start()
			defer bs.Stop(context.Background())

			ctx := context.Background()
			input := payload.Payloads{payload.New(5)} // 5 steps each

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pid := pidapi.PID{UniqID: fmt.Sprintf("p%d", i)}
				_, _ = bs.Submit(ctx, pid, &SimpleProcess{}, "", input)
			}
			b.StopTimer()

			bs.waitCompleted(int64(b.N), 30*time.Second)
		})
	}
}

// BenchmarkParallelSubmitScaling measures parallel submit vs worker count
func BenchmarkParallelSubmitScaling(b *testing.B) {
	for _, workers := range []int{1, 4, 8, 16, 32} {
		b.Run(fmt.Sprintf("%dw", workers), func(b *testing.B) {
			bs := newBenchScheduler(workers)
			bs.Start()
			defer bs.Stop(context.Background())

			ctx := context.Background()
			input := payload.Payloads{payload.New(3)}
			var counter atomic.Int64

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					i := counter.Add(1)
					pid := pidapi.PID{UniqID: fmt.Sprintf("p%d", i)}
					_, _ = bs.Submit(ctx, pid, &SimpleProcess{}, "", input)
				}
			})
			b.StopTimer()

			bs.waitCompleted(counter.Load(), 30*time.Second)
		})
	}
}

// =============================================================================
// 6. REAL-WORLD SCENARIO: Many-to-Many Messaging
// =============================================================================

// BenchmarkManyToMany measures N processes each sending M messages to random targets
func BenchmarkManyToMany(b *testing.B) {
	type config struct {
		procs    int
		messages int
		workers  int
	}

	configs := []config{
		{procs: 10, messages: 10, workers: 4},
		{procs: 50, messages: 10, workers: 4},
		{procs: 100, messages: 10, workers: 4},
		{procs: 100, messages: 10, workers: 8},
		{procs: 100, messages: 10, workers: 16},
	}

	for _, cfg := range configs {
		name := fmt.Sprintf("%dp_%dm_%dw", cfg.procs, cfg.messages, cfg.workers)
		b.Run(name, func(b *testing.B) {
			bs := newBenchScheduler(cfg.workers)
			bs.registerSendHandler()
			bs.Start()
			defer bs.Stop(context.Background())

			b.ResetTimer()
			for iter := 0; iter < b.N; iter++ {
				bs.reset()

				// Create all PIDs first
				pids := make([]pidapi.PID, cfg.procs)
				for j := 0; j < cfg.procs; j++ {
					pids[j] = pidapi.PID{UniqID: fmt.Sprintf("p%d-%d", iter, j)}
				}

				// Submit all processes concurrently
				var wg sync.WaitGroup
				for j := 0; j < cfg.procs; j++ {
					wg.Add(1)
					go func(idx int) {
						defer wg.Done()
						proc := &MessagingProcess{pid: pids[idx], targets: pids}
						_, _ = bs.Submit(context.Background(), pids[idx], proc, "",
							payload.Payloads{payload.New(pids), payload.New(cfg.messages)})
					}(j)
				}
				wg.Wait()

				// Wait for all to complete
				if !bs.waitCompleted(int64(cfg.procs), 30*time.Second) {
					b.Fatalf("timeout waiting for completion: got %d/%d", bs.completed.Load(), cfg.procs)
				}
			}
		})
	}
}

// MessagingProcess sends messages to other processes
type MessagingProcess struct {
	pid       pidapi.PID
	targets   []pidapi.PID
	remaining int
	received  int
}

func (p *MessagingProcess) Init(_ context.Context, _ string, input payload.Payloads) error {
	if len(input) >= 2 {
		p.targets = input[0].Data().([]pidapi.PID)
		p.remaining = input[1].Data().(int)
	}
	return nil
}

func (p *MessagingProcess) Step(events []process.Event, out *process.StepOutput) error {
	for _, e := range events {
		if e.Type == process.EventMessage {
			p.received++
		}
	}

	if p.remaining > 0 {
		p.remaining--
		target := p.targets[p.received%len(p.targets)]
		out.Yield(BenchSendCmd{From: p.pid, To: target}, 0)
		out.Continue()
		return nil
	}

	out.Done(payload.New(p.received))
	return nil
}

func (p *MessagingProcess) Send(*relay.Package) error { return nil }
func (p *MessagingProcess) Close()                    {}
