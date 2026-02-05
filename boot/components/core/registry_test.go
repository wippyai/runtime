package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/boot"
)

func TestReadKindSlice_InvalidTypeDoesNotOverrideDefaults(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(string(RegistryName), map[string]any{
		string(RegistryDispatchInternalKinds): 42,
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.False(t, ok)
	assert.Nil(t, kinds)
}

func TestReadKindSlice_ValidList(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(string(RegistryName), map[string]any{
		string(RegistryDispatchInternalKinds): []string{"registry.entry", "ns.dependency"},
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.True(t, ok)
	assert.Equal(t, []string{"registry.entry", "ns.dependency"}, []string(kinds))
}

func TestReadKindSlice_MixedAnyValues(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(string(RegistryName), map[string]any{
		string(RegistryDispatchInternalKinds): []any{"registry.entry", 7, "ns.definition"},
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.True(t, ok)
	assert.Equal(t, []string{"registry.entry", "ns.definition"}, []string(kinds))
}
