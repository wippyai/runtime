// SPDX-License-Identifier: MPL-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigFile = "wippy.yaml"
)

type ModuleConfig struct {
	ExcludeMeta  map[string][]string `yaml:"exclude_meta,omitempty"`
	Organization string              `yaml:"organization"`
	ModuleName   string              `yaml:"module"`
	Version      string              `yaml:"version"`
	Description  string              `yaml:"description,omitempty"`
	License      string              `yaml:"license,omitempty"`
	Repository   string              `yaml:"repository,omitempty"`
	Homepage     string              `yaml:"homepage,omitempty"`
	Keywords     []string            `yaml:"keywords,omitempty"`
	Authors      []string            `yaml:"authors,omitempty"`
	Include      []string            `yaml:"include,omitempty"`
	Exclude      []string            `yaml:"exclude,omitempty"`
	Metadata     map[string]any      `yaml:"metadata,omitempty"`
	Embed        []string            `yaml:"embed,omitempty"`
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
