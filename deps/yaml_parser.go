package deps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"go.uber.org/zap"
)

// YAMLParser handles parsing of YAML files for dependency discovery
type YAMLParser struct {
	logger *zap.Logger
}

// NewYAMLParser creates a new YAML parser
func NewYAMLParser(logger *zap.Logger) *YAMLParser {
	return &YAMLParser{logger: logger}
}

// ParseDependenciesFromDirectory scans a directory for YAML files and extracts dependencies
func (yp *YAMLParser) ParseDependenciesFromDirectory(moduleDir string, dependencyHandler func(ManifestDependency)) error {
	return filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		// Only process YAML files
		if !yp.isYAMLFile(info.Name()) {
			return nil
		}

		// Parse the YAML file
		deps, err := yp.parseYAMLFile(path)
		if err != nil {
			yp.logger.Debug("Failed to parse YAML file",
				zap.String("path", path),
				zap.Error(err))
			return nil // Continue processing other files
		}

		// Process found dependencies
		for _, dep := range deps {
			dependencyHandler(dep)
		}

		return nil
	})
}

// isYAMLFile checks if a file is a YAML file
func (yp *YAMLParser) isYAMLFile(filename string) bool {
	name := strings.ToLower(filename)
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// parseYAMLFile parses a single YAML file and extracts dependencies
func (yp *YAMLParser) parseYAMLFile(path string) ([]ManifestDependency, error) {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read YAML file %s: %w", path, err)
	}

	// Try to parse as manifest format first
	if deps := yp.parseManifestFormat(data); len(deps) > 0 {
		return deps, nil
	}

	// Try to parse as entries format
	if deps := yp.parseEntriesFormat(data); len(deps) > 0 {
		return deps, nil
	}

	return nil, nil // No dependencies found
}

// parseManifestFormat parses YAML in manifest format
func (yp *YAMLParser) parseManifestFormat(data []byte) []ManifestDependency {
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil
	}

	if len(manifest.Dependencies) == 0 {
		return nil
	}

	// Filter only remote dependencies
	var remoteDeps []ManifestDependency
	for _, dep := range manifest.Dependencies {
		if dep.Path == "" { // Only remote dependencies
			remoteDeps = append(remoteDeps, dep)
		}
	}

	return remoteDeps
}

// parseEntriesFormat parses YAML in entries format
func (yp *YAMLParser) parseEntriesFormat(data []byte) []ManifestDependency {
	var entriesDoc struct {
		Entries []struct {
			Name      string `yaml:"name"`
			Kind      string `yaml:"kind"`
			Component string `yaml:"component"`
			Version   string `yaml:"version"`
		} `yaml:"entries"`
	}

	if err := yaml.Unmarshal(data, &entriesDoc); err != nil {
		return nil
	}

	var deps []ManifestDependency
	for _, entry := range entriesDoc.Entries {
		if entry.Kind == "ns.dependency" && entry.Component != "" {
			parts := strings.Split(entry.Component, "/")
			if len(parts) == 2 {
				dep := ManifestDependency{
					Name:    Name{Organization: parts[0], Module: parts[1]},
					Version: entry.Version,
				}
				deps = append(deps, dep)
			}
		}
	}

	return deps
}
