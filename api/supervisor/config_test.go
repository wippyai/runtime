package supervisor

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycleConfig_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		config   LifecycleConfig
		expected string
		wantErr  bool
	}{
		{
			name: "basic config",
			config: LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 30 * time.Second,
				StopTimeout:  1 * time.Minute,
				RetryPolicy: RetryPolicy{
					InitialDelay:  1 * time.Second,
					MaxDelay:      30 * time.Second,
					BackoffFactor: 2.0,
					Jitter:        0.1,
					MaxAttempts:   5,
				},
				DependsOn: []string{"service1", "service2"},
			},
			expected: `{"auto_start":true,"start_timeout":"30s","stop_timeout":"1m0s","restart":{"initial_delay":"1s","max_delay":"30s","backoff_factor":2,"jitter":0.1,"max_attempts":5},"depends_on":["service1","service2"]}`,
			wantErr:  false,
		},
		{
			name: "zero values",
			config: LifecycleConfig{
				AutoStart: false,
			},
			expected: `{"auto_start":false,"start_timeout":"0s","stop_timeout":"0s","restart":{"initial_delay":"0s","max_delay":"0s","backoff_factor":0,"jitter":0,"max_attempts":0},"depends_on":null}`,
			wantErr:  false,
		},
		{
			name: "custom durations",
			config: LifecycleConfig{
				StartTimeout: 1*time.Hour + 30*time.Minute,
				StopTimeout:  2*time.Hour + 15*time.Minute,
				RetryPolicy: RetryPolicy{
					InitialDelay: 1*time.Minute + 30*time.Second,
					MaxDelay:     5*time.Minute + 45*time.Second,
				},
			},
			expected: `{"auto_start":false,"start_timeout":"1h30m0s","stop_timeout":"2h15m0s","restart":{"initial_delay":"1m30s","max_delay":"5m45s","backoff_factor":0,"jitter":0,"max_attempts":0},"depends_on":null}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			// Verify roundtrip
			var decoded LifecycleConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestLifecycleConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected LifecycleConfig
		wantErr  bool
	}{
		{
			name: "valid config",
			json: `{
				"auto_start": true,
				"start_timeout": "30s",
				"stop_timeout": "1m",
				"restart": {
					"initial_delay": "1s",
					"max_delay": "30s",
					"backoff_factor": 2.0,
					"jitter": 0.1,
					"max_attempts": 5
				},
				"depends_on": ["service1", "service2"]
			}`,
			expected: LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 30 * time.Second,
				StopTimeout:  1 * time.Minute,
				RetryPolicy: RetryPolicy{
					InitialDelay:  1 * time.Second,
					MaxDelay:      30 * time.Second,
					BackoffFactor: 2.0,
					Jitter:        0.1,
					MaxAttempts:   5,
				},
				DependsOn: []string{"service1", "service2"},
			},
			wantErr: false,
		},
		{
			name: "invalid start timeout",
			json: `{
				"start_timeout": "invalid",
				"stop_timeout": "30s"
			}`,
			wantErr: true,
		},
		{
			name: "invalid stop timeout",
			json: `{
				"start_timeout": "30s",
				"stop_timeout": "invalid"
			}`,
			wantErr: true,
		},
		{
			name: "invalid retry delay",
			json: `{
				"restart": {
					"initial_delay": "invalid"
				}
			}`,
			wantErr: true,
		},
		{
			name: "empty object",
			json: `{}`,
			expected: LifecycleConfig{
				RetryPolicy: RetryPolicy{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config LifecycleConfig
			err := json.Unmarshal([]byte(tt.json), &config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, config)
		})
	}
}

func TestRetryPolicy_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		policy   RetryPolicy
		expected string
		wantErr  bool
	}{
		{
			name: "basic policy",
			policy: RetryPolicy{
				InitialDelay:  1 * time.Second,
				MaxDelay:      30 * time.Second,
				BackoffFactor: 2.0,
				Jitter:        0.1,
				MaxAttempts:   5,
			},
			expected: `{"initial_delay":"1s","max_delay":"30s","backoff_factor":2,"jitter":0.1,"max_attempts":5}`,
			wantErr:  false,
		},
		{
			name:     "zero values",
			policy:   RetryPolicy{},
			expected: `{"initial_delay":"0s","max_delay":"0s","backoff_factor":0,"jitter":0,"max_attempts":0}`,
			wantErr:  false,
		},
		{
			name: "custom durations",
			policy: RetryPolicy{
				InitialDelay:  1*time.Minute + 30*time.Second,
				MaxDelay:      5*time.Minute + 45*time.Second,
				BackoffFactor: 1.5,
				Jitter:        0.2,
				MaxAttempts:   3,
			},
			expected: `{"initial_delay":"1m30s","max_delay":"5m45s","backoff_factor":1.5,"jitter":0.2,"max_attempts":3}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.policy)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			// Verify roundtrip
			var decoded RetryPolicy
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.policy, decoded)
		})
	}
}

