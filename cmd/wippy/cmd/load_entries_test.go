package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contextapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/boot/deps/lock"
	transcoder "github.com/wippyai/runtime/system/payload"
	yamlpayload "github.com/wippyai/runtime/system/payload/yaml"
	"go.uber.org/zap"
)

func TestLoadEntriesFromLockPaths_NilLock(t *testing.T) {
	loaded, err := loadEntriesFromLockPaths(context.Background(), nil, zap.NewNop())
	if err != nil {
		t.Fatalf("loadEntriesFromLockPaths returned error: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil entries for nil lock, got %d", len(loaded))
	}
}

func TestLoadEntriesFromLockPaths_ResolvesRequirementByModuleMeta(t *testing.T) {
	ctx := setupLoaderContext(t)
	logger := zap.NewNop()
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "app")
	moduleDir := filepath.Join(tmpDir, ".wippy", "vendor", "userspace", "users")

	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
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

	lockPath := filepath.Join(tmpDir, lock.DefaultFilename)
	lockObj, err := lock.New(lockPath)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}
	lockObj.SetDirectories(lock.Directories{
		Modules: ".wippy",
		Src:     "app",
	})
	lockObj.SetModule(lock.Module{
		Name:    "userspace/users",
		Version: "v1.0.0",
	})
	if err := lockObj.Write(); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	loaded, err := loadEntriesFromLockPaths(ctx, lockObj, logger)
	if err != nil {
		t.Fatalf("loadEntriesFromLockPaths failed: %v", err)
	}

	router := ""
	module := ""
	moduleVersion := ""
	for _, entry := range loaded {
		if entry.ID.String() != "userspace.user:login.endpoint" {
			continue
		}
		router = entry.Meta.GetString("router", "")
		module = entry.Meta.GetString("module", "")
		moduleVersion = entry.Meta.GetString("module_version", "")
	}

	if router != "app:api.public" {
		t.Fatalf("router = %q, want app:api.public", router)
	}
	if module != "userspace/users" {
		t.Fatalf("module = %q, want userspace/users", module)
	}
	if moduleVersion != "v1.0.0" {
		t.Fatalf("module_version = %q, want v1.0.0", moduleVersion)
	}
}

func setupLoaderContext(t *testing.T) context.Context {
	t.Helper()

	ctx := context.Background()
	appCtx := contextapi.NewAppContext()
	ctx = contextapi.WithAppContext(ctx, appCtx)

	logger := zap.NewNop()
	ctx = logapi.WithLogger(ctx, logger)

	dtt := transcoder.GlobalTranscoder()
	yamlpayload.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	loaderComponent := core.Loader()
	loadedCtx, err := loaderComponent.Load(ctx)
	if err != nil {
		t.Fatalf("loader component load failed: %v", err)
	}

	return loadedCtx
}
