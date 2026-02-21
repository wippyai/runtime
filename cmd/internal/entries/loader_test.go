package entries

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	contextapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/core"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
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

func setupTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx := context.Background()

	appCtx := contextapi.NewAppContext()
	ctx = contextapi.WithAppContext(ctx, appCtx)

	logger, _ := zap.NewDevelopment()
	ctx = logapi.WithLogger(ctx, logger)

	dtt := transcoder.GlobalTranscoder()
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
