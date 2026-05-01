// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"

	lua "github.com/wippyai/go-lua"
)

func setupModule(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("registry")
	if mod.Type() != lua.LTTable {
		t.Fatal("registry module not registered")
	}

	modTbl := mod.(*lua.LTable)

	functions := []string{
		"snapshot", "current_version", "versions",
		"parse_id", "history", "find", "get", "build_delta",
	}
	for _, fn := range functions {
		if modTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("registry").(*lua.LTable)
	mod2 := l2.GetGlobal("registry").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestParseID(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	setupModule(l)

	err := l.DoString(`
		local id = registry.parse_id("test:example")
		assert(type(id) == "table", "id should be a table")
		assert(id.ns == "test", "ns should be 'test', got: " .. tostring(id.ns))
		assert(id.name == "example", "name should be 'example', got: " .. tostring(id.name))
	`)
	if err != nil {
		t.Errorf("parse_id test failed: %v", err)
	}
}

func TestParseIDInvalidFormat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	setupModule(l)

	err := l.DoString(`
		local id = registry.parse_id("invalid_format")
		assert(type(id) == "table", "id should be a table")
		assert(id.ns == "", "ns should be empty for invalid format, got: " .. tostring(id.ns))
		assert(id.name == "invalid_format", "name should be full string, got: " .. tostring(id.name))
	`)
	if err != nil {
		t.Errorf("parse_id invalid format test failed: %v", err)
	}
}

func TestParseIDWithSlash(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	setupModule(l)

	err := l.DoString(`
		local id = registry.parse_id("namespace:path/to/name")
		assert(id.ns == "namespace", "ns should be 'namespace', got: " .. tostring(id.ns))
		assert(id.name == "path/to/name", "name should be 'path/to/name', got: " .. tostring(id.name))
	`)
	if err != nil {
		t.Errorf("parse_id with slash test failed: %v", err)
	}
}

func TestSnapshotWithoutRegistry(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local snap, err = registry.snapshot()
		assert(snap == nil, "snapshot should be nil without registry")
		assert(err ~= nil, "should return error without registry")
		assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		assert(err:retryable() == false, "error should not be retryable")
	`)
	if err != nil {
		t.Errorf("snapshot without registry test failed: %v", err)
	}
}

func TestCurrentVersionWithoutRegistry(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local version, err = registry.current_version()
		assert(version == nil, "version should be nil without registry")
		assert(err ~= nil, "should return error without registry")
		assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		assert(err:retryable() == false, "error should not be retryable")
	`)
	if err != nil {
		t.Errorf("current_version without registry test failed: %v", err)
	}
}

func TestVersionsWithoutRegistry(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local versions, err = registry.versions()
		assert(versions == nil, "versions should be nil without registry")
		assert(err ~= nil, "should return error without registry")
		assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		assert(err:retryable() == false, "error should not be retryable")
	`)
	if err != nil {
		t.Errorf("versions without registry test failed: %v", err)
	}
}

func TestHistoryWithoutRegistry(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local history, err = registry.history()
		assert(history == nil, "history should be nil without registry")
		assert(err ~= nil, "should return error without registry")
		assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		assert(err:retryable() == false, "error should not be retryable")
	`)
	if err != nil {
		t.Errorf("history without registry test failed: %v", err)
	}
}

func TestGetWithoutRegistry(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local entry, err = registry.get("test:example")
		assert(entry == nil, "entry should be nil without registry")
		assert(err ~= nil, "should return error without registry")
		assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		assert(err:retryable() == false, "error should not be retryable")
	`)
	if err != nil {
		t.Errorf("get without registry test failed: %v", err)
	}
}

func TestFindWithoutRegistry(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local entries, err = registry.find({kind = "test"})
		assert(entries == nil, "entries should be nil without registry")
		assert(err ~= nil, "should return error without registry")
		assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		assert(err:retryable() == false, "error should not be retryable")
	`)
	if err != nil {
		t.Errorf("find without registry test failed: %v", err)
	}
}

