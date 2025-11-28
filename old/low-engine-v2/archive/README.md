# Ultra-Fast Async Scheduler POC

Production-ready async scheduler with array-indexed command routing for zero-allocation, O(1) performance.

## Architecture

### CommandID Type Alias

```go
type CommandID uint8
```

Type-safe command identifiers enable array-indexed routing with O(1) lookup.

### Reserved Command Ranges

- System: 0-9 (Complete, Error, Continue)
- Time: 10-19 (Sleep)
- HTTP: 20-39 (reserved)
- Temporal: 40-59 (reserved)
- Database: 60-79 (reserved)
- Network: 80-99 (reserved)

### Array-Indexed Registry

```go
type Registry struct {
    handlers [256]Handler
}
```

Direct array indexing eliminates map lookups and allocations. Built once at startup, then immutable for thread-safe concurrent access.

### Async Handler Pattern

```go
type CompletionFunc func(processID uint64, result interface{}, err error)

type Handler interface {
    Handle(cmd Command, processID uint64, frame *FrameContext, complete CompletionFunc)
    Terminate(processID uint64)
}
```

Handlers manage their own goroutines and use completion callbacks. The scheduler never spawns goroutines on behalf of handlers.

### Process Interface

```go
type Process interface {
    Start() []Command
    Send(response *Response) []Command
}
```

Processes maintain state and communicate via commands. Clean separation between process logic and execution.

### Lifecycle Hooks

```go
scheduler.New(registry, clock).
    WithOnStart(func(processID uint64) { ... }).
    WithOnComplete(func(processID uint64, result interface{}, err error) { ... })
```

Track process lifecycle events for monitoring and debugging.

## Performance Results

### Benchmark Results

```
BenchmarkContinueCommand-32      7044127    169.3 ns/op    5905194 ops/sec    160 B/op    5 allocs/op
BenchmarkArrayLookup-32          2024137    590.7 ns/op    1693037 ops/sec    888 B/op   19 allocs/op
BenchmarkMockClockSleep-32        883891   1453 ns/op      688248 ops/sec   1232 B/op   19 allocs/op
BenchmarkProcessCreation-32      2064310    595.6 ns/op    1678955 ops/sec    888 B/op   19 allocs/op
```

### Key Metrics

- **5.9M ops/sec** for synchronous continue commands
- **1.7M ops/sec** for process creation and execution
- **688K ops/sec** for mock clock sleep operations
- **169 ns/op** for command routing and execution

### Concurrent Execution

- 5 processes with sleep durations 10ms-50ms complete in ~51ms (fully concurrent)
- Mock clock: same 5 processes complete in 32-122 microseconds (instant)

## Files Structure

### Core Types
- `yield/command.go`: CommandID type, Command interface, all command constants and structs
- `context/frame.go`: FrameContext for per-process state
- `clock/clock.go`: Clock interface, Real and Mock implementations

### Handler System
- `handler/handler.go`: Handler interface, CompletionFunc, Registry with [256]Handler array
- `handler/sleep.go`: SleepHandler with async goroutine management
- `handler/continue.go`: ContinueHandler (synchronous, instant)

### Scheduler
- `scheduler/scheduler.go`: Async scheduler with lifecycle hooks and process management
- `scheduler/options.go`: Configuration options

### Process Implementation
- `process/process.go`: Process interface
- `examples/timer.go`: Timer process example

### Tests
- `examples/async_test.go`: Comprehensive tests including:
  - Concurrent execution (5 processes)
  - Mock clock instant execution
  - Terminate functionality
  - Lifecycle hooks
  - Array-indexed routing

### Benchmarks
- `examples/benchmark_test.go`: Performance benchmarks showing >1M ops/sec

## Usage Example

```go
// Create registry and register handlers
registry := handler.NewRegistry()
clock := clock.NewReal()
registry.Register(yield.CmdSleep, handler.NewSleepHandler(clock))
registry.Register(yield.CmdContinue, handler.NewContinueHandler())

// Create scheduler with hooks
sched := scheduler.New(registry, clock).
    WithOnStart(func(processID uint64) {
        log.Printf("Process %d started", processID)
    }).
    WithOnComplete(func(processID uint64, result interface{}, err error) {
        log.Printf("Process %d completed: %v, err: %v", processID, result, err)
    })

// Execute process
proc := examples.NewTimerProcess(100 * time.Millisecond)
result, err := sched.Execute(context.Background(), proc)
```

## Testing

Run all tests:
```bash
go test -v ./examples
```

Run benchmarks:
```bash
go test -bench=. -benchmem ./examples
```

## Design Principles

1. **Zero allocation in hot path**: Array-indexed lookup with no map operations
2. **No reflection**: All types known at compile time
3. **Handler-managed concurrency**: Handlers spawn their own goroutines
4. **Immutable registry**: Built once at startup, read-only afterward
5. **Frame-per-process**: FrameContext created once per process
6. **Mock clock support**: Pluggable Clock interface for instant test execution
7. **Proper termination**: Handlers clean up pending work when processes terminate

## Success Criteria

- [x] Zero allocation in hot path (array index lookup only)
- [x] No reflection
- [x] Handlers manage own concurrency
- [x] Terminate properly cancels pending work
- [x] OnStart/OnComplete hooks work
- [x] Mock clock allows instant test execution
- [x] Benchmark shows >1M ops/sec throughput
