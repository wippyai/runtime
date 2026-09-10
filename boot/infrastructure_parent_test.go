// SPDX-License-Identifier: MPL-2.0
package boot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	"go.uber.org/zap"
)

func TestBootstrapContextPreservesParentLifetime(t *testing.T) {
	type testKey struct{}
	deadline := time.Now().Add(time.Minute)
	parent, cancel := context.WithDeadline(context.WithValue(context.Background(), testKey{}, "parent"), deadline)
	defer cancel()
	ctx, err := NewBootstrapContextWithParent(parent, zap.NewNop(), bootapi.NewConfig())
	require.NoError(t, err)
	require.Equal(t, "parent", ctx.Value(testKey{}))
	actual, ok := ctx.Deadline()
	require.True(t, ok)
	require.Equal(t, deadline, actual)
	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestBootstrapContextRejectsUnavailableParent(t *testing.T) {
	_, err := NewBootstrapContextWithParent(nil, zap.NewNop(), nil)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewBootstrapContextWithParent(ctx, zap.NewNop(), nil)
	require.ErrorIs(t, err, context.Canceled)
}