func TestRetryPolicy_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected RetryPolicy
		wantErr  bool
	}{
		{
			name: "valid policy",
			json: `{
				"initial_delay": "1s",
				"max_delay": "30s",
				"backoff_factor": 2.0,
				"jitter": 0.1,
				"max_attempts": 5
			}`,
			expected: RetryPolicy{
				InitialDelay:  1 * time.Second,
				MaxDelay:      30 * time.Second,
				BackoffFactor: 2.0,
				Jitter:        0.1,
				MaxAttempts:   5,
			},
			wantErr: false,
		},
		{
			name: "invalid initial delay",
			json: `{
				"initial_delay": "invalid",
				"max_delay": "30s"
			}`,
			wantErr: true,
		},
		{
			name: "invalid max delay",
			json: `{
				"initial_delay": "1s",
				"max_delay": "invalid"
			}`,
			wantErr: true,
		},
		{
			name:     "empty object",
			json:     `{}`,
			expected: RetryPolicy{},
			wantErr:  false,
		},
		{
			name: "complex duration strings",
			json: `{
				"initial_delay": "1h30m15s",
				"max_delay": "2h45m30s",
				"backoff_factor": 1.5,
				"jitter": 0.2,
				"max_attempts": 3
			}`,
			expected: RetryPolicy{
				InitialDelay:  1*time.Hour + 30*time.Minute + 15*time.Second,
				MaxDelay:      2*time.Hour + 45*time.Minute + 30*time.Second,
				BackoffFactor: 1.5,
				Jitter:        0.2,
				MaxAttempts:   3,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var policy RetryPolicy
			err := json.Unmarshal([]byte(tt.json), &policy)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, policy)
		})
	}
}

func TestLifecycleConfig_InitDefaults(t *testing.T) {
	tests := []struct {
		name          string
		initial       LifecycleConfig
		expectedAfter LifecycleConfig
		description   string
	}{
		{
			name:    "zero values - should set all defaults",
			initial: LifecycleConfig{},
			expectedAfter: LifecycleConfig{
				StartTimeout: 30 * time.Second,
				StopTimeout:  30 * time.Second,
				RetryPolicy: RetryPolicy{
					InitialDelay:  time.Second,
					MaxDelay:      30 * time.Second,
					BackoffFactor: 2.0,
					Jitter:        0.1,
					MaxAttempts:   5,
				},
			},
			description: "All zero values should be replaced with defaults",
		},
		{
			name: "custom values - should preserve them",
			initial: LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 1 * time.Minute,
				StopTimeout:  2 * time.Minute,
				RetryPolicy: RetryPolicy{
					InitialDelay:  2 * time.Second,
					MaxDelay:      1 * time.Minute,
					BackoffFactor: 3.0,
					Jitter:        0.2,
					MaxAttempts:   10,
				},
				DependsOn: []string{"service1"},
			},
			expectedAfter: LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 1 * time.Minute,
				StopTimeout:  2 * time.Minute,
				RetryPolicy: RetryPolicy{
					InitialDelay:  2 * time.Second,
					MaxDelay:      1 * time.Minute,
					BackoffFactor: 3.0,
					Jitter:        0.2,
					MaxAttempts:   10,
				},
				DependsOn: []string{"service1"},
			},
			description: "Existing non-zero values should be preserved",
		},
		{
			name: "partial values - should set remaining defaults",
			initial: LifecycleConfig{
				StartTimeout: 1 * time.Minute,
				RetryPolicy: RetryPolicy{
					InitialDelay: 2 * time.Second,
					MaxAttempts:  3,
				},
			},
			expectedAfter: LifecycleConfig{
				StartTimeout: 1 * time.Minute,
				StopTimeout:  30 * time.Second, // default
				RetryPolicy: RetryPolicy{
					InitialDelay:  2 * time.Second,  // preserved
					MaxDelay:      30 * time.Second, // default
					BackoffFactor: 2.0,              // default
					Jitter:        0.1,              // default
					MaxAttempts:   3,                // preserved
				},
			},
			description: "Only zero values should be replaced with defaults",
		},
		{
			name: "zero retry policy fields - should set retry defaults",
			initial: LifecycleConfig{
				StartTimeout: 1 * time.Minute,
				StopTimeout:  2 * time.Minute,
			},
			expectedAfter: LifecycleConfig{
				StartTimeout: 1 * time.Minute,
				StopTimeout:  2 * time.Minute,
				RetryPolicy: RetryPolicy{
					InitialDelay:  time.Second,
					MaxDelay:      30 * time.Second,
					BackoffFactor: 2.0,
					Jitter:        0.1,
					MaxAttempts:   5,
				},
			},
			description: "Zero RetryPolicy fields should get defaults while preserving other settings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.initial
			config.InitDefaults()

			// Verify all fields match expected values
			assert.Equal(t, tt.expectedAfter.AutoStart, config.AutoStart,
				"AutoStart mismatch")
			assert.Equal(t, tt.expectedAfter.StartTimeout, config.StartTimeout,
				"StartTimeout mismatch")
			assert.Equal(t, tt.expectedAfter.StopTimeout, config.StopTimeout,
				"StopTimeout mismatch")
			assert.Equal(t, tt.expectedAfter.DependsOn, config.DependsOn,
				"DependsOn mismatch")

			// Verify RetryPolicy fields
			assert.Equal(t, tt.expectedAfter.RetryPolicy.InitialDelay, config.RetryPolicy.InitialDelay,
				"RetryPolicy.InitialDelay mismatch")
			assert.Equal(t, tt.expectedAfter.RetryPolicy.MaxDelay, config.RetryPolicy.MaxDelay,
				"RetryPolicy.MaxDelay mismatch")
			assert.Equal(t, tt.expectedAfter.RetryPolicy.BackoffFactor, config.RetryPolicy.BackoffFactor,
				"RetryPolicy.BackoffFactor mismatch")
			assert.Equal(t, tt.expectedAfter.RetryPolicy.Jitter, config.RetryPolicy.Jitter,
				"RetryPolicy.Jitter mismatch")
			assert.Equal(t, tt.expectedAfter.RetryPolicy.MaxAttempts, config.RetryPolicy.MaxAttempts,
				"RetryPolicy.MaxAttempts mismatch")
		})
	}
}
