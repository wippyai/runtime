// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func TestShouldBypassInstallCache(t *testing.T) {
	t.Run("returns false when no flags exist", func(t *testing.T) {
		c := &cobra.Command{Use: "test"}
		if shouldBypassInstallCache(c) {
			t.Fatal("expected cache bypass to be false")
		}
	})

	t.Run("returns true for refresh", func(t *testing.T) {
		c := &cobra.Command{Use: "test"}
		c.Flags().Bool("refresh", false, "")
		if err := c.Flags().Set("refresh", "true"); err != nil {
			t.Fatalf("set refresh flag: %v", err)
		}
		if !shouldBypassInstallCache(c) {
			t.Fatal("expected cache bypass to be true for --refresh")
		}
	})

	t.Run("returns true for force alias", func(t *testing.T) {
		c := &cobra.Command{Use: "test"}
		c.Flags().Bool("force", false, "")
		if err := c.Flags().Set("force", "true"); err != nil {
			t.Fatalf("set force flag: %v", err)
		}
		if !shouldBypassInstallCache(c) {
			t.Fatal("expected cache bypass to be true for --force")
		}
	})

	t.Run("returns true for repair alias", func(t *testing.T) {
		c := &cobra.Command{Use: "test"}
		c.Flags().Bool("repair", false, "")
		if err := c.Flags().Set("repair", "true"); err != nil {
			t.Fatalf("set repair flag: %v", err)
		}
		if !shouldBypassInstallCache(c) {
			t.Fatal("expected cache bypass to be true for --repair")
		}
	})
}

func TestSelectInstallModulesSkipsReplacements(t *testing.T) {
	tmpDir := t.TempDir()
	lockObj, err := lock.New(filepath.Join(tmpDir, lock.DefaultFilename))
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}

	lockObj.SetModule(lock.Module{Name: "acme/ui", Version: "v1.0.0"})
	lockObj.SetModule(lock.Module{Name: "wippy/dataflow", Version: "v0.4.10"})
	lockObj.SetReplacement(lock.Replacement{From: "acme/ui", To: "../local-ui"})

	t.Run("default install selects only remote modules", func(t *testing.T) {
		selection := selectInstallModules(lockObj, nil, zap.NewNop())

		if selection.matched != 2 {
			t.Fatalf("matched = %d, want 2", selection.matched)
		}
		if selection.skippedReplaced != 1 {
			t.Fatalf("skippedReplaced = %d, want 1", selection.skippedReplaced)
		}
		if len(selection.modules) != 1 {
			t.Fatalf("selected modules = %d, want 1", len(selection.modules))
		}
		if selection.modules[0].Name != "wippy/dataflow" {
			t.Fatalf("selected module = %q, want wippy/dataflow", selection.modules[0].Name)
		}
	})

	t.Run("explicit replaced module is matched but not installed", func(t *testing.T) {
		selection := selectInstallModules(lockObj, []string{"acme/ui"}, zap.NewNop())

		if selection.matched != 1 {
			t.Fatalf("matched = %d, want 1", selection.matched)
		}
		if selection.skippedReplaced != 1 {
			t.Fatalf("skippedReplaced = %d, want 1", selection.skippedReplaced)
		}
		if len(selection.modules) != 0 {
			t.Fatalf("selected modules = %d, want 0", len(selection.modules))
		}
	})

	t.Run("explicit remote module is selected", func(t *testing.T) {
		selection := selectInstallModules(lockObj, []string{"wippy/dataflow"}, zap.NewNop())

		if selection.matched != 1 {
			t.Fatalf("matched = %d, want 1", selection.matched)
		}
		if selection.skippedReplaced != 0 {
			t.Fatalf("skippedReplaced = %d, want 0", selection.skippedReplaced)
		}
		if len(selection.modules) != 1 || selection.modules[0].Name != "wippy/dataflow" {
			t.Fatalf("selected modules = %+v, want only wippy/dataflow", selection.modules)
		}
	})
}
