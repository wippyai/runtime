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
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	relaysys "github.com/wippyai/runtime/system/relay"
)

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

func (d *dummyHost) Attach(pid relay.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	d.receivers.Store(pid.String(), ch)
	return func() {
		d.receivers.Delete(pid.String())
	}, nil
}

func (d *dummyHost) Detach(pid relay.PID) {
	d.receivers.Delete(pid.String())
}

// TestIntegration_CrossNodeMonitoring_EndToEnd tests the complete flow of monitoring
// a workflow on a virtual node from start to completion.
func TestIntegration_CrossNodeMonitoring_EndToEnd(t *testing.T) {
	// Setup: Create local node with router and topology
	localNode := relaysys.NewNode("local")
	router := relaysys.NewRouter(localNode, nil)
	topo := NewTopology(localNode, router, "local")

	// Register dummy host for the local node
	err := localNode.RegisterHost("myhost", &dummyHost{})
	require.NoError(t, err)

	// Setup: Create mock virtual node (simulating Temporal)
	virtualNode := NewMockVirtualNode("temporal-prod", router, t)
	err = router.RegisterVirtualNode("temporal-prod", virtualNode)
	require.NoError(t, err)

	// Setup PIDs
	localProcessPID := relay.PID{
		Node:   "local",
		Host:   "myhost",
		UniqID: "process-1",
	}.Precomputed()

	workflowPID := relay.PID{
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
	err = topo.Wait(localProcessPID, workflowPID)
	require.NoError(t, err)

	// Give time for message propagation
	time.Sleep(10 * time.Millisecond)

	// ASSERT: Virtual node should have received the monitor request
	watchers := virtualNode.GetWatchers(workflowPID)
	require.Len(t, watchers, 1, "virtual node should have 1 watcher")
	assert.Equal(t, localProcessPID, watchers[0])

	// ACT: Simulate workflow completion
	workflowResult := "workflow completed successfully"
	err = virtualNode.SimulateCompletion(workflowPID, workflowResult, nil)
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
					assert.Equal(t, topology.KindExit, exitEvt.Kind)
					assert.Nil(t, exitEvt.Result.Error)
				}
			}
		}
		assert.True(t, found, "package should contain ExitEvent")

	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for exit notification")
	}

	// ASSERT: Virtual node should have cleaned up monitors after completion
	watchers = virtualNode.GetWatchers(workflowPID)
	assert.Len(t, watchers, 0, "virtual node should cleanup monitors after completion")
}

