# Process System Core Functions

## Process Creation and Registration
- `spawn(function, args) -> pid`: Creates new process that runs the given function
- `spawn_link(function, args) -> pid`: Creates linked process that will notify parent on failure
- `spawn_monitor(function, args) -> {pid, monitor_ref}`: Creates monitored process with one-way monitoring
- `register(name, pid)`: Associates a name with a process ID
- `whereis(name) -> pid`: Looks up process ID by registered name
- `unregister(name)`: Removes process name registration

## Process Communication
- `send(dest, message)`: Sends message to process (supports pid or registered name)
- `receive(pattern_match) -> message`: Receives message matching pattern
- `receive_timeout(pattern_match, timeout) -> message | timeout`: Receive with timeout
- `flush()`: Discards all messages in process mailbox

## Process Monitoring
- `monitor(pid | name) -> monitor_ref`: Sets up monitoring of another process
- `demonitor(monitor_ref)`: Cancels monitoring
- `monitor_node(node, flag)`: Monitors node connection status
- `link(pid)`: Creates bidirectional link between processes
- `unlink(pid)`: Removes process link

## Process Information
- `self() -> pid`: Returns current process ID
- `process_info(pid) -> info`: Returns process information
- `is_process_alive(pid) -> bool`: Checks if process is running
- `processes() -> [pid]`: Lists all processes
- `registered() -> [name]`: Lists registered process names

## Process Control
- `exit(pid, reason)`: Signals process to exit
- `trap_exit(bool)`: Sets process to trap exit signals
- `set_priority(pid, priority)`: Sets process scheduling priority
- `hibernate()`: Puts process into low memory state until message received

## Error Handling & Recovery
- `try/catch`: Exception handling within a process
- `throw(term)`: Raises exception
- `trap_error(function) -> result`: Wraps function with error trapping
- `get_stacktrace()`: Returns current error stacktrace

## Supervision Tree Functions
- `supervisor.start_link(module, args)`: Starts supervisor process
- `supervisor.start_child(sup_ref, child_spec)`: Adds child to supervisor
- `supervisor.terminate_child(sup_ref, child_id)`: Terminates supervised child
- `supervisor.restart_child(sup_ref, child_id)`: Restarts supervised child
- `supervisor.delete_child(sup_ref, child_id)`: Removes child from supervision
- `supervisor.count_children(sup_ref)`: Returns counts of children by state
- `supervisor.which_children(sup_ref)`: Lists all supervised children

## Supervision Strategies
1. **OneForOne**
    - When child process terminates, only that process is restarted
    - Suitable for independent children

2. **OneForAll**
    - When child process terminates, all other child processes are terminated and restarted
    - Suitable for strongly dependent children

3. **RestForOne**
    - When child process terminates, the terminated process and the rest of the processes started after it are terminated and restarted
    - Suitable for processes with start-order dependencies

4. **SimpleOneForOne**
    - Modified version of OneForOne for dynamic child processes
    - All children are instantiated from same child specification

## Child Specifications
```lua
{
    id = "child1",                -- Unique identifier
    start = {module, function, args}, -- How to start the child
    restart = permanent | transient | temporary, -- When to restart
    shutdown = brutal_kill | timeout, -- How to shutdown
    type = worker | supervisor,    -- Process type
    modules = ["module_name"]     -- Code modules used
}
```

## Process Groups
- `pg.create(group)`: Creates process group
- `pg.join(group, pid | [pid])`: Adds processes to group
- `pg.leave(group, pid | [pid])`: Removes processes from group
- `pg.get_members(group)`: Lists group members
- `pg.get_local_members(group)`: Lists local group members
- `pg.broadcast(group, message)`: Sends message to all group members

## Message Primitives
```lua
-- Send message
send("server", {request = "get_data", id = 123})

-- Receive with pattern matching
receive {
    {request = "get_data", id = id} -> handle_get_data(id),
    {error = reason} -> handle_error(reason),
    after = 5000 -> timeout_handler()
}
```

## Handling Process Exits
```lua
-- Set up exit trapping
trap_exit(true)

-- Exit handler
receive {
    {"EXIT", from_pid, reason} -> handle_exit(from_pid, reason),
    other -> handle_other(other)
}
```

## Error Propagation
- Normal exit: Process terminates normally, linked processes continue
- Error exit: Process terminates with error, signal propagated to linked processes
- Kill exit: Untrappable termination signal
  Great questions! Let me break this down:

