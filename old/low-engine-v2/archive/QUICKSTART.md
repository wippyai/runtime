# Quick Start Guide

## Run Tests

```bash
# All tests
go test -v ./examples

# Specific test
go test -v ./examples -run TestConcurrentExecution

# Integration demo
go test -v ./examples -run TestIntegrationExample
```

## Run Benchmarks

```bash
# All benchmarks
go test -bench=. -benchmem ./examples

# Specific benchmark
go test -bench=BenchmarkContinueCommand -benchmem ./examples
```

## Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/wippyai/runtime/low-engine-v2/clock"
    "github.com/wippyai/runtime/low-engine-v2/handler"
    "github.com/wippyai/runtime/low-engine-v2/scheduler"
    "github.com/wippyai/runtime/low-engine-v2/yield"
)

// Define your process
type MyProcess struct {
    step int
}

func (p *MyProcess) Start() []yield.Command {
    return []yield.Command{yield.Sleep{Duration: 100 * time.Millisecond}}
}

func (p *MyProcess) Send(response *yield.Response) []yield.Command {
    p.step++
    if p.step >= 3 {
        return []yield.Command{yield.Complete{Result: "done"}}
    }
    return []yield.Command{yield.Sleep{Duration: 100 * time.Millisecond}}
}

func main() {
    // Setup
    registry := handler.NewRegistry()
    clk := clock.NewReal()
    registry.Register(yield.CmdSleep, handler.NewSleepHandler(clk))
    registry.Register(yield.CmdContinue, handler.NewContinueHandler())

    sched := scheduler.New(registry, clk)

    // Execute
    proc := &MyProcess{}
    result, err := sched.Execute(context.Background(), proc)

    fmt.Printf("Result: %v, Error: %v\n", result, err)
}
```

## Adding a New Command Type

```go
// 1. Define command ID (pick from reserved range or use 100+)
const CmdMyCommand CommandID = 100

// 2. Define command struct
type MyCommand struct {
    Data string
}

func (MyCommand) CmdID() CommandID { return CmdMyCommand }

// 3. Implement handler
type MyHandler struct{}

func (h *MyHandler) Handle(cmd yield.Command, processID uint64, frame *context.FrameContext, complete handler.CompletionFunc) {
    myCmd := cmd.(MyCommand)

    // For async operation, spawn goroutine
    go func() {
        // Do work...
        result := processData(myCmd.Data)

        // Notify scheduler
        complete(processID, result, nil)
    }()

    // For sync operation, just call complete immediately
    // complete(processID, result, nil)
}

func (h *MyHandler) Terminate(processID uint64) {
    // Clean up any pending work for this process
}

// 4. Register at startup
registry.Register(CmdMyCommand, &MyHandler{})

// 5. Use in process
func (p *MyProcess) Start() []yield.Command {
    return []yield.Command{MyCommand{Data: "hello"}}
}
```

## Testing with Mock Clock

```go
// Use mock clock for instant execution
mockClock := clock.NewMock(time.Now())
registry.Register(yield.CmdSleep, handler.NewSleepHandler(mockClock))

// Now all sleep operations complete instantly
// Perfect for tests!
```

## Key Files to Read

1. `README.md` - Full documentation
2. `ARCHITECTURE.md` - Design details and diagrams
3. `SUMMARY.md` - Implementation summary and results
4. `examples/integration_example.go` - Complete working example
5. `yield/command.go` - Command types
6. `handler/handler.go` - Handler interface and registry
7. `scheduler/scheduler.go` - Scheduler implementation

## Performance Tips

1. Array indexing is O(1) - no overhead for command routing
2. Handlers manage their own concurrency - spawn goroutines as needed
3. Use sync.Map for handler state if tracking many concurrent operations
4. Frame context is created once per process - reuse it
5. Mock clock for testing - no real sleep delays

## Common Patterns

### Lifecycle Hooks

```go
sched := scheduler.New(registry, clock).
    WithOnStart(func(processID uint64) {
        metrics.ProcessStarted(processID)
    }).
    WithOnComplete(func(processID uint64, result interface{}, err error) {
        metrics.ProcessCompleted(processID, err != nil)
        log.Printf("Process %d: %v", processID, result)
    })
```

### Process Termination

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

result, err := sched.Execute(ctx, proc)
// Automatically terminates if context cancelled
```

### Multiple Commands (Future)

```go
// Process can return multiple commands for parallel execution
func (p *MyProcess) Start() []yield.Command {
    return []yield.Command{
        yield.Sleep{Duration: 10 * time.Millisecond},
        // Future: support parallel execution
    }
}
```

## Command ID Ranges

- **0-9**: System (Complete, Error, Continue)
- **10-19**: Time (Sleep)
- **20-39**: HTTP (reserved)
- **40-59**: Temporal (reserved)
- **60-79**: Database (reserved)
- **80-99**: Network (reserved)
- **100-255**: Custom/User commands
