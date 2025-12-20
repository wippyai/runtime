package registry

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
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

	tbl := mod.(*lua.LTable)

	functions := []string{
		"snapshot", "current_version", "versions",
		"parse_id", "history", "find", "get", "build_delta",
	}
	for _, fn := range functions {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Module.Load(l1)
	Module.Load(l2)

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

func TestMapsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     map[string]any
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
		name     string
		a, b     interface{}
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
		name     string
		value    interface{}
		expected bool
	}{
		{"int", 42, true},
		{"int64", int64(42), true},
		{"float64", 42.5, true},
		{"string", "42", false},
		{"bool", true, false},
		{"nil", nil, false},
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
		name     string
		value    interface{}
		expected float64
	}{
		{"int", 42, 42.0},
		{"int8", int8(42), 42.0},
		{"int16", int16(42), 42.0},
		{"int32", int32(42), 42.0},
		{"int64", int64(42), 42.0},
		{"uint", uint(42), 42.0},
		{"uint8", uint8(42), 42.0},
		{"uint16", uint16(42), 42.0},
		{"uint32", uint32(42), 42.0},
		{"uint64", uint64(42), 42.0},
		{"float32", float32(42.5), 42.5},
		{"float64", 42.5, 42.5},
		{"unknown", "42", 0},
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
