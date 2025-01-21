package supervisor

import (
	"encoding/json"
	"fmt"
	"time"
)

type (
	// LifecycleConfig defines the configuration for a service managed by the supervisor.
	LifecycleConfig struct {
		// AutoStart determines if the service should start automatically when the supervisor starts.
		AutoStart bool `json:"auto_start" yaml:"auto_start" default:"false"`
		// StartTimeout specifies the maximum duration allowed for the service to start.
		StartTimeout time.Duration `json:"start_timeout" yaml:"start_timeout" default:"30s"`
		// StopTimeout specifies the maximum duration allowed for the service to stop.
		StopTimeout time.Duration `json:"stop_timeout" yamal:"stop_timeout" default:"30s"`
		// RetryPolicy defines the policy for retrying a failed service.
		RetryPolicy RetryPolicy `json:"restart" yaml:"restart"`
		// DependsOn specifies a list of service names that this service depends on.
		DependsOn []string `json:"depends_on" yaml:"depends_on" default:"[]"` // Empty array
	}

	// RetryPolicy defines the parameters for retrying a service after a failure.
	RetryPolicy struct {
		// InitialDelay specifies the initial delay before the first retry attempt.
		InitialDelay time.Duration `json:"initial_delay" yaml:"initial_delay" default:"1s"`
		// MaxDelay specifies the maximum delay between retry attempts.
		MaxDelay time.Duration `json:"max_delay" yaml:"max_delay" default:"30s"`
		// BackoffFactor determines the exponential backoff factor for increasing the delay between retries.
		BackoffFactor float64 `json:"backoff_factor" yaml:"backoff_factor" default:"2.0"`
		// Jitter introduces random variation to the retry delay to prevent synchronized retries.
		Jitter float64 `json:"jitter" yaml:"jitter" default:"0.1"`
		// MaxAttempts specifies the maximum number of retry attempts before giving up.
		MaxAttempts int `json:"max_attempts" yaml:"max_attempts" default:"5"`
	}
)

// UnmarshalJSON provides custom unmarshaling for LifecycleConfig, handling nested time.Duration fields.
func (cfg *LifecycleConfig) UnmarshalJSON(data []byte) error {
	type Alias LifecycleConfig
	aux := &struct {
		StartTimeout string `json:"start_timeout"`
		StopTimeout  string `json:"stop_timeout"`
		*Alias
	}{
		Alias: (*Alias)(cfg),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.StartTimeout != "" {
		cfg.StartTimeout, err = time.ParseDuration(aux.StartTimeout)
		if err != nil {
			return fmt.Errorf("invalid StartTimeout duration format: %w", err)
		}
	}

	if aux.StopTimeout != "" {
		cfg.StopTimeout, err = time.ParseDuration(aux.StopTimeout)
		if err != nil {
			return fmt.Errorf("invalid StopTimeout duration format: %w", err)
		}
	}

	return nil
}

// UnmarshalJSON provides custom unmarshaling for RetryPolicy, handling nested time.Duration fields.
func (p *RetryPolicy) UnmarshalJSON(data []byte) error {
	type Alias RetryPolicy
	aux := &struct {
		InitialDelay string `json:"initial_delay"`
		MaxDelay     string `json:"max_delay"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.InitialDelay != "" {
		p.InitialDelay, err = time.ParseDuration(aux.InitialDelay)
		if err != nil {
			return fmt.Errorf("invalid InitialDelay duration format: %w", err)
		}
	}

	if aux.MaxDelay != "" {
		p.MaxDelay, err = time.ParseDuration(aux.MaxDelay)
		if err != nil {
			return fmt.Errorf("invalid MaxDelay duration format: %w", err)
		}
	}

	return nil
}

func (cfg *LifecycleConfig) InitDefaults() {
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 30 * time.Second
	}

	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = 30 * time.Second
	}

	if cfg.RetryPolicy.InitialDelay == 0 {
		cfg.RetryPolicy.InitialDelay = time.Second
	}

	if cfg.RetryPolicy.MaxDelay == 0 {
		cfg.RetryPolicy.MaxDelay = 30 * time.Second
	}

	if cfg.RetryPolicy.BackoffFactor == 0 {
		cfg.RetryPolicy.BackoffFactor = 2.0
	}

	if cfg.RetryPolicy.Jitter == 0 {
		cfg.RetryPolicy.Jitter = 0.1
	}

	if cfg.RetryPolicy.MaxAttempts == 0 {
		cfg.RetryPolicy.MaxAttempts = 5
	}
}

// MarshalJSON provides custom marshaling for LifecycleConfig, converting time.Duration to strings
func (cfg *LifecycleConfig) MarshalJSON() ([]byte, error) {
	type Alias LifecycleConfig
	return json.Marshal(&struct {
		StartTimeout string `json:"start_timeout"`
		StopTimeout  string `json:"stop_timeout"`
		*Alias
	}{
		StartTimeout: cfg.StartTimeout.String(),
		StopTimeout:  cfg.StopTimeout.String(),
		Alias:        (*Alias)(cfg),
	})
}

// MarshalJSON provides custom marshaling for RetryPolicy, converting time.Duration to strings
func (p *RetryPolicy) MarshalJSON() ([]byte, error) {
	type Alias RetryPolicy
	return json.Marshal(&struct {
		InitialDelay string `json:"initial_delay"`
		MaxDelay     string `json:"max_delay"`
		*Alias
	}{
		InitialDelay: p.InitialDelay.String(),
		MaxDelay:     p.MaxDelay.String(),
		Alias:        (*Alias)(p),
	})
}
