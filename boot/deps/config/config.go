// SPDX-License-Identifier: MPL-2.0

package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigFile = "wippy.yaml"
)

type ModuleConfig struct {
	ExcludeMeta  map[string][]string `yaml:"exclude_meta,omitempty"`
	Metadata     map[string]any      `yaml:"metadata,omitempty"`
	Publish      PublishConfig       `yaml:"publish,omitempty"`
	Repository   string              `yaml:"repository,omitempty"`
	Description  string              `yaml:"description,omitempty"`
	License      string              `yaml:"license,omitempty"`
	Version      string              `yaml:"version"`
	Homepage     string              `yaml:"homepage,omitempty"`
	ModuleName   string              `yaml:"module"`
	Organization string              `yaml:"organization"`
	// Type is the module's kind on the hub: library, application, agent or
	// plugin. Declaring it here makes the manifest the source of truth — hub
	// requires it to create a module, and reclassifies the module when it
	// changes. Empty means "not declared", which only works for a module that
	// already exists.
	Type     string   `yaml:"type,omitempty"`
	Keywords []string `yaml:"keywords,omitempty"`
	Authors  []string `yaml:"authors,omitempty"`
	Include  []string `yaml:"include,omitempty"`
	Exclude  []string `yaml:"exclude,omitempty"`
	Embed    []string `yaml:"embed,omitempty"`
}

type PublishConfig struct {
	Profiles PublishProfilesConfig `yaml:"profiles,omitempty"`
}

type PublishProfilesConfig struct {
	Enabled *bool    `yaml:"enabled,omitempty"`
	Source  string   `yaml:"source,omitempty"`
	Include []string `yaml:"include,omitempty"`
}

func Load(dir string) (*ModuleConfig, error) {
	path := filepath.Join(dir, DefaultConfigFile)
	return LoadFrom(path)
}

func LoadFrom(path string) (*ModuleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("wippy.yaml not found at %s", path)
		}
		return nil, fmt.Errorf("failed to read wippy.yaml: %w", err)
	}

	var cfg ModuleConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse wippy.yaml: %w", err)
	}

	return &cfg, nil
}

// ModuleTypes are the module kinds hub accepts. Mirrors hub's
// domain.ModuleType constants and the modules.module_type CHECK constraint.
var ModuleTypes = []string{"library", "application", "agent", "plugin"}

// ValidateModuleType accepts the empty string ("not declared") and any of
// ModuleTypes. Anything else is a typo worth catching before the publish
// round-trips to hub — notably "app", which is what the badge shows for
// "application".
func ValidateModuleType(t string) error {
	if t == "" {
		return nil
	}
	if slices.Contains(ModuleTypes, t) {
		return nil
	}
	return fmt.Errorf("type must be one of %s (got %q)", strings.Join(ModuleTypes, ", "), t)
}

func (c *ModuleConfig) Validate() error {
	if c.Organization == "" {
		return fmt.Errorf("organization is required in wippy.yaml")
	}

	if !isValidIdentifier(c.Organization) {
		return fmt.Errorf("organization must be lowercase alphanumeric with hyphens")
	}

	if c.ModuleName == "" {
		return fmt.Errorf("module is required in wippy.yaml")
	}

	if !isValidIdentifier(c.ModuleName) {
		return fmt.Errorf("module must be lowercase alphanumeric with hyphens")
	}

	if c.Version != "" {
		if _, err := semver.NewVersion(c.Version); err != nil {
			return fmt.Errorf("version must be valid semver: %w", err)
		}
	}

	if err := ValidateModuleType(c.Type); err != nil {
		return err
	}

	return nil
}

func ValidateVersion(v string) error {
	if v == "" {
		return fmt.Errorf("version is required: provide --version flag or set version in wippy.yaml")
	}

	if _, err := semver.NewVersion(v); err != nil {
		return fmt.Errorf("version must be valid semver: %w", err)
	}

	return nil
}

func (c *ModuleConfig) ValidateForLabel() error {
	if c.Organization == "" {
		return fmt.Errorf("organization is required in wippy.yaml")
	}

	if !isValidIdentifier(c.Organization) {
		return fmt.Errorf("organization must be lowercase alphanumeric with hyphens")
	}

	if c.ModuleName == "" {
		return fmt.Errorf("module is required in wippy.yaml")
	}

	if !isValidIdentifier(c.ModuleName) {
		return fmt.Errorf("module must be lowercase alphanumeric with hyphens")
	}

	if err := ValidateModuleType(c.Type); err != nil {
		return err
	}

	return nil
}

