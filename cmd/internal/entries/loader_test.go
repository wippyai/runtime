// SPDX-License-Identifier: MPL-2.0

package entries

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/boot/deps/lock"
	transcoder "github.com/wippyai/runtime/system/payload"
	yamlpayload "github.com/wippyai/runtime/system/payload/yaml"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestPackReaderGetEntries(t *testing.T) {
	entries := []wapp.Entry{
		{
			ID:   wapp.NewID("test.ns", "entry1"),
			Kind: "test.kind",
			Meta: wapp.Metadata{"key": "value"},
			Data: map[string]any{"config": "data"},
		},
		{
			ID:   wapp.NewID("test.ns", "entry2"),
			Kind: "test.other",
			Data: "simple data",
		},
	}

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	if err := writer.PackEntries(wapp.Metadata{"name": "test"}, entries, &buf); err != nil {
		t.Fatalf("PackEntries failed: %v", err)
	}

	reader, err := NewPackReader(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("NewPackReader failed: %v", err)
	}

	result, err := reader.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	if len(result) != len(entries) {
		t.Fatalf("Entry count = %d, want %d", len(result), len(entries))
	}

	if result[0].ID.NS != "test.ns" || result[0].ID.Name != "entry1" {
		t.Errorf("Entry[0].ID = %v, want test.ns:entry1", result[0].ID)
	}
	if result[0].Kind != "test.kind" {
		t.Errorf("Entry[0].Kind = %v, want test.kind", result[0].Kind)
	}
	if val, ok := result[0].Meta.Get("key"); !ok || val != "value" {
		t.Errorf("Entry[0].Meta[key] = %v, want value", val)
	}

	if result[1].ID.NS != "test.ns" || result[1].ID.Name != "entry2" {
		t.Errorf("Entry[1].ID = %v, want test.ns:entry2", result[1].ID)
	}
}

func TestPackReaderEmptyEntries(t *testing.T) {
	var buf bytes.Buffer
	writer := wapp.NewWriter()
	if err := writer.PackEntries(wapp.Metadata{}, nil, &buf); err != nil {
		t.Fatalf("PackEntries failed: %v", err)
	}

	reader, err := NewPackReader(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("NewPackReader failed: %v", err)
	}

	result, err := reader.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Entry count = %d, want 0", len(result))
	}
}

func TestConvertToWappEntries(t *testing.T) {
	entries := []regapi.Entry{
		{
			ID:   regapi.NewID("test.ns", "entry1"),
			Kind: "test.kind",
			Meta: map[string]any{"key": "value"},
			Data: payload.New(map[string]any{"config": "data"}),
		},
		{
			ID:   regapi.NewID("test.ns", "entry2"),
			Kind: "test.other",
			Data: payload.New("string data"),
		},
	}

	result := ConvertToWappEntries(entries)

	if len(result) != len(entries) {
		t.Fatalf("Entry count = %d, want %d", len(result), len(entries))
	}

	if result[0].ID.Namespace != "test.ns" || result[0].ID.Name != "entry1" {
		t.Errorf("Entry[0].ID = %v, want test.ns:entry1", result[0].ID)
	}
	if result[0].Kind != "test.kind" {
		t.Errorf("Entry[0].Kind = %v, want test.kind", result[0].Kind)
	}

	if result[1].ID.Namespace != "test.ns" || result[1].ID.Name != "entry2" {
		t.Errorf("Entry[1].ID = %v, want test.ns:entry2", result[1].ID)
	}
}

func TestConvertToWappEntriesNilData(t *testing.T) {
	entries := []regapi.Entry{
		{
			ID:   regapi.NewID("test.ns", "entry1"),
			Kind: "test.kind",
			Data: nil,
		},
	}

	result := ConvertToWappEntries(entries)

	if len(result) != 1 {
		t.Fatalf("Entry count = %d, want 1", len(result))
	}

	if result[0].Data != nil {
		t.Errorf("Entry[0].Data = %v, want nil", result[0].Data)
	}
}

func createTestWappFile(t *testing.T, dir string, name string, entries []wapp.Entry) string {
	t.Helper()

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	if err := writer.PackEntries(wapp.Metadata{"name": name}, entries, &buf); err != nil {
		t.Fatalf("PackEntries failed: %v", err)
	}

	path := filepath.Join(dir, name+".wapp")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	return path
}

func createTestWappFileWithResources(t *testing.T, path, name string, entries []wapp.Entry, resources []wapp.ResourceSpec) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	writer := wapp.NewWriter()
	err = writer.PackWithResources(wapp.Metadata{"name": name}, entries, resources, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("PackWithResources failed: %v", err)
	}
}

func setupTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx := context.Background()

	appCtx := contextapi.NewAppContext()
	ctx = contextapi.WithAppContext(ctx, appCtx)

	logger, _ := zap.NewDevelopment()
	ctx = logapi.WithLogger(ctx, logger)

	dtt := transcoder.GlobalTranscoder()
	yamlpayload.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	loaderComponent := core.Loader()
	ctx, err := loaderComponent.Load(ctx)
	if err != nil {
		t.Fatalf("Loader component failed: %v", err)
	}

	return ctx
}

func TestLoadEntriesFromPathsSingleWapp(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	entries := []wapp.Entry{
		{
			ID:   wapp.NewID("test.ns", "entry1"),
			Kind: "test.kind",
			Meta: wapp.Metadata{"key": "value"},
		},
		{
			ID:   wapp.NewID("test.ns", "entry2"),
			Kind: "test.other",
		},
	}

	wappPath := createTestWappFile(t, tmpDir, "test", entries)

	result, err := LoadEntriesFromPaths(ctx, []string{wappPath}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	if len(result) != len(entries) {
		t.Fatalf("Entry count = %d, want %d", len(result), len(entries))
	}

	found := make(map[string]bool)
	for _, e := range result {
		found[e.ID.String()] = true
	}

	if !found["test.ns:entry1"] {
		t.Error("Entry test.ns:entry1 not found")
	}
	if !found["test.ns:entry2"] {
		t.Error("Entry test.ns:entry2 not found")
	}
}

func TestExtractWappToDirRestoresEmbeddedFilesystem(t *testing.T) {
	projectRoot := t.TempDir()
	vendorDir := filepath.Join(projectRoot, ".wippy", "vendor", "acme")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatalf("mkdir vendor dir: %v", err)
	}

	resourceRoot := filepath.Join(t.TempDir(), "resource")
	if err := os.MkdirAll(resourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir resource dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resourceRoot, "app.js"), []byte("export const ok = true;\n"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	wappPath := filepath.Join(vendorDir, "ui-v1.0.0.wapp")
	createTestWappFileWithResources(t, wappPath, "acme/ui", []wapp.Entry{{
		ID:   wapp.NewID("acme.ui", "static_fs"),
		Kind: "fs.embed",
		Meta: wapp.Metadata{"module": "acme/ui"},
		Data: map[string]any{},
	}}, []wapp.ResourceSpec{{
		ID: wapp.NewID("acme.ui", "static_fs"),
		FS: os.DirFS(resourceRoot),
	}})

	targetDir := filepath.Join(vendorDir, "ui")
	if err := ExtractWappToDir(wappPath, targetDir); err != nil {
		t.Fatalf("ExtractWappToDir failed: %v", err)
	}

	if _, err := os.Stat(wappPath); err == nil {
		t.Fatalf("packed file should be removed after extraction: %s", wappPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat packed file: %v", err)
	}

	extractedJS := filepath.Join(targetDir, "static_fs", "app.js")
	data, err := os.ReadFile(extractedJS)
	if err != nil {
		t.Fatalf("read extracted app.js: %v", err)
	}
	if string(data) != "export const ok = true;\n" {
		t.Fatalf("extracted app.js = %q", string(data))
	}

	var index struct {
		Entries []struct {
			Name      string `yaml:"name"`
			Kind      string `yaml:"kind"`
			Directory string `yaml:"directory"`
			Base      string `yaml:"base"`
		} `yaml:"entries"`
	}
	indexData, err := os.ReadFile(filepath.Join(targetDir, "_index.yaml"))
	if err != nil {
		t.Fatalf("read extracted index: %v", err)
	}
	if err := yaml.Unmarshal(indexData, &index); err != nil {
		t.Fatalf("parse extracted index: %v", err)
	}

	for _, entry := range index.Entries {
		if entry.Name != "static_fs" {
			continue
		}
		if entry.Kind != "fs.directory" {
			t.Fatalf("extracted kind = %q, want fs.directory", entry.Kind)
		}
		if entry.Directory != "static_fs" {
			t.Fatalf("extracted directory = %q, want %q", entry.Directory, "static_fs")
		}
		if entry.Base != "module" {
			t.Fatalf("extracted base = %q, want %q", entry.Base, "module")
		}
		return
	}
	t.Fatalf("static_fs entry not found in extracted index")
}

func TestLoadEntriesFromPathsMultipleWapps(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	lib1Entries := []wapp.Entry{
		{
			ID:   wapp.NewID("lib1", "calc"),
			Kind: "code.lua",
		},
	}
	lib1Path := createTestWappFile(t, tmpDir, "lib1", lib1Entries)

	lib2Entries := []wapp.Entry{
		{
			ID:   wapp.NewID("lib2", "utils"),
			Kind: "code.lua",
		},
	}
	lib2Path := createTestWappFile(t, tmpDir, "lib2", lib2Entries)

	appEntries := []wapp.Entry{
		{
			ID:   wapp.NewID("app", "main"),
			Kind: "process.lua",
		},
	}
	appPath := createTestWappFile(t, tmpDir, "app", appEntries)

	paths := []string{lib1Path, lib2Path, appPath}
	result, err := LoadEntriesFromPaths(ctx, paths, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("Entry count = %d, want 3", len(result))
	}

	found := make(map[string]bool)
	for _, e := range result {
		found[e.ID.String()] = true
	}

	if !found["lib1:calc"] {
		t.Error("Entry lib1:calc not found")
	}
	if !found["lib2:utils"] {
		t.Error("Entry lib2:utils not found")
	}
	if !found["app:main"] {
		t.Error("Entry app:main not found")
	}
}

func TestLoadEntriesFromPathsNonExistent(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	entries := []wapp.Entry{
		{ID: wapp.NewID("test", "entry"), Kind: "test"},
	}
	existingPath := createTestWappFile(t, tmpDir, "existing", entries)

	nonExistentPath := filepath.Join(tmpDir, "nonexistent.wapp")

	paths := []string{nonExistentPath, existingPath}
	_, err := LoadEntriesFromPaths(ctx, paths, logger)
	if err == nil {
		t.Fatal("expected error for non-existent .wapp path")
	}
}

func TestLoadEntriesFromPathsEmptyPaths(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	result, err := LoadEntriesFromPaths(ctx, []string{}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Entry count = %d, want 0", len(result))
	}
}

func TestLoadEntriesFromPathsUnknownExtension(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	txtPath := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(txtPath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	result, err := LoadEntriesFromPaths(ctx, []string{txtPath}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Entry count = %d, want 0", len(result))
	}
}

func TestLoadEntriesFromPathsMissingTranscoder(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	_, err := LoadEntriesFromPaths(ctx, []string{"/some/path.wapp"}, logger)
	if err == nil {
		t.Fatal("Expected error for missing transcoder")
	}

	if !errors.Is(err, ErrTranscoderNotFound) {
		t.Errorf("Error = %v, want ErrTranscoderNotFound", err)
	}
}

func TestLoadEntriesFromPathsMissingLoader(t *testing.T) {
	ctx := context.Background()

	appCtx := contextapi.NewAppContext()
	ctx = contextapi.WithAppContext(ctx, appCtx)

	dtt := transcoder.GlobalTranscoder()
	ctx = payload.WithTranscoder(ctx, dtt)

	logger := zap.NewNop()

	_, err := LoadEntriesFromPaths(ctx, []string{"/some/path.wapp"}, logger)
	if err == nil {
		t.Fatal("Expected error for missing loader")
	}

	if !errors.Is(err, ErrLoaderNotFound) {
		t.Errorf("Error = %v, want ErrLoaderNotFound", err)
	}
}

func TestLoadEntriesFromPathsDependencyOrder(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	depEntries := []wapp.Entry{
		{
			ID:   wapp.NewID("dep", "calc"),
			Kind: "code.lua",
			Data: "function add(a, b) return a + b end",
		},
	}
	depPath := createTestWappFile(t, tmpDir, "dep", depEntries)

	appEntries := []wapp.Entry{
		{
			ID:   wapp.NewID("app", "run"),
			Kind: "process.lua",
			Meta: wapp.Metadata{
				"imports": map[string]any{"calc": "dep:calc"},
			},
		},
	}
	appPath := createTestWappFile(t, tmpDir, "app", appEntries)

	paths := []string{depPath, appPath}
	result, err := LoadEntriesFromPaths(ctx, paths, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Entry count = %d, want 2", len(result))
	}

	depFound := false
	appFound := false
	for _, e := range result {
		if e.ID.String() == "dep:calc" {
			depFound = true
		}
		if e.ID.String() == "app:run" {
			appFound = true
		}
	}

	if !depFound {
		t.Error("Dependency entry dep:calc not found")
	}
	if !appFound {
		t.Error("App entry app:run not found")
	}
}

func TestLoadEntriesFromPathsPreservesMetadata(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	entries := []wapp.Entry{
		{
			ID:   wapp.NewID("test", "entry"),
			Kind: "test.kind",
			Meta: wapp.Metadata{
				"command": map[string]any{
					"name":  "run",
					"short": "Run the app",
				},
				"enabled": true,
			},
		},
	}

	wappPath := createTestWappFile(t, tmpDir, "test", entries)

	result, err := LoadEntriesFromPaths(ctx, []string{wappPath}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("Entry count = %d, want 1", len(result))
	}

	cmdVal, ok := result[0].Meta.Get("command")
	if !ok {
		t.Fatal("Meta[command] not found")
	}

	cmdMap, ok := cmdVal.(map[string]any)
	if !ok {
		t.Fatalf("Meta[command] type = %T, want map[string]any", cmdVal)
	}

	if cmdMap["name"] != "run" {
		t.Errorf("Meta[command][name] = %v, want run", cmdMap["name"])
	}
	if cmdMap["short"] != "Run the app" {
		t.Errorf("Meta[command][short] = %v, want 'Run the app'", cmdMap["short"])
	}

	enabledVal, ok := result[0].Meta.Get("enabled")
	if !ok || enabledVal != true {
		t.Errorf("Meta[enabled] = %v, want true", enabledVal)
	}
}

func TestLoadEntriesFromPathsMultipleWappsWithDifferentKinds(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	tmpDir := t.TempDir()

	libEntries := []wapp.Entry{
		{
			ID:   wapp.NewID("mylib", "calc"),
			Kind: "code.lua",
			Data: "local M = {}; function M.add(a, b) return a + b end; return M",
		},
		{
			ID:   wapp.NewID("mylib", "definition"),
			Kind: "ns.definition",
			Meta: wapp.Metadata{"title": "My Library"},
		},
	}
	libPath := createTestWappFile(t, tmpDir, "mylib", libEntries)

	appEntries := []wapp.Entry{
		{
			ID:   wapp.NewID("myapp", "definition"),
			Kind: "ns.definition",
			Meta: wapp.Metadata{"title": "My App"},
		},
		{
			ID:   wapp.NewID("myapp", "run"),
			Kind: "process.lua",
			Meta: wapp.Metadata{
				"command": map[string]any{"name": "run", "short": "Run the app"},
				"imports": map[string]any{"calc": "mylib:calc"},
			},
		},
	}
	appPath := createTestWappFile(t, tmpDir, "myapp", appEntries)

	paths := []string{libPath, appPath}
	result, err := LoadEntriesFromPaths(ctx, paths, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	if len(result) != 4 {
		t.Fatalf("Entry count = %d, want 4", len(result))
	}

	found := make(map[string]bool)
	kinds := make(map[string]regapi.Kind)
	for _, e := range result {
		found[e.ID.String()] = true
		kinds[e.ID.String()] = e.Kind
	}

	expectedEntries := []string{
		"mylib:calc",
		"mylib:definition",
		"myapp:definition",
		"myapp:run",
	}

	for _, id := range expectedEntries {
		if !found[id] {
			t.Errorf("Entry %s not found", id)
		}
	}

	if kinds["mylib:calc"] != "code.lua" {
		t.Errorf("mylib:calc kind = %v, want code.lua", kinds["mylib:calc"])
	}
	if kinds["myapp:run"] != "process.lua" {
		t.Errorf("myapp:run kind = %v, want process.lua", kinds["myapp:run"])
	}
}

func TestLoadEntriesFromModuleLoadPaths_ResolvesRequirementByModuleMeta(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()
	tmpDir := t.TempDir()

	moduleDir := filepath.Join(tmpDir, "module")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	moduleYAML := `version: "1.0"
namespace: userspace.user
entries:
  - name: public_router
    kind: ns.requirement
    targets:
      - entry: login.endpoint
        path: meta.router
  - name: login.endpoint
    kind: http.endpoint
    meta:
      router: public_router
`
	if err := os.WriteFile(filepath.Join(moduleDir, "_index.yaml"), []byte(moduleYAML), 0o644); err != nil {
		t.Fatalf("write module _index.yaml: %v", err)
	}

	appDir := filepath.Join(tmpDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	appYAML := `version: "1.0"
namespace: app.deps
entries:
  - name: users
    kind: ns.dependency
    component: userspace/users
    parameters:
      - name: public_router
        value: app:api.public
`
	if err := os.WriteFile(filepath.Join(appDir, "_index.yaml"), []byte(appYAML), 0o644); err != nil {
		t.Fatalf("write app _index.yaml: %v", err)
	}

	flatEntries, err := LoadEntriesFromPaths(ctx, []string{appDir, moduleDir}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromPaths failed: %v", err)
	}

	routerFlat := ""
	for _, entry := range flatEntries {
		if entry.ID.String() != "userspace.user:login.endpoint" {
			continue
		}
		routerFlat = entry.Meta.GetString("router", "")
	}
	if routerFlat != "public_router" {
		t.Fatalf("flat load router = %q, want unresolved alias", routerFlat)
	}

	moduleAwareEntries, err := LoadEntriesFromModuleLoadPaths(ctx, []lock.ModuleLoadPath{
		{Path: appDir},
		{Path: moduleDir, Module: "userspace/users", Version: "1.0.0"},
	}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromModuleLoadPaths failed: %v", err)
	}

	routerResolved := ""
	for _, entry := range moduleAwareEntries {
		if entry.ID.String() != "userspace.user:login.endpoint" {
			continue
		}
		routerResolved = entry.Meta.GetString("router", "")
	}
	if routerResolved != "app:api.public" {
		t.Fatalf("module-aware load router = %q, want app:api.public", routerResolved)
	}
}

func TestRegisterModuleSourceRoots_DirectoryModulesOnly(t *testing.T) {
	ctx := setupTestContext(t)
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "src")
	replacementDir := filepath.Join(tmpDir, "replacement")
	unpackedDir := filepath.Join(tmpDir, "vendor", "acme", "ui")
	missingDir := filepath.Join(tmpDir, "missing")
	packedPath := filepath.Join(tmpDir, "vendor", "acme", "packed-v1.0.0.wapp")

	for _, dir := range []string{appDir, replacementDir, unpackedDir, filepath.Dir(packedPath)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(packedPath, []byte("not loaded by this helper"), 0o644); err != nil {
		t.Fatalf("write packed path: %v", err)
	}

	registerModuleSourceRoots(ctx, []lock.ModuleLoadPath{
		{Path: appDir},
		{Path: replacementDir, Module: "acme/local"},
		{Path: unpackedDir, Module: "acme/ui", Version: "1.2.3"},
		{Path: packedPath, Module: "acme/packed", Version: "1.0.0"},
		{Path: missingDir, Module: "acme/missing", Version: "1.0.0"},
	})

	replacementRoot, ok := moduleapi.SourceRoot(ctx, "acme/local")
	if !ok {
		t.Fatal("replacement module source root not registered")
	}
	if replacementRoot != replacementDir {
		t.Fatalf("replacement root = %q, want %q", replacementRoot, replacementDir)
	}

	unpackedRoot, ok := moduleapi.SourceRoot(ctx, "acme/ui")
	if !ok {
		t.Fatal("unpacked module source root not registered")
	}
	if unpackedRoot != unpackedDir {
		t.Fatalf("unpacked root = %q, want %q", unpackedRoot, unpackedDir)
	}

	if _, ok := moduleapi.SourceRoot(ctx, "acme/packed"); ok {
		t.Fatal("packed .wapp module should not register a source root")
	}
	if _, ok := moduleapi.SourceRoot(ctx, "acme/missing"); ok {
		t.Fatal("missing module directory should not register a source root")
	}
}

func TestEnsureModulesInstalledSkipsReplacedModules(t *testing.T) {
	tests := []struct {
		name   string
		unpack bool
	}{
		{name: "packed mode", unpack: false},
		{name: "unpacked mode", unpack: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			lockPath := filepath.Join(tmpDir, lock.DefaultFilename)
			lockObj, err := lock.New(lockPath)
			if err != nil {
				t.Fatalf("create lock: %v", err)
			}

			replacementDir := filepath.Join(tmpDir, "local", "ui")
			if err := os.MkdirAll(replacementDir, 0o755); err != nil {
				t.Fatalf("mkdir replacement: %v", err)
			}

			lockObj.SetOptions(lock.Options{UnpackModules: tt.unpack})
			lockObj.SetModule(lock.Module{Name: "acme/ui", Version: "v1.0.0"})
			lockObj.SetReplacement(lock.Replacement{From: "acme/ui", To: "local/ui"})

			if err := ensureModulesInstalledFromLock(context.Background(), lockObj, zap.NewNop()); err != nil {
				t.Fatalf("ensureModulesInstalledFromLock failed: %v", err)
			}

			vendorModuleDir := filepath.Join(tmpDir, ".wippy", "vendor", "acme", "ui")
			if _, err := os.Stat(vendorModuleDir); err == nil {
				t.Fatalf("replaced module should not be installed to vendor directory: %s", vendorModuleDir)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat vendor module directory: %v", err)
			}

			vendorWapp := filepath.Join(tmpDir, ".wippy", "vendor", "acme", "ui-v1.0.0.wapp")
			if _, err := os.Stat(vendorWapp); err == nil {
				t.Fatalf("replaced module should not be installed to vendor pack: %s", vendorWapp)
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat vendor module pack: %v", err)
			}
		})
	}
}

func TestLoadEntriesFromModuleLoadPaths_AppliesSourceModuleExcludesToVersionedDependencies(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()
	tmpDir := t.TempDir()

	moduleDir := filepath.Join(tmpDir, "views")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	moduleConfig := `organization: wippy
module: views
exclude:
  - "app:**"
exclude_meta:
  type:
    - test
`
	if err := os.WriteFile(filepath.Join(moduleDir, "wippy.yaml"), []byte(moduleConfig), 0o644); err != nil {
		t.Fatalf("write module config: %v", err)
	}
	appYAML := `version: "1.0"
namespace: app
entries:
  - name: gateway
    kind: http.service
    addr: :19086
    lifecycle:
      auto_start: true
`
	if err := os.WriteFile(filepath.Join(moduleDir, "app.yaml"), []byte(appYAML), 0o644); err != nil {
		t.Fatalf("write module app.yaml: %v", err)
	}
	moduleYAML := `version: "1.0"
namespace: wippy.views
entries:
  - name: render
    kind: function.lua
    meta:
      type: test
    source: |
      return {}
  - name: public_api
    kind: http.endpoint
    meta:
      router: api.public
    path: /views
    method: GET
`
	if err := os.WriteFile(filepath.Join(moduleDir, "views.yaml"), []byte(moduleYAML), 0o644); err != nil {
		t.Fatalf("write module views.yaml: %v", err)
	}

	entries, err := LoadEntriesFromModuleLoadPaths(ctx, []lock.ModuleLoadPath{
		{Path: moduleDir, Module: "wippy/views", Version: "0.4.15"},
	}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromModuleLoadPaths failed: %v", err)
	}

	found := map[string]regapi.Entry{}
	for _, entry := range entries {
		found[entry.ID.String()] = entry
	}
	if _, ok := found["app:gateway"]; ok {
		t.Fatalf("versioned module leaked excluded app entry")
	}
	if _, ok := found["wippy.views:render"]; ok {
		t.Fatalf("versioned module leaked excluded meta.type=test entry")
	}
	publicAPI, ok := found["wippy.views:public_api"]
	if !ok {
		t.Fatalf("non-excluded module entry missing")
	}
	if got := publicAPI.Meta.GetString("module", ""); got != "wippy/views" {
		t.Fatalf("module meta = %q, want wippy/views", got)
	}
}

func TestLoadEntriesFromModuleLoadPaths_AppliesSourceModuleExcludesToReplacements(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()
	tmpDir := t.TempDir()

	moduleDir := filepath.Join(tmpDir, "dataflow-src")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}
	moduleConfig := `organization: wippy
module: dataflow
exclude_meta:
  type:
    - test
`
	if err := os.WriteFile(filepath.Join(moduleDir, "wippy.yaml"), []byte(moduleConfig), 0o644); err != nil {
		t.Fatalf("write module config: %v", err)
	}
	moduleYAML := `version: "1.0"
namespace: userspace.dataflow
entries:
  - name: local_test
    kind: function.lua
    meta:
      type: test
    source: |
      return {}
  - name: real_handler
    kind: function.lua
    source: |
      return {}
`
	if err := os.WriteFile(filepath.Join(moduleDir, "_index.yaml"), []byte(moduleYAML), 0o644); err != nil {
		t.Fatalf("write module _index.yaml: %v", err)
	}

	entries, err := LoadEntriesFromModuleLoadPaths(ctx, []lock.ModuleLoadPath{
		{Path: moduleDir, Module: "wippy/dataflow"},
	}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromModuleLoadPaths failed: %v", err)
	}

	found := map[string]regapi.Entry{}
	for _, entry := range entries {
		found[entry.ID.String()] = entry
	}
	if _, ok := found["userspace.dataflow:local_test"]; ok {
		t.Fatalf("replacement leaked excluded meta.type=test entry")
	}
	real, ok := found["userspace.dataflow:real_handler"]
	if !ok {
		t.Fatalf("non-excluded module entry missing")
	}
	if got := real.Meta.GetString("module", ""); got != "wippy/dataflow" {
		t.Fatalf("module meta = %q, want wippy/dataflow", got)
	}
}

// Mirrors the reported bug: a replacement points at a module's source tree
// that ships a test/_index.yaml defining entries under a namespace the host
// also uses. Without manifest filtering, the test fixture and the host's real
// entry collide; with filtering, only the host entry survives.
func TestLoadEntriesFromModuleLoadPaths_ReplacementHostCollisionFiltered(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()
	tmpDir := t.TempDir()

	moduleDir := filepath.Join(tmpDir, "facade-src")
	if err := os.MkdirAll(filepath.Join(moduleDir, "test"), 0o755); err != nil {
		t.Fatalf("mkdir module + test dir: %v", err)
	}
	moduleConfig := `organization: wippy
module: facade
exclude:
  - "app:**"
`
	if err := os.WriteFile(filepath.Join(moduleDir, "wippy.yaml"), []byte(moduleConfig), 0o644); err != nil {
		t.Fatalf("write module config: %v", err)
	}
	moduleYAML := `version: "1.0"
namespace: wippy.facade
entries:
  - name: real_handler
    kind: function.lua
    source: |
      return {}
`
	if err := os.WriteFile(filepath.Join(moduleDir, "_index.yaml"), []byte(moduleYAML), 0o644); err != nil {
		t.Fatalf("write module _index.yaml: %v", err)
	}
	testFixtureYAML := `version: "1.0"
namespace: app
entries:
  - name: gateway
    kind: http.service
    addr: :19085
`
	if err := os.WriteFile(filepath.Join(moduleDir, "test", "_index.yaml"), []byte(testFixtureYAML), 0o644); err != nil {
		t.Fatalf("write module test fixture: %v", err)
	}

	appDir := filepath.Join(tmpDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	hostYAML := `version: "1.0"
namespace: app
entries:
  - name: gateway
    kind: http.service
    addr: :8086
`
	if err := os.WriteFile(filepath.Join(appDir, "_index.yaml"), []byte(hostYAML), 0o644); err != nil {
		t.Fatalf("write host app _index.yaml: %v", err)
	}

	entries, err := LoadEntriesFromModuleLoadPaths(ctx, []lock.ModuleLoadPath{
		{Path: appDir},
		{Path: moduleDir, Module: "wippy/facade"},
	}, logger)
	if err != nil {
		t.Fatalf("LoadEntriesFromModuleLoadPaths failed: %v", err)
	}

	gatewayCount := 0
	var gateway regapi.Entry
	for _, entry := range entries {
		if entry.ID.String() != "app:gateway" {
			continue
		}
		gatewayCount++
		gateway = entry
	}
	if gatewayCount != 1 {
		t.Fatalf("app:gateway count = %d, want 1 (collision not filtered)", gatewayCount)
	}
	if got := gateway.Meta.GetString("module", ""); got != "" {
		t.Fatalf("app:gateway tagged with module=%q, want host (untagged) entry to win", got)
	}
}

func TestNormalizeEntries_PreLinkOverrideAffectsRequirementDefaults(t *testing.T) {
	ctx := setupTestContext(t)
	cfg := boot.NewConfig(boot.WithSection("override", map[string]any{
		"userspace.user:public_router:data.default": "app:api.pre",
	}))
	ctx = boot.WithConfig(ctx, cfg)

	items := []regapi.Entry{
		{
			ID:   regapi.NewID("userspace.user", "public_router"),
			Kind: regapi.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "app:api.default",
				"targets": []any{
					map[string]any{
						"entry": "login.endpoint",
						"path":  "meta.router",
					},
				},
			}),
		},
		{
			ID:   regapi.NewID("userspace.user", "login.endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{
				"router": "public_router",
			},
			Data: payload.New(map[string]any{
				"path":   "/user/token",
				"method": "POST",
				"func":   "login",
			}),
		},
	}

	if err := NormalizeEntries(ctx, &items); err != nil {
		t.Fatalf("NormalizeEntries failed: %v", err)
	}

	got := ""
	for _, entry := range items {
		if entry.ID.String() == "userspace.user:login.endpoint" {
			got = entry.Meta.GetString("router", "")
			break
		}
	}

	if got != "app:api.pre" {
		t.Fatalf("router = %q, want app:api.pre", got)
	}
}

func TestNormalizeEntries_PostLinkOverrideWinsFinalValue(t *testing.T) {
	ctx := setupTestContext(t)
	cfg := boot.NewConfig(boot.WithSection("override", map[string]any{
		"userspace.user:login.endpoint:meta.router": "app:api.final",
	}))
	ctx = boot.WithConfig(ctx, cfg)

	items := []regapi.Entry{
		{
			ID:   regapi.NewID("app.deps", "users"),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "userspace/users",
				"parameters": []any{
					map[string]any{
						"name":  "public_router",
						"value": "app:api.public",
					},
				},
			}),
		},
		{
			ID:   regapi.NewID("userspace.user", "public_router"),
			Kind: regapi.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "login.endpoint",
						"path":  "meta.router",
					},
				},
			}),
		},
		{
			ID:   regapi.NewID("userspace.user", "login.endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{
				"router": "public_router",
				"module": "userspace/users",
			},
			Data: payload.New(map[string]any{
				"path":   "/user/token",
				"method": "POST",
				"func":   "login",
			}),
		},
	}

	if err := NormalizeEntries(ctx, &items); err != nil {
		t.Fatalf("NormalizeEntries failed: %v", err)
	}

	got := ""
	for _, entry := range items {
		if entry.ID.String() == "userspace.user:login.endpoint" {
			got = entry.Meta.GetString("router", "")
			break
		}
	}

	if got != "app:api.final" {
		t.Fatalf("router = %q, want app:api.final", got)
	}
}

