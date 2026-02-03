package lsp

// DefaultAddress is the default TCP address for the LSP server.
const DefaultAddress = ":7777"

// Config defines LSP server configuration.
type Config struct {
	Address string `json:"address" yaml:"address"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Address: DefaultAddress,
	}
}

// Validate checks and normalizes configuration values.
func (c *Config) Validate() {
	if c.Address == "" {
		c.Address = DefaultAddress
	}
}
