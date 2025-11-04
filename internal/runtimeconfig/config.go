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

// Config represents a hierarchical runtime configuration store.
type Config struct {
	mu   sync.RWMutex
	data map[string]*Namespace
}

// Namespace represents a configuration namespace (e.g., "app").
type Namespace struct {
	entries map[string]string // key format: "entry.field" or "entry.nested.field"
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

	// Get or create namespace
	ns, exists := c.data[namespace]
	if !exists {
		ns = &Namespace{
			entries: make(map[string]string),
		}
		c.data[namespace] = ns
	}

	// Combine entry and field into a single key
	key := entry + "." + field
	ns.entries[key] = value
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

	key := entry + "." + field
	value, ok := ns.entries[key]
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
	if !exists {
		return nil, false
	}

	return c.getNamespaceUnlocked(ns)
}

// setNestedField sets a value in EntryConfig using dot-separated path as key.
func setNestedField(m EntryConfig, path string, value string) {
	m[path] = value
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
	for ns, namespace := range c.data {
		nsMap, _ := c.getNamespaceUnlocked(namespace)
		result[ns] = nsMap
	}
	return result
}

// getNamespaceUnlocked is an unlocked version of GetNamespace for internal use.
func (c *Config) getNamespaceUnlocked(ns *Namespace) (map[string]EntryConfig, bool) {
	if ns == nil {
		return nil, false
	}

	// Group entries by entry name
	result := make(map[string]EntryConfig)
	for key, value := range ns.entries {
		// Split key into entry and field
		dotIdx := strings.Index(key, ".")
		if dotIdx == -1 {
			continue
		}
		entryName := key[:dotIdx]
		fieldPath := key[dotIdx+1:]

		// Get or create entry map
		entryMap, ok := result[entryName]
		if !ok {
			entryMap = make(EntryConfig)
			result[entryName] = entryMap
		}

		// Set nested field in entry map
		setNestedField(entryMap, fieldPath, value)
	}

	return result, true
}
