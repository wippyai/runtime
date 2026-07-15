// SPDX-License-Identifier: MPL-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- isValidIdentifier ---

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"hello", true},
		{"my-module", true},
		{"abc123", true},
		{"a1-b2", true},
		{"", false},
		{"-start", false},
		{"end-", false},
		{"UPPER", false},
		{"has_underscore", false},
		{"has space", false},
		{"has.dot", false},
		{"a", true},
		{"123", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidIdentifier(tt.input))
		})
	}
}

// --- ModuleConfig.Validate ---

func TestModuleConfig_Validate_Valid(t *testing.T) {
	cfg := &ModuleConfig{
		Organization: "myorg",
		ModuleName:   "mymod",
		Version:      "1.0.0",
	}
	assert.NoError(t, cfg.Validate())
}

func TestModuleConfig_Validate_NoVersion(t *testing.T) {
	cfg := &ModuleConfig{
		Organization: "myorg",
		ModuleName:   "mymod",
	}
	assert.NoError(t, cfg.Validate())
}

func TestModuleConfig_Validate_EmptyOrg(t *testing.T) {
	cfg := &ModuleConfig{ModuleName: "mymod"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "organization is required")
}

func TestModuleConfig_Validate_InvalidOrg(t *testing.T) {
	cfg := &ModuleConfig{Organization: "My_Org", ModuleName: "mymod"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lowercase alphanumeric")
}

func TestModuleConfig_Validate_EmptyModule(t *testing.T) {
	cfg := &ModuleConfig{Organization: "myorg"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module is required")
}

func TestModuleConfig_Validate_InvalidModule(t *testing.T) {
	cfg := &ModuleConfig{Organization: "myorg", ModuleName: "Bad_Name"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module must be lowercase")
}

func TestModuleConfig_Validate_InvalidVersion(t *testing.T) {
	cfg := &ModuleConfig{
		Organization: "myorg",
		ModuleName:   "mymod",
		Version:      "not-semver",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid semver")
}

// --- ValidateVersion ---

func TestValidateVersion_Valid(t *testing.T) {
	assert.NoError(t, ValidateVersion("1.2.3"))
	assert.NoError(t, ValidateVersion("0.0.1-alpha"))
	assert.NoError(t, ValidateVersion("2.0.0+build.1"))
}

func TestValidateVersion_Empty(t *testing.T) {
	err := ValidateVersion("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version is required")
}

func TestValidateVersion_Invalid(t *testing.T) {
	err := ValidateVersion("abc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid semver")
}

// --- ValidateForLabel ---

func TestModuleConfig_ValidateForLabel_Valid(t *testing.T) {
	cfg := &ModuleConfig{Organization: "myorg", ModuleName: "mymod"}
	assert.NoError(t, cfg.ValidateForLabel())
}

func TestModuleConfig_ValidateForLabel_EmptyOrg(t *testing.T) {
	cfg := &ModuleConfig{ModuleName: "mymod"}
	err := cfg.ValidateForLabel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "organization is required")
}

func TestModuleConfig_ValidateForLabel_InvalidOrg(t *testing.T) {
	cfg := &ModuleConfig{Organization: "UPPER", ModuleName: "mymod"}
	err := cfg.ValidateForLabel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lowercase alphanumeric")
}

func TestModuleConfig_ValidateForLabel_EmptyModule(t *testing.T) {
	cfg := &ModuleConfig{Organization: "myorg"}
	err := cfg.ValidateForLabel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module is required")
}

func TestModuleConfig_ValidateForLabel_InvalidModule(t *testing.T) {
	cfg := &ModuleConfig{Organization: "myorg", ModuleName: "BAD"}
	err := cfg.ValidateForLabel()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module must be lowercase")
}

// --- Namespace, FullName, OutputFileName ---

func TestModuleConfig_Namespace(t *testing.T) {
	cfg := &ModuleConfig{Organization: "myorg", ModuleName: "mymod"}
	assert.Equal(t, "myorg.mymod", cfg.Namespace())
}

func TestModuleConfig_FullName(t *testing.T) {
	cfg := &ModuleConfig{Organization: "myorg", ModuleName: "mymod"}
	assert.Equal(t, "myorg/mymod", cfg.FullName())
}

func TestModuleConfig_OutputFileName_WithVersion(t *testing.T) {
	cfg := &ModuleConfig{ModuleName: "mymod", Version: "1.0.0"}
	assert.Equal(t, "mymod-1.0.0.wapp", cfg.OutputFileName())
}

func TestModuleConfig_OutputFileName_NoVersion(t *testing.T) {
	cfg := &ModuleConfig{ModuleName: "mymod"}
	assert.Equal(t, "mymod.wapp", cfg.OutputFileName())
}

// --- ResolveDescription ---

func TestModuleConfig_ResolveDescription_Plain(t *testing.T) {
	cfg := &ModuleConfig{Description: "A simple module"}
	assert.Equal(t, "A simple module", cfg.ResolveDescription("/base"))
}

func TestModuleConfig_ResolveDescription_Empty(t *testing.T) {
	cfg := &ModuleConfig{}
	assert.Equal(t, "", cfg.ResolveDescription("/base"))
}

func TestModuleConfig_ResolveDescription_FileRef(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "desc.md"), []byte("file content"), 0644))

	cfg := &ModuleConfig{Description: "file://desc.md"}
	assert.Equal(t, "file content", cfg.ResolveDescription(dir))
}

