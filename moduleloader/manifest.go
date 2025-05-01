package moduleloader

// Manifest represents the structure of a configuration file (likely YAML),
// defining a named entity and its dependencies.
type Manifest struct {
	Name         string               `yaml:"name"`
	Dependencies []ManifestDependency `yaml:"dependencies"`
}

// ManifestDependency defines a single dependency required by a Manifest.
type ManifestDependency struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Path    string `yaml:"path"`
}
