// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	regapi "github.com/wippyai/runtime/api/registry"
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
	loaded, _, err := loadEntriesFromLockPaths(context.Background(), nil, zap.NewNop())
	if err != nil {
		t.Fatalf("loadEntriesFromLockPaths returned error: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil entries for nil lock, got %d", len(loaded))
	}
}

func TestLoadEntriesFromLockPaths_ResolvesDeclaredModuleNamespace(t *testing.T) {
	ctx := setupLoaderContext(t)
	logger := zap.NewNop()
	tmpDir := t.TempDir()

	appDir := filepath.Join(tmpDir, "app")
	moduleDir := filepath.Join(tmpDir, ".wippy", "vendor", "example", "accounts")

	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir module dir: %v", err)
	}

	appYAML := `version: "1.0"
namespace: app.deps
entries:
  - name: accounts
    kind: ns.dependency
    component: example/accounts
    parameters:
      - name: public_router
        value: app:api.public
`
	if err := os.WriteFile(filepath.Join(appDir, "_index.yaml"), []byte(appYAML), 0o644); err != nil {
		t.Fatalf("write app _index.yaml: %v", err)
	}

	moduleYAML := `version: "1.0"
namespace: identity.account
entries:
  - name: definition
    kind: ns.definition
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
		Name:    "example/accounts",
		Version: "v1.0.0",
	})
	if err := lockObj.Write(); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	loaded, prov, err := loadEntriesFromLockPaths(ctx, lockObj, logger)
	if err != nil {
		t.Fatalf("loadEntriesFromLockPaths failed: %v", err)
	}

	router := ""
	var entryProv regapi.EntryProvenance
	for _, entry := range loaded {
		if entry.ID.String() != "identity.account:login.endpoint" {
			continue
		}
		router = entry.Meta.GetString("router", "")
		entryProv = prov[entry.ID.Canonical()]
	}

	if router != "app:api.public" {
		t.Fatalf("router = %q, want app:api.public", router)
	}
	if entryProv.Module != "example/accounts" {
		t.Fatalf("module provenance = %q, want example/accounts", entryProv.Module)
	}
	if entryProv.Version != "v1.0.0" {
		t.Fatalf("module_version provenance = %q, want v1.0.0", entryProv.Version)
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
