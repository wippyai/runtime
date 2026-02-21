package cmd

import (
	"testing"

	"github.com/spf13/cobra"
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
