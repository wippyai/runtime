// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyAccessDefaultsToVerifiedOffline(t *testing.T) {
	require.Equal(t, DependencyAccessVerifiedOffline, DependencyAccessFromContext(context.Background()))
	require.Equal(t, DependencyAccessVerifiedOffline, DependencyAccessFromContext(nil))
	require.False(t, DependencyDownloadsAllowed(context.Background()))
	require.False(t, DependencyDownloadsAllowed(nil))
}

func TestDependencyAccessCarriesGrantedPolicy(t *testing.T) {
	offline := WithDependencyAccess(context.Background(), DependencyAccessVerifiedOffline)
	require.Equal(t, DependencyAccessVerifiedOffline, DependencyAccessFromContext(offline))
	require.False(t, DependencyDownloadsAllowed(offline))

	online := WithDependencyAccess(context.Background(), DependencyAccessOnline)
	require.Equal(t, DependencyAccessOnline, DependencyAccessFromContext(online))
	require.True(t, DependencyDownloadsAllowed(online))
}

func TestDependencyAccessRejectsUnknownPolicy(t *testing.T) {
	ctx := WithDependencyAccess(context.Background(), DependencyAccess(200))
	require.Equal(t, DependencyAccessVerifiedOffline, DependencyAccessFromContext(ctx))
	require.False(t, DependencyDownloadsAllowed(ctx))
}

func TestDependencyAccessIgnoresForeignValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), dependencyAccessContextKey{}, "online")
	require.Equal(t, DependencyAccessVerifiedOffline, DependencyAccessFromContext(ctx))
}
