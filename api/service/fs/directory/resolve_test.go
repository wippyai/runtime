// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/registry"
)

func moduleEntry(module string) registry.Entry {
	e := registry.Entry{ID: registry.NewID("acme.ui", "ui_fs")}
	if module != "" {
		e.Meta = attrs.NewBagFrom(map[string]any{"module": module})
	}
	return e
}

func withSourceRoot(ctx context.Context, module, root string) context.Context {
	sources := modules.NewSourceRegistry()
	sources.Set(modules.Sources{module: {
		LoadPath:     root,
		ResourceRoot: root,
		Owner:        module,
	}})
	return modules.WithSourceRegistry(ctx, sources)
}

func TestResolveDirectory(t *testing.T) {
	abs := filepath.Join(string(filepath.Separator), "abs", "path")

	t.Run("nil config", func(t *testing.T) {
		if got := ResolveDirectory(ctxapi.NewRootContext(), moduleEntry("acme/ui"), nil); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		if got := ResolveDirectory(ctxapi.NewRootContext(), moduleEntry("acme/ui"), &Config{}); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("absolute path unchanged", func(t *testing.T) {
		if got := ResolveDirectory(ctxapi.NewRootContext(), moduleEntry("acme/ui"), &Config{Directory: abs}); got != abs {
			t.Fatalf("got %q, want %q", got, abs)
		}
	})

	t.Run("project base stays working-dir relative", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx = withSourceRoot(ctx, "acme/ui", abs)
		cfg := &Config{Directory: "./static", Base: BaseProject}
		if got := ResolveDirectory(ctx, moduleEntry("acme/ui"), cfg); got != "./static" {
			t.Fatalf("got %q, want ./static", got)
		}
	})

	t.Run("no module meta stays unchanged", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx = withSourceRoot(ctx, "acme/ui", abs)
		if got := ResolveDirectory(ctx, moduleEntry(""), &Config{Directory: "./static"}); got != "./static" {
			t.Fatalf("got %q, want ./static", got)
		}
	})

	t.Run("module meta without source root stays unchanged", func(t *testing.T) {
		if got := ResolveDirectory(ctxapi.NewRootContext(), moduleEntry("acme/ui"), &Config{Directory: "./static"}); got != "./static" {
			t.Fatalf("got %q, want ./static", got)
		}
	})

	t.Run("module meta with source root joins", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()
		ctx = withSourceRoot(ctx, "acme/ui", abs)
		want := filepath.Join(abs, "static")
		if got := ResolveDirectory(ctx, moduleEntry("acme/ui"), &Config{Directory: "./static"}); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
