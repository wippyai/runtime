// SPDX-License-Identifier: MPL-2.0
package peer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	sysrelay "github.com/wippyai/runtime/system/relay"
	systopology "github.com/wippyai/runtime/system/topology"
	"go.temporal.io/sdk/client"
)

type renewalRun struct {
	client.WorkflowRun
	done  <-chan struct{}
	value string
}

func (r renewalRun) Get(ctx context.Context, out interface{}) error {
	select {
	case <-r.done:
		*(out.(*payload.Payloads)) = payload.Payloads{payload.New(r.value)}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type renewalClient struct {
	client.Client
	runs     chan client.WorkflowRun
	selected chan string
}

func (c *renewalClient) GetWorkflow(_ context.Context, _ string, runID string) client.WorkflowRun {
	c.selected <- runID
	return <-c.runs
}

func TestNativeTemporalMonitorReusesCompletedWorkflowID(t *testing.T) {
	node := sysrelay.NewNode("local")
	router := sysrelay.NewRouter(node, nil)
	topo := systopology.NewTopology(router, "local")
	stop, err := topo.StartRemoteMonitoring(context.Background(), node, systopology.MonitorConfig{MaxPending: 4, RequestTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(stop)
	sdk := &renewalClient{runs: make(chan client.WorkflowRun, 2), selected: make(chan string, 2)}
	receiver := NewReceiver(context.Background(), "temporal", sdk, router, nil)
	t.Cleanup(receiver.Stop)
	handoff := &testRunHandoff{}
	receiver.handoff = handoff
	release, err := router.RegisterOwnedPeer("temporal", receiver)
	require.NoError(t, err)
	t.Cleanup(release)
	inbox := make(nativeMonitorInbox, 2)
	require.NoError(t, node.RegisterHost("app", inbox))
	caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
	target := pid.PID{Node: "temporal", Host: "queue", UniqID: "workflow"}
	require.NoError(t, topo.Register(caller))
	for _, runID := range []string{"first-run", "second-run"} {
		finished := make(chan struct{})
		sdk.runs <- renewalRun{done: finished, value: runID}
		handoff.Publish("temporal", "workflow", runID)
		require.NoError(t, topo.Monitor(caller, target))
		select {
		case selected := <-sdk.selected:
			require.Equal(t, runID, selected)
		case <-time.After(time.Second):
			t.Fatal("workflow observation not started")
		}
		close(finished)
		select {
		case p := <-inbox:
			result := p.Messages[0].Payloads[0].Data().(map[string]any)["result"].(*runtime.Result)
			require.Equal(t, runID, result.Value.Data())
			relay.ReleasePackage(p)
		case <-time.After(time.Second):
			t.Fatal("workflow completion not delivered")
		}
		receiver.observers.Wait()
	}
}

func TestPendingOldRunResultSurvivesNewRunObservation(t *testing.T) {
	sdk := &renewalClient{runs: make(chan client.WorkflowRun, 2), selected: make(chan string, 2)}
	r := NewReceiver(context.Background(), "temporal", sdk, &mockRouter{}, nil)
	defer r.Stop()
	target := pid.PID{Node: "temporal", Host: "queue", UniqID: "workflow"}
	first := topology.NativeMonitor{Caller: pid.PID{Node: "local", Host: "app", UniqID: "one"}, Target: target, Reference: "first"}
	done := make(chan struct{})
	sdk.runs <- renewalRun{done: done, value: "old-result"}
	require.NoError(t, r.AdmitMonitor(context.Background(), first, func(context.Context, *runtime.Result) error { return errors.New("receiver unavailable") }))
	<-sdk.selected
	close(done)
	r.observers.Wait()
	second := topology.NativeMonitor{Caller: pid.PID{Node: "local", Host: "app", UniqID: "two"}, Target: target, Reference: "second"}
	nextDone := make(chan struct{})
	sdk.runs <- renewalRun{done: nextDone, value: "new-result"}
	delivered := make(chan string, 1)
	require.NoError(t, r.AdmitMonitor(context.Background(), second, func(_ context.Context, result *runtime.Result) error {
		delivered <- result.Value.Data().(string)
		return nil
	}))
	<-sdk.selected
	var oldResult string
	require.NoError(t, r.AdmitMonitor(context.Background(), first, func(_ context.Context, result *runtime.Result) error {
		oldResult = result.Value.Data().(string)
		return nil
	}))
	require.Equal(t, "old-result", oldResult)
	close(nextDone)
	r.observers.Wait()
	require.Equal(t, "new-result", <-delivered)
}

func TestActiveObservationDoesNotConsumeFutureRunHandoff(t *testing.T) {
	r := NewReceiver(context.Background(), "temporal", nil, &mockRouter{}, nil)
	defer r.Stop()
	handoff := &testRunHandoff{}
	r.handoff = handoff
	handoff.Publish("temporal", "workflow", "next-run")
	watcher := &workflowWatcher{workflowID: "workflow", watching: true}
	r.assignRunIDIfAvailable(watcher)
	require.Empty(t, watcher.runID)
	watcher.watching = false
	r.assignRunIDIfAvailable(watcher)
	require.Equal(t, "next-run", watcher.runID)
}