// EntryExcludes returns the exclude patterns that target registry entries in
// namespace:name form. Patterns without a ':' separator are source-file globs
// (for example "_old/**", "test/**" or "*.test.lua") consumed when collecting
// files, not entry-ID patterns; they are omitted here so the entry-disable
// stage does not reject them as malformed entry patterns.
func (c *ModuleConfig) EntryExcludes() []string {
	if len(c.Exclude) == 0 {
		return nil
	}

	out := make([]string, 0, len(c.Exclude))
	for _, pattern := range c.Exclude {
		if strings.Contains(pattern, ":") {
			out = append(out, pattern)
		}
	}

	return out
}

// SourceExcludes returns module-root-relative file globs from exclude. Registry
// entry patterns contain ':' and are handled after entries are decoded.
func (c *ModuleConfig) SourceExcludes() []string {
	if len(c.Exclude) == 0 {
		return nil
	}

	out := make([]string, 0, len(c.Exclude))
	for _, pattern := range c.Exclude {
		if !strings.Contains(pattern, ":") {
			out = append(out, pattern)
		}
	}
	return out
}

// ExcludesSourcePath reports whether a module-root-relative source path matches
// a file exclusion. A ** path segment spans zero or more directory segments;
// other segments use path.Match semantics.
func (c *ModuleConfig) ExcludesSourcePath(relative string) bool {
	relative = cleanSourcePath(relative)
	if relative == "" {
		return false
	}
	for _, pattern := range c.SourceExcludes() {
		if matchSourceGlob(cleanSourcePath(pattern), relative) {
			return true
		}
	}
	return false
}

func cleanSourcePath(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	if value == "" || value == "." {
		return ""
	}
	return strings.TrimPrefix(path.Clean(value), "/")
}

func matchSourceGlob(pattern, relative string) bool {
	if pattern == "" || relative == "" {
		return false
	}
	patterns := strings.Split(pattern, "/")
	parts := strings.Split(relative, "/")
	type position struct{ pattern, part int }
	memo := make(map[position]bool)
	seen := make(map[position]bool)

	var match func(int, int) bool
	match = func(patternIndex, partIndex int) bool {
		key := position{pattern: patternIndex, part: partIndex}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true

		var matched bool
		switch {
		case patternIndex == len(patterns):
			matched = partIndex == len(parts)
		case patterns[patternIndex] == "**":
			matched = match(patternIndex+1, partIndex) ||
				(partIndex < len(parts) && match(patternIndex, partIndex+1))
		case partIndex < len(parts):
			segmentMatched, err := path.Match(patterns[patternIndex], parts[partIndex])
			matched = err == nil && segmentMatched && match(patternIndex+1, partIndex+1)
		}
		memo[key] = matched
		return matched
	}

	return match(0, 0)
}

func (c *ModuleConfig) Namespace() string {
	return c.Organization + "." + c.ModuleName
}

func (c *ModuleConfig) FullName() string {
	return c.Organization + "/" + c.ModuleName
}

func (c *ModuleConfig) OutputFileName() string {
	if c.Version != "" {
		return c.ModuleName + "-" + c.Version + ".wapp"
	}
	return c.ModuleName + ".wapp"
}

func (c *ModuleConfig) ResolveDescription(baseDir string) string {
	if c.Description == "" {
		return ""
	}

	if strings.HasPrefix(c.Description, "file://") {
		content, err := loadFileContent(c.Description, baseDir)
		if err != nil {
			return c.Description
		}
		return content
	}

	return c.Description
}

func loadFileContent(ref, baseDir string) (string, error) {
	var filePath string

	if strings.HasPrefix(ref, "file:///") {
		filePath = strings.TrimPrefix(ref, "file://")
	} else {
		filePath = strings.TrimPrefix(ref, "file://")
		filePath = filepath.Join(baseDir, filePath)
	}

	filePath = filepath.Clean(filePath)

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}

	for i, c := range s {
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '-' && i > 0 && i < len(s)-1 {
			continue
		}
		return false
	}

	return true
}
