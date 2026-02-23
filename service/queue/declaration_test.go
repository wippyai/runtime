// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	queuecfg "github.com/wippyai/runtime/api/service/queue/queue"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestDeclarationHandler_Add(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManagerForDecl{}
	dtt := &mockDTTForDecl{}

	handler := NewDeclarationHandler(bus, queueMgr, dtt, zap.NewNop())

	config := &queuecfg.Config{
		Driver: registry.NewID("test", "driver"),
		Options: attrs.Bag{
			queueapi.OptionMaxLength: 1000,
		},
	}

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

	config := &queuecfg.Config{
		Driver: registry.NewID("test", "driver"),
		Options: attrs.Bag{
			queueapi.OptionMaxLength: 1000,
		},
	}

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
	ctx := context.Background()
	bus := eventbus.NewBus()
	queueMgr := &mockQueueManagerForDecl{}
	dtt := &mockDTTForDecl{}

	handler := NewDeclarationHandler(bus, queueMgr, dtt, zap.NewNop())

	newConfig := &queuecfg.Config{
		Driver: registry.NewID("test", "new-driver"),
		Options: attrs.Bag{
			queueapi.OptionMaxLength: 2000,
		},
	}

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

func (m *mockDriverForDecl) Attach(_ context.Context, _ registry.ID, _ chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	return func() {}, nil
}

func (m *mockDriverForDecl) DeclareQueue(_ context.Context, _ registry.ID, _ attrs.Attributes) error {
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
