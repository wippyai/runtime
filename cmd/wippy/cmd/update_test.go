// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	bootapi "github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func TestConvertResolvedToLock_UsesProvidedPath(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "custom.lock")

	lockObj, err := convertResolvedToLock(lockPath, []hub.ResolvedModule{
		{
			Org:     "acme",
			Name:    "http",
			Version: "1.2.3",
			Digest:  "deadbeef",
		},
	}, ".wippy", ".")
	if err != nil {
		t.Fatalf("convertResolvedToLock failed: %v", err)
	}

	expectedPath, _ := filepath.Abs(lockPath)
	if lockObj.Path() != expectedPath {
		t.Fatalf("lock path = %q, want %q", lockObj.Path(), expectedPath)
	}

	mod, ok := lockObj.GetModule("acme/http")
	if !ok {
		t.Fatal("expected module acme/http")
	}
	if mod.Version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", mod.Version)
	}
	if mod.Hash != "deadbeef" {
		t.Fatalf("hash = %q, want deadbeef", mod.Hash)
	}
}

func TestPreserveReplacements_KeepsAll(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	lockObj, err := lock.New(lockPath)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}

	lockObj.SetModule(lock.Module{Name: "acme/http", Version: "1.0.0"})

	preserveReplacements(lockObj, []lock.Replacement{
		{From: "acme/http", To: "../local-http"},
		{From: "demo/sql", To: "../local-sql"},
	})

	repls := lockObj.GetReplacements()
	if len(repls) != 2 {
		t.Fatalf("replacement count = %d, want 2", len(repls))
	}
	if repls[0].From != "acme/http" {
		t.Fatalf("replacement[0].from = %q, want acme/http", repls[0].From)
	}
	if repls[1].From != "demo/sql" {
		t.Fatalf("replacement[1].from = %q, want demo/sql", repls[1].From)
	}
}

func TestPruneStaleVendorArtifacts_RemovesStaleArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	lockObj, err := lock.New(lockPath)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}

	vendorDir := filepath.Join(tmpDir, ".wippy", "vendor")

	// Removed module artifacts.
	removedDir := filepath.Join(vendorDir, "userspace", "users")
	removedLegacyDir := filepath.Join(vendorDir, "userspace", "users-v1.0.0")
	removedWapp := filepath.Join(vendorDir, "userspace", "users-v1.0.0.wapp")
	mustWriteFile(t, filepath.Join(removedDir, "keep.txt"))
	mustWriteFile(t, filepath.Join(removedLegacyDir, "keep.txt"))
	mustWriteFile(t, removedWapp)

	// Updated module artifacts.
	updatedDir := filepath.Join(vendorDir, "demo", "sql")
	updatedLegacyDir := filepath.Join(vendorDir, "demo", "sql-v1.0.0")
	updatedOldWapp := filepath.Join(vendorDir, "demo", "sql-v1.0.0.wapp")
	mustWriteFile(t, filepath.Join(updatedDir, "current.txt"))
	mustWriteFile(t, filepath.Join(updatedLegacyDir, "old.txt"))
	mustWriteFile(t, updatedOldWapp)

	changes := &lock.Changes{
		Removed: []lock.Module{
			{Name: "userspace/users", Version: "v1.0.0"},
		},
		Updated: []lock.ModuleChange{
			{Name: "demo/sql", OldVersion: "v1.0.0", NewVersion: "v2.0.0"},
		},
	}

	pruneStaleVendorArtifacts(lockObj, changes, zap.NewNop())

	assertPathMissing(t, removedDir)
	assertPathMissing(t, removedLegacyDir)
	assertPathMissing(t, removedWapp)
	assertPathMissing(t, updatedDir)
	assertPathMissing(t, updatedLegacyDir)
	assertPathMissing(t, updatedOldWapp)
}

func TestLoadDependencyScanEntriesIncludesReplacementSources(t *testing.T) {
	ctx := setupLoaderContext(t)
	ldr := bootapi.GetLoader(ctx)
	if ldr == nil {
		t.Fatal("loader not available in test context")
	}

	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "app")
	replacementDir := filepath.Join(tmpDir, "local", "ui")
	replacementSrcDir := filepath.Join(replacementDir, "src")
	for _, dir := range []string{appDir, replacementSrcDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	appYAML := `version: "1.0"
namespace: app.deps
entries:
  - name: facade
    kind: ns.dependency
    component: wippy/facade
    version: ">=v0.5.39"
`
	if err := os.WriteFile(filepath.Join(appDir, "_index.yaml"), []byte(appYAML), 0o644); err != nil {
		t.Fatalf("write app index: %v", err)
	}

	replacementYAML := `version: "1.0"
namespace: local.ui.deps
entries:
  - name: dataflow
    kind: ns.dependency
    component: wippy/dataflow
    version: ">=v0.4.10"
`
	if err := os.WriteFile(filepath.Join(replacementSrcDir, "_index.yaml"), []byte(replacementYAML), 0o644); err != nil {
		t.Fatalf("write replacement index: %v", err)
	}

	nodeModulesYAML := `version: "1.0"
namespace: should.not.load
entries:
  - name: ignored
    kind: ns.dependency
    component: bad/module
`
	if err := os.MkdirAll(filepath.Join(replacementDir, "frontend", "node_modules", "noise"), 0o755); err != nil {
		t.Fatalf("mkdir replacement node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(replacementDir, "frontend", "node_modules", "noise", "_index.yaml"), []byte(nodeModulesYAML), 0o644); err != nil {
		t.Fatalf("write replacement noise index: %v", err)
	}

	lockPath := filepath.Join(tmpDir, lock.DefaultFilename)
	lockObj, err := lock.New(lockPath)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "app"})
	lockObj.SetModule(lock.Module{Name: "acme/ui", Version: "v1.0.0"})
	lockObj.SetReplacement(lock.Replacement{From: "acme/ui", To: "local/ui"})

	loaded, err := loadDependencyScanEntries(ctx, ldr, appDir, lockObj, zap.NewNop())
	if err != nil {
		t.Fatalf("loadDependencyScanEntries failed: %v", err)
	}

	deps := extractRootDependencies(loaded, payload.GetTranscoder(ctx))
	got := map[string]string{}
	for _, dep := range deps {
		got[dep.Org+"/"+dep.Module] = dep.Constraint
	}

	if got["wippy/facade"] != ">=v0.5.39" {
		t.Fatalf("app dependency missing: got %v", got)
	}
	if got["wippy/dataflow"] != ">=v0.4.10" {
		t.Fatalf("replacement dependency missing: got %v", got)
	}
	if _, ok := got["bad/module"]; ok {
		t.Fatalf("replacement scan should use src subtree and ignore node_modules noise: got %v", got)
	}
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected path to be removed: %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}