func TestBuildDeltaWithoutTranscoder(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local delta, err = registry.build_delta({}, {})
		assert(delta == nil, "delta should be nil without transcoder")
		assert(err ~= nil, "should return error without transcoder")
		assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		assert(err:retryable() == false, "error should not be retryable")
	`)
	if err != nil {
		t.Errorf("build_delta without transcoder test failed: %v", err)
	}
}

func TestBuildDeltaDetectsMetaOnlyChanges(t *testing.T) {
	ctx := setupContextWithTranscoder()

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local from = {
			{
				id = "app.test:handler",
				kind = "function.lua",
				meta = { comment = "old", tags = { "a" } },
				data = { source = "return true", method = "main" },
			},
		}
		local to = {
			{
				id = "app.test:handler",
				kind = "function.lua",
				meta = { comment = "new", tags = { "a" } },
				data = { source = "return true", method = "main" },
			},
		}

		local delta, err = registry.build_delta(from, to)
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(delta ~= nil, "delta should not be nil")
		assert(#delta == 1, "expected one update, got " .. tostring(#delta))
		assert(delta[1].kind == "entry.update", "expected entry.update, got " .. tostring(delta[1].kind))
		assert(delta[1].entry.meta.comment == "new", "updated meta should be present")
	`)
	if err != nil {
		t.Errorf("build_delta meta-only change test failed: %v", err)
	}
}