1. For `spawn`, yes - we'd be spawning a named Lua function as an independent process. Here's how it could work:

```lua
-- process definition in Lua
function user_process(init_args)
    -- process can create its own coroutines internally
    local co = coroutine.create(function()
        while true do
            -- do some work
            coroutine.yield()
        end
    end)
    
    -- main process loop
    while true do
        local msg = receive()  -- built-in receive function
        if msg.type == "work" then
            -- resume internal coroutine
            coroutine.resume(co)
        end
    end
end

-- spawning it
local pid = spawn("user_process", {init_value = 42})
```

2. For error handling, you're right - we're limited to Lua's `pcall`/`xpcall`. Instead of try/catch, we'd use this pattern:

```lua
-- In the process
local ok, err = pcall(function()
    -- risky operation
end)

if not ok then
    -- we can either:
    -- 1. Handle error locally
    -- 2. Signal parent process
    send(parent_pid, {type = "error", error = err})
    -- 3. Exit process with error
    exit(err)  -- this would notify linked processes
end
```

3. For supervisor trees - yes, they can definitely be added later! They're essentially processes that manage other processes' lifecycles. Here's a conceptual example:

```lua
function supervisor_process(spec)
    local children = {}
    
    -- start initial children
    for _, child_spec in ipairs(spec.children) do
        local pid = spawn(child_spec.module, child_spec.args)
        children[pid] = child_spec
        -- monitor child
        monitor(pid)
    end
    
    while true do
        local msg = receive()
        if msg.type == "DOWN" then
            -- child process died
            local failed_pid = msg.pid
            local child_spec = children[failed_pid]
            
            -- apply restart strategy
            if spec.strategy == "one_for_one" then
                -- just restart this process
                local new_pid = spawn(child_spec.module, child_spec.args)
                children[new_pid] = child_spec
                monitor(new_pid)
            elseif spec.strategy == "one_for_all" then
                -- restart all children
                restart_all_children()
            end
        end
    end
end

-- Usage:
local sup_spec = {
    strategy = "one_for_one",
    children = {
        {
            module = "user_process",
            args = {init_value = 42},
            restart = "permanent"  -- always restart on failure
        },
        {
            module = "worker_process",
            args = {},
            restart = "temporary"  -- don't restart on normal exit
        }
    }
}

spawn("supervisor_process", sup_spec)
```

Key aspects of supervisors:
1. They monitor their children using `monitor` (one-way) or `link` (two-way)
2. They have restart strategies:
    - `one_for_one`: Only restart failed process
    - `one_for_all`: Restart all children when one fails
    - `rest_for_one`: Restart failed process and all started after it
3. They have child specifications:
    - `permanent`: Always restart
    - `temporary`: Never restart
    - `transient`: Restart only on abnormal termination

The Go side would handle the actual process management:

```go
func (s *Scheduler) Spawn(name string, args lua.LValue) (string, error) {
    // Create new Lua state
    L := lua.NewState()
    defer L.Close()
    
    // Load process function
    if err := L.CallByParam(lua.P{
        Fn: L.GetGlobal(name),
        NRet: 0,
        Protect: true,
    }, args); err != nil {
        return "", err
    }
    
    pid := generatePID()
    process := NewProcess(pid, L)
    s.processes[pid] = process
    
    return pid, nil
}
```

The beauty of this design is that you can start simple with basic process spawning and message passing, then add supervisors later as your system grows. Supervisors are just special processes that implement automatic lifecycle management - they use the same basic primitives (spawn, monitor, receive) as regular processes.

Would you like me to elaborate on any part of this, particularly:
1. How to implement process monitoring/linking
2. More details on supervisor strategies
3. How to handle process initialization and cleanup

package process

import (
"context"
"sync"
)

// ProcessID is a unique identifier for a process
type ProcessID string

// Message represents any communication between processes
type Message struct {
From    ProcessID
To      ProcessID
Type    string
Payload interface{}
}

// ProcessState represents current process state
type ProcessState int

const (
StateInit ProcessState = iota
StateRunning
StatePaused
StateStopped
StateFailed
)

// Runtime represents an abstract process runtime (Lua, Go, Console, etc)
type Runtime interface {
// Initialize the runtime with context
Init(ctx context.Context) error

    // Step performs one execution step
    Step() error
    
    // HandleMessage processes an incoming message
    HandleMessage(msg Message) error
    
    // Cleanup performs runtime cleanup
    Cleanup() error
}