// TestIntegration_CrossNodeLinking_EndToEnd tests the complete flow of linking
// with a workflow on a virtual node and receiving link-down on failure.
func TestIntegration_CrossNodeLinking_EndToEnd(t *testing.T) {
	// Setup
	localNode := relaysys.NewNode("local")
	router := relaysys.NewRouter(localNode, nil)
	topo := NewTopology(localNode, router, "local")

	// Register dummy host for the local node
	err := localNode.RegisterHost("myhost", &dummyHost{})
	require.NoError(t, err)

	virtualNode := NewMockVirtualNode("temporal-prod", router, t)
	err = router.RegisterVirtualNode("temporal-prod", virtualNode)
	require.NoError(t, err)

	localProcessPID := relay.PID{
		Node:   "local",
		Host:   "myhost",
		UniqID: "process-1",
	}.Precomputed()

	workflowPID := relay.PID{
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

	virtualLinks := virtualNode.GetLinkedProcesses(workflowPID)
	require.Len(t, virtualLinks, 1, "virtual side should have link")
	assert.Equal(t, localProcessPID, virtualLinks[0])

	// ACT: Simulate workflow failure
	workflowErr := fmt.Errorf("workflow failed")
	err = virtualNode.SimulateFailure(workflowPID, workflowErr)
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
					assert.Equal(t, topology.KindLinkDown, exitEvt.Kind)
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
	topo := NewTopology(localNode, router, "local")

	// Register hosts for all processes
	err := localNode.RegisterHost("host1", &dummyHost{})
	require.NoError(t, err)
	err = localNode.RegisterHost("host2", &dummyHost{})
	require.NoError(t, err)
	err = localNode.RegisterHost("host3", &dummyHost{})
	require.NoError(t, err)

	virtualNode := NewMockVirtualNode("temporal-prod", router, t)
	err = router.RegisterVirtualNode("temporal-prod", virtualNode)
	require.NoError(t, err)

	// Create multiple local processes
	process1PID := relay.PID{Node: "local", Host: "host1", UniqID: "p1"}.Precomputed()
	process2PID := relay.PID{Node: "local", Host: "host2", UniqID: "p2"}.Precomputed()
	process3PID := relay.PID{Node: "local", Host: "host3", UniqID: "p3"}.Precomputed()

	workflowPID := relay.PID{Node: "temporal-prod", Host: "queue", UniqID: "wf-789"}.Precomputed()

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
	err = topo.Wait(process1PID, workflowPID)
	require.NoError(t, err)
	err = topo.Wait(process2PID, workflowPID)
	require.NoError(t, err)
	err = topo.Wait(process3PID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	watchers := virtualNode.GetWatchers(workflowPID)
	assert.Len(t, watchers, 3, "virtual node should have 3 watchers")

	// ACT: Workflow completes
	err = virtualNode.SimulateCompletion(workflowPID, "result", nil)
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
	topo := NewTopology(localNode, router, "local")

	// Register host
	err := localNode.RegisterHost("host1", &dummyHost{})
	require.NoError(t, err)

	virtualNode := NewMockVirtualNode("temporal-prod", router, t)
	err = router.RegisterVirtualNode("temporal-prod", virtualNode)
	require.NoError(t, err)

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "p1"}.Precomputed()
	workflowPID := relay.PID{Node: "temporal-prod", Host: "queue", UniqID: "wf-release"}.Precomputed()

	err = topo.Register(localPID)
	require.NoError(t, err)

	// ACT: Monitor then release
	err = topo.Wait(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	assert.Len(t, virtualNode.GetWatchers(workflowPID), 1)

	err = topo.Release(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// ASSERT: Virtual node should have no watchers
	assert.Len(t, virtualNode.GetWatchers(workflowPID), 0,
		"virtual node should have no watchers after release")
}

// TestIntegration_UnlinkBeforeFailure tests unlinking before workflow failure.
func TestIntegration_UnlinkBeforeFailure(t *testing.T) {
	// Setup
	localNode := relaysys.NewNode("local")
	router := relaysys.NewRouter(localNode, nil)
	topo := NewTopology(localNode, router, "local")

	// Register host
	err := localNode.RegisterHost("host1", &dummyHost{})
	require.NoError(t, err)

	virtualNode := NewMockVirtualNode("temporal-prod", router, t)
	err = router.RegisterVirtualNode("temporal-prod", virtualNode)
	require.NoError(t, err)

	localPID := relay.PID{Node: "local", Host: "host1", UniqID: "p1"}.Precomputed()
	workflowPID := relay.PID{Node: "temporal-prod", Host: "queue", UniqID: "wf-unlink"}.Precomputed()

	err = topo.Register(localPID)
	require.NoError(t, err)

	linkDownCh := make(chan *relay.Package, 10)
	cancel, _ := localNode.Attach(localPID, linkDownCh)
	defer cancel()

	// ACT: Link then unlink
	err = topo.Link(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	assert.Len(t, virtualNode.GetLinkedProcesses(workflowPID), 1)

	err = topo.Unlink(localPID, workflowPID)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	// ASSERT: Virtual node should have no links
	assert.Len(t, virtualNode.GetLinkedProcesses(workflowPID), 0)

	// ACT: Simulate failure after unlink
	err = virtualNode.SimulateFailure(workflowPID, fmt.Errorf("failed"))
	require.NoError(t, err)

	// ASSERT: No link-down event should be received
	select {
	case <-linkDownCh:
		t.Fatal("should not receive link-down after unlinking")
	case <-time.After(50 * time.Millisecond):
		// Expected - no event received
	}
}