func TestUnwrapPayloadData(t *testing.T) {
	t.Run("returns non-map data as-is", func(t *testing.T) {
		result := unwrapPayloadData("string value")
		if result != "string value" {
			t.Errorf("expected string value, got %v", result)
		}

		result = unwrapPayloadData(42)
		if result != 42 {
			t.Errorf("expected 42, got %v", result)
		}

		result = unwrapPayloadData(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("unwraps payload wrapper structure", func(t *testing.T) {
		wrapped := map[string]any{
			"Data":   "inner value",
			"Format": "json",
		}
		result := unwrapPayloadData(wrapped)
		if result != "inner value" {
			t.Errorf("expected 'inner value', got %v", result)
		}
	})

	t.Run("returns map as-is if not payload wrapper", func(t *testing.T) {
		regularMap := map[string]any{
			"key1": "value1",
			"key2": "value2",
		}
		result := unwrapPayloadData(regularMap)
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}
		if resultMap["key1"] != "value1" {
			t.Errorf("expected key1=value1, got %v", resultMap["key1"])
		}
	})

	t.Run("returns map with extra fields as-is", func(t *testing.T) {
		mapWithExtra := map[string]any{
			"Data":   "inner",
			"Format": "json",
			"Extra":  "field",
		}
		result := unwrapPayloadData(mapWithExtra)
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}
		if resultMap["Extra"] != "field" {
			t.Errorf("expected Extra=field in result")
		}
	})

	t.Run("handles map with only Data field", func(t *testing.T) {
		mapOnlyData := map[string]any{
			"Data": "value",
		}
		result := unwrapPayloadData(mapOnlyData)
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("expected map, got %T", result)
		}
		if resultMap["Data"] != "value" {
			t.Errorf("expected Data=value in result")
		}
	})
}

