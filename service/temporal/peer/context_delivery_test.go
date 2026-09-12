// SPDX-License-Identifier: MPL-2.0
package peer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"go.temporal.io/sdk/client"
)

type blockedRelayTemporalClient struct {
	client.Client
	entered  chan struct{}
	finished chan struct{}
}

func (c *blockedRelayTemporalClient) wait(ctx context.Context) error {
	close(c.entered)
	<-ctx.Done()
	close(c.finished)
	return ctx.Err()
}
func (c *blockedRelayTemporalClient) SignalWorkflow(ctx context.Context, _ string, _ string, _ string, _ interface{}) error {
	return c.wait(ctx)
}
func (c *blockedRelayTemporalClient) CancelWorkflow(ctx context.Context, _ string, _ string) error {
	return c.wait(ctx)
}

func TestRelayTemporalDeliveryCancellation(t *testing.T) {
	for _, operation := range []string{"signal", "cancel"} {
		for _, cause := range []string{"caller", "receiver"} {
			t.Run(operation+"/"+cause, func(t *testing.T) {
				sdk := &blockedRelayTemporalClient{entered: make(chan struct{}), finished: make(chan struct{})}
				router := sysrelay.NewRouter(sysrelay.NewNode("local"), nil)
				receiver := NewReceiver(context.Background(), "temporal", sdk, router, nil)
				defer receiver.Stop()
				require.NoError(t, router.RegisterPeer("temporal", receiver))
				source := pid.PID{Node: "local", Host: "process", UniqID: "caller"}
				target := pid.PID{Node: "temporal", Host: "queue", UniqID: "workflow"}
				p := relay.NewPackage(source, target, "signal", payload.New("data"))
				if operation == "cancel" {
					relay.ReleasePackage(p)
					p = relay.NewPackage(source, target, topology.TopicEvents, payload.New(&topology.CancelEvent{Kind: topology.Cancel, Reason: "run"}))
				}
				defer relay.ReleasePackage(p)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- router.SendContext(ctx, p) }()
				select {
				case <-sdk.entered:
				case <-time.After(time.Second):
					t.Fatal("Temporal call not reached through cancellable router")
				}
				if cause == "caller" {
					cancel()
				} else {
					receiver.Stop()
					select {
					case <-sdk.finished:
					default:
						t.Fatal("Stop did not join synchronous SDK call")
					}
				}
				select {
				case err := <-done:
					require.ErrorIs(t, err, context.Canceled)
				case <-time.After(time.Second):
					t.Fatal("cancellation did not stop Temporal call")
				}
				require.True(t, p.Target.Equal(target), "refusal must preserve package ownership")
			})
		}
	}
}

func TestTemporalUnsupportedMonitorRefusedBeforeSignal(t *testing.T) {
	receiver := NewReceiver(context.Background(), "temporal", nil, &mockRouter{}, nil)
	defer receiver.Stop()
	p := relay.NewPackage(pid.PID{Node: "local"}, pid.PID{Node: "temporal", UniqID: "workflow"}, topology.TopicEvents, payload.New(map[string]any{"kind": topology.MonitorRequest, "v": uint64(1)}))
	p.AddMessage("application", payload.New("must not be partially delivered"))
	defer relay.ReleasePackage(p)
	require.ErrorContains(t, receiver.SendContext(context.Background(), p), "acknowledged monitor")
	require.Len(t, p.Messages, 2)
	require.Empty(t, receiver.watchers)
}
