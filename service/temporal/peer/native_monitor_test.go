// SPDX-License-Identifier: MPL-2.0
package peer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	sysrelay "github.com/wippyai/runtime/system/relay"
	systopology "github.com/wippyai/runtime/system/topology"
	"go.temporal.io/sdk/client"
)

type nativeMonitorRun struct {
	client.WorkflowRun
	finish  <-chan struct{}
	started chan<- struct{}
}

func (r nativeMonitorRun) Get(ctx context.Context, _ interface{}) error {
	r.started <- struct{}{}
	select {
	case <-r.finish:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type nativeMonitorClient struct {
	client.Client
	finish  chan struct{}
	started chan struct{}
}

func (c *nativeMonitorClient) GetWorkflow(context.Context, string, string) client.WorkflowRun {
	return nativeMonitorRun{finish: c.finish, started: c.started}
}

type nativeMonitorInbox chan *relay.Package

func (in nativeMonitorInbox) BindLocal(target pid.PID) (relay.ContextSender, error) {
	return boundNativeInbox{in: in, target: target}, nil
}

type boundNativeInbox struct {
	in     nativeMonitorInbox
	target pid.PID
}

func (b boundNativeInbox) SendContext(ctx context.Context, p *relay.Package) error {
	if !p.Target.Equal(b.target) {
		return relay.ErrBindingTarget
	}
	return b.in.SendContext(ctx, p)
}

func (in nativeMonitorInbox) Send(p *relay.Package) error {
	return in.SendContext(context.Background(), p)
}
func (in nativeMonitorInbox) SendContext(ctx context.Context, p *relay.Package) error {
	select {
	case in <- p:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestTopologyMonitorsTemporalThroughNativeProvider(t *testing.T) {
	for _, releaseFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "release-and-reobserve"}[releaseFirst], func(t *testing.T) {
			node := sysrelay.NewNode("local")
			router := sysrelay.NewRouter(node, nil)
			topo := systopology.NewTopology(router, "local")
			stop, err := topo.StartRemoteMonitoring(context.Background(), node, systopology.MonitorConfig{MaxPending: 4, RequestTimeout: time.Second})
			require.NoError(t, err)
			t.Cleanup(stop)
			sdk := &nativeMonitorClient{finish: make(chan struct{}), started: make(chan struct{}, 4)}
			receiver := NewReceiver(context.Background(), "temporal", sdk, router, nil, WithMonitorConfig(temporalapi.MonitorConfig{MaxTargets: 2, MaxObservers: 2, DeliveryTimeout: time.Second}))
			t.Cleanup(receiver.Stop)
			release, err := router.RegisterOwnedPeer("temporal", receiver)
			require.NoError(t, err)
			t.Cleanup(release)
			inbox := make(nativeMonitorInbox, 2)
			require.NoError(t, node.RegisterHost("app", inbox))
			caller := pid.PID{Node: "local", Host: "app", UniqID: "caller"}
			target := pid.PID{Node: "temporal", Host: "queue", UniqID: "workflow"}
			require.NoError(t, topo.Register(caller))
			require.NoError(t, topo.Monitor(caller, target))
			select {
			case <-sdk.started:
			case <-time.After(time.Second):
				t.Fatal("native provider did not start workflow observation")
			}
			if releaseFirst {
				require.NoError(t, topo.Demonitor(caller, target))
				require.NoError(t, topo.Monitor(caller, target))
				select {
				case <-sdk.started:
				case <-time.After(time.Second):
					t.Fatal("released reference could not be replaced")
				}
			}
			close(sdk.finish)
			select {
			case p := <-inbox:
				require.Empty(t, p.ReceivedFrom, "local provider must not fake TLS ingress")
				require.True(t, p.Source.Equal(target))
				fields := p.Messages[0].Payloads[0].Data().(map[string]any)
				require.Equal(t, "pid.exit", fields["kind"])
				require.NotContains(t, fields, "ref")
				relay.ReleasePackage(p)
			case <-time.After(time.Second):
				t.Fatal("native Temporal completion not delivered")
			}
			receiver.Stop()
			select {
			case p := <-inbox:
				relay.ReleasePackage(p)
				t.Fatal("duplicate completion")
			default:
			}
		})
	}
}

func TestOldObservationAttemptCannotCancelRestartedWatcher(t *testing.T) {
	r := NewReceiver(context.Background(), "temporal", observationClient{}, &mockRouter{}, nil)
	defer r.Stop()
	old, next := new(workflowObservation), new(workflowObservation)
	watcher := &workflowWatcher{workflowID: "workflow", watching: true, observation: next}
	r.watchers[watcher.workflowID] = watcher
	r.suspendWorkflowObservationAttempt(watcher, old, context.Canceled)
	require.True(t, watcher.watching)
	require.Same(t, next, watcher.observation)
}
