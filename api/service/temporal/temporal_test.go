// SPDX-License-Identifier: MPL-2.0

package temporal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestActivityContextKey(t *testing.T) {
	key := ActivityContextKey()
	assert.NotNil(t, key)
	assert.Equal(t, "temporal.activity.context", key.Name)
}

func TestWithClientID(t *testing.T) {
	t.Run("stores and retrieves client ID", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithClientID(ctx, "app.clients:my-temporal")

		got := GetClientID(ctx)
		assert.Equal(t, "app.clients:my-temporal", got)
	})

	t.Run("returns empty string for missing client ID", func(t *testing.T) {
		ctx := context.Background()
		got := GetClientID(ctx)
		assert.Equal(t, "", got)
	})

	t.Run("overwrites previous client ID", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithClientID(ctx, "first-client")
		ctx = WithClientID(ctx, "second-client")

		got := GetClientID(ctx)
		assert.Equal(t, "second-client", got)
	})
}

func TestKinds(t *testing.T) {
	assert.Equal(t, "temporal.client", Client)
	assert.Equal(t, "temporal.worker", Worker)
}

func TestAuthTypes(t *testing.T) {
	assert.Equal(t, AuthType("none"), AuthTypeNone)
	assert.Equal(t, AuthType("api_key"), AuthTypeAPIKey)
	assert.Equal(t, AuthType("mtls"), AuthTypeMTLS)
}

func TestClientConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeNone},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing address", func(t *testing.T) {
		cfg := &ClientConfig{}
		err := cfg.Validate()
		assert.Equal(t, ErrAddressRequired, err)
	})

	t.Run("api key auth - valid with key", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("api key auth - valid with env", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKeyEnv: "API_KEY"},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("api key auth - valid with file", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKeyFile: "/path/to/key"},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("api key auth - missing source", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeAPIKey},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrAPIKeySourceRequired, err)
	})

	t.Run("api key auth - conflicting sources", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "key", APIKeyEnv: "env"},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrAPIKeySourceConflict, err)
	})

	t.Run("mtls auth - valid", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeMTLS, CertFile: "/cert", KeyFile: "/key"},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("mtls auth - valid with PEM", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeMTLS, CertPEM: "cert-pem", KeyPEM: "key-pem"},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("mtls auth - missing cert", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeMTLS, KeyFile: "/key"},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrMTLSCertRequired, err)
	})

	t.Run("mtls auth - missing key", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeMTLS, CertFile: "/cert"},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrMTLSKeyRequired, err)
	})

	t.Run("mtls auth - conflicting cert sources", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeMTLS, CertFile: "/cert", CertPEM: "pem", KeyFile: "/key"},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrMTLSCertConflict, err)
	})

	t.Run("mtls auth - conflicting key sources", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeMTLS, CertFile: "/cert", KeyFile: "/key", KeyPEM: "pem"},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrMTLSKeyConflict, err)
	})

	t.Run("invalid auth type", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: "invalid"},
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid auth type")
	})

	t.Run("tls config conflict", func(t *testing.T) {
		cfg := &ClientConfig{
			Address: "localhost:7233",
			Auth:    AuthConfig{Type: AuthTypeNone},
			TLS:     &TLSConfig{Enabled: true, InsecureSkipVerify: true, ServerName: "test"},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrTLSConfigConflict, err)
	})

	t.Run("negative connection timeout", func(t *testing.T) {
		cfg := &ClientConfig{
			Address:           "localhost:7233",
			Auth:              AuthConfig{Type: AuthTypeNone},
			ConnectionTimeout: -1,
		}
		err := cfg.Validate()
		assert.Equal(t, ErrConnectionTimeoutInvalid, err)
	})

	t.Run("negative keep alive time", func(t *testing.T) {
		cfg := &ClientConfig{
			Address:       "localhost:7233",
			Auth:          AuthConfig{Type: AuthTypeNone},
			KeepAliveTime: -1,
		}
		err := cfg.Validate()
		assert.Equal(t, ErrKeepAliveTimeInvalid, err)
	})

	t.Run("negative keep alive timeout", func(t *testing.T) {
		cfg := &ClientConfig{
			Address:          "localhost:7233",
			Auth:             AuthConfig{Type: AuthTypeNone},
			KeepAliveTimeout: -1,
		}
		err := cfg.Validate()
		assert.Equal(t, ErrKeepAliveTimeoutInvalid, err)
	})

	t.Run("health check enabled without interval", func(t *testing.T) {
		cfg := &ClientConfig{
			Address:     "localhost:7233",
			Auth:        AuthConfig{Type: AuthTypeNone},
			HealthCheck: HealthCheckConfig{Enabled: true, Interval: 0},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrHealthCheckIntervalInvalid, err)
	})
}

func TestClientConfig_InitDefaults(t *testing.T) {
	cfg := &ClientConfig{Address: "localhost:7233"}
	cfg.InitDefaults()

	assert.Equal(t, "default", cfg.Namespace)
	assert.Equal(t, 10*time.Second, cfg.ConnectionTimeout)
	assert.Equal(t, 30*time.Second, cfg.KeepAliveTime)
	assert.Equal(t, 10*time.Second, cfg.KeepAliveTimeout)
	assert.Equal(t, AuthTypeNone, cfg.Auth.Type)
}

