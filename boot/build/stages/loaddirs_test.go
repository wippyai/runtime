package stages

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	transcoder "github.com/wippyai/runtime/system/payload"
	jpayload "github.com/wippyai/runtime/system/payload/json"
	ypayload "github.com/wippyai/runtime/system/payload/yaml"
	"go.uber.org/zap"
)

func setupLoadDirsContext() context.Context {
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	ypayload.Register(dtt)

	appCtx := ctxapi.NewAppContext()
	ctx := context.Background()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx = payload.WithTranscoder(ctx, dtt)
	ctx = logs.WithLogger(ctx, zap.NewNop())

	return ctx
}

func TestLoadDirs_LoadsSingleDirectory(t *testing.T) {
	ctx := setupLoadDirsContext()

	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("create app dir: %v", err)
	}

	yamlContent := `namespace: app
name: service1
kind: service
component: http/server
`
	if err := os.WriteFile(filepath.Join(appDir, "service.yaml"), []byte(yamlContent), 0600); err != nil {
		t.Fatalf("write yaml file: %v", err)
	}

	stage := LoadDirs([]string{appDir})
	entries := []registry.Entry{}

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID.Name != "service1" {
		t.Errorf("expected name service1, got %s", entries[0].ID.Name)
	}
	if entries[0].Kind != "service" {
		t.Errorf("expected kind service, got %s", entries[0].Kind)
	}
}

func TestLoadDirs_LoadsMultipleDirectories(t *testing.T) {
	ctx := setupLoadDirsContext()

	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "app")
	moduleDir := filepath.Join(tmpDir, "module")

	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("create app dir: %v", err)
	}
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		t.Fatalf("create module dir: %v", err)
	}

	appYaml := `namespace: app
name: app-service
kind: service
component: http/server
`
	moduleYaml := `namespace: mod
name: module-service
kind: service
component: sql/client
`

	if err := os.WriteFile(filepath.Join(appDir, "app.yaml"), []byte(appYaml), 0600); err != nil {
		t.Fatalf("write app yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "module.yaml"), []byte(moduleYaml), 0600); err != nil {
		t.Fatalf("write module yaml: %v", err)
	}

	stage := LoadDirs([]string{appDir, moduleDir})
	entries := []registry.Entry{}

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	hasApp := false
	hasMod := false
	for _, e := range entries {
		if e.ID.NS == "app" && e.ID.Name == "app-service" {
			hasApp = true
		}
		if e.ID.NS == "mod" && e.ID.Name == "module-service" {
			hasMod = true
		}
	}

	if !hasApp {
		t.Error("expected app-service entry")
	}
	if !hasMod {
		t.Error("expected module-service entry")
	}
}

func TestLoadDirs_SkipsNonexistentDirectory(t *testing.T) {
	ctx := setupLoadDirsContext()

	tmpDir := t.TempDir()
	existingDir := filepath.Join(tmpDir, "existing")
	missingDir := filepath.Join(tmpDir, "missing")

	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatalf("create existing dir: %v", err)
	}

	yamlContent := `namespace: app
name: service1
kind: service
component: http/server
`
	if err := os.WriteFile(filepath.Join(existingDir, "service.yaml"), []byte(yamlContent), 0600); err != nil {
		t.Fatalf("write yaml file: %v", err)
	}

	stage := LoadDirs([]string{existingDir, missingDir})
	entries := []registry.Entry{}

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() should not error on missing dir, got: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry from existing dir, got %d", len(entries))
	}
}

func TestLoadDirs_HandlesEmptyDirectoryList(t *testing.T) {
	ctx := setupLoadDirsContext()

	stage := LoadDirs([]string{})
	entries := []registry.Entry{}

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestLoadDirs_LoadsNestedYamlFiles(t *testing.T) {
	ctx := setupLoadDirsContext()

	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "app", "nested")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	rootYaml := `namespace: app
name: root-service
kind: service
`
	nestedYaml := `namespace: app
name: nested-service
kind: service
`

	if err := os.WriteFile(filepath.Join(tmpDir, "app", "root.yaml"), []byte(rootYaml), 0600); err != nil {
		t.Fatalf("write root yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "nested.yaml"), []byte(nestedYaml), 0600); err != nil {
		t.Fatalf("write nested yaml: %v", err)
	}

	stage := LoadDirs([]string{filepath.Join(tmpDir, "app")})
	entries := []registry.Entry{}

	if err := stage.Execute(ctx, &entries); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (root + nested), got %d", len(entries))
	}
}

func TestLoadDirs_ErrorsWhenTranscoderMissing(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	stage := LoadDirs([]string{tmpDir})
	entries := []registry.Entry{}

	err := stage.Execute(ctx, &entries)
	if err == nil {
		t.Fatal("expected error when transcoder missing")
	}

	if err.Error() != "transcoder not found in context" {
		t.Errorf("expected transcoder error, got: %v", err)
	}
}

func TestLoadDirs_Name(t *testing.T) {
	stage := LoadDirs([]string{})
	if stage.Name() != "loaddirs" {
		t.Errorf("expected stage name 'loaddirs', got %s", stage.Name())
	}
}
