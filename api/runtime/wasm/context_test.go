// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestSetTransportRegistry_GetTransportRegistry(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	r1 := &mockTransportRegistry{
		data: map[string]any{"payload": "ok"},
	}
	r2 := &mockTransportRegistry{
		data: map[string]any{"payload": "override"},
	}

	ctx = SetTransportRegistry(ctx, r1)
	ctx = SetTransportRegistry(ctx, r2) // set-once semantics

	got := GetTransportRegistry(ctx)
	assert.NotNil(t, got)
	v, ok := got.Get("payload")
	assert.True(t, ok)
	assert.Equal(t, "ok", v)
}

func TestGetTransportRegistry_NoAppContext(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, GetTransportRegistry(ctx))
}

func TestSetTransportRegistry_NoAppContext(t *testing.T) {
	ctx := context.Background()
	r := &mockTransportRegistry{}
	result := SetTransportRegistry(ctx, r)
	assert.Equal(t, ctx, result)
}

func TestSetAsyncState_GetAsyncState(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()

	st := &AsyncState{
		Tag:     "abc",
		Waiting: true,
		Payload: "payload",
	}

	err := SetAsyncState(ctx, st)
	assert.NoError(t, err)

	got := GetAsyncState(ctx)
	assert.NotNil(t, got)
	assert.Equal(t, "abc", got.Tag)
	assert.True(t, got.Waiting)
	assert.Equal(t, "payload", got.Payload)
}

func TestSetAsyncState_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	err := SetAsyncState(ctx, &AsyncState{Tag: "x"})
	assert.Equal(t, ctxapi.ErrNoFrameContext, err)
}

func TestGetAsyncState_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, GetAsyncState(ctx))
}

func TestGetAsyncState_NotSet(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()

	assert.Nil(t, GetAsyncState(ctx))
}

type mockTransportRegistry struct {
	data map[string]any
}

func (m *mockTransportRegistry) Get(name string) (any, bool) {
	v, ok := m.data[name]
	return v, ok
}