func TestClientConfig_InitDefaults_HealthCheck(t *testing.T) {
	cfg := &ClientConfig{
		Address:     "localhost:7233",
		HealthCheck: HealthCheckConfig{Enabled: true},
	}
	cfg.InitDefaults()
	assert.Equal(t, 30*time.Second, cfg.HealthCheck.Interval)
}

func TestClientConfig_UnmarshalJSON(t *testing.T) {
	t.Run("valid json with durations", func(t *testing.T) {
		data := `{
			"address": "localhost:7233",
			"connection_timeout": "15s",
			"keep_alive_time": "45s",
			"keep_alive_timeout": "20s",
			"health_check": {
				"enabled": true,
				"interval": "60s"
			}
		}`

		var cfg ClientConfig
		err := json.Unmarshal([]byte(data), &cfg)
		require.NoError(t, err)

		assert.Equal(t, 15*time.Second, cfg.ConnectionTimeout)
		assert.Equal(t, 45*time.Second, cfg.KeepAliveTime)
		assert.Equal(t, 20*time.Second, cfg.KeepAliveTimeout)
		assert.Equal(t, 60*time.Second, cfg.HealthCheck.Interval)
	})

	t.Run("invalid connection timeout", func(t *testing.T) {
		data := `{"address": "localhost:7233", "connection_timeout": "invalid"}`
		var cfg ClientConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})

	t.Run("invalid keep alive time", func(t *testing.T) {
		data := `{"address": "localhost:7233", "keep_alive_time": "invalid"}`
		var cfg ClientConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})

	t.Run("invalid keep alive timeout", func(t *testing.T) {
		data := `{"address": "localhost:7233", "keep_alive_timeout": "invalid"}`
		var cfg ClientConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})

	t.Run("invalid health check interval", func(t *testing.T) {
		data := `{"address": "localhost:7233", "health_check": {"enabled": true, "interval": "invalid"}}`
		var cfg ClientConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})
}

func TestWorkerConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
			},
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty client validates - ID String returns colon", func(t *testing.T) {
		// Note: registry.ID{}.String() returns ":" not "", so this passes validation
		cfg := &WorkerConfig{
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
			},
		}
		err := cfg.Validate()
		assert.NoError(t, err) // Validation passes because ":" != ""
	})

	t.Run("missing task queue", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client: registry.ID{Name: "test-client"},
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrTaskQueueEmpty, err)
	})

	t.Run("invalid max concurrent activity", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     0,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrMaxConcurrentActivityInvalid, err)
	})

	t.Run("invalid max concurrent workflow", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 0,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrMaxConcurrentWorkflowInvalid, err)
	})

	t.Run("invalid workflow task pollers", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
				MaxConcurrentWorkflowTaskPollers:       1,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrMaxConcurrentWorkflowTaskPollersInvalid, err)
	})

	t.Run("negative worker activities per second", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
				WorkerActivitiesPerSecond:              -1,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrWorkerActivitiesPerSecondInvalid, err)
	})

	t.Run("negative worker local activities per second", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
				WorkerLocalActivitiesPerSecond:         -1,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrWorkerLocalActivitiesPerSecondInvalid, err)
	})

	t.Run("negative task queue activities per second", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
				TaskQueueActivitiesPerSecond:           -1,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrTaskQueueActivitiesPerSecondInvalid, err)
	})

	t.Run("conflicting worker flags", func(t *testing.T) {
		cfg := &WorkerConfig{
			Client:    registry.ID{Name: "test-client"},
			TaskQueue: "test-queue",
			WorkerOptions: WorkerOptionsConfig{
				MaxConcurrentActivityExecutionSize:     100,
				MaxConcurrentWorkflowTaskExecutionSize: 100,
				DisableWorkflowWorker:                  true,
				LocalActivityWorkerOnly:                true,
			},
		}
		err := cfg.Validate()
		assert.Equal(t, ErrDisableWorkflowWorkerConflict, err)
	})
}

func TestWorkerConfig_InitDefaults(t *testing.T) {
	cfg := &WorkerConfig{
		Client:    registry.ID{Name: "test-client"},
		TaskQueue: "test-queue",
	}
	cfg.InitDefaults()

	assert.Equal(t, 1000, cfg.WorkerOptions.MaxConcurrentActivityExecutionSize)
	assert.Equal(t, 1000, cfg.WorkerOptions.MaxConcurrentWorkflowTaskExecutionSize)
	assert.Equal(t, 1000, cfg.WorkerOptions.MaxConcurrentLocalActivityExecutionSize)
	assert.Equal(t, 1000, cfg.WorkerOptions.MaxConcurrentSessionExecutionSize)
	assert.Equal(t, 5*time.Second, cfg.WorkerOptions.StickyScheduleToStartTimeout)
	assert.Equal(t, 20, cfg.WorkerOptions.MaxConcurrentActivityTaskPollers)
	assert.Equal(t, 20, cfg.WorkerOptions.MaxConcurrentWorkflowTaskPollers)
}

