// SPDX-License-Identifier: MPL-2.0
package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

func TestOverrideFlagsPreserveIndependentDeclarations(t *testing.T) {
	base := boot.NewConfig(boot.WithSection("override", map[string]any{"app:actor:limits.memory_bytes": 65536, "app:gateway:addr": ":8080"}))
	cfg, err := applyOverrideFlags(base, []string{"app:actor:options.limits.memory_bytes=131072"}, nil)
	require.NoError(t, err)
	layers := boot.ConfigLayers(cfg.Sub("override"))
	require.Len(t, layers, 2)
	_, present := layers[1].Get("app:actor:limits.memory_bytes")
	require.False(t, present, "CLI layer must not redeclare inherited aliases")
	_, present = layers[1].Get("app:gateway:addr")
	require.False(t, present)
	require.Equal(t, ":8080", cfg.GetString("override.app:gateway:addr", ""))
	_, present = base.Get("override.app:actor:options.limits.memory_bytes")
	require.False(t, present, "input must remain unchanged")
	require.Equal(t, 131072, cfg.GetInt("override.app:actor:options.limits.memory_bytes", 0))
}
