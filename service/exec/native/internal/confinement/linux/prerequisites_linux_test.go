// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func skipHostConfinementUnavailable(t *testing.T, message string, err error) {
	t.Helper()
	if !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES) {
		return
	}
	if os.Getenv("WIPPY_REQUIRE_CONFINEMENT") == "1" {
		t.Fatalf("%s: %v", message, err)
	}
	t.Skipf("%s: %v", message, err)
}
