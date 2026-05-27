// SPDX-License-Identifier: MPL-2.0

// Package supervisor provides service lifecycle management and supervision.
package supervisor

import (
	"encoding/json"
	"time"

	"github.com/wippyai/runtime/api/security"
)

type (
	StartupMode string

	// LifecycleConfig defines the configuration for a service managed by the supervisor.
	LifecycleConfig struct {
		// Startup controls whether an auto-start root is strict or may degrade independently.
		Startup  StartupMode      `json:"startup,omitempty" yaml:"startup" default:"required"`
		Security *security.Config `json:"security,omitempty" yaml:"security,omitempty"`
		// Requires lists other supervisor services that must be running before this one starts.
		Requires []string `json:"requires" yaml:"requires" default:"[]"`
		// DependsOn is the legacy spelling kept for older modules. New manifests should use Requires.
		DependsOn       []string      `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
		RetryPolicy     RetryPolicy   `json:"restart" yaml:"restart"`
		StartTimeout    time.Duration `json:"start_timeout,omitzero,format:units" yaml:"start_timeout" default:"10s"`
		StopTimeout     time.Duration `json:"stop_timeout,omitzero,format:units" yaml:"stop_timeout" default:"10s"`
		StableThreshold time.Duration `json:"stable_threshold,omitzero,format:units" yaml:"stable_threshold" default:"5s"`
		AutoStart       bool          `json:"auto_start" yaml:"auto_start" default:"false"`
	}

	// RetryPolicy defines the parameters for retrying a service after a failure.
	RetryPolicy struct {
		// InitialDelay specifies the initial delay before the first retry attempt.
		InitialDelay time.Duration `json:"initial_delay,omitzero,format:units" yaml:"initial_delay" default:"1s"`
		// MaxDelay specifies the maximum delay between retry attempts.
		MaxDelay time.Duration `json:"max_delay,omitzero,format:units" yaml:"max_delay" default:"90s"`
		// BackoffFactor determines the exponential backoff factor for increasing the delay between retries.
		BackoffFactor float64 `json:"backoff_factor" yaml:"backoff_factor" default:"2.0"`
		// Jitter introduces random variation to the retry delay to prevent synchronized retries.
		Jitter float64 `json:"jitter" yaml:"jitter" default:"0.1"`
		// MaxAttempts specifies the maximum number of retry attempts before giving up, 0 - infinite.
		MaxAttempts int `json:"max_attempts" yaml:"max_attempts" default:"0"`
	}
)

const (
	StartupRequired StartupMode = "required"
	StartupOptional StartupMode = "optional"
)

func (cfg LifecycleConfig) RequiredServices() []string {
	if len(cfg.Requires) == 0 {
		return append([]string(nil), cfg.DependsOn...)
	}

	seen := make(map[string]struct{}, len(cfg.Requires)+len(cfg.DependsOn))
	result := make([]string, 0, len(cfg.Requires)+len(cfg.DependsOn))
	for _, dep := range cfg.Requires {
		if _, exists := seen[dep]; exists {
			continue
		}
		seen[dep] = struct{}{}
		result = append(result, dep)
	}
	for _, dep := range cfg.DependsOn {
		if _, exists := seen[dep]; exists {
			continue
		}
		seen[dep] = struct{}{}
		result = append(result, dep)
	}

	return result
}

func (cfg LifecycleConfig) StartupMode() StartupMode {
	if cfg.Startup == "" {
		return StartupRequired
	}
	if cfg.Startup == StartupOptional {
		return StartupOptional
	}
	return StartupRequired
}

func (cfg LifecycleConfig) StartupRequired() bool {
	return cfg.StartupMode() == StartupRequired
}

// InitDefaults initializes the LifecycleConfig with default values if they are not set.
// This includes setting default timeouts, retry policies, and backoff parameters.
func (cfg *LifecycleConfig) InitDefaults() {
	if cfg.Startup == "" {
		cfg.Startup = StartupRequired
	}

	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 10 * time.Second
	}

	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = 10 * time.Second
	}

	if cfg.StableThreshold == 0 {
		cfg.StableThreshold = 5 * time.Second
	}

	if cfg.RetryPolicy.InitialDelay == 0 {
		cfg.RetryPolicy.InitialDelay = time.Second
	}

	if cfg.RetryPolicy.MaxDelay == 0 {
		cfg.RetryPolicy.MaxDelay = 90 * time.Second
	}

	if cfg.RetryPolicy.BackoffFactor == 0 {
		cfg.RetryPolicy.BackoffFactor = 2.0
	}

	if cfg.RetryPolicy.Jitter == 0 {
		cfg.RetryPolicy.Jitter = 0.1
	}
}

// lifecycleConfigJSON is used for JSON marshaling/unmarshaling with string durations
type lifecycleConfigJSON struct {
	Security        *security.Config `json:"security,omitempty"`
	StartTimeout    string           `json:"start_timeout,omitempty"`
	StopTimeout     string           `json:"stop_timeout,omitempty"`
	StableThreshold string           `json:"stable_threshold,omitempty"`
	Startup         StartupMode      `json:"startup,omitempty"`
	Requires        []string         `json:"requires"`
	DependsOn       []string         `json:"depends_on,omitempty"`
	RetryPolicy     RetryPolicy      `json:"restart"`
	AutoStart       bool             `json:"auto_start"`
}

// UnmarshalJSON implements json.Unmarshaler to handle duration strings
func (cfg *LifecycleConfig) UnmarshalJSON(data []byte) error {
	var raw lifecycleConfigJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	cfg.AutoStart = raw.AutoStart
	cfg.RetryPolicy = raw.RetryPolicy
	cfg.Requires = raw.Requires
	cfg.DependsOn = raw.DependsOn
	cfg.Startup = raw.Startup
	cfg.Security = raw.Security

	if raw.StartTimeout != "" {
		d, err := time.ParseDuration(raw.StartTimeout)
		if err != nil {
			return err
		}
		cfg.StartTimeout = d
	}
	if raw.StopTimeout != "" {
		d, err := time.ParseDuration(raw.StopTimeout)
		if err != nil {
			return err
		}
		cfg.StopTimeout = d
	}
	if raw.StableThreshold != "" {
		d, err := time.ParseDuration(raw.StableThreshold)
		if err != nil {
			return err
		}
		cfg.StableThreshold = d
	}

	return nil
}

// MarshalJSON implements json.Marshaler to output durations as strings
func (cfg LifecycleConfig) MarshalJSON() ([]byte, error) {
	startup := cfg.Startup
	if startup != "" {
		startup = cfg.StartupMode()
	}

	raw := lifecycleConfigJSON{
		AutoStart:   cfg.AutoStart,
		RetryPolicy: cfg.RetryPolicy,
		Startup:     startup,
		Requires:    cfg.RequiredServices(),
		Security:    cfg.Security,
	}
	if cfg.StartTimeout != 0 {
		raw.StartTimeout = cfg.StartTimeout.String()
	}
	if cfg.StopTimeout != 0 {
		raw.StopTimeout = cfg.StopTimeout.String()
	}
	if cfg.StableThreshold != 0 {
		raw.StableThreshold = cfg.StableThreshold.String()
	}
	return json.Marshal(raw)
}

// retryPolicyJSON is used for JSON marshaling/unmarshaling with string durations
type retryPolicyJSON struct {
	InitialDelay  string  `json:"initial_delay,omitempty"`
	MaxDelay      string  `json:"max_delay,omitempty"`
	BackoffFactor float64 `json:"backoff_factor"`
	Jitter        float64 `json:"jitter"`
	MaxAttempts   int     `json:"max_attempts"`
}

// UnmarshalJSON implements json.Unmarshaler to handle duration strings
func (rp *RetryPolicy) UnmarshalJSON(data []byte) error {
	var raw retryPolicyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	rp.BackoffFactor = raw.BackoffFactor
	rp.Jitter = raw.Jitter
	rp.MaxAttempts = raw.MaxAttempts

	if raw.InitialDelay != "" {
		d, err := time.ParseDuration(raw.InitialDelay)
		if err != nil {
			return err
		}
		rp.InitialDelay = d
	}
	if raw.MaxDelay != "" {
		d, err := time.ParseDuration(raw.MaxDelay)
		if err != nil {
			return err
		}
		rp.MaxDelay = d
	}

	return nil
}

// MarshalJSON implements json.Marshaler to output durations as strings
func (rp RetryPolicy) MarshalJSON() ([]byte, error) {
	raw := retryPolicyJSON{
		BackoffFactor: rp.BackoffFactor,
		Jitter:        rp.Jitter,
		MaxAttempts:   rp.MaxAttempts,
	}
	if rp.InitialDelay != 0 {
		raw.InitialDelay = rp.InitialDelay.String()
	}
	if rp.MaxDelay != 0 {
		raw.MaxDelay = rp.MaxDelay.String()
	}
	return json.Marshal(raw)
}
