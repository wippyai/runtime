// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestEmbedFSCollectsModuleRelativeDirectory(t *testing.T) {
	moduleRoot := t.TempDir()
	staticDir := filepath.Join(moduleRoot, "static", "app")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "app.js"), []byte("export const ok = true;\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	t.Chdir(moduleRoot)

	entry := registry.Entry{
		ID:   registry.NewID("acme.ui", "static_fs"),
		Kind: dirapi.Kind,
		Data: payload.New(map[string]any{
			"base":      dirapi.BaseModule,
			"directory": "./static/app",
		}),
	}

	prov := moduleProv("acme/ui", entry.ID)
	resources, err := collectResources(context.Background(), "", prov, []registry.Entry{entry}, zap.NewNop())
	if err != nil {
		t.Fatalf("collectResources failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(resources))
	}
	if got := resources[0].ID.String(); got != "acme.ui:static_fs" {
		t.Fatalf("resource id = %q, want acme.ui:static_fs", got)
	}
	data, err := fs.ReadFile(resources[0].FS, "app.js")
	if err != nil {
		t.Fatalf("read embedded app.js: %v", err)
	}
	if string(data) != "export const ok = true;\n" {
		t.Fatalf("embedded app.js = %q", string(data))
	}

	transformed := transformEntries([]registry.Entry{entry}, []registry.ID{entry.ID})
	if len(transformed) != 1 {
		t.Fatalf("transformed count = %d, want 1", len(transformed))
	}
	if transformed[0].Kind != embedapi.Kind {
		t.Fatalf("transformed kind = %q, want %q", transformed[0].Kind, embedapi.Kind)
	}
}

func TestEmbedFSResolvesModuleRootWithoutChdir(t *testing.T) {
	moduleRoot := t.TempDir()
	staticDir := filepath.Join(moduleRoot, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>ui</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	t.Chdir(t.TempDir())

	entry := registry.Entry{
		ID:   registry.NewID("acme.ui", "ui_fs"),
		Kind: dirapi.Kind,
		Data: payload.New(map[string]any{"directory": "./static"}),
	}

	resources, err := collectResources(
		context.Background(),
		moduleRoot,
		moduleProv("acme/ui", entry.ID),
		[]registry.Entry{entry},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("collectResources failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(resources))
	}
	data, err := fs.ReadFile(resources[0].FS, "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	if string(data) != "<html>ui</html>" {
		t.Fatalf("embedded index.html = %q", string(data))
	}
}

// Pack mode: entries come from many modules at once, so the owning module of
// each entry — and with it the source root its relative directory resolves
// against — comes from the loader provenance.
func TestEmbedFSResolvesModuleSourceRootFromProvenance(t *testing.T) {
	moduleRoot := t.TempDir()
	staticDir := filepath.Join(moduleRoot, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>ui</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	t.Chdir(t.TempDir())

	ctx := ctxapi.NewRootContext()
	sources := modules.NewSourceRegistry()
	sources.Set(modules.Sources{"acme/ui": {
		LoadPath: moduleRoot, ResourceRoot: moduleRoot, Owner: "acme/ui",
	}})
	ctx = modules.WithSourceRegistry(ctx, sources)

	entry := registry.Entry{
		ID:   registry.NewID("acme.ui", "ui_fs"),
		Kind: dirapi.Kind,
		Data: payload.New(map[string]any{"directory": "./static"}),
	}

	resources, err := collectResources(ctx, "", moduleProv("acme/ui", entry.ID), []registry.Entry{entry}, zap.NewNop())
	if err != nil {
		t.Fatalf("collectResources failed: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resource count = %d, want 1", len(resources))
	}
	if data, err := fs.ReadFile(resources[0].FS, "index.html"); err != nil || string(data) != "<html>ui</html>" {
		t.Fatalf("embedded index.html = %q err=%v", string(data), err)
	}

	// A host-authored entry has no module source root to resolve against.
	if _, err := collectResources(ctx, "", nil, []registry.Entry{entry}, zap.NewNop()); err == nil {
		t.Fatal("expected error for host-authored entry with a module-relative directory, got nil")
	}
}

func TestEmbedFSCollectsMultipleResources(t *testing.T) {
	moduleRoot := t.TempDir()
	for _, sub := range []string{"static", "wasm"} {
		dir := filepath.Join(moduleRoot, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		if err := os.WriteFile(filepath.Join(dir, sub+".bin"), []byte(sub), 0o644); err != nil {
			t.Fatalf("write %s: %v", sub, err)
		}
	}
	t.Chdir(t.TempDir())

	entries := []registry.Entry{
		{
			ID:   registry.NewID("acme.app", "ui_fs"),
			Kind: dirapi.Kind,
			Data: payload.New(map[string]any{"directory": "./static"}),
		},
		{
			ID:   registry.NewID("acme.app", "wasm_fs"),
			Kind: dirapi.Kind,
			Data: payload.New(map[string]any{"directory": "./wasm"}),
		},
	}

	resources, err := collectResources(
		context.Background(),
		moduleRoot,
		moduleProv("acme/app", entries[0].ID, entries[1].ID),
		entries,
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("collectResources failed: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("resource count = %d, want 2", len(resources))
	}
}

func TestEmbedFSErrorsOnMissingDirectory(t *testing.T) {
	moduleRoot := t.TempDir()
	t.Chdir(t.TempDir())

	entry := registry.Entry{
		ID:   registry.NewID("acme.ui", "ui_fs"),
		Kind: dirapi.Kind,
		Data: payload.New(map[string]any{"directory": "./does-not-exist"}),
	}

	_, err := collectResources(
		context.Background(),
		moduleRoot,
		moduleProv("acme/ui", entry.ID),
		[]registry.Entry{entry},
		zap.NewNop(),
	)
	if err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}

func TestEmbedFSErrorsOnNilEntryData(t *testing.T) {
	entry := registry.Entry{ID: registry.NewID("acme.ui", "ui_fs"), Kind: dirapi.Kind}

	_, err := collectResources(context.Background(), "", nil, []registry.Entry{entry}, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for entry with nil data, got nil")
	}
}

func TestEmbedFSClearsResourcesWhenNoDirectories(t *testing.T) {
	moduleRoot := t.TempDir()
	staticDir := filepath.Join(moduleRoot, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>ui</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	ctx := context.Background()
	entries := []registry.Entry{{
		ID:   registry.NewID("acme.ui", "ui_fs"),
		Kind: dirapi.Kind,
		Data: payload.New(map[string]any{"directory": "./static"}),
	}}
	if err := EmbedFS(moduleRoot, nil, "ui_fs").Execute(ctx, &entries); err != nil {
		t.Fatalf("first stage execute: %v", err)
	}
	if got := len(GetResources(ctx)); got != 1 {
		t.Fatalf("resources after first run = %d, want 1", got)
	}

	entries = []registry.Entry{{ID: registry.NewID("acme.ui", "definition"), Kind: "ns.definition"}}
	if err := EmbedFS(moduleRoot, nil).Execute(ctx, &entries); err != nil {
		t.Fatalf("second stage execute: %v", err)
	}
	if got := len(GetResources(ctx)); got != 0 {
		t.Fatalf("resources after no-directory run = %d, want 0", got)
	}
}

func TestFilterEmbeddableEntries(t *testing.T) {
	dirA := registry.Entry{ID: registry.NewID("acme.app", "ui_fs"), Kind: dirapi.Kind}
	dirB := registry.Entry{ID: registry.NewID("acme.app", "wasm_fs"), Kind: dirapi.Kind}
	notDir := registry.Entry{ID: registry.NewID("acme.app", "compute"), Kind: "function.wasm"}
	all := []registry.Entry{dirA, dirB, notDir}

	ids := func(entries []registry.ID) []string {
		out := make([]string, len(entries))
		for i, id := range entries {
			out[i] = id.String()
		}
		return out
	}

	t.Run("no patterns embeds every directory", func(t *testing.T) {
		got := ids(filterEmbeddableEntries(all, nil))
		want := []string{"acme.app:ui_fs", "acme.app:wasm_fs"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("wildcard pattern embeds every directory", func(t *testing.T) {
		got := ids(filterEmbeddableEntries(all, []string{"**"}))
		want := []string{"acme.app:ui_fs", "acme.app:wasm_fs"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("match by full id", func(t *testing.T) {
		got := ids(filterEmbeddableEntries(all, []string{"acme.app:wasm_fs"}))
		if len(got) != 1 || got[0] != "acme.app:wasm_fs" {
			t.Fatalf("got %v, want [acme.app:wasm_fs]", got)
		}
	})

	t.Run("match by name", func(t *testing.T) {
		got := ids(filterEmbeddableEntries(all, []string{"ui_fs"}))
		if len(got) != 1 || got[0] != "acme.app:ui_fs" {
			t.Fatalf("got %v, want [acme.app:ui_fs]", got)
		}
	})

	t.Run("non-matching pattern embeds nothing", func(t *testing.T) {
		if got := filterEmbeddableEntries(all, []string{"nope"}); len(got) != 0 {
			t.Fatalf("got %v, want empty", ids(got))
		}
	})

	t.Run("non-directory kinds are never embeddable", func(t *testing.T) {
		if got := filterEmbeddableEntries([]registry.Entry{notDir}, nil); len(got) != 0 {
			t.Fatalf("got %v, want empty", ids(got))
		}
	})
}

func TestEmbedFSEndToEndPacksBothResources(t *testing.T) {
	moduleRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(moduleRoot, "static"), 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleRoot, "static", "index.html"), []byte("<html>ui</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(moduleRoot, "wasm"), 0o755); err != nil {
		t.Fatalf("mkdir wasm: %v", err)
	}
	wasmBytes := []byte("\x00asm\x01\x00\x00\x00")
	if err := os.WriteFile(filepath.Join(moduleRoot, "wasm", "compute.wasm"), wasmBytes, 0o644); err != nil {
		t.Fatalf("write compute.wasm: %v", err)
	}
	t.Chdir(t.TempDir())

	entries := []registry.Entry{
		{ID: registry.NewID("acme.app", "definition"), Kind: "ns.definition"},
		{
			ID:   registry.NewID("acme.app", "ui_fs"),
			Kind: dirapi.Kind,
			Data: payload.New(map[string]any{"directory": "./static"}),
		},
		{
			ID:   registry.NewID("acme.app", "wasm_fs"),
			Kind: dirapi.Kind,
			Data: payload.New(map[string]any{"directory": "./wasm"}),
		},
	}

	stage := EmbedFS(moduleRoot, nil, "ui_fs", "wasm_fs")
	if err := stage.Execute(context.Background(), &entries); err != nil {
		t.Fatalf("stage execute: %v", err)
	}

	for _, e := range entries {
		if (e.ID.Name == "ui_fs" || e.ID.Name == "wasm_fs") && e.Kind != embedapi.Kind {
			t.Fatalf("entry %s kind = %q, want %q", e.ID.String(), e.Kind, embedapi.Kind)
		}
	}

	specs := GetResources(context.Background())
	if len(specs) != 2 {
		t.Fatalf("collected resources = %d, want 2", len(specs))
	}

	var buf bytes.Buffer
	if err := wapp.NewWriter().PackWithResources(wapp.Metadata{}, nil, specs, &buf); err != nil {
		t.Fatalf("pack: %v", err)
	}

	reader, err := wapp.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if got := len(reader.ListResources()); got != 2 {
		t.Fatalf("packed resources = %d, want 2", got)
	}

	uiFS, err := reader.GetFS(wapp.NewID("acme.app", "ui_fs"))
	if err != nil {
		t.Fatalf("get ui fs: %v", err)
	}
	if data, err := fs.ReadFile(uiFS, "index.html"); err != nil || string(data) != "<html>ui</html>" {
		t.Fatalf("ui content = %q err=%v", string(data), err)
	}

	wasmFS, err := reader.GetFS(wapp.NewID("acme.app", "wasm_fs"))
	if err != nil {
		t.Fatalf("get wasm fs: %v", err)
	}
	if data, err := fs.ReadFile(wasmFS, "compute.wasm"); err != nil || !bytes.Equal(data, wasmBytes) {
		t.Fatalf("wasm content mismatch err=%v", err)
	}
}

// moduleProv builds the loader provenance naming the module that owns the
// entries being embedded.
func moduleProv(module string, ids ...registry.ID) registry.ProvMap {
	prov := make(registry.ProvMap, len(ids))
	for _, id := range ids {
		prov[id] = registry.EntryProvenance{Module: module}
	}
	return prov
}

// A supplied provenance map is total over the entries the stage attributes.
// The nil map is the documented single-source build with no module world.
func TestEmbedFSProvenanceTotality(t *testing.T) {
	moduleRoot := t.TempDir()
	staticDir := filepath.Join(moduleRoot, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html>ui</html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	t.Chdir(moduleRoot)

	entry := registry.Entry{
		ID:   registry.NewID("acme.ui", "ui_fs"),
		Kind: dirapi.Kind,
		Data: payload.New(map[string]any{"directory": "./static"}),
	}
	other := registry.NewID("acme.ui", "other_fs")

	for _, tc := range []struct {
		prov    registry.ProvMap
		name    string
		wantErr bool
	}{
		{name: "no module world", prov: nil},
		{name: "entry named", prov: registry.ProvMap{entry.ID: {}}},
		{name: "another entry named", prov: registry.ProvMap{other: {Module: "acme/ui"}}, wantErr: true},
		{name: "empty map names nothing", prov: registry.ProvMap{}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ctxapi.NewRootContext()
			entries := []registry.Entry{entry}
			err := EmbedFS("", tc.prov, "ui_fs").Execute(ctx, &entries)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("execute failed: %v", err)
				}
				return
			}
			if !errors.Is(err, registry.ErrMissingProvenance) {
				t.Fatalf("err = %v, want ErrMissingProvenance", err)
			}
		})
	}
}
