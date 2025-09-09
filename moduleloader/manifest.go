package moduleloader

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// Name represents a structured naming convention with Organization and Module parts.
type Name struct {
	Organization string
	Module       string
}

// String returns string representation of the name.
func (n *Name) String() string {
	return fmt.Sprintf("%s/%s", n.Organization, n.Module)
}

// UnmarshalYAML implements yaml.Unmarshaler interface to parse "Organization/Module" format.
func (n *Name) UnmarshalYAML(data []byte) error {
	var nameStr string
	if err := yaml.Unmarshal(data, &nameStr); err != nil {
		return err
	}

	return n.SetName(nameStr)
}

// SetName converts given string into name.
func (n *Name) SetName(nameStr string) error {
	parts := strings.Split(nameStr, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid name format: %s, expected format: Organization/Module", nameStr)
	}

	n.Organization = parts[0]
	n.Module = parts[1]
	return nil
}

// Manifest represents the structure of a configuration file (likely YAML),
// defining a named entity and its dependencies.
type Manifest struct {
	Name         string               `yaml:"name"`
	Dependencies []ManifestDependency `yaml:"dependencies"`
}

// ManifestDependency defines a single dependency required by a Manifest.
type ManifestDependency struct {
	Name    Name   `yaml:"name"`
	Version string `yaml:"version"`
	Path    string `yaml:"path"`
}

// LoadedModule represents a module that has been loaded with its selected version and path.
type LoadedModule struct {
	Name         Name   `json:"name"`
	Version      string `json:"version"`
	Path         string `json:"path"`
	Organization string `json:"organization"`
	Module       string `json:"module"`
}

// ModuleStats represents statistics for a single module operation
type ModuleStats struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Status  string `json:"status"` // "from cache", "downloaded", "from replacement", "skipped"
}

// LoadResult represents the result of loading modules from the registry.
type LoadResult struct {
	Modules     []LoadedModule `json:"modules"`
	ModuleStats []ModuleStats  `json:"module_stats"`
}
