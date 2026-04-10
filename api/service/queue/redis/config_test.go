// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, registry.Kind("queue.driver.redis"), Kind)
}

func TestConfig_InitDefaults(t *testing.T) {
	var cfg Config
	cfg.InitDefaults()

	assert.Equal(t, []string{"localhost:6379"}, cfg.Addrs)
	assert.Equal(t, DefaultClaimInterval, cfg.ClaimInterval)
	assert.Equal(t, DefaultClaimMinIdle, cfg.ClaimMinIdle)
}

func TestConfig_InitDefaults_PreservesExisting(t *testing.T) {
	cfg := Config{
		Addrs:         []string{"redis.example.com:6380"},
		ClaimInterval: 10 * time.Second,
		ClaimMinIdle:  2 * time.Minute,
	}
	cfg.InitDefaults()

	assert.Equal(t, []string{"redis.example.com:6380"}, cfg.Addrs)
	assert.Equal(t, 10*time.Second, cfg.ClaimInterval)
	assert.Equal(t, 2*time.Minute, cfg.ClaimMinIdle)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct { //nolint:govet // fieldalignment: test table readability preferred
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "no addrs",
			config:  Config{},
			wantErr: "at least one address",
		},
		{
			name:   "valid single addr",
			config: Config{Addrs: []string{"localhost:6379"}},
		},
		{
			name:   "valid multiple addrs",
			config: Config{Addrs: []string{"node1:6379", "node2:6379", "node3:6379"}},
		},
		{
			name: "tls cert without key",
			config: Config{
				Addrs: []string{"localhost:6379"},
				TLS:   &TLSConfig{CertFile: "/cert.pem"},
			},
			wantErr: "cert_file requires key_file",
		},
		{
			name: "tls key without cert",
			config: Config{
				Addrs: []string{"localhost:6379"},
				TLS:   &TLSConfig{KeyFile: "/key.pem"},
			},
			wantErr: "key_file requires cert_file",
		},
		{
			name: "tls both cert and key",
			config: Config{
				Addrs: []string{"localhost:6379"},
				TLS:   &TLSConfig{CertFile: "/cert.pem", KeyFile: "/key.pem"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		check   func(t *testing.T, cfg Config)
		wantErr string
	}{
		{
			name: "basic addrs",
			json: `{"addrs":["redis:6379"]}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, []string{"redis:6379"}, cfg.Addrs)
			},
		},
		{
			name: "backward compat addr",
			json: `{"addr":"redis:6379"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, []string{"redis:6379"}, cfg.Addrs)
			},
		},
		{
			name: "addrs takes precedence over addr",
			json: `{"addrs":["node1:6379","node2:6379"],"addr":"ignored:6379"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, []string{"node1:6379", "node2:6379"}, cfg.Addrs)
			},
		},
		{
			name: "duration fields parsed",
			json: `{"addrs":["redis:6379"],"dial_timeout":"10s","read_timeout":"3s","write_timeout":"5s","pool_timeout":"15s","conn_max_idle_time":"30m","conn_max_lifetime":"1h"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, 10*time.Second, cfg.DialTimeout)
				assert.Equal(t, 3*time.Second, cfg.ReadTimeout)
				assert.Equal(t, 5*time.Second, cfg.WriteTimeout)
				assert.Equal(t, 15*time.Second, cfg.PoolTimeout)
				assert.Equal(t, 30*time.Minute, cfg.ConnMaxIdleTime)
				assert.Equal(t, time.Hour, cfg.ConnMaxLifetime)
			},
		},
		{
			name: "retry backoff durations",
			json: `{"addrs":["redis:6379"],"min_retry_backoff":"8ms","max_retry_backoff":"512ms"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, 8*time.Millisecond, cfg.MinRetryBackoff)
				assert.Equal(t, 512*time.Millisecond, cfg.MaxRetryBackoff)
			},
		},
		{
			name: "claim fields parsed",
			json: `{"addrs":["redis:6379"],"claim_interval":"15s","claim_min_idle":"2m"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, 15*time.Second, cfg.ClaimInterval)
				assert.Equal(t, 2*time.Minute, cfg.ClaimMinIdle)
			},
		},
		{
			name: "sentinel config",
			json: `{"addrs":["sentinel1:26379","sentinel2:26379"],"master_name":"mymaster","sentinel_username":"suser","sentinel_password":"spass"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, "mymaster", cfg.MasterName)
				assert.Equal(t, "suser", cfg.SentinelUsername)
				assert.Equal(t, "spass", cfg.SentinelPassword)
			},
		},
		{
			name: "cluster config",
			json: `{"addrs":["node1:6379"],"is_cluster_mode":true,"max_redirects":5}`,
			check: func(t *testing.T, cfg Config) {
				assert.True(t, cfg.IsClusterMode)
				assert.Equal(t, 5, cfg.MaxRedirects)
			},
		},
		{
			name: "pool settings",
			json: `{"addrs":["redis:6379"],"pool_size":20,"min_idle_conns":5,"max_idle_conns":10,"max_active_conns":50,"pool_fifo":true}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, 20, cfg.PoolSize)
				assert.Equal(t, 5, cfg.MinIdleConns)
				assert.Equal(t, 10, cfg.MaxIdleConns)
				assert.Equal(t, 50, cfg.MaxActiveConns)
				assert.True(t, cfg.PoolFIFO)
			},
		},
		{
			name: "auth and db",
			json: `{"addrs":["redis:6379"],"username":"user1","password":"pass1","db":3,"protocol":3,"client_name":"myapp","context_timeout_enabled":true}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, "user1", cfg.Username)
				assert.Equal(t, "pass1", cfg.Password)
				assert.Equal(t, 3, cfg.DB)
				assert.Equal(t, 3, cfg.Protocol)
				assert.Equal(t, "myapp", cfg.ClientName)
				assert.True(t, cfg.ContextTimeoutEnabled)
			},
		},
		{
			name: "tls config",
			json: `{"addrs":["redis:6379"],"tls":{"enabled":true,"server_name":"redis.example.com","insecure_skip_verify":true}}`,
			check: func(t *testing.T, cfg Config) {
				require.NotNil(t, cfg.TLS)
				assert.True(t, cfg.TLS.Enabled)
				assert.Equal(t, "redis.example.com", cfg.TLS.ServerName)
				assert.True(t, cfg.TLS.InsecureSkipVerify)
			},
		},
		{
			name:    "invalid duration",
			json:    `{"addrs":["redis:6379"],"dial_timeout":"invalid"}`,
			wantErr: "invalid dial_timeout",
		},
		{
			name:    "invalid claim_interval",
			json:    `{"addrs":["redis:6379"],"claim_interval":"bad"}`,
			wantErr: "invalid claim_interval",
		},
		{
			name:    "invalid JSON",
			json:    `{broken`,
			wantErr: "invalid character",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(tt.json), &cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

func TestConfig_MarshalJSON(t *testing.T) {
	cfg := Config{
		Addrs:           []string{"redis:6379"},
		Username:        "user",
		DialTimeout:     10 * time.Second,
		ReadTimeout:     3 * time.Second,
		ClaimInterval:   15 * time.Second,
		ClaimMinIdle:    2 * time.Minute,
		ConnMaxIdleTime: 30 * time.Minute,
		PoolSize:        20,
		DB:              1,
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	assert.Equal(t, "10s", raw["dial_timeout"])
	assert.Equal(t, "3s", raw["read_timeout"])
	assert.Equal(t, "15s", raw["claim_interval"])
	assert.Equal(t, "2m0s", raw["claim_min_idle"])
	assert.Equal(t, "30m0s", raw["conn_max_idle_time"])
}

func TestConfig_MarshalJSON_RoundTrip(t *testing.T) {
	original := Config{
		Addrs:           []string{"node1:6379", "node2:6379"},
		Username:        "admin",
		Password:        "secret",
		MasterName:      "mymaster",
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolTimeout:     10 * time.Second,
		ClaimInterval:   20 * time.Second,
		ClaimMinIdle:    45 * time.Second,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,
		ConnMaxIdleTime: 30 * time.Minute,
		PoolSize:        50,
		MaxRetries:      5,
		DB:              2,
		PoolFIFO:        true,
		IsClusterMode:   true,
		MaxRedirects:    3,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Config
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.Addrs, restored.Addrs)
	assert.Equal(t, original.Username, restored.Username)
	assert.Equal(t, original.Password, restored.Password)
	assert.Equal(t, original.MasterName, restored.MasterName)
	assert.Equal(t, original.DialTimeout, restored.DialTimeout)
	assert.Equal(t, original.ReadTimeout, restored.ReadTimeout)
	assert.Equal(t, original.WriteTimeout, restored.WriteTimeout)
	assert.Equal(t, original.PoolTimeout, restored.PoolTimeout)
	assert.Equal(t, original.ClaimInterval, restored.ClaimInterval)
	assert.Equal(t, original.ClaimMinIdle, restored.ClaimMinIdle)
	assert.Equal(t, original.MinRetryBackoff, restored.MinRetryBackoff)
	assert.Equal(t, original.MaxRetryBackoff, restored.MaxRetryBackoff)
	assert.Equal(t, original.ConnMaxIdleTime, restored.ConnMaxIdleTime)
	assert.Equal(t, original.PoolSize, restored.PoolSize)
	assert.Equal(t, original.MaxRetries, restored.MaxRetries)
	assert.Equal(t, original.DB, restored.DB)
	assert.Equal(t, original.PoolFIFO, restored.PoolFIFO)
	assert.Equal(t, original.IsClusterMode, restored.IsClusterMode)
	assert.Equal(t, original.MaxRedirects, restored.MaxRedirects)
}

func TestConfig_ToUniversalOptions(t *testing.T) {
	cfg := Config{
		Addrs:                 []string{"node1:6379", "node2:6379"},
		MasterName:            "mymaster",
		ClientName:            "testclient",
		Username:              "user",
		Password:              "pass",
		SentinelUsername:      "suser",
		SentinelPassword:      "spass",
		Protocol:              3,
		DB:                    2,
		MaxRetries:            5,
		MinRetryBackoff:       8 * time.Millisecond,
		MaxRetryBackoff:       512 * time.Millisecond,
		DialTimeout:           10 * time.Second,
		ReadTimeout:           3 * time.Second,
		WriteTimeout:          5 * time.Second,
		ContextTimeoutEnabled: true,
		PoolSize:              20,
		MinIdleConns:          5,
		MaxIdleConns:          10,
		MaxActiveConns:        50,
		PoolFIFO:              true,
		PoolTimeout:           15 * time.Second,
		ConnMaxIdleTime:       30 * time.Minute,
		ConnMaxLifetime:       time.Hour,
		MaxRedirects:          3,
		IsClusterMode:         true,
	}

	opts := cfg.ToUniversalOptions()

	assert.Equal(t, cfg.Addrs, opts.Addrs)
	assert.Equal(t, cfg.MasterName, opts.MasterName)
	assert.Equal(t, cfg.ClientName, opts.ClientName)
	assert.Equal(t, cfg.Username, opts.Username)
	assert.Equal(t, cfg.Password, opts.Password)
	assert.Equal(t, cfg.SentinelUsername, opts.SentinelUsername)
	assert.Equal(t, cfg.SentinelPassword, opts.SentinelPassword)
	assert.Equal(t, cfg.Protocol, opts.Protocol)
	assert.Equal(t, cfg.DB, opts.DB)
	assert.Equal(t, cfg.MaxRetries, opts.MaxRetries)
	assert.Equal(t, cfg.MinRetryBackoff, opts.MinRetryBackoff)
	assert.Equal(t, cfg.MaxRetryBackoff, opts.MaxRetryBackoff)
	assert.Equal(t, cfg.DialTimeout, opts.DialTimeout)
	assert.Equal(t, cfg.ReadTimeout, opts.ReadTimeout)
	assert.Equal(t, cfg.WriteTimeout, opts.WriteTimeout)
	assert.Equal(t, cfg.ContextTimeoutEnabled, opts.ContextTimeoutEnabled)
	assert.Equal(t, cfg.PoolSize, opts.PoolSize)
	assert.Equal(t, cfg.MinIdleConns, opts.MinIdleConns)
	assert.Equal(t, cfg.MaxIdleConns, opts.MaxIdleConns)
	assert.Equal(t, cfg.MaxActiveConns, opts.MaxActiveConns)
	assert.Equal(t, cfg.PoolFIFO, opts.PoolFIFO)
	assert.Equal(t, cfg.PoolTimeout, opts.PoolTimeout)
	assert.Equal(t, cfg.ConnMaxIdleTime, opts.ConnMaxIdleTime)
	assert.Equal(t, cfg.ConnMaxLifetime, opts.ConnMaxLifetime)
	assert.Equal(t, cfg.MaxRedirects, opts.MaxRedirects)
	assert.Equal(t, cfg.IsClusterMode, opts.IsClusterMode)
}

func TestConfig_ToUniversalOptions_ZeroValues(t *testing.T) {
	cfg := Config{
		Addrs: []string{"localhost:6379"},
	}
	opts := cfg.ToUniversalOptions()

	assert.Equal(t, []string{"localhost:6379"}, opts.Addrs)
	assert.Empty(t, opts.MasterName)
	assert.Zero(t, opts.DB)
	assert.Zero(t, opts.PoolSize)
	assert.False(t, opts.IsClusterMode)
}

func TestConfig_MarshalJSON_ZeroDurations(t *testing.T) {
	cfg := Config{
		Addrs: []string{"redis:6379"},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// Zero durations should not appear in JSON
	assert.Nil(t, raw["dial_timeout"])
	assert.Nil(t, raw["read_timeout"])
	assert.Nil(t, raw["claim_interval"])
	assert.Nil(t, raw["claim_min_idle"])
}

func TestTLSConfig_BuildTLSConfig_Nil(t *testing.T) {
	var tlsCfg *TLSConfig
	result, err := tlsCfg.BuildTLSConfig()
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestTLSConfig_BuildTLSConfig_Disabled(t *testing.T) {
	tlsCfg := &TLSConfig{Enabled: false}
	result, err := tlsCfg.BuildTLSConfig()
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestTLSConfig_BuildTLSConfig_Enabled(t *testing.T) {
	tlsCfg := &TLSConfig{
		Enabled:            true,
		ServerName:         "redis.example.com",
		InsecureSkipVerify: true,
	}
	result, err := tlsCfg.BuildTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "redis.example.com", result.ServerName)
	assert.True(t, result.InsecureSkipVerify)
}

func TestTLSConfig_BuildTLSConfig_MissingCertFile(t *testing.T) {
	tlsCfg := &TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}
	_, err := tlsCfg.BuildTLSConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load client cert")
}

func TestTLSConfig_BuildTLSConfig_MissingCAFile(t *testing.T) {
	tlsCfg := &TLSConfig{
		Enabled: true,
		CAFile:  "/nonexistent/ca.pem",
	}
	_, err := tlsCfg.BuildTLSConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read ca file")
}

func TestDefaultClaimConstants(t *testing.T) {
	assert.Equal(t, 30*time.Second, DefaultClaimInterval)
	assert.Equal(t, time.Minute, DefaultClaimMinIdle)
}
