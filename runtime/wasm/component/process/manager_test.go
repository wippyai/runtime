package process

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"go.uber.org/zap"
)

type testBus struct {
	events []event.Event
	onSend func()
}

func (b *testBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *testBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *testBus) Unsubscribe(context.Context, event.SubscriberID) {}
func (b *testBus) Send(_ context.Context, evt event.Event) {
	if b.onSend != nil {
		b.onSend()
	}
	b.events = append(b.events, evt)
}

type testPrepareAwaitWaiter struct {
	result event.AwaitResult
}

func (w *testPrepareAwaitWaiter) Wait() event.AwaitResult { return w.result }
func (w *testPrepareAwaitWaiter) Close()                  {}

type testPrepareAwaitService struct {
	prepared bool
	result   event.AwaitResult
}

func (a *testPrepareAwaitService) Prepare(context.Context, event.System, event.Kind, event.Path, time.Duration) (event.AwaitWaiter, error) {
	a.prepared = true
	return &testPrepareAwaitWaiter{result: a.result}, nil
}

func (a *testPrepareAwaitService) Await(context.Context, event.System, event.Kind, event.Path, time.Duration) event.AwaitResult {
	return event.AwaitResult{Accepted: false, Error: fmt.Errorf("unexpected Await call")}
}
func (a *testPrepareAwaitService) Start(context.Context) error { return nil }
func (a *testPrepareAwaitService) Stop() error                 { return nil }

func TestManagerInvalidKind(t *testing.T) {
	m := NewManager(zap.NewNop(), nil, nil)
	ctx := context.Background()
	entry := registry.Entry{Kind: "invalid.kind"}

	err := m.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")

	err = m.Update(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")

	err = m.Delete(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid entry kind")
}

func TestDeleteSendsFactoryDelete(t *testing.T) {
	bus := &testBus{}
	m := NewManager(zap.NewNop(), bus, nil)
	entry := registry.Entry{
		ID:   registry.ParseID("app.test:proc"),
		Kind: api.ProcessWASM,
	}

	require.NoError(t, m.Delete(context.Background(), entry))
	require.Len(t, bus.events, 1)
	assert.Equal(t, processapi.System, bus.events[0].System)
	assert.Equal(t, processapi.FactoryDelete, bus.events[0].Kind)
	assert.Equal(t, entry.ID.String(), bus.events[0].Path)
}

func TestRegisterFactoryRequiresAwaitService(t *testing.T) {
	m := NewManager(zap.NewNop(), &testBus{}, nil)
	id := registry.ParseID("app.test:proc")
	cfg := &configEntry{method: "run"}

	err := m.registerFactory(ctxapi.NewRootContext(), id, cfg, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to register process factory")
}

func TestRegisterFactoryPreparesBeforeSend(t *testing.T) {
	bus := &testBus{}
	m := NewManager(zap.NewNop(), bus, nil)
	id := registry.ParseID("app.test:proc")
	cfg := &configEntry{method: "run"}

	awaitSvc := &testPrepareAwaitService{
		result: event.AwaitResult{Accepted: true},
	}
	sendBeforePrepare := false
	bus.onSend = func() {
		if !awaitSvc.prepared {
			sendBeforePrepare = true
		}
	}

	ctx := event.WithAwaitService(ctxapi.NewRootContext(), awaitSvc)
	err := m.registerFactory(ctx, id, cfg, nil)
	require.NoError(t, err)
	assert.False(t, sendBeforePrepare, "factory register was sent before await prepare")
}
