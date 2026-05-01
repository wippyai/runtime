// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuecfg "github.com/wippyai/runtime/api/service/queue/queue"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// startDeclareAckStub subscribes to queueapi.Declare / queueapi.Delete events
// on the bus and echoes back queue.accept for the same path. The declaration
// handler waits for that acknowledgement before returning — mirroring the
// real queue Manager without pulling it into every unit test.
func startDeclareAckStub(t *testing.T, bus event.Bus) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sub, err := eventbus.NewSubscriber(ctx, bus, queueapi.System,
		"queue.queue.(declare|delete)", func(e event.Event) {
			bus.Send(ctx, event.Event{
				System: queueapi.System,
				Kind:   "queue.accept",
				Path:   e.Path,
			})
		})
	require.NoError(t, err)
	t.Cleanup(sub.Close)
}

func newDeclarationTestContext(t *testing.T, bus event.Bus) context.Context {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	t.Cleanup(func() { _ = awaitSvc.Stop() })
	return event.WithAwaitService(ctx, awaitSvc)
}

func newTestConfig(driverName string) *queuecfg.Config {
	bag := attrs.NewBag()
	amqpBag := attrs.NewBag()
	amqpBag.Set("max_length", 1000)
	bag.Set("amqp", amqpBag)
	return &queuecfg.Config{
		Driver:        registry.NewID("test", driverName),
		DriverOptions: bag,
	}
}

func TestDeclarationHandler_Add(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newDeclarationTestContext(t, bus)
	startDeclareAckStub(t, bus)
	queueMgr := &mockQueueManagerForDecl{}
	dtt := &mockDTTForDecl{}

	handler := NewDeclarationHandler(bus, queueMgr, dtt, zap.NewNop())

	config := newTestConfig("driver")

	entry := registry.Entry{
		ID:   registry.NewID("app", "tasks"),
		Kind: queuecfg.Kind,
		Data: payload.New(config),
	}

	err := handler.Add(ctx, entry)
	require.NoError(t, err)
}

func TestDeclarationHandler_Add_DriverNotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManagerForDecl{
		driverNotFound: true,
	}
	dtt := &mockDTTForDecl{}

	handler := NewDeclarationHandler(bus, queueMgr, dtt, zap.NewNop())

	config := newTestConfig("driver")

	entry := registry.Entry{
		ID:   registry.NewID("app", "tasks"),
		Kind: queuecfg.Kind,
		Data: payload.New(config),
	}

	err := handler.Add(ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "driver not found")
}

func TestDeclarationHandler_Delete(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManagerForDecl{}
	dtt := &mockDTTForDecl{}

	handler := NewDeclarationHandler(bus, queueMgr, dtt, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("app", "tasks"),
		Kind: queuecfg.Kind,
	}

	err := handler.Delete(ctx, entry)
	require.NoError(t, err)
}

func TestDeclarationHandler_Update(t *testing.T) {
	bus := eventbus.NewBus()
	ctx := newDeclarationTestContext(t, bus)
	startDeclareAckStub(t, bus)
	queueMgr := &mockQueueManagerForDecl{}
	dtt := &mockDTTForDecl{}

	handler := NewDeclarationHandler(bus, queueMgr, dtt, zap.NewNop())

	newConfig := newTestConfig("new-driver")

	entry := registry.Entry{
		ID:   registry.NewID("app", "tasks"),
		Kind: queuecfg.Kind,
		Data: payload.New(newConfig),
	}

	err := handler.Update(ctx, entry)
	require.NoError(t, err)
}

type mockQueueManagerForDecl struct {
	driverNotFound bool
}

func (m *mockQueueManagerForDecl) Publish(_ context.Context, _ registry.ID, _ ...*queueapi.Message) error {
	return nil
}

func (m *mockQueueManagerForDecl) GetDriver(_ registry.ID) (queueapi.Driver, bool) {
	if m.driverNotFound {
		return nil, false
	}
	return &mockDriverForDecl{}, true
}

func (m *mockQueueManagerForDecl) GetQueue(_ registry.ID) (*queueapi.Queue, bool) {
	return nil, false
}

func (m *mockQueueManagerForDecl) RegisterInterceptor(_ string, _ queueapi.PublishInterceptor, _ int) {
}

func (m *mockQueueManagerForDecl) UnregisterInterceptor(_ string) {}

type mockDriverForDecl struct{}

func (m *mockDriverForDecl) Publish(_ context.Context, _ registry.ID, _ ...*queueapi.Message) error {
	return nil
}

func (m *mockDriverForDecl) Attach(_ context.Context, _ registry.ID, _ *queueapi.ConsumerOptions, _ chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	return func() {}, nil
}

func (m *mockDriverForDecl) DeclareQueue(_ context.Context, _ registry.ID, _ *queueapi.Config) error {
	return nil
}

func (m *mockDriverForDecl) GetQueueInfo(_ context.Context, _ registry.ID) (attrs.Attributes, error) {
	return attrs.NewBag(), nil
}

type mockDTTForDecl struct{}

func (m *mockDTTForDecl) Unmarshal(p payload.Payload, v any) error {
	if config, ok := v.(*queuecfg.Config); ok {
		if src, ok := p.Data().(*queuecfg.Config); ok {
			*config = *src
			return nil
		}
	}
	return nil
}

func (m *mockDTTForDecl) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}
