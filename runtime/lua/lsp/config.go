package lsp

// DefaultAddress is the default TCP address for the LSP server.
const DefaultAddress = ":7777"

// DefaultHTTPAddress is the default TCP address for the LSP HTTP server.
const DefaultHTTPAddress = ":7778"

// DefaultHTTPPath is the default HTTP path for JSON-RPC requests.
const DefaultHTTPPath = "/lsp"

// DefaultHTTPAllowOrigin sets the default CORS origin when HTTP is enabled.
const DefaultHTTPAllowOrigin = "*"

// DefaultMaxMessageBytes caps incoming JSON-RPC payload size.
const DefaultMaxMessageBytes = 8 << 20

// Config defines LSP server configuration.
type Config struct {
	Address         string `json:"address" yaml:"address"`
	HTTPAddress     string `json:"http_address" yaml:"http_address"`
	HTTPPath        string `json:"http_path" yaml:"http_path"`
	HTTPAllowOrigin string `json:"http_allow_origin" yaml:"http_allow_origin"`
	MaxMessageBytes int    `json:"max_message_bytes" yaml:"max_message_bytes"`
	Enabled         bool   `json:"enabled" yaml:"enabled"`
	HTTPEnabled     bool   `json:"http_enabled" yaml:"http_enabled"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Address:         DefaultAddress,
		MaxMessageBytes: DefaultMaxMessageBytes,
		HTTPEnabled:     false,
		HTTPAddress:     DefaultHTTPAddress,
		HTTPPath:        DefaultHTTPPath,
		HTTPAllowOrigin: DefaultHTTPAllowOrigin,
	}
}

// Validate checks and normalizes configuration values.
func (c *Config) Validate() {
	if c.Address == "" {
		c.Address = DefaultAddress
	}
	if c.MaxMessageBytes <= 0 {
		c.MaxMessageBytes = DefaultMaxMessageBytes
	}
	if c.HTTPAddress == "" {
		c.HTTPAddress = DefaultHTTPAddress
	}
	if c.HTTPPath == "" {
		c.HTTPPath = DefaultHTTPPath
	}
	if c.HTTPAllowOrigin == "" {
		c.HTTPAllowOrigin = DefaultHTTPAllowOrigin
	}
}
