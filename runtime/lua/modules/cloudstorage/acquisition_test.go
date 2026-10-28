// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
)

type deniedAcquisitionRegistry struct {
	resource.Registry
	t *testing.T
}

func (r *deniedAcquisitionRegistry) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	r.t.Fatal("denied acquisition reaches the resource registry")
	return nil, nil
}

func TestGetPermissionDenied(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := resource.WithRegistry(ctxapi.NewRootContext(), &deniedAcquisitionRegistry{t: t})
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	if err := secapi.SetActor(ctx, secapi.Actor{ID: "test-actor"}); err != nil {
		t.Fatal(err)
	}
	if err := secapi.SetScope(ctx, secsystem.NewScope(nil)); err != nil {
		t.Fatal(err)
	}
	l.SetContext(ctx)

	err := l.CallByParam(lua.P{Fn: l.NewFunction(apiGet), NRet: 2, Protect: true}, lua.LString("test:denied"))
	if err != nil {
		t.Fatalf("denied acquisition raises instead of returning an error: %v", err)
	}
	if l.Get(-2) != lua.LNil {
		t.Fatalf("expected nil resource, got %v", l.Get(-2))
	}
	got, ok := l.Get(-1).(*lua.Error)
	if !ok {
		t.Fatalf("expected structured error, got %T", l.Get(-1))
	}
	if got.Kind() != lua.PermissionDenied {
		t.Errorf("kind = %s, want PermissionDenied", got.Kind())
	}
	if got.Retryable() != lua.TernaryFalse {
		t.Errorf("retryable = %s, want false", got.Retryable())
	}
	if got.Message != "not allowed to access cloud storage resource: test:denied" {
		t.Errorf("message = %q", got.Message)
	}
}