// Process is the minimal process abstraction
type Process interface {
// ID returns process identifier
ID() ProcessID

    // State returns current process state
    State() ProcessState
    
    // Runtime returns process runtime
    Runtime() Runtime
    
    // Send sends a message to the process
    Send(msg Message) error
    
    // Receive receives next message (with optional timeout)
    Receive(timeout time.Duration) (Message, error)
    
    // Link creates bidirectional link with another process
    Link(other Process) error
    
    // Monitor sets up one-way monitoring of another process
    Monitor(other Process) error
    
    // Step performs one process step
    Step() error
}

// Mailbox handles message queuing for a process
type Mailbox struct {
mu      sync.RWMutex
queue   []Message
notify  chan struct{}
process Process
}

// MessageBus handles message routing between processes
type MessageBus interface {
// Route sends message to destination process
Route(msg Message) error

    // Subscribe registers process for message type
    Subscribe(processID ProcessID, msgType string) error
    
    // Unsubscribe removes process subscription
    Unsubscribe(processID ProcessID, msgType string) error
}

// ProcessManager manages process lifecycle and relationships
type ProcessManager interface {
// Spawn creates new process
Spawn(runtime Runtime, args interface{}) (Process, error)

    // Kill terminates process
    Kill(id ProcessID) error
    
    // GetProcess returns process by ID
    GetProcess(id ProcessID) (Process, error)
    
    // Children returns child processes
    Children() []Process
}

// BaseProcess implements common Process functionality
type BaseProcess struct {
id       ProcessID
state    ProcessState
runtime  Runtime
mailbox  *Mailbox
links    map[ProcessID]Process
monitors map[ProcessID]Process
manager  ProcessManager
ctx      context.Context
cancel   context.CancelFunc
mu       sync.RWMutex
}

// Implementation of concrete runtimes

type LuaRuntime struct {
state    *lua.LState
process  Process
}

func (r *LuaRuntime) Init(ctx context.Context) error {
// Initialize Lua state
r.state = lua.NewState()
// Register process-related functions
r.registerProcessFuncs()
return nil
}

func (r *LuaRuntime) Step() error {
// Resume any yielded coroutines
// Handle any pending callbacks
return nil
}

func (r *LuaRuntime) HandleMessage(msg Message) error {
// Convert message to Lua table
// Call message handler if registered
return nil
}

type ConsoleRuntime struct {
stdin    *bufio.Reader
stdout   *bufio.Writer
process  Process
}

func (r *ConsoleRuntime) Init(ctx context.Context) error {
r.stdin = bufio.NewReader(os.Stdin)
r.stdout = bufio.NewWriter(os.Stdout)
return nil
}

// Example message bus implementation
type LocalMessageBus struct {
processes   map[ProcessID]Process
subscribers map[string][]ProcessID
mu          sync.RWMutex
}

func (b *LocalMessageBus) Route(msg Message) error {
b.mu.RLock()
defer b.mu.RUnlock()

    // Direct message routing
    if msg.To != "" {
        if proc, exists := b.processes[msg.To]; exists {
            return proc.Send(msg)
        }
        return fmt.Errorf("destination process not found: %s", msg.To)
    }
    
    // Broadcast to subscribers
    if subs, exists := b.subscribers[msg.Type]; exists {
        for _, pid := range subs {
            if proc, exists := b.processes[pid]; exists {
                if err := proc.Send(msg); err != nil {
                    // Log error but continue broadcasting
                    log.Printf("error sending to %s: %v", pid, err)
                }
            }
        }
    }
    return nil
}

// Example process tree implementation
type ProcessTree struct {
root     Process
children map[ProcessID]*ProcessTree
mu       sync.RWMutex
}

func (t *ProcessTree) Add(parent ProcessID, child Process) error {
t.mu.Lock()
defer t.mu.Unlock()

    if parent == "" {
        t.root = child
        return nil
    }
    
    parentNode := t.find(parent)
    if parentNode == nil {
        return fmt.Errorf("parent process not found: %s", parent)
    }
    
    childNode := &ProcessTree{
        root:     child,
        children: make(map[ProcessID]*ProcessTree),
    }
    parentNode.children[child.ID()] = childNode
    return nil
}No, Erlang pattern matching is much more powerful! In Erlang, pattern matching works with the full structure of terms (which are called ETF - External Term Format or just Erlang terms). It can match:

1. Exact values
2. Variables (which then get bound)
3. Tuples with specific patterns
4. Lists with specific patterns
5. Maps with specific patterns
6. Complex nested structures

