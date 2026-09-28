// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"encoding/json"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

func TestOptionsCompatibility_FunctionSourceMatrix(t *testing.T) {
	base := `"fs":"app:fs","path":"/fn.wasm","hash":"sha256:0","method":"run"`
	cases := map[string]string{
		"canonical_root_options":  `{` + base + `,"options":{"limits":{"max_execution_ms":41}}}`,
		"legacy_metadata_options": `{` + base + `,"meta":{"options":{"limits":{"max_execution_ms":41}}}}`,
		"legacy_flat_limits":      `{` + base + `,"limits":{"max_execution_ms":41}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var cfg FunctionConfig
			require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
			require.NoError(t, cfg.Validate())
			assert.Equal(t, 41, cfg.Limits.MaxExecutionMS)
			if name == "canonical_root_options" {
				assert.Empty(t, cfg.DeprecatedOptionPaths())
			} else {
				assert.Len(t, cfg.DeprecatedOptionPaths(), 1)
			}
		})
	}
}

func TestOptionsCompatibility_ProcessSeparateGroupsCoexist(t *testing.T) {
	data := map[string]any{"options": map[string]any{"mailbox": map[string]any{"capacity": 64}, "worker_class": "wasm"}, "limits": map[string]any{"memory_bytes": 65536}}
	groups, diagnostics, err := NormalizeEntryOptions(ProcessWASM, data, nil)
	require.NoError(t, err)
	assert.Len(t, groups, 3)
	assert.Equal(t, []DeprecatedOptionPath{{Path: "limits", Replacement: "options.limits"}}, diagnostics)
}

func TestOptionsCompatibility_DuplicateGroupsRejectIncludingNull(t *testing.T) {
	cases := []struct {
		data map[string]any
		meta attrs.Bag
		name string
	}{
		{name: "root_and_meta", data: map[string]any{"options": map[string]any{"limits": map[string]any{}}}, meta: attrs.Bag{"options": map[string]any{"limits": map[string]any{}}}},
		{name: "legacy_null_and_root", data: map[string]any{"options": map[string]any{"limits": map[string]any{}}, "limits": nil}, meta: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := NormalizeEntryOptions(FunctionWASM, tc.data, tc.meta)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "duplicate options.limits")
		})
	}
}

func TestOptionsCompatibility_CanonicalNullAndUnsupportedSecurityReject(t *testing.T) {
	_, _, err := NormalizeEntryOptions(FunctionWASM, map[string]any{"options": nil}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "options must be an object")

	_, _, err = NormalizeEntryOptions(ProcessWASM, map[string]any{"options": map[string]any{"security": map[string]any{}}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown field "security"`)
}

func TestOptionsCompatibility_LegacyMetaNullIsAbsent(t *testing.T) {
	groups, diagnostics, err := NormalizeEntryOptions(FunctionWASM, nil, attrs.Bag{"options": nil})
	require.NoError(t, err)
	assert.Empty(t, groups)
	assert.Empty(t, diagnostics)
}

func TestOptionsCompatibility_DoesNotMutateInputAndCanonicalPaths(t *testing.T) {
	limits := map[string]any{"max_execution_ms": 77}
	data := map[string]any{"limits": limits}
	meta := attrs.Bag{"description": "unchanged"}
	_, _, err := NormalizeEntryOptions(FunctionWASM, data, meta)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"max_execution_ms": 77}, limits)
	assert.Equal(t, "unchanged", meta.GetString("description", ""))

	got, ok := CanonicalOptionPath(FunctionWASM, "limits.max_execution_ms")
	assert.True(t, ok)
	assert.Equal(t, "options.limits.max_execution_ms", got)
	got, ok = CanonicalOptionPath(ProcessWASM, "mailbox.capacity")
	assert.False(t, ok)
	assert.Empty(t, got)
	got, ok = CanonicalOptionPath(ProcessWASM, "options.mailbox.capacity")
	assert.True(t, ok)
	assert.Equal(t, "options.mailbox.capacity", got)
}

func TestOptionsCompatibility_YAMLCanonicalParity(t *testing.T) {
	var cfg FunctionConfig
	require.NoError(t, yaml.Unmarshal([]byte("fs: app:fs\npath: /fn.wasm\nhash: sha256:0\nmethod: run\noptions:\n  limits:\n    max_execution_ms: 9\n"), &cfg))
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 9, cfg.Limits.MaxExecutionMS)
}

