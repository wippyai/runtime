// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	relaysys "github.com/wippyai/runtime/system/relay"
)

// exitPackage creates an exit notification package for testing.
func exitPackage(p pid.PID, result payload.Payload, err error) *relay.Package {
	return relay.NewPackage(
		topology.SystemPID,
		p,
		topology.TopicEvents,
		payload.New(&topology.ExitEvent{
			At:   time.Now(),
			From: p,
			Kind: topology.Exit,
			Result: &runtime.Result{
				Value: result,
				Error: err,
			},
		}),
	)
}

// dummyHost is a simple host implementation for testing
type dummyHost struct {
	receivers sync.Map // map[PID]chan *relay.Package
}

func (d *dummyHost) Send(pkg *relay.Package) error {
	if receiver, ok := d.receivers.Load(pkg.Target.String()); ok {
		ch := receiver.(chan *relay.Package)
		select {
		case ch <- pkg:
		default:
		}
	}
	return nil
}

func (d *dummyHost) Attach(pid pid.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	d.receivers.Store(pid.String(), ch)
	return func() {
		d.receivers.Delete(pid.String())
	}, nil
}

func (d *dummyHost) Detach(pid pid.PID) {
	d.receivers.Delete(pid.String())
}

// MockPeerNode simulates a peer node (like Temporal) for testing.
// It can receive packages, handle monitoring/linking requests, and simulate completions.
type MockPeerNode struct {
	router   relay.Receiver
	logger   *testing.T
	monitors sync.Map
	links    sync.Map
	nodeID   pid.NodeID
}

type monitorState struct {
	targetPID pid.PID
	watchers  sync.Map // map[callerPID]bool
}

type linkState struct {
	targetPID pid.PID
	linked    sync.Map // map[remotePID]bool
}

// NewMockPeerNode creates a new mock peer node.
func NewMockPeerNode(nodeID pid.NodeID, router relay.Receiver, t *testing.T) *MockPeerNode {
	return &MockPeerNode{
		nodeID: nodeID,
		router: router,
		logger: t,
	}
}

