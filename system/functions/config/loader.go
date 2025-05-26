package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config represents the module configuration
type Config struct {
	Version string `yaml:"version"`
	Meta    struct {
		Interceptors struct {
			Global struct {
				OTEL struct {
					Enabled          bool              `yaml:"enabled"`
					ServiceName      string            `yaml:"service_name"`
					CustomAttributes map[string]string `yaml:"custom_attributes"`
				} `yaml:"otel"`
				Retry struct {
					Enabled         bool    `yaml:"enabled"`
					MaxAttempts     int     `yaml:"max_attempts"`
					InitialInterval string  `yaml:"initial_interval"`
					MaxInterval     string  `yaml:"max_interval"`
					Multiplier      float64 `yaml:"multiplier"`
				} `yaml:"retry"`
				RateLimit struct {
					Enabled           bool `yaml:"enabled"`
					RequestsPerSecond int  `yaml:"requests_per_second"`
					Burst             int  `yaml:"burst"`
				} `yaml:"rate_limit"`
			} `yaml:"global"`
			Functions map[string]struct {
				OTEL struct {
					Enabled          bool              `yaml:"enabled"`
					CustomAttributes map[string]string `yaml:"custom_attributes"`
				} `yaml:"otel"`
				Retry struct {
					Enabled         bool    `yaml:"enabled"`
					MaxAttempts     int     `yaml:"max_attempts"`
					InitialInterval string  `yaml:"initial_interval"`
					MaxInterval     string  `yaml:"max_interval"`
					Multiplier      float64 `yaml:"multiplier"`
				} `yaml:"retry"`
				RateLimit struct {
					Enabled           bool `yaml:"enabled"`
					RequestsPerSecond int  `yaml:"requests_per_second"`
					Burst             int  `yaml:"burst"`
				} `yaml:"rate_limit"`
			} `yaml:"functions"`
		} `yaml:"interceptors"`
	} `yaml:"meta"`
}

// Load loads the configuration from a YAML file
func Load(data []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &config, nil
}

// GetFunctionConfig returns the configuration for a specific function
func (c *Config) GetFunctionConfig(functionName string) (map[string]interface{}, error) {
	if c.Meta.Interceptors.Functions == nil {
		return nil, fmt.Errorf("no function configurations found")
	}

	config, ok := c.Meta.Interceptors.Functions[functionName]
	if !ok {
		return nil, fmt.Errorf("no configuration found for function %s", functionName)
	}

	// Convert to map for easier access
	result := make(map[string]interface{})

	// Add OTEL config
	if config.OTEL.Enabled {
		result["otel"] = map[string]interface{}{
			"enabled":           config.OTEL.Enabled,
			"custom_attributes": config.OTEL.CustomAttributes,
		}
	}

	// Add retry config
	if config.Retry.Enabled {
		result["retry"] = map[string]interface{}{
			"enabled":          config.Retry.Enabled,
			"max_attempts":     config.Retry.MaxAttempts,
			"initial_interval": config.Retry.InitialInterval,
			"max_interval":     config.Retry.MaxInterval,
			"multiplier":       config.Retry.Multiplier,
		}
	}

	// Add rate limit config
	if config.RateLimit.Enabled {
		result["rate_limit"] = map[string]interface{}{
			"enabled":             config.RateLimit.Enabled,
			"requests_per_second": config.RateLimit.RequestsPerSecond,
			"burst":               config.RateLimit.Burst,
		}
	}

	return result, nil
}

// GetGlobalConfig returns the global interceptor configuration
func (c *Config) GetGlobalConfig() map[string]interface{} {
	result := make(map[string]interface{})

	// Add OTEL config
	if c.Meta.Interceptors.Global.OTEL.Enabled {
		result["otel"] = map[string]interface{}{
			"enabled":           c.Meta.Interceptors.Global.OTEL.Enabled,
			"service_name":      c.Meta.Interceptors.Global.OTEL.ServiceName,
			"custom_attributes": c.Meta.Interceptors.Global.OTEL.CustomAttributes,
		}
	}

	// Add retry config
	if c.Meta.Interceptors.Global.Retry.Enabled {
		result["retry"] = map[string]interface{}{
			"enabled":          c.Meta.Interceptors.Global.Retry.Enabled,
			"max_attempts":     c.Meta.Interceptors.Global.Retry.MaxAttempts,
			"initial_interval": c.Meta.Interceptors.Global.Retry.InitialInterval,
			"max_interval":     c.Meta.Interceptors.Global.Retry.MaxInterval,
			"multiplier":       c.Meta.Interceptors.Global.Retry.Multiplier,
		}
	}

	// Add rate limit config
	if c.Meta.Interceptors.Global.RateLimit.Enabled {
		result["rate_limit"] = map[string]interface{}{
			"enabled":             c.Meta.Interceptors.Global.RateLimit.Enabled,
			"requests_per_second": c.Meta.Interceptors.Global.RateLimit.RequestsPerSecond,
			"burst":               c.Meta.Interceptors.Global.RateLimit.Burst,
		}
	}

	return result
}