func TestOptionsCompatibility_FunctionLegacyInterceptorDefaults(t *testing.T) {
	raw := `{"fs":"app:fs","path":"/fn.wasm","hash":"sha256:0","method":"run","limits":{"max_execution_ms":12},"meta":{"options":{"retry":{"max_attempts":3}}}}`
	var cfg FunctionConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 12, cfg.Limits.MaxExecutionMS)
	legacy, ok := cfg.Meta.GetBag("options")
	require.True(t, ok)
	assert.Equal(t, 3, func() int {
		retry, ok := legacy.GetBag("retry")
		if !ok {
			return 0
		}
		return retry.GetInt("max_attempts", 0)
	}())

	raw = `{"fs":"app:fs","path":"/fn.wasm","hash":"sha256:0","method":"run","options":{"limits":{"max_execution_ms":12}},"meta":{"options":{"retry":{"max_attempts":3}}}}`
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	require.NoError(t, cfg.Validate())
	legacy, ok = cfg.Meta.GetBag("options")
	require.True(t, ok)
	assert.Equal(t, 3, func() int {
		retry, ok := legacy.GetBag("retry")
		if !ok {
			return 0
		}
		return retry.GetInt("max_attempts", 0)
	}())
}

func TestOptionsCompatibility_LegacyNullGroupIsAbsent(t *testing.T) {
	for _, raw := range []string{
		`{"fs":"app:fs","path":"/fn.wasm","hash":"sha256:0","method":"run","limits":null}`,
		`{"fs":"app:fs","path":"/fn.wasm","hash":"sha256:0","method":"run","meta":{"options":{"limits":null}}}`,
	} {
		var cfg FunctionConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
		require.NoError(t, cfg.Validate())
		assert.Zero(t, cfg.Limits.MaxExecutionMS)
	}
}

func TestOptionsCompatibility_LegacyFunctionControlsStayStrict(t *testing.T) {
	var cfg WATFunctionConfig
	require.NoError(t, json.Unmarshal([]byte(`{"source":"(module)","method":"run","meta":{"options":{"limits":{"unknown_limit":1}}}}`), &cfg))
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown field "unknown_limit" in meta.options.limits`)
}

func TestOptionsCompatibility_WATLegacyInterceptorDefaults(t *testing.T) {
	var cfg WATFunctionConfig
	raw := `{"source":"(module)","method":"run","limits":{"max_execution_ms":12},"meta":{"options":{"retry":{"max_attempts":3}}}}`
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	require.NoError(t, cfg.Validate())
	assert.Equal(t, 12, cfg.Limits.MaxExecutionMS)
	legacy, ok := cfg.Meta.GetBag("options")
	require.True(t, ok)
	retry, ok := legacy.GetBag("retry")
	require.True(t, ok)
	assert.Equal(t, 3, retry.GetInt("max_attempts", 0))
}

func TestOptionPathIdentityUsesOneCatalog(t *testing.T) {
	for _, kind := range []registry.Kind{FunctionWASM, FunctionWAT, ProcessWASM} {
		info, ok := ResolveOptionPath(kind, "meta.options.limits.max_execution_ms")
		require.True(t, ok)
		assert.Equal(t, "options.limits.max_execution_ms", info.CanonicalPath)
		assert.Equal(t, "options.limits", info.CanonicalGroup)
		assert.Equal(t, "meta.options.limits", info.AuthoredGroup)
		assert.Equal(t, ".max_execution_ms", info.Suffix)
		require.ElementsMatch(t, []string{"options.limits", "meta.options.limits", "limits"}, info.GroupAliases)
		for _, alias := range info.GroupAliases {
			other, found := ResolveOptionPath(kind, alias+info.Suffix)
			require.True(t, found)
			assert.Equal(t, info.CanonicalPath, other.CanonicalPath)
		}
		info.GroupAliases[0] = "corrupted"
		next, found := ResolveOptionPath(kind, "options.limits")
		require.True(t, found)
		assert.NotContains(t, next.GroupAliases, "corrupted")
	}
	pool, ok := ResolveOptionPath(FunctionWASM, "options.pool.size")
	require.True(t, ok)
	assert.Equal(t, "pool", pool.CanonicalGroup)
	assert.Equal(t, "pool.size", pool.CanonicalPath)
	_, ok = ResolveOptionPath(ProcessWASM, "options.pool.size")
	assert.False(t, ok)
	_, ok = ResolveOptionPath(ProcessWASM, "mailbox.capacity")
	assert.False(t, ok)
	_, ok = ResolveOptionPath(ProcessWASM, "options.limits_extra.value")
	assert.False(t, ok)
}

func TestNormalizerCanonicalTypedNilIsNotLegacyAbsence(t *testing.T) {
	var function *FunctionOptions
	var process *ProcessOptions
	for _, tc := range []struct {
		value any
		kind  registry.Kind
	}{{kind: FunctionWASM, value: function}, {kind: FunctionWAT, value: function}, {kind: ProcessWASM, value: process}} {
		_, _, err := NormalizeEntryOptions(tc.kind, map[string]any{"options": tc.value}, nil)
		require.Error(t, err)
		_, _, err = NormalizeEntryOptions(tc.kind, nil, attrs.Bag{"options": tc.value})
		require.NoError(t, err)
	}
}
