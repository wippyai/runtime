package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config represents lint configuration
type Config struct {
	// Rules maps rule names to their configuration
	Rules map[string]RuleConfig `json:"rules"`

	// Extends lists config files to inherit from
	Extends []string `json:"extends,omitempty"`
}

// RuleConfig configures a single rule
type RuleConfig struct {
	// Severity overrides the rule's default severity
	// Values: "off", "hint", "warning", "error"
	Severity string `json:"severity,omitempty"`

	// Options are rule-specific configuration
	Options map[string]any `json:"options,omitempty"`
}

// DefaultConfig returns a config with all rules at default severity
func DefaultConfig() *Config {
	return &Config{
		Rules: make(map[string]RuleConfig),
	}
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Process extends
	if len(cfg.Extends) > 0 {
		dir := filepath.Dir(path)
		for _, extend := range cfg.Extends {
			extendPath := filepath.Join(dir, extend)
			parent, err := LoadConfig(extendPath)
			if err != nil {
				return nil, err
			}
			cfg = cfg.Merge(parent)
		}
	}

	return &cfg, nil
}

// Merge combines two configs, with c taking precedence
func (c Config) Merge(parent *Config) Config {
	if parent == nil {
		return c
	}

	result := Config{
		Rules: make(map[string]RuleConfig),
	}

	// Copy parent rules
	for name, rule := range parent.Rules {
		result.Rules[name] = rule
	}

	// Override with child rules
	for name, rule := range c.Rules {
		result.Rules[name] = rule
	}

	return result
}

// Apply applies the configuration to a registry
func (c *Config) Apply(registry *Registry) {
	if c == nil || registry == nil {
		return
	}

	for name, rule := range c.Rules {
		severity := ParseSeverity(rule.Severity)
		registry.SetSeverity(name, severity)
	}
}

// ParseSeverity converts a string to a Severity level
func ParseSeverity(s string) Severity {
	switch s {
	case "off", "0":
		return SeverityOff
	case "hint", "1":
		return SeverityHint
	case "warning", "warn", "2":
		return SeverityWarning
	case "error", "err", "3":
		return SeverityError
	default:
		return SeverityWarning // default to warning
	}
}

// SeverityString returns the string representation of a severity
func SeverityString(s Severity) string {
	switch s {
	case SeverityOff:
		return "off"
	case SeverityHint:
		return "hint"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}
