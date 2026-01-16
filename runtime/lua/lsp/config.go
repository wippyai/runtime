package lsp

// Config defines LSP server configuration.
type Config struct {
	// Enabled controls whether LSP server starts with the runtime.
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Mode specifies the transport: "stdio" or "tcp".
	Mode string `json:"mode" yaml:"mode"`

	// Address for TCP mode (e.g., ":7777" or "localhost:7777").
	Address string `json:"address" yaml:"address"`

	// Debug enables verbose logging.
	Debug bool `json:"debug" yaml:"debug"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled: false,
		Mode:    "stdio",
		Address: ":7777",
		Debug:   false,
	}
}

// Validate checks and normalizes configuration values.
func (c *Config) Validate() {
	if c.Mode != "stdio" && c.Mode != "tcp" {
		c.Mode = "stdio"
	}
	if c.Mode == "tcp" && c.Address == "" {
		c.Address = ":7777"
	}
}
