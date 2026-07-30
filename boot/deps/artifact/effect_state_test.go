// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"os"
	"path/filepath"
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
