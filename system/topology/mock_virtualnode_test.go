package topology

import (
	"fmt"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

// MockVirtualNode simulates a virtual node (like Temporal) for testing.
// It can receive packages, handle monitoring/linking requests, and simulate completions.
type MockVirtualNode struct {
	nodeID   relay.NodeID
	router   relay.Receiver
	monitors sync.Map // map[workflowID]*monitorState
	links    sync.Map // map[workflowID]*linkState
	logger   *testing.T
	mu       sync.Mutex
}

type monitorState struct {
	targetPID relay.PID
	watchers  sync.Map // map[callerPID]bool
}

type linkState struct {
	targetPID relay.PID
	linked    sync.Map // map[remotePID]bool
}

// NewMockVirtualNode creates a new mock virtual node.
func NewMockVirtualNode(nodeID relay.NodeID, router relay.Receiver, t *testing.T) *MockVirtualNode {
	return &MockVirtualNode{
		nodeID: nodeID,
		router: router,
		logger: t,
	}
}

// Send implements relay.Receiver interface.
// Handles incoming packages and routes them to appropriate handlers.
func (n *MockVirtualNode) Send(pkg *relay.Package) error {
	// Parse event type from payload
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

	return fmt.Errorf("unknown event type in package")
}

func (n *MockVirtualNode) handleMonitorRequest(caller, target relay.PID) error {
	n.logger.Logf("MockVirtualNode %s: received monitor request from %s for %s",
		n.nodeID, caller, target)

	// Get or create monitor state
	value, _ := n.monitors.LoadOrStore(target.UniqID, &monitorState{
		targetPID: target,
	})
	state := value.(*monitorState)

	// Add caller to watchers
	state.watchers.Store(caller.String(), true)

	return nil
}

func (n *MockVirtualNode) handleMonitorRelease(caller, target relay.PID) error {
	n.logger.Logf("MockVirtualNode %s: received release request from %s for %s",
		n.nodeID, caller, target)

	value, ok := n.monitors.Load(target.UniqID)
	if !ok {
		return nil
	}

	state := value.(*monitorState)
	state.watchers.Delete(caller.String())

	// Cleanup if no watchers left
	empty := true
	state.watchers.Range(func(_, _ interface{}) bool {
		empty = false
		return false
	})
	if empty {
		n.monitors.Delete(target.UniqID)
	}

	return nil
}

func (n *MockVirtualNode) handleLinkRequest(from, to relay.PID) error {
	n.logger.Logf("MockVirtualNode %s: received link request from %s to %s",
		n.nodeID, from, to)

	// Get or create link state
	value, _ := n.links.LoadOrStore(to.UniqID, &linkState{
		targetPID: to,
	})
	state := value.(*linkState)

	// Add remote PID to linked set
	state.linked.Store(from.String(), true)

	return nil
}

func (n *MockVirtualNode) handleUnlinkRequest(from, to relay.PID) error {
	n.logger.Logf("MockVirtualNode %s: received unlink request from %s to %s",
		n.nodeID, from, to)

	value, ok := n.links.Load(to.UniqID)
	if !ok {
		return nil
	}

	state := value.(*linkState)
	state.linked.Delete(from.String())

	// Cleanup if no links left
	empty := true
	state.linked.Range(func(_, _ interface{}) bool {
		empty = false
		return false
	})
	if empty {
		n.links.Delete(to.UniqID)
	}

	return nil
}

// SimulateCompletion simulates a workflow/process completing on the virtual node.
// Sends exit events to all watchers.
func (n *MockVirtualNode) SimulateCompletion(targetPID relay.PID, result interface{}, err error) error {
	n.logger.Logf("MockVirtualNode %s: simulating completion for %s", n.nodeID, targetPID)

	value, ok := n.monitors.Load(targetPID.UniqID)
	if !ok {
		return fmt.Errorf("no monitors for %s", targetPID)
	}

	state := value.(*monitorState)

	// Send exit event to all watchers
	state.watchers.Range(func(key, _ interface{}) bool {
		callerPIDStr := key.(string)
		callerPID, parseErr := relay.ParsePID(callerPIDStr)
		if parseErr != nil {
			n.logger.Logf("MockVirtualNode %s: failed to parse watcher PID %s: %v",
				n.nodeID, callerPIDStr, parseErr)
			return true
		}

		exitPkg := topology.Exit(targetPID, payload.New(result), err)
		exitPkg.Target = callerPID

		n.logger.Logf("MockVirtualNode %s: sending exit event to watcher %s",
			n.nodeID, callerPID)

		if sendErr := n.router.Send(exitPkg); sendErr != nil {
			n.logger.Logf("MockVirtualNode %s: failed to send exit event: %v",
				n.nodeID, sendErr)
		}

		return true
	})

	// Cleanup after completion
	n.monitors.Delete(targetPID.UniqID)

	return nil
}

// GetWatchers returns all PIDs monitoring the given target PID.
func (n *MockVirtualNode) GetWatchers(targetPID relay.PID) []relay.PID {
	var watchers []relay.PID

	value, ok := n.monitors.Load(targetPID.UniqID)
	if !ok {
		return watchers
	}

	state := value.(*monitorState)
	state.watchers.Range(func(key, _ interface{}) bool {
		callerPIDStr := key.(string)
		callerPID, err := relay.ParsePID(callerPIDStr)
		if err == nil {
			watchers = append(watchers, callerPID)
		}
		return true
	})

	return watchers
}

// GetLinkedProcesses returns all PIDs linked to the given target PID.
func (n *MockVirtualNode) GetLinkedProcesses(targetPID relay.PID) []relay.PID {
	var linked []relay.PID

	value, ok := n.links.Load(targetPID.UniqID)
	if !ok {
		return linked
	}

	state := value.(*linkState)
	state.linked.Range(func(key, _ interface{}) bool {
		linkedPIDStr := key.(string)
		linkedPID, err := relay.ParsePID(linkedPIDStr)
		if err == nil {
			linked = append(linked, linkedPID)
		}
		return true
	})

	return linked
}

// SimulateFailure simulates a workflow/process failing on the virtual node.
// Sends link-down events to linked processes (if error is not nil).
func (n *MockVirtualNode) SimulateFailure(targetPID relay.PID, err error) error {
	n.logger.Logf("MockVirtualNode %s: simulating failure for %s", n.nodeID, targetPID)

	// Send exit to monitors
	if monValue, ok := n.monitors.Load(targetPID.UniqID); ok {
		monState := monValue.(*monitorState)

		monState.watchers.Range(func(key, _ interface{}) bool {
			callerPIDStr := key.(string)
			callerPID, parseErr := relay.ParsePID(callerPIDStr)
			if parseErr != nil {
				return true
			}

			exitPkg := topology.Exit(targetPID, payload.New(nil), err)
			exitPkg.Target = callerPID

			n.logger.Logf("MockVirtualNode %s: sending exit event to watcher %s",
				n.nodeID, callerPID)

			_ = n.router.Send(exitPkg)
			return true
		})

		n.monitors.Delete(targetPID.UniqID)
	}

	// Send link-down to linked processes
	if linkValue, ok := n.links.Load(targetPID.UniqID); ok {
		linkState := linkValue.(*linkState)

		linkState.linked.Range(func(key, _ interface{}) bool {
			linkedPIDStr := key.(string)
			linkedPID, parseErr := relay.ParsePID(linkedPIDStr)
			if parseErr != nil {
				return true
			}

			linkDownPkg := relay.NewPackage(
				relay.PID{UniqID: "topology"},
				linkedPID,
				topology.TopicEvents,
				payload.New(&topology.ExitEvent{
					From:   targetPID,
					Kind:   topology.KindLinkDown,
					Result: &runtime.Result{Error: err},
				}),
			)

			n.logger.Logf("MockVirtualNode %s: sending link-down event to %s",
				n.nodeID, linkedPID)

			_ = n.router.Send(linkDownPkg)
			return true
		})

		n.links.Delete(targetPID.UniqID)
	}

	return nil
}
