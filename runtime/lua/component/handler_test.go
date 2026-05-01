// SPDX-License-Identifier: MPL-2.0

package component

import (
	"context"
	"errors"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/system/eventbus"
)

func TestBuildImportsEmpty(t *testing.T) {
	imports := BuildImports(nil, nil)
	if len(imports) != 0 {
		t.Errorf("len(imports) = %d, want 0", len(imports))
	}
}

func TestBuildImportsWithImports(t *testing.T) {
	imports := map[string]registry.ID{
		"mylib": {NS: "app", Name: "libs.mylib"},
	}
	result := BuildImports(imports, nil)

	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Alias != "mylib" {
		t.Errorf("result[0].Alias = %q, want %q", result[0].Alias, "mylib")
	}
	if result[0].ID.Name != "libs.mylib" {
		t.Errorf("result[0].ID.Name = %q, want %q", result[0].ID.Name, "libs.mylib")
	}
}

func TestBuildImportsWithModules(t *testing.T) {
	modules := []string{"json", "base64"}
	result := BuildImports(nil, modules)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	found := make(map[string]bool)
	for _, imp := range result {
		found[imp.Alias] = true
		if imp.Alias != imp.ID.Name {
			t.Errorf("for module import, Alias should equal ID.Name: %q != %q", imp.Alias, imp.ID.Name)
		}
	}

	if !found["json"] {
		t.Error("expected json module in imports")
	}
	if !found["base64"] {
		t.Error("expected base64 module in imports")
	}
}

func TestBuildImportsMixed(t *testing.T) {
	imports := map[string]registry.ID{
		"mylib": {NS: "app", Name: "libs.mylib"},
	}
	modules := []string{"json"}
	result := BuildImports(imports, modules)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
}

func TestHandlerInvalidateRequestAcksOnlyMatchingKinds(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	bus := eventbus.NewBus()
	ctx = event.WithBus(ctx, bus)

	acks := make(chan event.Event, 2)
	subID, err := bus.SubscribeP(ctx, luaapi.System, luaapi.InvalidateNodesAccept, acks)
	if err != nil {
		t.Fatalf("SubscribeP failed: %v", err)
	}
	defer bus.Unsubscribe(ctx, subID)

	entity := &testEntityHandler{}
	handler := NewHandler("function.lua.**", entity)

	fnID := registry.NewID("app", "fn")
	libID := registry.NewID("app", "lib")
	err = handler.Handle(ctx, event.Event{
		System: luaapi.System,
		Kind:   luaapi.InvalidateNodes,
		Data: luaapi.InvalidateNodesRequest{
			AckPrefix: "ack/1",
			Nodes: []luaapi.InvalidateNode{
				{ID: fnID, Kind: luaapi.Function},
				{ID: libID, Kind: luaapi.Library},
			},
		},
	})
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if len(entity.invalidated) != 1 || !entity.invalidated[0].Equal(fnID) {
		t.Fatalf("invalidated = %#v, want only %s", entity.invalidated, fnID.String())
	}

	select {
	case ack := <-acks:
		if ack.Path != "ack/1/"+fnID.String() {
			t.Fatalf("ack path = %q, want function path", ack.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("expected function invalidation ack")
	}

	select {
	case ack := <-acks:
		t.Fatalf("unexpected extra ack: %#v", ack)
	default:
	}
}

func TestHandlerInvalidateRequestRejectsOnEntityError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	bus := eventbus.NewBus()
	ctx = event.WithBus(ctx, bus)

	rejects := make(chan event.Event, 1)
	subID, err := bus.SubscribeP(ctx, luaapi.System, luaapi.InvalidateNodesReject, rejects)
	if err != nil {
		t.Fatalf("SubscribeP failed: %v", err)
	}
	defer bus.Unsubscribe(ctx, subID)

	entityErr := errors.New("reload failed")
	entity := &testEntityHandler{invalidateErr: entityErr}
	handler := NewHandler("function.lua.**", entity)

	fnID := registry.NewID("app", "fn")
	err = handler.Handle(ctx, event.Event{
		System: luaapi.System,
		Kind:   luaapi.InvalidateNodes,
		Data: luaapi.InvalidateNodesRequest{
			AckPrefix: "ack/2",
			Nodes: []luaapi.InvalidateNode{
				{ID: fnID, Kind: luaapi.Function},
			},
		},
	})
	if !errors.Is(err, entityErr) {
		t.Fatalf("Handle error = %v, want %v", err, entityErr)
	}

	select {
	case reject := <-rejects:
		if reject.Path != "ack/2/"+fnID.String() {
			t.Fatalf("reject path = %q, want function path", reject.Path)
		}
		if !errors.Is(reject.Data.(error), entityErr) {
			t.Fatalf("reject data = %v, want %v", reject.Data, entityErr)
		}
	case <-time.After(time.Second):
		t.Fatal("expected function invalidation reject")
	}
}

type testEntityHandler struct {
	invalidateErr error
	invalidated   []registry.ID
}

func (h *testEntityHandler) Add(context.Context, registry.Entry) error    { return nil }
func (h *testEntityHandler) Update(context.Context, registry.Entry) error { return nil }
func (h *testEntityHandler) Delete(context.Context, registry.Entry) error { return nil }
func (h *testEntityHandler) Invalidate(_ context.Context, ids []registry.ID) error {
	h.invalidated = append(h.invalidated, ids...)
	return h.invalidateErr
}
