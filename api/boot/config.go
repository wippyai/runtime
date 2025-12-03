// Package boot provides application boot and component loading.
package boot

import (
	"strings"
	"time"
)

// ConfigSep is the separator used for hierarchical config keys.
const ConfigSep = "."

type (
	// Name is a string alias for typed identifiers (component names, config keys, etc).
	Name = string
	// Config provides typed access to configuration values with prefix-based scoping.
	Config interface {
		// Get retrieves a raw value by key. Returns the value and true if found, nil and false otherwise.
		Get(key string) (any, bool)

		// GetString retrieves a string value by key, returning def if not found.
		GetString(key string, def string) string

		// GetInt retrieves an int value by key, returning def if not found or type mismatch.
		GetInt(key string, def int) int

		// GetBool retrieves a bool value by key, returning def if not found or type mismatch.
		GetBool(key string, def bool) bool

		// GetDuration retrieves a time.Duration value by key, returning def if not found or type mismatch.
		GetDuration(key string, def time.Duration) time.Duration

		// Keys returns all keys in this config scope.
		Keys() []string

		// Sub returns a new Config scoped to the given prefix.
		// The separator is automatically appended.
		Sub(prefix string) Config
	}

	// ConfigOption is a functional option for configuring Config.
	ConfigOption func(*config)

	config struct {
		prefix  string
		buckets map[string]map[string]any
	}
)

// WithSection adds a configuration section with the given name and values.
func WithSection(name string, values map[string]any) ConfigOption {
	return func(c *config) {
		c.buckets[name] = values
	}
}

// NewConfig creates a new Config with the provided options.
func NewConfig(opts ...ConfigOption) Config {
	cfg := &config{
		prefix:  "",
		buckets: make(map[string]map[string]any),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

func (c *config) Get(key string) (any, bool) {
	fullKey := c.prefix + key
	parts := strings.Split(fullKey, ConfigSep)

	if len(parts) < 2 {
		return nil, false
	}

	bucket, ok := c.buckets[parts[0]]
	if !ok {
		return nil, false
	}

	subKey := strings.Join(parts[1:], ConfigSep)
	v, ok := bucket[subKey]
	return v, ok
}

func (c *config) GetString(key string, def string) string {
	v, ok := c.Get(key)
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func (c *config) GetInt(key string, def int) int {
	v, ok := c.Get(key)
	if !ok {
		return def
	}
	if i, ok := v.(int); ok {
		return i
	}
	return def
}

func (c *config) GetBool(key string, def bool) bool {
	v, ok := c.Get(key)
	if !ok {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func (c *config) GetDuration(key string, def time.Duration) time.Duration {
	v, ok := c.Get(key)
	if !ok {
		return def
	}
	if d, ok := v.(time.Duration); ok {
		return d
	}
	return def
}

func (c *config) Keys() []string {
	keys := make([]string, 0)

	if c.prefix == "" {
		for section := range c.buckets {
			for key := range c.buckets[section] {
				keys = append(keys, section+ConfigSep+key)
			}
		}
		return keys
	}

	parts := strings.Split(strings.TrimSuffix(c.prefix, ConfigSep), ConfigSep)
	if len(parts) == 0 {
		return keys
	}

	section := parts[0]
	bucket, ok := c.buckets[section]
	if !ok {
		return keys
	}

	prefixInBucket := ""
	if len(parts) > 1 {
		prefixInBucket = strings.Join(parts[1:], ConfigSep) + ConfigSep
	}

	for key := range bucket {
		if strings.HasPrefix(key, prefixInBucket) {
			keys = append(keys, strings.TrimPrefix(key, prefixInBucket))
		}
	}

	return keys
}

func (c *config) Sub(prefix string) Config {
	return &config{
		prefix:  c.prefix + prefix + ConfigSep,
		buckets: c.buckets,
	}
}
