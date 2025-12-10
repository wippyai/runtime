package registry

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

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
