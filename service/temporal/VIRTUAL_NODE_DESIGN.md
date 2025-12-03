# Temporal Virtual Node Design

## Problem

When a regular process wants to monitor a Temporal workflow, we need a way to:
1. Set up a subscription to workflow completion
2. Notify the monitoring process when workflow completes
3. Clean up subscriptions when monitoring is released

## Solution: Temporal as Virtual Node

Temporal should register as a **virtual node** in the relay system. When topology receives a monitor request for a workflow PID, it routes the request to the Temporal virtual node, which manages the subscription.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Local Node (wippy runtime)                                  │
│                                                              │
│  Process A (regular process)                                │
│      ↓ spawns with monitor=true                             │
│  TopologyMutator adds Wait() hook                           │
│      ↓                                                       │
│  Topology.Wait(processA_PID, workflowB_PID)                 │
│      ↓ detects workflowB_PID.Node = "temporal:default"      │
│      ↓ sends MonitorRequest package                         │
│  Router.Send(pkg) → routes to virtual node                  │
└──────────────────────────┼───────────────────────────────────┘
                           │
                           ↓ relay package
┌─────────────────────────────────────────────────────────────┐
│ Virtual Node: "temporal:default"                            │
│                                                              │
│  TemporalVirtualNode.Send(MonitorRequest)                   │
│      ↓                                                       │
│  Spawns goroutine:                                          │
│      client.GetWorkflow(workflowID, runID)                  │
│      run.Get(ctx, &result)  // blocks until complete        │
│      ↓                                                       │
│  When workflow completes:                                   │
│      router.Send(topology.Exit{                             │
│          Target: processA_PID,                              │
│          From:   workflowB_PID,                             │
│          Result: workflow result                            │
│      })                                                      │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Plan

### 1. Create TemporalVirtualNode

**File:** `service/temporal/virtualnode/receiver.go`

```go
type TemporalVirtualNode struct {
    nodeID   relay.NodeID  // "temporal:default"
    router   relay.Receiver
    client   client.Client  // Temporal SDK client
    monitors sync.Map      // map[workflowID]*monitorState
    links    sync.Map      // map[workflowID]*linkState
    log      *zap.Logger
}

type monitorState struct {
    targetPID relay.PID
    watchers  sync.Map  // map[callerPID]bool
    once      sync.Once // ensure single watcher goroutine
}
```

### 2. Implement relay.Receiver Interface

```go
func (n *TemporalVirtualNode) Send(pkg *relay.Package) error {
    // Parse incoming packages
    for _, msg := range pkg.Messages {
        for _, p := range msg.Payloads {
            switch event := p.Data().(type) {
            case *topology.MonitorRequestEvent:
                return n.handleMonitorRequest(event.Caller, event.Target)
            case *topology.MonitorReleaseEvent:
                return n.handleMonitorRelease(event.Caller, event.Target)
            case *topology.LinkRequestEvent:
                return n.handleLinkRequest(event.From, event.To)
            case *topology.UnlinkRequestEvent:
                return n.handleUnlinkRequest(event.From, event.To)
            }
        }
    }
    return nil
}
```

### 3. Handle Monitor Requests

```go
func (n *TemporalVirtualNode) handleMonitorRequest(caller, target relay.PID) error {
    // Get or create monitor state
    value, _ := n.monitors.LoadOrStore(target.UniqID, &monitorState{
        targetPID: target,
    })
    state := value.(*monitorState)

    // Add caller to watchers
    state.watchers.Store(caller.String(), true)

    // Spawn watcher goroutine (only once per workflow)
    state.once.Do(func() {
        go n.watchWorkflow(target, state)
    })

    return nil
}

func (n *TemporalVirtualNode) watchWorkflow(workflowPID relay.PID, state *monitorState) {
    ctx := context.Background()

    // Parse workflow ID and run ID from PID
    workflowID, runID := parseWorkflowPID(workflowPID.UniqID)

    // Get workflow handle
    run := n.client.GetWorkflow(ctx, workflowID, runID)

    // Block until workflow completes
    var result interface{}
    err := run.Get(ctx, &result)

    // Notify all watchers
    state.watchers.Range(func(key, _ interface{}) bool {
        callerPID, parseErr := relay.ParsePID(key.(string))
        if parseErr != nil {
            return true
        }

        exitPkg := topology.Exit(workflowPID, payload.New(result), err)
        exitPkg.Target = callerPID

        if sendErr := n.router.Send(exitPkg); sendErr != nil {
            n.log.Error("failed to send exit event",
                zap.String("workflow", workflowPID.String()),
                zap.String("watcher", callerPID.String()),
                zap.Error(sendErr))
        }

        return true
    })

    // Cleanup
    n.monitors.Delete(workflowPID.UniqID)
}
```

### 4. Register Virtual Node on Startup

**In client manager's Start() method:**

```go
func (m *ClientManager) Start(ctx context.Context) error {
    // ... existing client creation ...

    // Create virtual node receiver
    virtualNode := virtualnode.NewTemporalVirtualNode(
        "temporal:default",
        m.router,
        client,
        m.log,
    )

    // Register as virtual node
    m.bus.Send(ctx, event.Event{
        System: relay.VirtualNodeSystem,
        Kind:   relay.VirtualNodeRegister,
        Path:   "temporal:default",
        Data:   relay.VirtualNodeInfo{
            NodeID:   "temporal:default",
            Receiver: virtualNode,
        },
    })

    return nil
}
```

### 5. Workflow PID Format

Workflow PIDs should include the node ID:

```go
workflowPID := relay.PID{
    Node:   "temporal:default",
    UniqID: fmt.Sprintf("%s/%s", workflowID, runID),
}
```

## Benefits

1. **No special casing** - topology mutator treats all processes uniformly
2. **Clean separation** - Temporal SDK interactions isolated to virtual node
3. **Scalable** - multiple processes can monitor same workflow
4. **Proper lifecycle** - MonitorRelease cleans up, completion notifies
5. **Node abstraction** - Temporal appears as another node in the system

## Reference Implementation

See `system/topology/mock_virtualnode_test.go` for a complete example of virtual node implementation pattern.
