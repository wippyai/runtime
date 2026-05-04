// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	amqpapi "github.com/wippyai/runtime/api/service/queue/amqp"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type urlTranscoder struct{ url string }

func (t *urlTranscoder) Transcode(p payload.Payload, f payload.Format) (payload.Payload, error) {
	return payload.NewPayload(p.Data(), f), nil
}

func (t *urlTranscoder) Unmarshal(_ payload.Payload, v any) error {
	cfg, ok := v.(*amqpapi.Config)
	if ok {
		cfg.URL = t.url
	}
	return nil
}

// Manager.Update must actually rebuild the running driver when the registry
// entry changes. Today it decodes the new config but keeps pushing events
// keyed to the *old* driver instance, so a URL or TLS change never takes
// effect until the process restarts. The rebuild contract: Remove the old
// driver's service, register a replacement driver, then emit DriverRegister
// so consumers pick up the new instance.
func TestManagerUpdate_RebuildsDriverAndEmitsLifecycleEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
	defer cancel()

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { _ = awaitSvc.Stop() }()
	ctx = event.WithAwaitService(ctx, awaitSvc)

	ackSub, err := eventbus.NewSubscriber(ctx, bus, queueapi.System,
		"queue.driver.(register|delete)", func(e event.Event) {
			bus.Send(ctx, event.Event{
				System: queueapi.System,
				Kind:   "queue.accept",
				Path:   e.Path,
			})
		})
	require.NoError(t, err)
	defer ackSub.Close()

	tc := &urlTranscoder{url: "amqp://localhost:5672"}
	mgr := NewManager(bus, tc, zap.NewNop())

	supEvents := make(chan event.Event, 16)
	_, err = bus.Subscribe(ctx, supervisor.System, supEvents)
	require.NoError(t, err)

	drvEvents := make(chan event.Event, 16)
	_, err = bus.Subscribe(ctx, queueapi.System, drvEvents)
	require.NoError(t, err)

	entry := registry.Entry{
		ID:   registry.NewID("test", "amqp"),
		Kind: amqpapi.Kind,
		Data: payload.New(map[string]any{}),
	}
	require.NoError(t, mgr.Add(ctx, entry))

	originalDriver, ok := mgr.drivers[entry.ID]
	require.True(t, ok, "Add must populate the drivers map")

	drainChan(supEvents)
	drainChan(drvEvents)

	tc.url = "amqp://other-host:5672"
	require.NoError(t, mgr.Update(ctx, entry))

	newDriver, ok := mgr.drivers[entry.ID]
	require.True(t, ok, "Update must leave the drivers map populated")
	assert.NotSame(t, originalDriver, newDriver,
		"Update must replace the driver instance so new config takes effect")
	assert.Equal(t, "amqp://other-host:5672", newDriver.cfg.URL,
		"replacement driver must carry the updated config")

	supKinds := collectEventKinds(supEvents, 2)
	assert.Contains(t, supKinds, supervisor.ServiceRemove,
		"old driver must be torn down so the supervisor drops it")
	assert.Contains(t, supKinds, supervisor.ServiceRegister,
		"the replacement driver must be registered with the supervisor")

	drvKinds := collectEventKinds(drvEvents, 4)
	assert.Contains(t, drvKinds, queueapi.DriverDelete,
		"queue listeners must observe the old driver's deletion")
	assert.Contains(t, drvKinds, queueapi.DriverRegister,
		"queue listeners must observe the replacement driver")
}

func drainChan(ch <-chan event.Event) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func collectEventKinds(ch <-chan event.Event, n int) []event.Kind {
	kinds := make([]event.Kind, 0, n)
	deadline := time.After(500 * time.Millisecond)
	for len(kinds) < n {
		select {
		case ev := <-ch:
			kinds = append(kinds, ev.Kind)
		case <-deadline:
			return kinds
		}
	}
	return kinds
}