func TestPackReaderReader(t *testing.T) {
	tmpDir := t.TempDir()
	wappPath := filepath.Join(tmpDir, "test.wapp")

	// Create test wapp file
	writer := wapp.NewWriter()
	f, err := os.Create(wappPath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	err = writer.PackEntries(nil, nil, f)
	f.Close()
	if err != nil {
		t.Fatalf("Failed to write wapp: %v", err)
	}

	// Open and create PackReader
	file, err := os.Open(wappPath)
	if err != nil {
		t.Fatalf("Failed to open wapp: %v", err)
	}
	defer file.Close()

	pr, err := NewPackReader(file, nil)
	if err != nil {
		t.Fatalf("Failed to create PackReader: %v", err)
	}

	// Test Reader() method
	reader := pr.Reader()
	if reader == nil {
		t.Error("Reader() returned nil")
	}
}

func TestWaitForListenerReadiness_NoCoordinator(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	err := waitForListenerReadiness(ctx, logger)
	if err != nil {
		t.Fatalf("waitForListenerReadiness returned error without coordinator: %v", err)
	}
}

func TestWaitForListenerReadiness_PendingCompletes(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	ready := bootpkg.NewReadiness()
	ready.Add(1)
	ctx = bootpkg.WithReadiness(ctx, ready)

	done := make(chan error, 1)
	go func() {
		done <- waitForListenerReadiness(ctx, logger)
	}()

	// Give waiter a chance to block, then complete readiness.
	time.Sleep(10 * time.Millisecond)
	ready.Done()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waitForListenerReadiness returned error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("waitForListenerReadiness did not finish after readiness completion")
	}
}

func TestWaitForListenerReadiness_ContextCancelled(t *testing.T) {
	ctx := setupTestContext(t)
	logger := zap.NewNop()

	ready := bootpkg.NewReadiness()
	ready.Add(1)
	ctx = bootpkg.WithReadiness(ctx, ready)

	cancelCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	err := waitForListenerReadiness(cancelCtx, logger)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
}
