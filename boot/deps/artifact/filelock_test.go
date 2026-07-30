// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"errors"
	"testing"
)

func TestArtifactLockSerializesMaterializers(t *testing.T) {
	root := t.TempDir()
	unlock, err := acquireArtifactLock(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireArtifactLock(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("second lock error = %v, want context cancellation", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}

	unlock, err = acquireArtifactLock(context.Background(), root)
	if err != nil {
		t.Fatalf("reacquire lock: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}