func TestModuleConfig_ResolveDescription_FileNotFound(t *testing.T) {
	cfg := &ModuleConfig{Description: "file://nonexistent.md"}
	// returns the raw reference when file not found
	assert.Equal(t, "file://nonexistent.md", cfg.ResolveDescription("/tmp/empty"))
}

// --- Load / LoadFrom ---

func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()
	content := `
organization: myorg
module: mymod
version: "1.0.0"
description: test module
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, DefaultConfigFile), []byte(content), 0644))

	cfg, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "myorg", cfg.Organization)
	assert.Equal(t, "mymod", cfg.ModuleName)
	assert.Equal(t, "1.0.0", cfg.Version)
	assert.Equal(t, "test module", cfg.Description)
}

func TestLoad_NotFound(t *testing.T) {
	_, err := Load(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wippy.yaml not found")
}

func TestLoadFrom_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("{{invalid yaml"), 0644))

	_, err := LoadFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestLoad_WithOptionalFields(t *testing.T) {
	dir := t.TempDir()
	content := `
organization: myorg
module: mymod
license: MIT
repository: https://github.com/example/repo
homepage: https://example.com
keywords:
  - test
  - example
authors:
  - alice
  - bob
include:
  - "*.lua"
exclude:
  - "*.test.lua"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, DefaultConfigFile), []byte(content), 0644))

	cfg, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "MIT", cfg.License)
	assert.Equal(t, "https://github.com/example/repo", cfg.Repository)
	assert.Equal(t, "https://example.com", cfg.Homepage)
	assert.Equal(t, []string{"test", "example"}, cfg.Keywords)
	assert.Equal(t, []string{"alice", "bob"}, cfg.Authors)
	assert.Equal(t, []string{"*.lua"}, cfg.Include)
	assert.Equal(t, []string{"*.test.lua"}, cfg.Exclude)
}

func TestLoad_WithMetadata(t *testing.T) {
	dir := t.TempDir()
	content := `
organization: myorg
module: mymod
metadata:
  runtime.lsp.enabled: true
  runtime:
    logger:
      level: debug
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, DefaultConfigFile), []byte(content), 0644))

	cfg, err := Load(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg.Metadata)
	assert.Equal(t, true, cfg.Metadata["runtime.lsp.enabled"])

	runtimeMap, ok := cfg.Metadata["runtime"].(map[string]any)
	require.True(t, ok)
	loggerMap, ok := runtimeMap["logger"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "debug", loggerMap["level"])
}

func TestLoad_WithPublishProfileConfig(t *testing.T) {
	dir := t.TempDir()
	content := `
organization: myorg
module: mymod
publish:
  profiles:
    enabled: false
    source: config/profiles.yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, DefaultConfigFile), []byte(content), 0644))

	cfg, err := Load(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg.Publish.Profiles.Enabled)
	assert.False(t, *cfg.Publish.Profiles.Enabled)
	assert.Equal(t, "config/profiles.yaml", cfg.Publish.Profiles.Source)
}

// --- EntryExcludes ---

func TestEntryExcludes(t *testing.T) {
	tests := []struct {
		name    string
		exclude []string
		want    []string
	}{
		{name: "nil", exclude: nil, want: nil},
		{name: "only file globs", exclude: []string{"_old/**", "test/**", "*.test.lua"}, want: []string{}},
		{name: "only entry patterns", exclude: []string{"app:**", "app.env:**"}, want: []string{"app:**", "app.env:**"}},
		{name: "mixed file globs and entry patterns", exclude: []string{"_old/**", "app:**"}, want: []string{"app:**"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &ModuleConfig{Exclude: tc.exclude}
			assert.Equal(t, tc.want, cfg.EntryExcludes())
		})
	}
}

func TestValidateModuleType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty means not declared", input: "", wantErr: false},
		{name: "library", input: "library", wantErr: false},
		{name: "application", input: "application", wantErr: false},
		{name: "agent", input: "agent", wantErr: false},
		{name: "plugin", input: "plugin", wantErr: false},
		// "app" is the badge label for "application" — an easy thing to write
		// in wippy.yaml and exactly what the CHECK constraint would reject.
		{name: "app is not application", input: "app", wantErr: true},
		{name: "unknown", input: "widget", wantErr: true},
		{name: "case sensitive", input: "Library", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateModuleType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateModuleType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestModuleConfig_ParsesType proves the new key actually round-trips through
// the YAML decoder — the whole feature depends on it reaching hub.
func TestModuleConfig_ParsesType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultConfigFile)
	body := "organization: acme\nmodule: widgets\nversion: 1.0.0\ntype: plugin\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(): %v", err)
	}
	if cfg.Type != "plugin" {
		t.Errorf("cfg.Type = %q, want %q", cfg.Type, "plugin")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate(): %v", err)
	}
}

// TestModuleConfig_RejectsBadType: a typo in wippy.yaml must fail before the
// publish leaves the machine.
func TestModuleConfig_RejectsBadType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultConfigFile)
	body := "organization: acme\nmodule: widgets\nversion: 1.0.0\ntype: app\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() must reject type: app")
	}
}