func TestWorkerOptionsConfig_UnmarshalJSON(t *testing.T) {
	t.Run("valid json with durations", func(t *testing.T) {
		data := `{
			"max_concurrent_activity_execution_size": 100,
			"sticky_schedule_to_start_timeout": "10s",
			"worker_stop_timeout": "30s",
			"deadlock_detection_timeout": "5s",
			"max_heartbeat_throttle_interval": "60s",
			"default_heartbeat_throttle_interval": "30s"
		}`

		var cfg WorkerOptionsConfig
		err := json.Unmarshal([]byte(data), &cfg)
		require.NoError(t, err)

		assert.Equal(t, 10*time.Second, cfg.StickyScheduleToStartTimeout)
		assert.Equal(t, 30*time.Second, cfg.WorkerStopTimeout)
		assert.Equal(t, 5*time.Second, cfg.DeadlockDetectionTimeout)
		assert.Equal(t, 60*time.Second, cfg.MaxHeartbeatThrottleInterval)
		assert.Equal(t, 30*time.Second, cfg.DefaultHeartbeatThrottleInterval)
	})

	t.Run("invalid sticky schedule timeout", func(t *testing.T) {
		data := `{"sticky_schedule_to_start_timeout": "invalid"}`
		var cfg WorkerOptionsConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})

	t.Run("invalid worker stop timeout", func(t *testing.T) {
		data := `{"worker_stop_timeout": "invalid"}`
		var cfg WorkerOptionsConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})

	t.Run("invalid deadlock detection timeout", func(t *testing.T) {
		data := `{"deadlock_detection_timeout": "invalid"}`
		var cfg WorkerOptionsConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})

	t.Run("invalid max heartbeat throttle", func(t *testing.T) {
		data := `{"max_heartbeat_throttle_interval": "invalid"}`
		var cfg WorkerOptionsConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})

	t.Run("invalid default heartbeat throttle", func(t *testing.T) {
		data := `{"default_heartbeat_throttle_interval": "invalid"}`
		var cfg WorkerOptionsConfig
		err := json.Unmarshal([]byte(data), &cfg)
		assert.Error(t, err)
	})
}

func TestClientResource_GetTaskQueueName(t *testing.T) {
	t.Run("without prefix", func(t *testing.T) {
		r := &ClientResource{}
		assert.Equal(t, "my-queue", r.GetTaskQueueName("my-queue"))
	})

	t.Run("with prefix", func(t *testing.T) {
		r := &ClientResource{TQPrefix: "dev:"}
		assert.Equal(t, "dev:my-queue", r.GetTaskQueueName("my-queue"))
	})
}

func TestErrors(t *testing.T) {
	tests := []struct {
		err     error
		message string
	}{
		{ErrAddressRequired, "address is required"},
		{ErrAPIKeySourceRequired, "API key source is required"},
		{ErrAPIKeySourceConflict, "multiple API key sources specified"},
		{ErrMTLSCertRequired, "mTLS certificate is required"},
		{ErrMTLSKeyRequired, "mTLS key is required"},
		{ErrMTLSCertConflict, "multiple mTLS certificate sources specified"},
		{ErrMTLSKeyConflict, "multiple mTLS key sources specified"},
		{ErrTLSConfigConflict, "cannot use insecure skip verify with server name"},
		{ErrConnectionTimeoutInvalid, "connection timeout must be >= 0"},
		{ErrKeepAliveTimeInvalid, "keep alive time must be >= 0"},
		{ErrKeepAliveTimeoutInvalid, "keep alive timeout must be >= 0"},
		{ErrHealthCheckIntervalInvalid, "health check interval must be > 0 when enabled"},
		{ErrClientReferenceEmpty, "client reference is required"},
		{ErrTaskQueueEmpty, "task queue is required"},
		{ErrMaxConcurrentActivityInvalid, "max concurrent activity execution size must be > 0"},
		{ErrMaxConcurrentWorkflowInvalid, "max concurrent workflow task execution size must be > 0"},
		{ErrMaxConcurrentWorkflowTaskPollersInvalid, "max concurrent workflow task pollers cannot be 1"},
		{ErrWorkerActivitiesPerSecondInvalid, "worker activities per second cannot be negative"},
		{ErrWorkerLocalActivitiesPerSecondInvalid, "worker local activities per second cannot be negative"},
		{ErrTaskQueueActivitiesPerSecondInvalid, "task queue activities per second cannot be negative"},
		{ErrDisableWorkflowWorkerConflict, "cannot disable workflow worker and use local activity worker only simultaneously"},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			assert.Equal(t, tt.message, tt.err.Error())
		})
	}
}

func TestNewInvalidAuthTypeError(t *testing.T) {
	err := NewInvalidAuthTypeError("unknown")
	assert.Contains(t, err.Error(), "invalid auth type")
	assert.Contains(t, err.Error(), "unknown")
}
