# Architecture Overview

## Command Flow

```
Process.Start()
    |
    v
[yield.Command]
    |
    v
Scheduler.Execute()
    |
    v
Registry.Handle(cmd.CmdID()) --- O(1) array lookup: handlers[cmdID]
    |
    v
Handler.Handle(cmd, processID, frame, complete)
    |
    +-- Sync Handler (Continue) --- complete() called immediately
    |
    +-- Async Handler (Sleep) --- spawns goroutine, complete() called later
            |
            v
        completion callback
            |
            v
        Scheduler resumes
            |
            v
        Process.Send(response)
            |
            v
        [next yield.Command]
```

## Array-Indexed Routing

```go
// Command defines its own ID
type Sleep struct {
    Duration time.Duration
}
func (Sleep) CmdID() CommandID { return 10 }

// Registry uses array for O(1) lookup
type Registry struct {
    handlers [256]Handler  // Direct array indexing
}

// Handler registered once at startup
registry.Register(yield.CmdSleep, handler.NewSleepHandler(clock))

// Execution uses zero-allocation lookup
handler := registry.handlers[cmd.CmdID()]  // O(1), no map, no alloc
handler.Handle(cmd, processID, frame, complete)
```

## Handler Concurrency Pattern

### Synchronous Handler (Continue)

```go
func (h *ContinueHandler) Handle(cmd Command, processID uint64, frame *FrameContext, complete CompletionFunc) {
    // Process immediately
    complete(processID, nil, nil)
    // Returns immediately
}
```

### Asynchronous Handler (Sleep)

```go
func (h *SleepHandler) Handle(cmd Command, processID uint64, frame *FrameContext, complete CompletionFunc) {
    state := &sleepState{cancel: make(chan struct{})}
    h.pending.Store(processID, state)

    // Spawn goroutine to handle async work
    go func() {
        // Do async work (sleep, http request, etc)
        h.clock.Sleep(duration)

        // Notify scheduler when done
        complete(processID, result, nil)
    }()

    // Returns immediately, goroutine continues in background
}
```

## Process Lifecycle

```
1. scheduler.Execute(ctx, process)
   |
   v
2. OnStart(processID) hook
   |
   v
3. commands := process.Start()
   |
   v
4. for each command:
   |  - registry.Handle(cmd, processID, frame, complete)
   |  - wait for completion callback
   |  - commands = process.Send(response)
   |
   v
5. yield.Complete or yield.Error
   |
   v
6. OnComplete(processID, result, err) hook
   |
   v
7. return result, err
```

## Termination Flow

```
scheduler.Terminate(processID)
    |
    +-- Cancel process context
    |
    +-- registry.Terminate(processID)
            |
            v
        For each handler:
            |
            v
        handler.Terminate(processID)
            |
            v
        Handler cleans up:
            - Close cancel channels
            - Stop pending work
            - Remove from tracking map
```

## Mock Clock for Testing

```go
// Real Clock
type Real struct{}
func (r *Real) Sleep(d time.Duration) {
    time.Sleep(d)  // Real wall time
}

// Mock Clock
type Mock struct {
    current time.Time
}
func (m *Mock) Sleep(d time.Duration) {
    m.current = m.current.Add(d)  // Instant, no waiting
}

// Test with instant execution
mockClock := clock.NewMock(time.Now())
registry.Register(yield.CmdSleep, handler.NewSleepHandler(mockClock))

// 5 processes with 10-50ms sleeps complete in microseconds
```

## Performance Characteristics

### Hot Path (per command execution)

1. Array index: `handlers[cmd.CmdID()]` - O(1), zero allocation
2. Handler dispatch - interface call
3. Completion callback - channel send/receive

### Allocations Per Process

- FrameContext: 1 allocation per process
- Process state: 1 allocation per process
- Handler state: 1 allocation per async operation
- Channels: 1 allocation for result channel

### Scalability

- Concurrent processes: unlimited (sync.Map for tracking)
- Handlers: 256 max (uint8 command IDs)
- Per-handler overhead: single array entry (pointer)
- Registration: O(1) array assignment

## Command ID Ranges

```go
type CommandID uint8  // 0-255

const (
    // System commands (0-9)
    CmdComplete CommandID = 0
    CmdError    CommandID = 1
    CmdContinue CommandID = 2

    // Time commands (10-19)
    CmdSleep CommandID = 10

    // HTTP commands (20-39) - reserved
    CmdHTTPGet  CommandID = 20  // future
    CmdHTTPPost CommandID = 21  // future

    // Temporal commands (40-59) - reserved
    CmdTemporalActivity CommandID = 40  // future
    CmdTemporalSignal   CommandID = 41  // future

    // Database commands (60-79) - reserved
    // Network commands (80-99) - reserved
    // Custom commands (100-255) - user space
)
```

## Extension Pattern

Adding a new command type:

```go
// 1. Define command with ID
type HTTPGet struct {
    URL string
}
func (HTTPGet) CmdID() CommandID { return 20 }

// 2. Implement handler
type HTTPHandler struct {
    client *http.Client
    pending sync.Map
}

func (h *HTTPHandler) Handle(cmd Command, processID uint64, frame *FrameContext, complete CompletionFunc) {
    req := cmd.(HTTPGet)

    // Spawn goroutine for async HTTP request
    go func() {
        resp, err := h.client.Get(req.URL)
        if err != nil {
            complete(processID, nil, err)
            return
        }
        // Process response...
        complete(processID, data, nil)
    }()
}

func (h *HTTPHandler) Terminate(processID uint64) {
    // Cancel any pending requests for this process
}

// 3. Register at startup
registry.Register(yield.CmdHTTPGet, handler.NewHTTPHandler())

// 4. Use in process
func (p *MyProcess) Start() []yield.Command {
    return []yield.Command{HTTPGet{URL: "https://api.example.com/data"}}
}
```
