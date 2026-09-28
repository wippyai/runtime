// SPDX-License-Identifier: MPL-2.0

package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// TestDependencyRestoreErrorKeepsActionableCause proves the boot boundary
// reports a restore failure without flattening the detail a user acts on.
func TestDependencyRestoreErrorKeepsActionableCause(t *testing.T) {
	cause := apierror.New(apierror.Invalid, "verified dependency evidence is unavailable during offline startup").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"operation": "load artifact",
			"module":    "acme/worker",
			"hint":      "run an explicit wippy update/install while online, then retry startup",
		}))

	wrapped := NewDependencyRestoreError(cause)
	require.ErrorContains(t, wrapped, "failed to prepare dependency restore")
	require.ErrorIs(t, wrapped, cause)

	var recovered apierror.Error
	require.True(t, errors.As(errors.Unwrap(wrapped), &recovered))

	hint, ok := recovered.Details().Get("hint")
	require.True(t, ok, "the boot boundary must not strip the recovery hint")
	require.Contains(t, hint, "wippy update/install")

	module, ok := recovered.Details().Get("module")
	require.True(t, ok)
	require.Equal(t, "acme/worker", module)
}
