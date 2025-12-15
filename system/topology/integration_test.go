package topology

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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
	nodeID   pid.NodeID
	router   relay.Receiver
	monitors sync.Map // map[workflowID]*monitorState
	links    sync.Map // map[workflowID]*linkState
	logger   *testing.T
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
	state.watchers.Range(func(_, _ interface{}) bool {
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
	state.linked.Range(func(_, _ interface{}) bool {
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
func (n *MockPeerNode) SimulateCompletion(targetPID pid.PID, result interface{}, err error) error {
	n.logger.Logf("MockPeerNode %s: simulating completion for %s", n.nodeID, targetPID)

	value, ok := n.monitors.Load(targetPID.UniqID)
	if !ok {
		return fmt.Errorf("no monitors for %s", targetPID)
	}

	state := value.(*monitorState)

	state.watchers.Range(func(key, _ interface{}) bool {
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
	state.watchers.Range(func(key, _ interface{}) bool {
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
	state.linked.Range(func(key, _ interface{}) bool {
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

		monState.watchers.Range(func(key, _ interface{}) bool {
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

		lnkState.linked.Range(func(key, _ interface{}) bool {
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
	// Setup: Create local node with router and topology
	localNode := relaysys.NewNode("local")
	router := relaysys.NewRouter(localNode, nil)
	topo := NewTopology(router, "local")

	// Register dummy host for the local node
	err := localNode.RegisterHost("myhost", &dummyHost{})
	require.NoError(t, err)

	// Setup: Create mock peer node (simulating Temporal)
	peerNode := NewMockPeerNode("temporal-prod", router, t)
	err = router.RegisterPeer("temporal-prod", peerNode)
	require.NoError(t, err)

	// Setup PIDs
	localProcessPID := pid.PID{
		Node:   "local",
		Host:   "myhost",
		UniqID: "process-1",
	}.Precomputed()

	workflowPID := pid.PID{
		Node:   "temporal-prod",
		Host:   "my-task-queue",
		UniqID: "workflow-123",
	}.Precomputed()

	// Register local process
	err = topo.Register(localProcessPID)
	require.NoError(t, err)

	// Setup: Create channel to receive exit notifications
	exitCh := make(chan *relay.Package, 10)
	cancel, err := localNode.Attach(localProcessPID, exitCh)
	require.NoError(t, err)
	defer cancel()

	// ACT: Local process starts monitoring the workflow
	err = topo.Monitor(localProcessPID, workflowPID)
	require.NoError(t, err)

	// Give time for message propagation
	time.Sleep(10 * time.Millisecond)

	// ASSERT: Virtual node should have received the monitor request
	watchers := peerNode.GetWatchers(workflowPID)
	require.Len(t, watchers, 1, "peer node should have 1 watcher")
	assert.Equal(t, localProcessPID, watchers[0])

	// ACT: Simulate workflow completion
	workflowResult := "workflow completed successfully"
	err = peerNode.SimulateCompletion(workflowPID, workflowResult, nil)
	require.NoError(t, err)

	// ASSERT: Local process should receive exit notification
	select {
	case pkg := <-exitCh:
		assert.Equal(t, localProcessPID, pkg.Target, "exit event should target local process")

		var found bool
		for _, msg := range pkg.Messages {
			for _, p := range msg.Payloads {
				if exitEvt, ok := p.Data().(*topology.ExitEvent); ok {
					found = true
					assert.Equal(t, workflowPID, exitEvt.From)
					assert.Equal(t, topology.Exit, exitEvt.Kind)
					assert.Nil(t, exitEvt.Result.Error)
				}
			}
		}
		assert.True(t, found, "package should contain ExitEvent")

	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for exit notification")
	}

	// ASSERT: Virtual node should have cleaned up monitors after completion
	watchers = peerNode.GetWatchers(workflowPID)
	assert.Len(t, watchers, 0, "peer node should cleanup monitors after completion")
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
	}.Precomputed()

	workflowPID := pid.PID{
		Node:   "temporal-prod",
		Host:   "my-task-queue",
		UniqID: "workflow-456",
	}.Precomputed()

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
	// Setup
	localNode := relaysys.NewNode("local")
	router := relaysys.NewRouter(localNode, nil)
	topo := NewTopology(router, "local")

	// Register hosts for all processes
	err := localNode.RegisterHost("host1", &dummyHost{})
	require.NoError(t, err)
	err = localNode.RegisterHost("host2", &dummyHost{})
	require.NoError(t, err)
	err = localNode.RegisterHost("host3", &dummyHost{})
	require.NoError(t, err)

	peerNode := NewMockPeerNode("temporal-prod", router, t)
	err = router.RegisterPeer("temporal-prod", peerNode)
	require.NoError(t, err)

	// Create multiple local processes
	process1PID := pid.PID{Node: "local", Host: "host1", UniqID: "p1"}.Precomputed()
	process2PID := pid.PID{Node: "local", Host: "host2", UniqID: "p2"}.Precomputed()
	process3PID := pid.PID{Node: "local", Host: "host3", UniqID: "p3"}.Precomputed()

	workflowPID := pid.PID{Node: "temporal-prod", Host: "queue", UniqID: "wf-789"}.Precomputed()

	err = topo.Register(process1PID)
	require.NoError(t, err)
	err = topo.Register(process2PID)
	require.NoError(t, err)
	err = topo.Register(process3PID)
	require.NoError(t, err)

	// Setup channels for all processes
	ch1 := make(chan *relay.Package, 10)
	cancel1, _ := localNode.Attach(process1PID, ch1)
	defer cancel1()

	ch2 := make(chan *relay.Package, 10)
	cancel2, _ := localNode.Attach(process2PID, ch2)
	defer cancel2()

	ch3 := make(chan *relay.Package, 10)
	cancel3, _ := localNode.Attach(process3PID, ch3)
	defer cancel3()

	// ACT: All three processes monitor the same workflow
	err = topo.Monitor(process1PID, workflowPID)
	require.NoError(t, err)
	err = topo.Monitor(process2PID, workflowPID)
	require.NoError(t, err)
	err = topo.Monitor(process3PID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	watchers := peerNode.GetWatchers(workflowPID)
	assert.Len(t, watchers, 3, "peer node should have 3 watchers")

	// ACT: Workflow completes
	err = peerNode.SimulateCompletion(workflowPID, "result", nil)
	require.NoError(t, err)

	// ASSERT: All three processes should receive exit notification
	var receivedCount int32
	done := make(chan bool, 3)

	checkExit := func(ch chan *relay.Package) {
		select {
		case pkg := <-ch:
			for _, msg := range pkg.Messages {
				for _, p := range msg.Payloads {
					if _, ok := p.Data().(*topology.ExitEvent); ok {
						atomic.AddInt32(&receivedCount, 1)
						done <- true
						return
					}
				}
			}
		case <-time.After(100 * time.Millisecond):
			done <- false
		}
	}

	go checkExit(ch1)
	go checkExit(ch2)
	go checkExit(ch3)

	for i := 0; i < 3; i++ {
		<-done
	}

	assert.Equal(t, int32(3), receivedCount, "all 3 processes should receive exit notification")
}

// TestIntegration_ReleaseMonitor tests releasing monitoring before completion.
func TestIntegration_ReleaseMonitor(t *testing.T) {
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

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "p1"}.Precomputed()
	workflowPID := pid.PID{Node: "temporal-prod", Host: "queue", UniqID: "wf-release"}.Precomputed()

	err = topo.Register(localPID)
	require.NoError(t, err)

	// ACT: Monitor then release
	err = topo.Monitor(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	assert.Len(t, peerNode.GetWatchers(workflowPID), 1)

	err = topo.Demonitor(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// ASSERT: Virtual node should have no watchers
	assert.Len(t, peerNode.GetWatchers(workflowPID), 0,
		"peer node should have no watchers after release")
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

	localPID := pid.PID{Node: "local", Host: "host1", UniqID: "p1"}.Precomputed()
	workflowPID := pid.PID{Node: "temporal-prod", Host: "queue", UniqID: "wf-unlink"}.Precomputed()

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
