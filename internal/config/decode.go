package config

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// DecodeAndInitConfig decodes the configuration and initializes defaults
func DecodeAndInitConfig[T any](dtt payload.Transcoder, entry registry.Entry) (*T, error) {
	if entry.Data == nil {
		return nil, fmt.Errorf("configuration data is required")
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Initialize defaults if the config implements InitDefaults
	if defaulter, ok := interface{}(cfg).(interface{ InitDefaults() }); ok {
		defaulter.InitDefaults()
	}

	// Validate if the config implements Validate
	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return cfg, nil
}
