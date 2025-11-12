package boot

import (
	"context"
	"strings"
	"time"
)

// ConfigSep is the separator used for hierarchical config keys.
const ConfigSep = "."

// ConfigKey is a string alias for typed config key declarations in plugins.
type ConfigKey string

type configCtxKey struct{}

// WithConfig attaches Config to context.
func WithConfig(ctx context.Context, cfg Config) context.Context {
	return context.WithValue(ctx, configCtxKey{}, cfg)
}

// GetConfig retrieves Config from context.
// Returns nil if no Config is found.
func GetConfig(ctx context.Context) Config {
	if cfg, ok := ctx.Value(configCtxKey{}).(Config); ok {
		return cfg
	}
	return nil
}

// Config provides typed access to configuration values with prefix-based scoping.
type Config interface {
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

type config struct {
	prefix string
	values map[string]any
}

// NewConfig creates a new Config from a map of string keys to arbitrary values.
func NewConfig(values map[string]any) Config {
	return &config{
		prefix: "",
		values: values,
	}
}

func (c *config) Get(key string) (any, bool) {
	v, ok := c.values[c.prefix+key]
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
	for k := range c.values {
		if strings.HasPrefix(k, c.prefix) {
			keys = append(keys, strings.TrimPrefix(k, c.prefix))
		}
	}
	return keys
}

func (c *config) Sub(prefix string) Config {
	return &config{
		prefix: c.prefix + prefix + ConfigSep,
		values: c.values,
	}
}
