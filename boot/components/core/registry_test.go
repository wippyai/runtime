// SPDX-License-Identifier: MPL-2.0

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

func TestReadKindSlice_InvalidTypeDoesNotOverrideDefaults(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryDispatchInternalKinds: 42,
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.False(t, ok)
	assert.Nil(t, kinds)
}

func TestReadKindSlice_ValidList(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryDispatchInternalKinds: []string{"registry.entry", "ns.dependency"},
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.True(t, ok)
	assert.Equal(t, []string{"registry.entry", "ns.dependency"}, kinds)
}

func TestReadKindSlice_MixedAnyValues(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryDispatchInternalKinds: []any{"registry.entry", 7, "ns.definition"},
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.True(t, ok)
	assert.Equal(t, []string{"registry.entry", "ns.definition"}, kinds)
}

func TestCoreDependencyPatternsIncludeExplicitMetadataAndLifecycleRefs(t *testing.T) {
	patterns := append(getDefaultDependencyPatterns(), getLifecycleDependencyPatterns()...)
	paths := make(map[string]bool, len(patterns))
	for _, pattern := range patterns {
		paths[pattern.Path] = pattern.AllowWildcard
	}

	require.Contains(t, paths, "meta.depends_on")
	require.True(t, paths["meta.depends_on"])
	require.Contains(t, paths, "data.*.depends_on")
	require.True(t, paths["data.*.depends_on"])
	require.Contains(t, paths, "data.lifecycle.requires")
	require.True(t, paths["data.lifecycle.requires"])
	require.Contains(t, paths, "data.lifecycle.depends_on")
	require.True(t, paths["data.lifecycle.depends_on"])
}
