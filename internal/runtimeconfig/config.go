package runtimeconfig

import (
	"fmt"
	"strings"
	"sync"
)

// EntryConfig represents a configuration entry with nested fields.
// Keys are field paths (can contain dots for nesting), values are strings.
// Example: {"addr": "8080", "timeouts.read": "30s", "timeouts.write": "60s"}
type EntryConfig map[string]string

// ConfigEntry represents a single configuration entry.
type ConfigEntry struct {
	Namespace string
	Entry     string
	Field     string
	Value     string
}

// Config represents a hierarchical runtime configuration store.
type Config struct {
	mu   sync.RWMutex
	data map[string]*Namespace
}

// Namespace represents a configuration namespace (e.g., "app").
type Namespace struct {
	entries map[string]EntryConfig // key is entry name, value is EntryConfig with fields
}

// New creates a new Config instance.
func New() *Config {
	return &Config{
		data: make(map[string]*Namespace),
	}
}

// Parse parses a configuration entry in the format: namespace:entry:field=value
// Examples:
//   - app:gateway:addr=8080
//   - app:user:name=John
//   - app:agents_by_name.endpoint:meta.router=core:api
//
// Entry and field names can contain dots, so colons are used as separators.
func Parse(input string) (namespace string, entry string, field string, value string, err error) {
	// Find the first colon to separate namespace from the rest
	firstColonIdx := strings.Index(input, ":")
	if firstColonIdx == -1 {
		return "", "", "", "", fmt.Errorf("invalid format: missing ':' separator (expected namespace:entry:field=value)")
	}

	namespace = strings.TrimSpace(input[:firstColonIdx])
	if namespace == "" {
		return "", "", "", "", fmt.Errorf("invalid format: namespace cannot be empty")
	}

	remainder := input[firstColonIdx+1:]

	// Find the second colon to separate entry from field
	secondColonIdx := strings.Index(remainder, ":")
	if secondColonIdx == -1 {
		return "", "", "", "", fmt.Errorf("invalid format: missing second ':' separator (expected namespace:entry:field=value)")
	}

	entry = strings.TrimSpace(remainder[:secondColonIdx])
	if entry == "" {
		return "", "", "", "", fmt.Errorf("invalid format: entry cannot be empty")
	}

	fieldPart := remainder[secondColonIdx+1:]

	// Find the equals sign to separate field from value
	equalsIdx := strings.Index(fieldPart, "=")
	if equalsIdx == -1 {
		return "", "", "", "", fmt.Errorf("invalid format: missing '=' separator (expected namespace:entry:field=value)")
	}

	field = strings.TrimSpace(fieldPart[:equalsIdx])
	if field == "" {
		return "", "", "", "", fmt.Errorf("invalid format: field cannot be empty")
	}

	value = fieldPart[equalsIdx+1:]
	// Note: we don't trim the value to preserve intentional whitespace

	return namespace, entry, field, value, nil
}

// Set stores a configuration value using the parsed components.
// Entry is the entry name (can contain dots), field is the field path (can contain dots for nesting).
func (c *Config) Set(namespace, entry, field, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate field is not empty
	if field == "" {
		return fmt.Errorf("invalid field: cannot be empty")
	}

	// Validate field path doesn't contain empty segments (e.g., "addr..port")
	if strings.Contains(field, "..") {
		return fmt.Errorf("invalid field: field path cannot contain empty segments")
	}

	// Get or create namespace
	ns, exists := c.data[namespace]
	if !exists {
		ns = &Namespace{
			entries: make(map[string]EntryConfig),
		}
		c.data[namespace] = ns
	}

	// Get or create entry config
	entryConfig, exists := ns.entries[entry]
	if !exists {
		entryConfig = make(EntryConfig)
		ns.entries[entry] = entryConfig
	}

	// Set field value
	entryConfig[field] = value
	return nil
}

// SetFromString parses and sets a configuration entry from a full string.
func (c *Config) SetFromString(input string) error {
	namespace, entry, field, value, err := Parse(input)
	if err != nil {
		return err
	}
	return c.Set(namespace, entry, field, value)
}

// Get retrieves a configuration value as a string.
// Returns the value, whether it exists, and any error.
// Entry is the entry name, field is the field path (can contain dots for nesting).
func (c *Config) Get(namespace, entry, field string) (string, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ns, exists := c.data[namespace]
	if !exists {
		return "", false, nil
	}

	entryConfig, exists := ns.entries[entry]
	if !exists {
		return "", false, nil
	}

	value, ok := entryConfig[field]
	if !ok {
		return "", false, nil
	}

	return value, true, nil
}

// GetString retrieves a configuration value as a string.
// This is an alias for Get() for API consistency.
func (c *Config) GetString(namespace, entry, field string) (string, bool, error) {
	return c.Get(namespace, entry, field)
}

// Has checks if a configuration value exists.
func (c *Config) Has(namespace, entry, field string) bool {
	_, exists, _ := c.Get(namespace, entry, field)
	return exists
}

// GetNamespace retrieves all entries in a namespace.
// Returns entries grouped by entry name for compatibility with app.go.
func (c *Config) GetNamespace(namespace string) (map[string]EntryConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ns, exists := c.data[namespace]
	if !exists || ns == nil {
		return nil, false
	}

	// Build result map: key is entry name, value is EntryConfig (map[field]value)
	result := make(map[string]EntryConfig, len(ns.entries))
	for entryName, entryConfig := range ns.entries {
		// Create a copy of EntryConfig to avoid external modification
		entryConfigCopy := make(EntryConfig, len(entryConfig))
		for field, value := range entryConfig {
			entryConfigCopy[field] = value
		}
		result[entryName] = entryConfigCopy
	}

	return result, true
}

// GetAllNamespaces returns a list of all namespace names.
func (c *Config) GetAllNamespaces() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	namespaces := make([]string, 0, len(c.data))
	for ns := range c.data {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// ToMap converts the entire configuration to a nested map structure.
func (c *Config) ToMap() map[string]map[string]EntryConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]map[string]EntryConfig, len(c.data))
	for namespaceName, namespace := range c.data {
		if namespace == nil {
			continue
		}

		// Build entry map for this namespace
		entryMap := make(map[string]EntryConfig, len(namespace.entries))
		for entryName, entryConfig := range namespace.entries {
			// Create a copy of EntryConfig to avoid external modification
			entryConfigCopy := make(EntryConfig, len(entryConfig))
			for field, value := range entryConfig {
				entryConfigCopy[field] = value
			}
			entryMap[entryName] = entryConfigCopy
		}
		result[namespaceName] = entryMap
	}

	return result
}
