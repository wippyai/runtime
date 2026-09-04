// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestWrapExecErrorPreservesMetadataAndDefaultsMissingFields(t *testing.T) {
	l := setupState()
	defer l.Close()

	typed := lua.NewError("unavailable").WithKind(lua.Unavailable).WithRetryable(true)
	wrapped := wrapExecError(l, typed, "start process", lua.Internal)
	require.Equal(t, lua.Unavailable, wrapped.Kind())
	require.Equal(t, lua.TernaryTrue, wrapped.Retryable())

	wrapped = wrapExecError(l, errors.New("plain failure"), "start process", lua.Internal)
	require.Equal(t, lua.Internal, wrapped.Kind())
	require.Equal(t, lua.TernaryFalse, wrapped.Retryable())
}
