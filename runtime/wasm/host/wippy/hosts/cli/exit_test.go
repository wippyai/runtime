// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"context"
	"testing"
)

func TestExitHost(t *testing.T) {
	host := NewExitHost()
	if host.Namespace() != ExitNamespace {
		t.Fatalf("Namespace() = %q, want %q", host.Namespace(), ExitNamespace)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Exit() should panic with ExitError")
		}
		exitErr, ok := r.(ExitError)
		if !ok {
			t.Fatalf("panic type = %T, want ExitError", r)
		}
		if exitErr.Status != 17 {
			t.Fatalf("ExitError.Status = %d, want 17", exitErr.Status)
		}
	}()

	host.Exit(context.Background(), 17)
}
