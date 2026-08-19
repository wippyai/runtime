// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyAccessContext(t *testing.T) {
	require.Equal(t, DependencyAccessUnspecified, DependencyAccessFromContext(context.Background()))

	ctx := WithDependencyAccess(context.Background(), DependencyAccessVerifiedOffline)
	require.Equal(t, DependencyAccessVerifiedOffline, DependencyAccessFromContext(ctx))
	require.Equal(t, DependencyAccessUnspecified, DependencyAccessFromContext(nil))
}
