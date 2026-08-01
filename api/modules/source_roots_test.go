// SPDX-License-Identifier: MPL-2.0

package modules

import (
	"context"
	"slices"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	regapi "github.com/wippyai/runtime/api/registry"
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

func TestSourceModulesAreStableAndPathFree(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = WithSourceRoots(ctx, SourceRoots{
		"acme/zeta":  "/private/zeta",
		"acme/alpha": "/private/alpha",
		"":           "/ignored",
	})

	got := SourceModules(ctx)
	if !slices.Equal(got, []string{"acme/alpha", "acme/zeta"}) {
		t.Fatalf("SourceModules = %v", got)
	}
}

func TestSourceLoaderUsesRegisteredDeploymentCapability(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	WithSourceLoader(ctx, func(context.Context) ([]regapi.Entry, error) {
		return []regapi.Entry{{ID: regapi.NewID("example", "entry"), Kind: "registry.entry"}}, nil
	})

	entries, err := LoadSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID.String() != "example:entry" {
		t.Fatalf("LoadSources = %v", entries)
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

func TestSwapSourceRootsReplacesOnlyControlledSubset(t *testing.T) {
	ctx := WithSourceRootRegistry(ctxapi.NewRootContext())
	WithSourceRoots(ctx, SourceRoots{
		"acme/old":       "/repo/old",
		"acme/updated":   "/repo/updated-v1",
		"acme/unrelated": "/repo/unrelated",
	})

	previous := SwapSourceRoots(ctx, SourceRoots{
		"acme/updated":   "/repo/updated-v2",
		"acme/new":       "/repo/new",
		"acme/unrelated": "/must-not-apply",
	}, "acme/old", "acme/updated", "acme/new")

	if len(previous) != 2 || previous["acme/old"] != "/repo/old" || previous["acme/updated"] != "/repo/updated-v1" {
		t.Fatalf("previous roots = %#v", previous)
	}
	if _, ok := SourceRoot(ctx, "acme/old"); ok {
		t.Fatal("controlled root omitted from desired must be removed")
	}
	if root, ok := SourceRoot(ctx, "acme/updated"); !ok || root != "/repo/updated-v2" {
		t.Fatalf("updated root = %q, %v", root, ok)
	}
	if root, ok := SourceRoot(ctx, "acme/new"); !ok || root != "/repo/new" {
		t.Fatalf("new root = %q, %v", root, ok)
	}
	if root, ok := SourceRoot(ctx, "acme/unrelated"); !ok || root != "/repo/unrelated" {
		t.Fatalf("unrelated root = %q, %v", root, ok)
	}

	SwapSourceRoots(ctx, previous, "acme/old", "acme/updated", "acme/new")
	if root, ok := SourceRoot(ctx, "acme/old"); !ok || root != "/repo/old" {
		t.Fatalf("restored old root = %q, %v", root, ok)
	}
	if root, ok := SourceRoot(ctx, "acme/updated"); !ok || root != "/repo/updated-v1" {
		t.Fatalf("restored updated root = %q, %v", root, ok)
	}
	if _, ok := SourceRoot(ctx, "acme/new"); ok {
		t.Fatal("new root must be removed during restore")
	}
}
