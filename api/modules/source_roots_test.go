// SPDX-License-Identifier: MPL-2.0

package modules

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestSourceRoots(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	WithSourceRoots(ctx, SourceRoots{
		"acme/ui":    "/repo/ui",
		"":           "/ignored",
		"empty/root": "",
	})

	root, ok := SourceRoot(ctx, "acme/ui")
	if !ok || root != "/repo/ui" {
		t.Fatalf("SourceRoot = %q, %v; want /repo/ui, true", root, ok)
	}

	if _, ok := SourceRoot(ctx, "empty/root"); ok {
		t.Fatal("empty roots must not be registered")
	}

	WithSourceRoots(ctx, SourceRoots{
		"acme/ui":     "/repo/ui-v2",
		"acme/plugin": "/repo/plugin",
	})

	root, ok = SourceRoot(ctx, "acme/ui")
	if !ok || root != "/repo/ui-v2" {
		t.Fatalf("merged SourceRoot = %q, %v; want /repo/ui-v2, true", root, ok)
	}

	root, ok = SourceRoot(ctx, "acme/plugin")
	if !ok || root != "/repo/plugin" {
		t.Fatalf("new SourceRoot = %q, %v; want /repo/plugin, true", root, ok)
	}
}

func TestSourceRootsWithoutAppContext(t *testing.T) {
	ctx := context.Background()

	WithSourceRoots(ctx, SourceRoots{"acme/ui": "/repo/ui"})
	if _, ok := SourceRoot(ctx, "acme/ui"); ok {
		t.Fatal("source root should not be available without AppContext")
	}
}

func TestSourceRootsCanUpdateAfterAppContextSealWhenRegistryExists(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = WithSourceRootRegistry(ctx)

	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		t.Fatal("expected AppContext")
	}
	ac.Seal()

	WithSourceRoots(ctx, SourceRoots{"acme/ui": "/repo/ui"})

	root, ok := SourceRoot(ctx, "acme/ui")
	if !ok || root != "/repo/ui" {
		t.Fatalf("SourceRoot after seal = %q, %v; want /repo/ui, true", root, ok)
	}
}

func TestSourceRootsNoPanicWhenSealedWithoutRegistry(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		t.Fatal("expected AppContext")
	}
	ac.Seal()

	WithSourceRoots(ctx, SourceRoots{"acme/ui": "/repo/ui"})
	if _, ok := SourceRoot(ctx, "acme/ui"); ok {
		t.Fatal("source root should not be registered when sealed registry is absent")
	}
}
