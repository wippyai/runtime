package interceptor

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func TestRegistry_RegisterUnregister(t *testing.T) {
	reg := NewInterceptorRegistry(zap.NewNop())

	interceptor := &testInterceptor{name: "test"}
	reg.Register("test", interceptor, 100)

	assert.Len(t, reg.entries, 1)
	assert.Equal(t, "test", reg.entries[0].name)
	assert.Equal(t, 100, reg.entries[0].priority)

	reg.Unregister("test")
	assert.Len(t, reg.entries, 0)
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg := NewInterceptorRegistry(zap.NewNop())

	interceptor1 := &testInterceptor{name: "test1"}
	interceptor2 := &testInterceptor{name: "test2"}

	reg.Register("test", interceptor1, 100)
	reg.Register("test", interceptor2, 200)

	assert.Len(t, reg.entries, 1, "duplicate registration should be ignored")
	assert.Equal(t, interceptor1, reg.entries[0].interceptor)
}

func TestRegistry_PriorityOrdering(t *testing.T) {
	reg := NewInterceptorRegistry(zap.NewNop())

	int1 := &testInterceptor{name: "high-priority"}
	int2 := &testInterceptor{name: "low-priority"}
	int3 := &testInterceptor{name: "mid-priority"}

	reg.Register("low", int2, 300)
	reg.Register("high", int1, 100)
	reg.Register("mid", int3, 200)

	require.Len(t, reg.entries, 3)
	assert.Equal(t, "high-priority", reg.entries[0].interceptor.(*testInterceptor).name)
	assert.Equal(t, "mid-priority", reg.entries[1].interceptor.(*testInterceptor).name)
	assert.Equal(t, "low-priority", reg.entries[2].interceptor.(*testInterceptor).name)
}

func TestRegistry_Publish_NoInterceptors(t *testing.T) {
	reg := NewInterceptorRegistry(zap.NewNop())

	publishCalled := false
	reg.SetPublishFunc(func(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
		publishCalled = true
		return nil
	})

	ctx := context.Background()
	queueID := registry.ID{NS: "test", Name: "queue"}
	msg := queueapi.NewMessage(payload.New("test"))

	err := reg.Publish(ctx, queueID, msg)
	require.NoError(t, err)
	assert.True(t, publishCalled, "publish function should be called directly when no interceptors")
}

func TestRegistry_Publish_WithInterceptors(t *testing.T) {
	reg := NewInterceptorRegistry(zap.NewNop())

	int1 := &testInterceptor{name: "int1"}
	int2 := &testInterceptor{name: "int2"}
	int3 := &testInterceptor{name: "int3"}

	reg.Register("int1", int1, 100)
	reg.Register("int2", int2, 200)
	reg.Register("int3", int3, 300)

	publishCalled := false
	reg.SetPublishFunc(func(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
		publishCalled = true
		return nil
	})

	ctx := context.Background()
	queueID := registry.ID{NS: "test", Name: "queue"}
	msg := queueapi.NewMessage(payload.New("test"))

	err := reg.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	assert.True(t, int1.called.Load(), "interceptor 1 should be called")
	assert.True(t, int2.called.Load(), "interceptor 2 should be called")
	assert.True(t, int3.called.Load(), "interceptor 3 should be called")
	assert.True(t, publishCalled, "final publish function should be called")
}

func TestRegistry_Publish_InterceptorOrder(t *testing.T) {
	reg := NewInterceptorRegistry(zap.NewNop())

	var callOrder []string

	int1 := &orderInterceptor{name: "high", callOrder: &callOrder}
	int2 := &orderInterceptor{name: "mid", callOrder: &callOrder}
	int3 := &orderInterceptor{name: "low", callOrder: &callOrder}

	reg.Register("low", int3, 300)
	reg.Register("high", int1, 100)
	reg.Register("mid", int2, 200)

	reg.SetPublishFunc(func(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
		callOrder = append(callOrder, "publish")
		return nil
	})

	ctx := context.Background()
	queueID := registry.ID{NS: "test", Name: "queue"}
	msg := queueapi.NewMessage(payload.New("test"))

	err := reg.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	assert.Equal(t, []string{"high", "mid", "low", "publish"}, callOrder)
}

func TestRegistry_Unregister_Rebuilds(t *testing.T) {
	reg := NewInterceptorRegistry(zap.NewNop())

	int1 := &testInterceptor{name: "int1"}
	int2 := &testInterceptor{name: "int2"}

	reg.Register("int1", int1, 100)
	reg.Register("int2", int2, 200)

	reg.SetPublishFunc(func(ctx context.Context, q registry.ID, msgs ...*queueapi.Message) error {
		return nil
	})

	reg.Unregister("int1")

	ctx := context.Background()
	queueID := registry.ID{NS: "test", Name: "queue"}
	msg := queueapi.NewMessage(payload.New("test"))

	err := reg.Publish(ctx, queueID, msg)
	require.NoError(t, err)

	assert.False(t, int1.called.Load(), "unregistered interceptor should not be called")
	assert.True(t, int2.called.Load(), "remaining interceptor should be called")
}

type testInterceptor struct {
	name   string
	called atomic.Bool
}

func (i *testInterceptor) Handle(ctx context.Context, queue registry.ID, msgs []*queueapi.Message, next func(context.Context, registry.ID, []*queueapi.Message) error) error {
	i.called.Store(true)
	return next(ctx, queue, msgs)
}

type orderInterceptor struct {
	name      string
	callOrder *[]string
}

func (i *orderInterceptor) Handle(ctx context.Context, queue registry.ID, msgs []*queueapi.Message, next func(context.Context, registry.ID, []*queueapi.Message) error) error {
	*i.callOrder = append(*i.callOrder, i.name)
	return next(ctx, queue, msgs)
}
