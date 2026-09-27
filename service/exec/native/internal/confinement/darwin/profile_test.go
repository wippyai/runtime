// SPDX-License-Identifier: MPL-2.0

package darwin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileProfileMatchesSupportedPolicy(t *testing.T) {
	profile := CompileProfile(Profile{AllowFork: true})
	require.Contains(t, profile, `(allow file-read* file-write* file-map-executable process-exec)`)
	require.Contains(t, profile, `(allow network*)`)
	require.Contains(t, profile, `(allow process-fork)`)
}

func TestCompileProfileCanPreventDescendants(t *testing.T) {
	profile := CompileProfile(Profile{})
	require.NotContains(t, profile, `(allow process-fork)`)
	require.Contains(t, profile, `(deny default)`)
}
