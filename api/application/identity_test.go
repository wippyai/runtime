// SPDX-License-Identifier: MPL-2.0

package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityContext(t *testing.T) {
	_, ok := FromContext(context.Background())
	require.False(t, ok)
	want := Identity{Module: "acme/app", Version: "1.2.3"}
	ctx := WithIdentity(context.Background(), want)
	got, ok := FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, want, got)
	got, ok = FromContext(context.WithValue(ctx, struct{}{}, "unrelated"))
	require.True(t, ok)
	require.Equal(t, want, got)
}
