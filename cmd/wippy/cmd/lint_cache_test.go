// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

func TestLintCacheEnabled(t *testing.T) {
	store := cache.NewDiskStore(t.TempDir())

	tests := []struct {
		name   string
		lcache lintCache
		want   bool
	}{
		{
			name: "enabled with store and identity",
			lcache: lintCache{
				store: store,
				cfg: cache.Config{
					Enabled:           true,
					ToolchainIdentity: "test-toolchain-identity",
				},
			},
			want: true,
		},
		{
			name: "enabled with store and empty identity",
			lcache: lintCache{
				store: store,
				cfg: cache.Config{
					Enabled:           true,
					ToolchainIdentity: "",
				},
			},
			want: false,
		},
		{
			name: "disabled with store and identity",
			lcache: lintCache{
				store: store,
				cfg: cache.Config{
					Enabled:           false,
					ToolchainIdentity: "test-toolchain-identity",
				},
			},
			want: false,
		},
		{
			name: "disabled with store and empty identity",
			lcache: lintCache{
				store: store,
				cfg: cache.Config{
					Enabled:           false,
					ToolchainIdentity: "",
				},
			},
			want: false,
		},
		{
			name: "enabled without store",
			lcache: lintCache{
				store: nil,
				cfg: cache.Config{
					Enabled:           true,
					ToolchainIdentity: "test-toolchain-identity",
				},
			},
			want: false,
		},
		{
			name: "disabled without store",
			lcache: lintCache{
				store: nil,
				cfg: cache.Config{
					Enabled:           false,
					ToolchainIdentity: "test-toolchain-identity",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, lintCacheEnabled(tt.lcache))
		})
	}
}
