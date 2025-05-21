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
