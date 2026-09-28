package stages

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	wasm "github.com/wippyai/runtime/api/runtime/wasm"
)

func TestOverride_TypedContainerRelocatesAlias(t *testing.T) {
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: wasm.FunctionWASM, Data: payload.New(map[string]any{"limits": map[string]any{"max_execution_ms": 1}})}}
	opts := wasm.FunctionOptions{Limits: wasm.LimitsConfig{MaxExecutionMS: 42}}
	require.NoError(t, executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:options": opts}), &entries))
	data := entries[0].Data.Data().(map[string]any)
	groups, _, err := wasm.NormalizeEntryOptions(entries[0].Kind, data, entries[0].Meta)
	require.NoError(t, err)
	require.Equal(t, 42, groups["limits"].(map[string]any)["max_execution_ms"])
}
func TestOverride_TypedSourceCannotEraseAmbiguity(t *testing.T) {
	opts := wasm.FunctionOptions{Limits: wasm.LimitsConfig{MaxExecutionMS: 2}}
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: wasm.FunctionWASM, Data: payload.New(map[string]any{"limits": map[string]any{"max_execution_ms": 1}}), Meta: attrs.Bag{"options": &opts}}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:meta.options": map[string]any{"retry": 3}}), &entries)
	require.ErrorContains(t, err, "duplicate option group")
}

func TestOverride_TypedContainerMatrix(t *testing.T) {
	fn := wasm.FunctionOptions{Limits: wasm.LimitsConfig{MaxExecutionMS: 42}}
	proc := wasm.ProcessOptions{WorkerClass: "default", Limits: wasm.ProcessLimitsConfig{MaxExecutionMS: 42}}
	for _, tc := range []struct {
		value any
		name  string
		kind  registry.Kind
	}{
		{name: "function", kind: wasm.FunctionWASM, value: fn}, {name: "function_pointer", kind: wasm.FunctionWASM, value: &fn},
		{name: "wat", kind: wasm.FunctionWAT, value: fn}, {name: "process", kind: wasm.ProcessWASM, value: proc}, {name: "process_pointer", kind: wasm.ProcessWASM, value: &proc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"options", "data.options", "meta.options"} {
				t.Run(path, func(t *testing.T) {
					source := map[string]any{"limits": map[string]any{"max_execution_ms": 1}}
					entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: tc.kind, Data: payload.New(source)}}
					require.NoError(t, executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:" + path: tc.value}), &entries))
					data := entries[0].Data.Data().(map[string]any)
					_, _, err := wasm.NormalizeEntryOptions(tc.kind, data, entries[0].Meta)
					require.NoError(t, err)
					require.EqualValues(t, 42, data["limits"].(map[string]any)["max_execution_ms"])
					require.Equal(t, 1, source["limits"].(map[string]any)["max_execution_ms"])
				})
			}
		})
	}
}

func TestOverride_TypedContainerConflictsBeforeWrites(t *testing.T) {
	opts := wasm.FunctionOptions{Limits: wasm.LimitsConfig{MaxExecutionMS: 42}}
	source := map[string]any{"limits": map[string]any{"max_execution_ms": 1}}
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: wasm.FunctionWASM, Data: payload.New(source)}}
	err := executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:options": &opts, "app:converter:limits.max_open_sockets": 7}), &entries)
	require.Error(t, err)
	require.Equal(t, source, entries[0].Data.Data())
}

func TestOverride_TypedSourceLeafPreservesSibling(t *testing.T) {
	opts := wasm.FunctionOptions{Limits: wasm.LimitsConfig{MaxExecutionMS: 42, MaxOpenSockets: 7}}
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: wasm.FunctionWASM, Meta: attrs.Bag{"options": &opts}}}
	require.NoError(t, executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:options.limits.max_execution_ms": 3}), &entries))
	groups, _, err := wasm.NormalizeEntryOptions(wasm.FunctionWASM, nil, entries[0].Meta)
	require.NoError(t, err)
	require.EqualValues(t, 3, groups["limits"].(map[string]any)["max_execution_ms"])
	require.EqualValues(t, 7, groups["limits"].(map[string]any)["max_open_sockets"])
	require.Equal(t, 42, opts.Limits.MaxExecutionMS)
}

func TestOverride_TypedInvalidAndNil(t *testing.T) {
	for _, value := range []any{wasm.FunctionOptions{Limits: wasm.LimitsConfig{MaxExecutionMS: -1}}, (*wasm.FunctionOptions)(nil), wasm.ProcessOptions{}} {
		entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: wasm.FunctionWASM}}
		require.Error(t, executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:options": value}), &entries))
	}
	entries := []registry.Entry{{ID: registry.NewID("app", "converter"), Kind: wasm.FunctionWASM, Meta: attrs.Bag{"options": (*wasm.FunctionOptions)(nil)}}}
	require.NoError(t, executeOverride(t, layeredOverrideConfig(map[string]any{"app:converter:options.limits.max_execution_ms": 3}), &entries))
}

func TestLink_TypedContainerRelocatesAlias(t *testing.T) {
	ctx, _ := setupTestContext()
	opts := wasm.FunctionOptions{Limits: wasm.LimitsConfig{MaxExecutionMS: 42}}
	entries := []registry.Entry{
		{ID: registry.NewID("test", "options"), Kind: registry.NamespaceRequirement, Data: payload.New(map[string]any{"default": opts, "targets": []any{map[string]any{"entry": "converter", "path": "meta.options"}}})},
		{ID: registry.NewID("test", "converter"), Kind: wasm.FunctionWASM, Data: payload.New(map[string]any{"options": map[string]any{"limits": map[string]any{"max_execution_ms": 1}}})},
	}
	require.NoError(t, executeLinkFixture(ctx, &entries, Link()))
	target := findEntry(entries, "test", "converter")
	data := target.Data.Data().(map[string]any)
	_, _, err := wasm.NormalizeEntryOptions(target.Kind, data, target.Meta)
	require.NoError(t, err)
	require.Equal(t, 42, data["options"].(map[string]any)["limits"].(map[string]any)["max_execution_ms"])
}