func TestMapsEqual(t *testing.T) {
	tests := []struct {
		a        map[string]any
		b        map[string]any
		name     string
		expected bool
	}{
		{
			name:     "empty maps",
			a:        map[string]any{},
			b:        map[string]any{},
			expected: true,
		},
		{
			name:     "equal simple maps",
			a:        map[string]any{"key": "value"},
			b:        map[string]any{"key": "value"},
			expected: true,
		},
		{
			name:     "different values",
			a:        map[string]any{"key": "value1"},
			b:        map[string]any{"key": "value2"},
			expected: false,
		},
		{
			name:     "different keys",
			a:        map[string]any{"key1": "value"},
			b:        map[string]any{"key2": "value"},
			expected: false,
		},
		{
			name:     "different lengths",
			a:        map[string]any{"key": "value"},
			b:        map[string]any{"key": "value", "key2": "value2"},
			expected: false,
		},
		{
			name:     "nested maps equal",
			a:        map[string]any{"nested": map[string]any{"key": "value"}},
			b:        map[string]any{"nested": map[string]any{"key": "value"}},
			expected: true,
		},
		{
			name:     "nested maps different",
			a:        map[string]any{"nested": map[string]any{"key": "value1"}},
			b:        map[string]any{"nested": map[string]any{"key": "value2"}},
			expected: false,
		},
		{
			name:     "numeric values equal",
			a:        map[string]any{"num": 42},
			b:        map[string]any{"num": float64(42)},
			expected: true,
		},
		{
			name:     "arrays equal",
			a:        map[string]any{"arr": []any{1, 2, 3}},
			b:        map[string]any{"arr": []any{1, 2, 3}},
			expected: true,
		},
		{
			name:     "arrays different length",
			a:        map[string]any{"arr": []any{1, 2}},
			b:        map[string]any{"arr": []any{1, 2, 3}},
			expected: false,
		},
		{
			name:     "arrays different values",
			a:        map[string]any{"arr": []any{1, 2, 3}},
			b:        map[string]any{"arr": []any{1, 2, 4}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapsEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("mapsEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValuesEqual(t *testing.T) {
	tests := []struct {
		a        any
		b        any
		name     string
		expected bool
	}{
		{
			name:     "equal strings",
			a:        "hello",
			b:        "hello",
			expected: true,
		},
		{
			name:     "different strings",
			a:        "hello",
			b:        "world",
			expected: false,
		},
		{
			name:     "equal ints",
			a:        42,
			b:        42,
			expected: true,
		},
		{
			name:     "int and float64 equal",
			a:        42,
			b:        float64(42),
			expected: true,
		},
		{
			name:     "different types",
			a:        "42",
			b:        42,
			expected: false,
		},
		{
			name:     "nil values",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "bool equal",
			a:        true,
			b:        true,
			expected: true,
		},
		{
			name:     "bool different",
			a:        true,
			b:        false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := valuesEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("valuesEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		value    any
		name     string
		expected bool
	}{
		{42, "int", true},
		{int64(42), "int64", true},
		{42.5, "float64", true},
		{"42", "string", false},
		{true, "bool", false},
		{nil, "nil", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNumeric(tt.value)
			if result != tt.expected {
				t.Errorf("isNumeric() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		value    any
		name     string
		expected float64
	}{
		{42, "int", 42.0},
		{int8(42), "int8", 42.0},
		{int16(42), "int16", 42.0},
		{int32(42), "int32", 42.0},
		{int64(42), "int64", 42.0},
		{uint(42), "uint", 42.0},
		{uint8(42), "uint8", 42.0},
		{uint16(42), "uint16", 42.0},
		{uint32(42), "uint32", 42.0},
		{uint64(42), "uint64", 42.0},
		{float32(42.5), "float32", 42.5},
		{42.5, "float64", 42.5},
		{"42", "unknown", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toFloat64(tt.value)
			if result != tt.expected {
				t.Errorf("toFloat64() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestModuleConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"typeSnapshot", typeSnapshot, "registry.Snapshot"},
		{"typeChanges", typeChanges, "registry.Changes"},
		{"typeVersion", typeVersion, "registry.Version"},
		{"typeHistory", typeHistory, "registry.History"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.Log == nil {
		t.Error("expected default log to be non-nil")
	}
}

func TestNewModuleWithNilLog(t *testing.T) {
	opts := Options{
		Log: nil,
	}

	mod := NewModule(opts)

	if mod == nil {
		t.Fatal("expected module to be non-nil")
		return
	}

	if mod.Name != "registry" {
		t.Errorf("expected module name 'registry', got %s", mod.Name)
	}
}

func TestModuleInfo(t *testing.T) {
	mod := Module

	if mod.Name != "registry" {
		t.Errorf("expected module name 'registry', got %s", mod.Name)
	}

	if mod.Description == "" {
		t.Error("expected non-empty description")
	}

	if len(mod.Class) == 0 {
		t.Error("expected at least one class")
	}
}

func TestModuleBuild(t *testing.T) {
	mod := Module

	table, yields := mod.Build()

	if table == nil {
		t.Fatal("expected non-nil table")
		return
	}

	if !table.Immutable {
		t.Error("expected module table to be immutable")
	}

	if yields != nil {
		t.Errorf("expected nil yields, got %v", yields)
	}
}

func TestParseIDEdgeCases(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	setupModule(l)

	tests := []struct {
		name     string
		input    string
		wantNS   string
		wantName string
	}{
		{"single colon", ":", "", ""},
		{"multiple colons", "a:b:c", "a", "b:c"},
		{"trailing colon", "ns:", "ns", ""},
		{"leading colon", ":name", "", "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := l.DoString(`
				local id = registry.parse_id("` + tt.input + `")
				assert(id.ns == "` + tt.wantNS + `", "ns mismatch for ` + tt.name + `")
				assert(id.name == "` + tt.wantName + `", "name mismatch for ` + tt.name + `")
			`)
			if err != nil {
				t.Errorf("test %s failed: %v", tt.name, err)
			}
		})
	}
}

// mockRegistry implements regapi.Registry for testing
type mockRegistry struct {
	entries map[string]regapi.Entry
}

func (m *mockRegistry) GetEntry(id regapi.ID) (regapi.Entry, error) {
	entry, ok := m.entries[id.String()]
	if !ok {
		return regapi.Entry{}, context.Canceled // placeholder error
	}
	return entry, nil
}

func (m *mockRegistry) GetAllEntries() ([]regapi.Entry, error) {
	return nil, nil
}

func (m *mockRegistry) Current() (regapi.Version, error) {
	return nil, nil
}

func (m *mockRegistry) Apply(_ context.Context, _ regapi.ChangeSet) (regapi.Version, error) {
	return nil, nil
}

func (m *mockRegistry) ApplyVersion(_ context.Context, _ regapi.Version) error {
	return nil
}

func (m *mockRegistry) LoadState(_ context.Context, _ regapi.State, _ regapi.Version) error {
	return nil
}

func (m *mockRegistry) History() regapi.History {
	return nil
}

func (m *mockRegistry) RegisterDependencyPattern(_ regapi.DependencyPattern) error {
	return nil
}

func setupContextWithTranscoder() context.Context {
	// Create app context
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)

	// Set up transcoder
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	luapayload.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	// Disable strict mode for tests (allows access without full security context)
	ctx = security.SetStrictMode(ctx, false)

	return ctx
}

func TestRegistryGetWithEntryData(t *testing.T) {
	// Create context with transcoder
	ctx := setupContextWithTranscoder()

	// Create mock registry with entry containing data
	mockReg := &mockRegistry{
		entries: map[string]regapi.Entry{
			"test:function": {
				ID:   regapi.NewID("test", "function"),
				Kind: "function.lua",
				Meta: map[string]any{
					"type":        "tool",
					"description": "Test function",
				},
				Data: payload.NewPayload(map[string]any{
					"source":  "function test() return true end",
					"method":  "test",
					"modules": []any{"json", "http"},
					"imports": map[string]any{
						"lib": map[string]any{
							"ns":   "test",
							"name": "lib",
						},
					},
				}, payload.Golang),
			},
		},
	}

	// Add registry to context
	ctx = regapi.WithRegistry(ctx, mockReg)

	// Create Lua state with context
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	lua.OpenErrors(l)
	setupModule(l)

	// Test registry.get with entry that has data
	err := l.DoString(`
		local entry, err = registry.get("test:function")
		if err then
			error("failed to get entry: " .. tostring(err))
		end

		assert(entry ~= nil, "entry should not be nil")
		assert(entry.id == "test:function", "id mismatch: " .. tostring(entry.id))
		assert(entry.kind == "function.lua", "kind mismatch: " .. tostring(entry.kind))
		assert(entry.meta ~= nil, "meta should not be nil")
		assert(entry.meta.type == "tool", "meta.type mismatch")
		assert(entry.data ~= nil, "data should not be nil")
		assert(entry.data.source == "function test() return true end", "data.source mismatch")
		assert(entry.data.method == "test", "data.method mismatch")
	`)
	if err != nil {
		t.Errorf("registry.get with entry data failed: %v", err)
	}
}

func TestRegistryGetWithComplexEntryData(t *testing.T) {
	// This test replicates the structure of a real function.lua entry
	ctx := setupContextWithTranscoder()

	mockReg := &mockRegistry{
		entries: map[string]regapi.Entry{
			"userspace.dataflow.session:delegate": {
				ID:   regapi.NewID("userspace.dataflow.session", "delegate"),
				Kind: "function.lua",
				Meta: map[string]any{
					"type":        "tool",
					"title":       "Delegate To Agent",
					"description": "Handles delegation by running the target agent",
					"input_schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"message": map[string]any{
								"type":        "string",
								"description": "The message to forward to the target agent",
							},
						},
						"required": []any{"message"},
					},
				},
				Data: payload.NewPayload(map[string]any{
					"source":  "local function handle() end return { handle = handle }",
					"method":  "handle",
					"modules": []any{"json", "uuid", "ctx", "security"},
					"imports": map[string]any{
						"agent_registry": map[string]any{"ns": "wippy.agent.discovery", "name": "registry"},
						"client":         map[string]any{"ns": "userspace.dataflow", "name": "client"},
						"consts":         map[string]any{"ns": "userspace.dataflow", "name": "consts"},
					},
					"pool": map[string]any{
						"type":    "",
						"size":    0,
						"workers": 0,
					},
				}, payload.Golang),
			},
		},
	}

	ctx = regapi.WithRegistry(ctx, mockReg)

	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	lua.OpenErrors(l)
	setupModule(l)

	err := l.DoString(`
		local entry, err = registry.get("userspace.dataflow.session:delegate")
		if err then
			error("failed to get entry: " .. tostring(err))
		end

		assert(entry ~= nil, "entry should not be nil")
		assert(entry.kind == "function.lua", "kind mismatch")
		assert(entry.meta ~= nil, "meta should not be nil")
		assert(entry.meta.type == "tool", "meta.type mismatch")
		assert(entry.data ~= nil, "data should not be nil")
		assert(entry.data.method == "handle", "data.method mismatch")
	`)
	if err != nil {
		t.Errorf("registry.get with complex entry data failed: %v", err)
	}
}
