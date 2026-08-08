// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRollbackPendingRetriesRestoration(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "npm")
	backup := filepath.Join(root, ".npm.artifact-backup-test")
	staging := filepath.Join(root, ".npm.artifact-stage-test")
	for path, content := range map[string]string{
		destination: "new",
		backup:      "old",
		staging:     "staged",
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "value"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	effect := &Effect{
		root: root,
		activated: []activatedRoot{{
			destination: destination,
			backup:      backup,
			hadTarget:   true,
		}},
		pending: []stagedRoot{{staging: staging}},
		state:   wappEffectRollbackPending,
	}
	if err := effect.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("restored content = %q", data)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("pending staging directory remains: %v", err)
	}
	if effect.state != wappEffectRolledBack {
		t.Fatalf("state = %d, want rolled back", effect.state)
	}
}

func TestRecoverInterruptedRootsRestoresBackupAndRemovesStage(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, ".npm.artifact-backup-test")
	stage := filepath.Join(root, ".npm.artifact-stage-test")
	for _, directory := range []string{backup, stage} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(backup, "value"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := recoverInterruptedRoots(root, []string{"npm"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "npm", "value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("restored content = %q", data)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted stage remains: %v", err)
	}
}

func TestRecoverInterruptedRootsKeepsDestinationAndRemovesBackup(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "npm")
	backup := filepath.Join(root, ".npm.artifact-backup-test")
	for path, content := range map[string]string{destination: "current", backup: "old"} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "value"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := recoverInterruptedRoots(root, []string{"npm"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "current" {
		t.Fatalf("destination content = %q", data)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed backup remains: %v", err)
	}
}

func TestRecoverInterruptedRootsTreatsFormatRootLiterally(t *testing.T) {
	root := t.TempDir()
	managedRoot := "packages[1]"
	backup := filepath.Join(root, ".packages[1].artifact-backup-test")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "value"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := recoverInterruptedRoots(root, []string{managedRoot}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, managedRoot, "value"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("restored content = %q", data)
	}
}

func TestRecoverInterruptedRootsRejectsNonDirectoryBackup(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, ".npm.artifact-backup-test")
	if err := os.WriteFile(backup, []byte("not a root"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := recoverInterruptedRoots(root, []string{"npm"})
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("recovery error = %v", err)
	}
}
