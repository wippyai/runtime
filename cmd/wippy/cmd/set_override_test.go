// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

func TestCoerceSetValue(t *testing.T) {
	assert.Equal(t, true, coerceSetValue("true"))
	assert.Equal(t, false, coerceSetValue("false"))
	assert.Equal(t, 3, coerceSetValue("3"))
	assert.Equal(t, 7946, coerceSetValue("7946"))
	assert.Equal(t, 0.5, coerceSetValue("0.5"))
	assert.Equal(t, "5s", coerceSetValue("5s")) // duration stays string (GetDuration parses)
	assert.Equal(t, "node-2:7946", coerceSetValue("node-2:7946"))
	assert.Equal(t, "", coerceSetValue(""))
}

func setTestClusterConfig() boot.Config {
	return boot.NewConfig(boot.WithSection("cluster", map[string]any{
		"enabled":               false,
		"membership.bind_port":  7946,
		"membership.join_addrs": "seed:7946",
	}))
}

func TestApplySetOverrides_OverridesLeafPreservesRest(t *testing.T) {
	cfg, err := applySetOverrides(setTestClusterConfig(), []string{"cluster.enabled=true"})
	require.NoError(t, err)
	assert.True(t, cfg.GetBool("cluster.enabled", false))
	// sibling leaves in the same section are preserved, not wiped
	assert.Equal(t, 7946, cfg.GetInt("cluster.membership.bind_port", 0))
	assert.Equal(t, "seed:7946", cfg.GetString("cluster.membership.join_addrs", ""))
}

func TestApplySetOverrides_NestedPath(t *testing.T) {
	cfg, err := applySetOverrides(boot.NewConfig(), []string{"cluster.raft.bootstrap_expect=3"})
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.Sub("cluster").GetInt("raft.bootstrap_expect", 1))
}

func TestApplySetOverrides_OverrideWinsOverFile(t *testing.T) {
	cfg, err := applySetOverrides(setTestClusterConfig(), []string{
		"cluster.enabled=true",
		"cluster.membership.bind_port=9999",
	})
	require.NoError(t, err)
	assert.True(t, cfg.GetBool("cluster.enabled", false))
	assert.Equal(t, 9999, cfg.GetInt("cluster.membership.bind_port", 0))
}

func TestApplySetOverrides_DurationValue(t *testing.T) {
	cfg, err := applySetOverrides(boot.NewConfig(), []string{"cluster.raft.reconcile_debounce=10s"})
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, cfg.Sub("cluster").GetDuration("raft.reconcile_debounce", 0))
}

func TestApplySetOverrides_Malformed(t *testing.T) {
	for _, bad := range []string{"clusterenabled", "=true", "nodotkey=1", "cluster.=1"} {
		_, err := applySetOverrides(boot.NewConfig(), []string{bad})
		assert.Error(t, err, "expected error for %q", bad)
	}
}