// Send implements relay.Receiver interface.
// Handles incoming packages and routes them to appropriate handlers.
func (n *MockPeerNode) Send(pkg *relay.Package) error {
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

func (n *MockPeerNode) handleMonitorRequest(caller, target pid.PID) error {
	n.logger.Logf("MockPeerNode %s: received monitor request from %s for %s",
		n.nodeID, caller, target)

	value, _ := n.monitors.LoadOrStore(target.UniqID, &monitorState{
		targetPID: target,
	})
	state := value.(*monitorState)
	state.watchers.Store(caller.String(), true)

	return nil
}

func (n *MockPeerNode) handleMonitorRelease(caller, target pid.PID) error {
	n.logger.Logf("MockPeerNode %s: received release request from %s for %s",
		n.nodeID, caller, target)

	value, ok := n.monitors.Load(target.UniqID)
	if !ok {
		return nil
	}

	state := value.(*monitorState)
	state.watchers.Delete(caller.String())

	empty := true
	state.watchers.Range(func(_, _ any) bool {
		empty = false
		return false
	})
	if empty {
		n.monitors.Delete(target.UniqID)
	}

	return nil
}

func (n *MockPeerNode) handleLinkRequest(from, to pid.PID) error {
	n.logger.Logf("MockPeerNode %s: received link request from %s to %s",
		n.nodeID, from, to)

	value, _ := n.links.LoadOrStore(to.UniqID, &linkState{
		targetPID: to,
	})
	state := value.(*linkState)
	state.linked.Store(from.String(), true)

	return nil
}

func (n *MockPeerNode) handleUnlinkRequest(from, to pid.PID) error {
	n.logger.Logf("MockPeerNode %s: received unlink request from %s to %s",
		n.nodeID, from, to)

	value, ok := n.links.Load(to.UniqID)
	if !ok {
		return nil
	}

	state := value.(*linkState)
	state.linked.Delete(from.String())

	empty := true
	state.linked.Range(func(_, _ any) bool {
		empty = false
		return false
	})
	if empty {
		n.links.Delete(to.UniqID)
	}

	return nil
}

// SimulateCompletion simulates a workflow/process completing on the peer node.
// Sends exit events to all watchers.
func (n *MockPeerNode) SimulateCompletion(targetPID pid.PID, result any, err error) error {
	n.logger.Logf("MockPeerNode %s: simulating completion for %s", n.nodeID, targetPID)

	value, ok := n.monitors.Load(targetPID.UniqID)
	if !ok {
		return fmt.Errorf("no monitors for %s", targetPID)
	}

	state := value.(*monitorState)

	state.watchers.Range(func(key, _ any) bool {
		callerPIDStr := key.(string)
		callerPID, parseErr := pid.ParsePID(callerPIDStr)
		if parseErr != nil {
			n.logger.Logf("MockPeerNode %s: failed to parse watcher PID %s: %v",
				n.nodeID, callerPIDStr, parseErr)
			return true
		}

		exitPkg := exitPackage(targetPID, payload.New(result), err)
		exitPkg.Target = callerPID

		n.logger.Logf("MockPeerNode %s: sending exit event to watcher %s",
			n.nodeID, callerPID)

		if sendErr := n.router.Send(exitPkg); sendErr != nil {
			n.logger.Logf("MockPeerNode %s: failed to send exit event: %v",
				n.nodeID, sendErr)
		}

		return true
	})

	n.monitors.Delete(targetPID.UniqID)

	return nil
}

// GetWatchers returns all PIDs monitoring the given target PID.
func (n *MockPeerNode) GetWatchers(targetPID pid.PID) []pid.PID {
	var watchers []pid.PID

	value, ok := n.monitors.Load(targetPID.UniqID)
	if !ok {
		return watchers
	}

	state := value.(*monitorState)
	state.watchers.Range(func(key, _ any) bool {
		callerPIDStr := key.(string)
		callerPID, err := pid.ParsePID(callerPIDStr)
		if err == nil {
			watchers = append(watchers, callerPID)
		}
		return true
	})

	return watchers
}

// GetLinkedProcesses returns all PIDs linked to the given target PID.
func (n *MockPeerNode) GetLinkedProcesses(targetPID pid.PID) []pid.PID {
	var linked []pid.PID

	value, ok := n.links.Load(targetPID.UniqID)
	if !ok {
		return linked
	}

	state := value.(*linkState)
	state.linked.Range(func(key, _ any) bool {
		linkedPIDStr := key.(string)
		linkedPID, err := pid.ParsePID(linkedPIDStr)
		if err == nil {
			linked = append(linked, linkedPID)
		}
		return true
	})

	return linked
}

// SimulateFailure simulates a workflow/process failing on the peer node.
// Sends link-down events to linked processes (if error is not nil).
func (n *MockPeerNode) SimulateFailure(targetPID pid.PID, err error) error {
	n.logger.Logf("MockPeerNode %s: simulating failure for %s", n.nodeID, targetPID)

	if monValue, ok := n.monitors.Load(targetPID.UniqID); ok {
		monState := monValue.(*monitorState)

		monState.watchers.Range(func(key, _ any) bool {
			callerPIDStr := key.(string)
			callerPID, parseErr := pid.ParsePID(callerPIDStr)
			if parseErr != nil {
				return true
			}

			exitPkg := exitPackage(targetPID, payload.New(nil), err)
			exitPkg.Target = callerPID

			n.logger.Logf("MockPeerNode %s: sending exit event to watcher %s",
				n.nodeID, callerPID)

			_ = n.router.Send(exitPkg)
			return true
		})

		n.monitors.Delete(targetPID.UniqID)
	}

	if linkValue, ok := n.links.Load(targetPID.UniqID); ok {
		lnkState := linkValue.(*linkState)

		lnkState.linked.Range(func(key, _ any) bool {
			linkedPIDStr := key.(string)
			linkedPID, parseErr := pid.ParsePID(linkedPIDStr)
			if parseErr != nil {
				return true
			}

			linkDownPkg := relay.NewPackage(
				pid.PID{UniqID: "topology"},
				linkedPID,
				topology.TopicEvents,
				payload.New(&topology.ExitEvent{
					From:   targetPID,
					Kind:   topology.LinkDown,
					Result: &runtime.Result{Error: err},
				}),
			)

			n.logger.Logf("MockPeerNode %s: sending link-down event to %s",
				n.nodeID, linkedPID)

			_ = n.router.Send(linkDownPkg)
			return true
		})

		n.links.Delete(targetPID.UniqID)
	}

	return nil
}

// TestIntegration_CrossNodeMonitoring_EndToEnd tests the complete flow of monitoring
// a workflow on a peer node from start to completion.
func TestIntegration_CrossNodeMonitoring_EndToEnd(t *testing.T) {
	// Production sender, target admission and completion handler across native
	// codec boundaries. OS-process/TLS proof lives in the boot harness.
	f := newMonitorSenderFixture(t)
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	f.remote.Complete(f.target, &runtime.Result{Value: payload.New("completed")})
	require.Len(t, f.deliveries, 1)
	require.True(t, f.deliveryTargets[0].Equal(f.caller))
	require.Equal(t, topology.Exit, f.deliveries[0]["kind"])
	require.True(t, f.deliveries[0]["from"].(pid.PID).Equal(f.target))
	require.NotNil(t, f.deliveries[0]["result"])
	f.remote.Complete(f.target, &runtime.Result{})
	require.Len(t, f.deliveries, 1, "completed target must not notify twice")
}

// TestIntegration_CrossNodeLinking_EndToEnd tests the complete flow of linking
// with a workflow on a peer node and receiving link-down on failure.
func TestIntegration_CrossNodeLinking_EndToEnd(t *testing.T) {
	// Setup
	localNode := relaysys.NewNode("local")
	router := relaysys.NewRouter(localNode, nil)
	topo := NewTopology(router, "local")

	// Register dummy host for the local node
	err := localNode.RegisterHost("myhost", &dummyHost{})
	require.NoError(t, err)

	peerNode := NewMockPeerNode("temporal-prod", router, t)
	err = router.RegisterPeer("temporal-prod", peerNode)
	require.NoError(t, err)

	localProcessPID := pid.PID{
		Node:   "local",
		Host:   "myhost",
		UniqID: "process-1",
	}
	localProcessPID = localProcessPID.Precomputed()

	workflowPID := pid.PID{
		Node:   "temporal-prod",
		Host:   "my-task-queue",
		UniqID: "workflow-456",
	}
	workflowPID = workflowPID.Precomputed()

	err = topo.Register(localProcessPID)
	require.NoError(t, err)

	linkDownCh := make(chan *relay.Package, 10)
	cancel, err := localNode.Attach(localProcessPID, linkDownCh)
	require.NoError(t, err)
	defer cancel()

	// ACT: Establish link
	err = topo.Link(localProcessPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// ASSERT: Both sides should have the link established
	localLinks := topo.GetLinks(localProcessPID)
	require.Len(t, localLinks, 1, "local side should have link")
	assert.Equal(t, workflowPID, localLinks[0])

	virtualLinks := peerNode.GetLinkedProcesses(workflowPID)
	require.Len(t, virtualLinks, 1, "virtual side should have link")
	assert.Equal(t, localProcessPID, virtualLinks[0])

	// ACT: Simulate workflow failure
	workflowErr := fmt.Errorf("workflow failed")
	err = peerNode.SimulateFailure(workflowPID, workflowErr)
	require.NoError(t, err)

	// ASSERT: Local process should receive link-down notification
	select {
	case pkg := <-linkDownCh:
		var found bool
		for _, msg := range pkg.Messages {
			for _, p := range msg.Payloads {
				if exitEvt, ok := p.Data().(*topology.ExitEvent); ok {
					found = true
					assert.Equal(t, workflowPID, exitEvt.From)
					assert.Equal(t, topology.LinkDown, exitEvt.Kind)
					assert.NotNil(t, exitEvt.Result.Error)
				}
			}
		}
		assert.True(t, found, "package should contain link-down event")

	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for link-down notification")
	}
}

// TestIntegration_MultipleWatchers tests multiple processes monitoring the same workflow.
func TestIntegration_MultipleWatchers(t *testing.T) {
	f := newMonitorSenderFixture(t)
	callers := []pid.PID{f.caller, {Node: f.caller.Node, Host: "second", UniqID: "two"}, {Node: f.caller.Node, Host: "third", UniqID: "three"}}
	for i, caller := range callers {
		if i != 0 {
			require.NoError(t, f.local.Register(caller))
		}
		require.NoError(t, f.local.Monitor(caller, f.target))
	}
	f.remote.Complete(f.target, &runtime.Result{})
	require.Len(t, f.deliveries, 3)
	got := make(map[string]bool)
	for _, target := range f.deliveryTargets {
		require.False(t, got[target.String()])
		got[target.String()] = true
	}
	for _, caller := range callers {
		require.True(t, got[caller.String()])
	}
}

// TestIntegration_ReleaseMonitor tests releasing monitoring before completion.
func TestIntegration_ReleaseMonitor(t *testing.T) {
	f := newMonitorSenderFixture(t)
	other := pid.PID{Node: f.caller.Node, Host: "other", UniqID: "observer"}
	require.NoError(t, f.local.Register(other))
	require.NoError(t, f.local.Monitor(f.caller, f.target))
	require.NoError(t, f.local.Monitor(other, f.target))
	require.NoError(t, f.local.Demonitor(f.caller, f.target))
	f.remote.Complete(f.target, &runtime.Result{})
	require.Len(t, f.deliveries, 1, "released observer must not receive completion")
	require.True(t, f.deliveryTargets[0].Equal(other))
}

// TestIntegration_UnlinkBeforeFailure tests unlinking before workflow failure.
func TestIntegration_UnlinkBeforeFailure(t *testing.T) {
	// Setup
	localNode := relaysys.NewNode("local")
	router := relaysys.NewRouter(localNode, nil)
	topo := NewTopology(router, "local")

	// Register host
	err := localNode.RegisterHost("host1", &dummyHost{})
	require.NoError(t, err)

	peerNode := NewMockPeerNode("temporal-prod", router, t)
	err = router.RegisterPeer("temporal-prod", peerNode)
	require.NoError(t, err)

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "p1"}
	localPID = localPID.Precomputed()
	workflowPID := pid.PID{Node: "temporal-prod", Host: "queue", UniqID: "wf-unlink"}
	workflowPID = workflowPID.Precomputed()

	err = topo.Register(localPID)
	require.NoError(t, err)

	linkDownCh := make(chan *relay.Package, 10)
	cancel, _ := localNode.Attach(localPID, linkDownCh)
	defer cancel()

	// ACT: Link then unlink
	err = topo.Link(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	assert.Len(t, peerNode.GetLinkedProcesses(workflowPID), 1)

	err = topo.Unlink(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// ASSERT: Virtual node should have no links
	assert.Len(t, peerNode.GetLinkedProcesses(workflowPID), 0)

	// ACT: Simulate failure after unlink
	err = peerNode.SimulateFailure(workflowPID, fmt.Errorf("failed"))
	require.NoError(t, err)

	// ASSERT: No link-down event should be received
	select {
	case <-linkDownCh:
		t.Fatal("should not receive link-down after unlinking")
	case <-time.After(50 * time.Millisecond):
		// Expected - no event received
	}
}
