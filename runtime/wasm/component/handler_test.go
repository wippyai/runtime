// SPDX-License-Identifier: MPL-2.0

package component

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

type handlerTestEntity struct {
	invalidate []registry.ID
	addCount   int
}

func (h *handlerTestEntity) Add(context.Context, registry.Entry) error {
	h.addCount++
	return nil
}

func (h *handlerTestEntity) Update(context.Context, registry.Entry) error { return nil }
func (h *handlerTestEntity) Delete(context.Context, registry.Entry) error { return nil }
func (h *handlerTestEntity) Invalidate(_ context.Context, ids []registry.ID) {
	h.invalidate = append(h.invalidate, ids...)
}

type handlerTestBus struct {
	events []event.Event
}

func (b *handlerTestBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *handlerTestBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (b *handlerTestBus) Unsubscribe(context.Context, event.SubscriberID) {}
func (b *handlerTestBus) Send(_ context.Context, evt event.Event) {
	b.events = append(b.events, evt)
}

func TestHandlerPattern(t *testing.T) {
	h := NewHandler("function.wasm", &handlerTestEntity{})
	pattern := h.Pattern()
	if pattern.System != "(registry|wasm)" {
		t.Fatalf("Pattern().System = %q, want %q", pattern.System, "(registry|wasm)")
	}
	if pattern.Kind != "(entry|wasm).(create|update|delete|reset_code)" {
		t.Fatalf("Pattern().Kind = %q", pattern.Kind)
	}
}

func TestHandlerHandleInvalidate(t *testing.T) {
	entity := &handlerTestEntity{}
	h := NewHandler("function.wasm", entity)

	id := registry.NewID("app.test", "wasm")
	if err := h.Handle(context.Background(), event.Event{
		System: wasmapi.System,
		Kind:   wasmapi.InvalidateNodes,
		Data:   []registry.ID{id},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if len(entity.invalidate) != 1 || entity.invalidate[0] != id {
		t.Fatalf("Invalidate ids = %#v, want [%v]", entity.invalidate, id)
	}
}

func TestHandlerHandleRegistryDelegates(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	bus := &handlerTestBus{}
	ctx = event.WithBus(ctx, bus)

	entity := &handlerTestEntity{}
	h := NewHandler("function.wasm", entity)

	entry := registry.Entry{
		ID:   registry.NewID("app.test", "wasm"),
		Kind: wasmapi.FunctionWASM,
		Data: payload.New("ok"),
	}
	err := h.Handle(ctx, event.Event{
		System: registry.System,
		Kind:   registry.EntryCreate,
		Path:   entry.ID.String(),
		Data:   entry,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if entity.addCount != 1 {
		t.Fatalf("Add() calls = %d, want 1", entity.addCount)
	}
	if len(bus.events) != 1 {
		t.Fatalf("bus events len = %d, want 1", len(bus.events))
	}
	if bus.events[0].Kind != registry.EntryAccept {
		t.Fatalf("bus event kind = %q, want %q", bus.events[0].Kind, registry.EntryAccept)
	}
}

var _ event.Bus = (*handlerTestBus)(nil)
