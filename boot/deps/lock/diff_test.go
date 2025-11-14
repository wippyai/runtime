package lock

import (
	"path/filepath"
	"testing"
)

func TestDiff(t *testing.T) {
	t.Run("empty lock files produce no changes", func(t *testing.T) {
		tmpDir := t.TempDir()
		old, _ := New(filepath.Join(tmpDir, "old.lock"))
		new, _ := New(filepath.Join(tmpDir, "new.lock"))

		changes := Diff(old, new)

		if len(changes.Installed) != 0 {
			t.Errorf("expected 0 installed, got %d", len(changes.Installed))
		}
		if len(changes.Updated) != 0 {
			t.Errorf("expected 0 updated, got %d", len(changes.Updated))
		}
		if len(changes.Removed) != 0 {
			t.Errorf("expected 0 removed, got %d", len(changes.Removed))
		}
	})

	t.Run("detects newly installed modules", func(t *testing.T) {
		tmpDir := t.TempDir()
		old, _ := New(filepath.Join(tmpDir, "old.lock"))
		new, _ := New(filepath.Join(tmpDir, "new.lock"))
		new.SetModule(Module{Name: "wippy/test", Version: "v1.0.0"})

		changes := Diff(old, new)

		if len(changes.Installed) != 1 {
			t.Fatalf("expected 1 installed, got %d", len(changes.Installed))
		}
		if changes.Installed[0].Name != "wippy/test" {
			t.Errorf("expected wippy/test, got %s", changes.Installed[0].Name)
		}
		if len(changes.Updated) != 0 {
			t.Errorf("expected 0 updated, got %d", len(changes.Updated))
		}
		if len(changes.Removed) != 0 {
			t.Errorf("expected 0 removed, got %d", len(changes.Removed))
		}
	})

	t.Run("detects removed modules", func(t *testing.T) {
		tmpDir := t.TempDir()
		old, _ := New(filepath.Join(tmpDir, "old.lock"))
		old.SetModule(Module{Name: "wippy/test", Version: "v1.0.0"})
		new, _ := New(filepath.Join(tmpDir, "new.lock"))

		changes := Diff(old, new)

		if len(changes.Removed) != 1 {
			t.Fatalf("expected 1 removed, got %d", len(changes.Removed))
		}
		if changes.Removed[0].Name != "wippy/test" {
			t.Errorf("expected wippy/test, got %s", changes.Removed[0].Name)
		}
		if len(changes.Installed) != 0 {
			t.Errorf("expected 0 installed, got %d", len(changes.Installed))
		}
		if len(changes.Updated) != 0 {
			t.Errorf("expected 0 updated, got %d", len(changes.Updated))
		}
	})

	t.Run("detects version updates", func(t *testing.T) {
		tmpDir := t.TempDir()
		old, _ := New(filepath.Join(tmpDir, "old.lock"))
		old.SetModule(Module{Name: "wippy/test", Version: "v1.0.0"})
		new, _ := New(filepath.Join(tmpDir, "new.lock"))
		new.SetModule(Module{Name: "wippy/test", Version: "v2.0.0"})

		changes := Diff(old, new)

		if len(changes.Updated) != 1 {
			t.Fatalf("expected 1 updated, got %d", len(changes.Updated))
		}
		if changes.Updated[0].Name != "wippy/test" {
			t.Errorf("expected wippy/test, got %s", changes.Updated[0].Name)
		}
		if changes.Updated[0].OldVersion != "v1.0.0" {
			t.Errorf("expected old version v1.0.0, got %s", changes.Updated[0].OldVersion)
		}
		if changes.Updated[0].NewVersion != "v2.0.0" {
			t.Errorf("expected new version v2.0.0, got %s", changes.Updated[0].NewVersion)
		}
		if len(changes.Installed) != 0 {
			t.Errorf("expected 0 installed, got %d", len(changes.Installed))
		}
		if len(changes.Removed) != 0 {
			t.Errorf("expected 0 removed, got %d", len(changes.Removed))
		}
	})

	t.Run("detects hash updates", func(t *testing.T) {
		tmpDir := t.TempDir()
		old, _ := New(filepath.Join(tmpDir, "old.lock"))
		old.SetModule(Module{Name: "wippy/test", Version: "v1.0.0", Hash: "abc123"})
		new, _ := New(filepath.Join(tmpDir, "new.lock"))
		new.SetModule(Module{Name: "wippy/test", Version: "v1.0.0", Hash: "def456"})

		changes := Diff(old, new)

		if len(changes.Updated) != 1 {
			t.Fatalf("expected 1 updated, got %d", len(changes.Updated))
		}
		if changes.Updated[0].OldHash != "abc123" {
			t.Errorf("expected old hash abc123, got %s", changes.Updated[0].OldHash)
		}
		if changes.Updated[0].NewHash != "def456" {
			t.Errorf("expected new hash def456, got %s", changes.Updated[0].NewHash)
		}
	})

	t.Run("no change when versions and hashes match", func(t *testing.T) {
		tmpDir := t.TempDir()
		old, _ := New(filepath.Join(tmpDir, "old.lock"))
		old.SetModule(Module{Name: "wippy/test", Version: "v1.0.0", Hash: "abc123"})
		new, _ := New(filepath.Join(tmpDir, "new.lock"))
		new.SetModule(Module{Name: "wippy/test", Version: "v1.0.0", Hash: "abc123"})

		changes := Diff(old, new)

		if len(changes.Installed) != 0 {
			t.Errorf("expected 0 installed, got %d", len(changes.Installed))
		}
		if len(changes.Updated) != 0 {
			t.Errorf("expected 0 updated, got %d", len(changes.Updated))
		}
		if len(changes.Removed) != 0 {
			t.Errorf("expected 0 removed, got %d", len(changes.Removed))
		}
	})

	t.Run("handles multiple changes simultaneously", func(t *testing.T) {
		tmpDir := t.TempDir()
		old, _ := New(filepath.Join(tmpDir, "old.lock"))
		old.SetModule(Module{Name: "wippy/actor", Version: "v1.0.0"})
		old.SetModule(Module{Name: "wippy/llm", Version: "v0.0.11"})
		old.SetModule(Module{Name: "wippy/old", Version: "v1.0.0"})

		new, _ := New(filepath.Join(tmpDir, "new.lock"))
		new.SetModule(Module{Name: "wippy/actor", Version: "v2.0.0"})
		new.SetModule(Module{Name: "wippy/llm", Version: "v0.0.11"})
		new.SetModule(Module{Name: "wippy/new", Version: "v1.0.0"})

		changes := Diff(old, new)

		if len(changes.Installed) != 1 {
			t.Errorf("expected 1 installed, got %d", len(changes.Installed))
		}
		if len(changes.Updated) != 1 {
			t.Errorf("expected 1 updated, got %d", len(changes.Updated))
		}
		if len(changes.Removed) != 1 {
			t.Errorf("expected 1 removed, got %d", len(changes.Removed))
		}

		if changes.Installed[0].Name != "wippy/new" {
			t.Errorf("expected wippy/new installed, got %s", changes.Installed[0].Name)
		}
		if changes.Updated[0].Name != "wippy/actor" {
			t.Errorf("expected wippy/actor updated, got %s", changes.Updated[0].Name)
		}
		if changes.Removed[0].Name != "wippy/old" {
			t.Errorf("expected wippy/old removed, got %s", changes.Removed[0].Name)
		}
	})
}
