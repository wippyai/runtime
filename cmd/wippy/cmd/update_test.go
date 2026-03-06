// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

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

func TestPreserveReplacementsForPresentModules_FiltersStale(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	lockObj, err := lock.New(lockPath)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}

	lockObj.SetModule(lock.Module{Name: "acme/http", Version: "1.0.0"})

	preserveReplacementsForPresentModules(lockObj, []lock.Replacement{
		{From: "acme/http", To: "../local-http"},
		{From: "demo/sql", To: "../local-sql"},
	})

	repls := lockObj.GetReplacements()
	if len(repls) != 1 {
		t.Fatalf("replacement count = %d, want 1", len(repls))
	}
	if repls[0].From != "acme/http" {
		t.Fatalf("replacement.from = %q, want acme/http", repls[0].From)
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