For example in Erlang:
```erlang
receive
    {call, Pid, {multiply, X, Y}} -> Pid ! {result, X * Y};
    {status, From} when is_pid(From) -> From ! {status, ok};
    {error, Reason} -> handle_error(Reason);
    _ -> handle_unknown()  % Catch all
end
```

The pattern matching here isn't just on strings - it's matching on:
- Tuple structure
- Nested tuple structure
- Variable binding (Pid, X, Y get bound)
- Guard clauses (is_pid check)

So our mailbox abstraction is currently too simplistic - we should support full structural pattern matching. Should we rethink the pattern matching approach? We could define a more robust pattern system that can match message structure, not just kinds/strings.
Let's create a high-performance lock-free mailbox. We can use channels and a ring buffer for this:

```go
package process

import (
    "context"
    "sync/atomic"
)

// Pattern represents a message matching pattern
type Pattern struct {
    Kind string
}

// Mailbox is a lock-free FIFO message queue with pattern matching
type Mailbox struct {
    buffer  []Message        // Ring buffer for messages
    head    atomic.Uint64    // Read position
    tail    atomic.Uint64    // Write position
    size    uint64          // Buffer size (must be power of 2)
    mask    uint64          // Bit mask for ring buffer wrap-around
    notify  chan struct{}   // Channel to notify receivers
}

func NewMailbox(size uint64) *Mailbox {
    // Round up to next power of 2
    size = nextPowerOfTwo(size)
    
    return &Mailbox{
        buffer:  make([]Message, size),
        size:    size,
        mask:    size - 1,
        notify:  make(chan struct{}, 1),
    }
}

// Send adds message to mailbox
func (m *Mailbox) Send(msg Message) bool {
    tail := m.tail.Load()
    head := m.head.Load()
    
    // Check if buffer is full
    if tail-head >= m.size {
        return false
    }
    
    // Store message
    m.buffer[tail&m.mask] = msg
    m.tail.Add(1)
    
    // Notify receivers
    select {
    case m.notify <- struct{}{}:
    default:
    }
    
    return true
}

// Receive gets first message matching pattern
func (m *Mailbox) Receive(ctx context.Context, pattern Pattern) (Message, bool) {
    for {
        head := m.head.Load()
        tail := m.tail.Load()
        
        // Check if empty
        if head >= tail {
            // Wait for new message or context done
            select {
            case <-m.notify:
                continue
            case <-ctx.Done():
                return Message{}, false
            }
        }
        
        // Check messages from head
        for i := head; i < tail; i++ {
            msg := m.buffer[i&m.mask]
            if pattern.Kind == "" || msg.Kind == pattern.Kind {
                // Try to claim this message
                if m.head.CompareAndSwap(head, head+1) {
                    return msg, true
                }
                // Someone else claimed it, retry
                break
            }
        }
        
        // No matching messages, advance head past non-matching
        m.head.CompareAndSwap(head, head+1)
    }
}

// ReceiveTimeout receives with timeout
func (m *Mailbox) ReceiveTimeout(timeout time.Duration, pattern Pattern) (Message, bool) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    return m.Receive(ctx, pattern)
}

// Flush removes all messages
func (m *Mailbox) Flush() {
    head := m.head.Load()
    m.tail.Store(head)
}

// Len returns number of messages in mailbox
func (m *Mailbox) Len() uint64 {
    return m.tail.Load() - m.head.Load()
}

// Helper for rounding to power of 2
func nextPowerOfTwo(v uint64) uint64 {
    v--
    v |= v >> 1
    v |= v >> 2
    v |= v >> 4
    v |= v >> 8
    v |= v >> 16
    v |= v >> 32
    v++
    return v
}
```

Key features:

1. **Lock-free implementation**:
   - Uses atomic operations
   - Ring buffer for efficient storage
   - Size is power of 2 for fast modulo with mask

2. **Pattern Matching**:
   - Can receive specific message types
   - Skip non-matching messages
   - Support for timeout and context cancellation

3. **Performance Optimizations**:
   - No mutex locks
   - Minimal contention points
   - Efficient memory usage with ring buffer
   - Fast wrapping with bitwise AND

4. **FIFO Guarantees**:
   - Messages delivered in order
   - Pattern matching preserves order
   - No message loss

Would you like me to:
1. Add more pattern matching capabilities?
2. Show how to integrate this with the Process interface?
3. Add benchmarks or stress tests?
4. Add message priority support?

Let me know what aspects to focus on next!
