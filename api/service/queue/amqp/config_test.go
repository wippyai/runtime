// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	entrycfg "github.com/wippyai/runtime/system/entry"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, registry.Kind("queue.driver.amqp"), Kind)
}

func TestConfig_InitDefaults(t *testing.T) {
	var cfg Config
	cfg.InitDefaults()

	assert.Equal(t, "amqp://guest:guest@localhost:5672/", cfg.URL)
	assert.Equal(t, time.Second, cfg.ReconnectDelay)
	assert.Equal(t, 30*time.Second, cfg.ReconnectMaxDelay)
}

func TestConfig_InitDefaults_PreservesExisting(t *testing.T) {
	cfg := Config{
		URL:               "amqp://user:pass@rabbit:5672/",
		ReconnectDelay:    5 * time.Second,
		ReconnectMaxDelay: time.Minute,
	}
	cfg.InitDefaults()

	assert.Equal(t, "amqp://user:pass@rabbit:5672/", cfg.URL)
	assert.Equal(t, 5*time.Second, cfg.ReconnectDelay)
	assert.Equal(t, time.Minute, cfg.ReconnectMaxDelay)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct { //nolint:govet // fieldalignment: test table readability preferred
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "empty url",
			config:  Config{},
			wantErr: "url is required",
		},
		{
			name:   "valid url",
			config: Config{URL: "amqp://localhost:5672/"},
		},
		{
			name:   "valid auth mechanism PLAIN",
			config: Config{URL: "amqp://localhost:5672/", AuthMechanism: "PLAIN"},
		},
		{
			name:   "valid auth mechanism EXTERNAL",
			config: Config{URL: "amqp://localhost:5672/", AuthMechanism: "EXTERNAL"},
		},
		{
			name:   "valid auth mechanism AMQPLAIN",
			config: Config{URL: "amqp://localhost:5672/", AuthMechanism: "AMQPLAIN"},
		},
		{
			name:    "invalid auth mechanism",
			config:  Config{URL: "amqp://localhost:5672/", AuthMechanism: "INVALID"},
			wantErr: "unsupported auth_mechanism",
		},
		{
			name: "tls cert without key",
			config: Config{
				URL: "amqp://localhost:5672/",
				TLS: &TLSConfig{Enabled: true, Cert: "pem"},
			},
			wantErr: "cert and key must be provided together",
		},
		{
			name: "tls key without cert",
			config: Config{
				URL: "amqp://localhost:5672/",
				TLS: &TLSConfig{Enabled: true, Key: "pem"},
			},
			wantErr: "cert and key must be provided together",
		},
		{
			name: "tls both cert and key inline",
			config: Config{
				URL: "amqp://localhost:5672/",
				TLS: &TLSConfig{Enabled: true, Cert: "pem", Key: "pem"},
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
			name: "basic url",
			json: `{"url":"amqp://localhost:5672/"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, "amqp://localhost:5672/", cfg.URL)
			},
		},
		{
			name: "full connection config",
			json: `{"url":"amqp://user:pass@rabbit:5672/myvhost","vhost":"/custom","connection_name":"myapp","auth_mechanism":"EXTERNAL","frame_size":131072,"prefetch_count":25,"channel_max":100}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, "amqp://user:pass@rabbit:5672/myvhost", cfg.URL)
				assert.Equal(t, "/custom", cfg.Vhost)
				assert.Equal(t, "myapp", cfg.ConnectionName)
				assert.Equal(t, "EXTERNAL", cfg.AuthMechanism)
				assert.Equal(t, 131072, cfg.FrameSize)
				assert.Equal(t, 25, cfg.PrefetchCount)
				assert.Equal(t, uint16(100), cfg.ChannelMax)
			},
		},
		{
			name: "duration fields parsed",
			json: `{"url":"amqp://localhost/","heartbeat":"30s","connection_timeout":"10s","default_message_ttl":"5m","default_queue_ttl":"1h","default_queue_expiry":"24h"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, 30*time.Second, cfg.Heartbeat)
				assert.Equal(t, 10*time.Second, cfg.ConnectionTimeout)
				assert.Equal(t, 5*time.Minute, cfg.DefaultMessageTTL)
				assert.Equal(t, time.Hour, cfg.DefaultQueueTTL)
				assert.Equal(t, 24*time.Hour, cfg.DefaultQueueExpiry)
			},
		},
		{
			name: "reconnect fields parsed",
			json: `{"url":"amqp://localhost/","reconnect_delay":"2s","reconnect_max_delay":"1m"}`,
			check: func(t *testing.T, cfg Config) {
				assert.Equal(t, 2*time.Second, cfg.ReconnectDelay)
				assert.Equal(t, time.Minute, cfg.ReconnectMaxDelay)
			},
		},
		{
			name: "tls config",
			json: `{"url":"amqp://localhost/","tls":{"enabled":true,"server_name":"rabbit.example.com","insecure_skip_verify":true}}`,
			check: func(t *testing.T, cfg Config) {
				require.NotNil(t, cfg.TLS)
				assert.True(t, cfg.TLS.Enabled)
				assert.Equal(t, "rabbit.example.com", cfg.TLS.ServerName)
				assert.True(t, cfg.TLS.InsecureSkipVerify)
			},
		},
		{
			name:    "invalid heartbeat duration",
			json:    `{"url":"amqp://localhost/","heartbeat":"bad"}`,
			wantErr: "invalid heartbeat",
		},
		{
			name:    "invalid connection_timeout",
			json:    `{"url":"amqp://localhost/","connection_timeout":"nope"}`,
			wantErr: "invalid connection_timeout",
		},
		{
			name:    "invalid default_message_ttl",
			json:    `{"url":"amqp://localhost/","default_message_ttl":"wrong"}`,
			wantErr: "invalid default_message_ttl",
		},
		{
			name:    "invalid default_queue_ttl",
			json:    `{"url":"amqp://localhost/","default_queue_ttl":"x"}`,
			wantErr: "invalid default_queue_ttl",
		},
		{
			name:    "invalid default_queue_expiry",
			json:    `{"url":"amqp://localhost/","default_queue_expiry":"y"}`,
			wantErr: "invalid default_queue_expiry",
		},
		{
			name:    "invalid reconnect_delay",
			json:    `{"url":"amqp://localhost/","reconnect_delay":"z"}`,
			wantErr: "invalid reconnect_delay",
		},
		{
			name:    "invalid reconnect_max_delay",
			json:    `{"url":"amqp://localhost/","reconnect_max_delay":"w"}`,
			wantErr: "invalid reconnect_max_delay",
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
		URL:               "amqp://localhost:5672/",
		Heartbeat:         30 * time.Second,
		ConnectionTimeout: 10 * time.Second,
		DefaultMessageTTL: 5 * time.Minute,
		ReconnectDelay:    2 * time.Second,
		ReconnectMaxDelay: time.Minute,
		PrefetchCount:     25,
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	assert.Equal(t, "amqp://localhost:5672/", raw["url"])
	assert.Equal(t, "30s", raw["heartbeat"])
	assert.Equal(t, "10s", raw["connection_timeout"])
	assert.Equal(t, "5m0s", raw["default_message_ttl"])
	assert.Equal(t, "2s", raw["reconnect_delay"])
	assert.Equal(t, "1m0s", raw["reconnect_max_delay"])
	assert.Equal(t, float64(25), raw["prefetch_count"])
}

func TestConfig_MarshalJSON_RoundTrip(t *testing.T) {
	original := Config{
		URL:                "amqp://admin:secret@rabbit:5672/prod",
		Vhost:              "/production",
		ConnectionName:     "myservice",
		AuthMechanism:      "PLAIN",
		Heartbeat:          60 * time.Second,
		ConnectionTimeout:  30 * time.Second,
		DefaultMessageTTL:  10 * time.Minute,
		DefaultQueueTTL:    time.Hour,
		DefaultQueueExpiry: 24 * time.Hour,
		ReconnectDelay:     2 * time.Second,
		ReconnectMaxDelay:  time.Minute,
		FrameSize:          131072,
		PrefetchCount:      50,
		ChannelMax:         200,
		TLS: &TLSConfig{
			Enabled:            true,
			ServerName:         "rabbit.example.com",
			InsecureSkipVerify: false,
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Config
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.URL, restored.URL)
	assert.Equal(t, original.Vhost, restored.Vhost)
	assert.Equal(t, original.ConnectionName, restored.ConnectionName)
	assert.Equal(t, original.AuthMechanism, restored.AuthMechanism)
	assert.Equal(t, original.Heartbeat, restored.Heartbeat)
	assert.Equal(t, original.ConnectionTimeout, restored.ConnectionTimeout)
	assert.Equal(t, original.DefaultMessageTTL, restored.DefaultMessageTTL)
	assert.Equal(t, original.DefaultQueueTTL, restored.DefaultQueueTTL)
	assert.Equal(t, original.DefaultQueueExpiry, restored.DefaultQueueExpiry)
	assert.Equal(t, original.ReconnectDelay, restored.ReconnectDelay)
	assert.Equal(t, original.ReconnectMaxDelay, restored.ReconnectMaxDelay)
	assert.Equal(t, original.FrameSize, restored.FrameSize)
	assert.Equal(t, original.PrefetchCount, restored.PrefetchCount)
	assert.Equal(t, original.ChannelMax, restored.ChannelMax)
	require.NotNil(t, restored.TLS)
	assert.Equal(t, original.TLS.Enabled, restored.TLS.Enabled)
	assert.Equal(t, original.TLS.ServerName, restored.TLS.ServerName)
}

func TestConfig_MarshalJSON_ZeroDurations(t *testing.T) {
	cfg := Config{
		URL: "amqp://localhost:5672/",
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// Zero durations should not appear in JSON
	assert.Nil(t, raw["heartbeat"])
	assert.Nil(t, raw["connection_timeout"])
	assert.Nil(t, raw["default_message_ttl"])
	assert.Nil(t, raw["reconnect_delay"])
	assert.Nil(t, raw["reconnect_max_delay"])
}

func TestTLSConfig_BuildTLSConfig_Nil(t *testing.T) {
	var tlsCfg *TLSConfig
	result, err := tlsCfg.BuildTLSConfig(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestTLSConfig_BuildTLSConfig_Disabled(t *testing.T) {
	tlsCfg := &TLSConfig{Enabled: false}
	result, err := tlsCfg.BuildTLSConfig(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestTLSConfig_BuildTLSConfig_Enabled(t *testing.T) {
	tlsCfg := &TLSConfig{
		Enabled:            true,
		ServerName:         "rabbit.example.com",
		InsecureSkipVerify: true,
	}
	result, err := tlsCfg.BuildTLSConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "rabbit.example.com", result.ServerName)
	assert.True(t, result.InsecureSkipVerify)
}

func TestTLSConfig_BuildTLSConfig_InvalidCertPEM(t *testing.T) {
	tlsCfg := &TLSConfig{
		Enabled: true,
		Cert:    "not-a-pem",
		Key:     "not-a-pem",
	}
	_, err := tlsCfg.BuildTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load client cert")
}

func TestTLSConfig_BuildTLSConfig_InvalidCAPEM(t *testing.T) {
	tlsCfg := &TLSConfig{
		Enabled: true,
		CA:      "not-a-pem",
	}
	_, err := tlsCfg.BuildTLSConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse ca certificate")
}

// --- central *_env resolution at decode ----------------------------------

// selfSignedPEM returns a throwaway self-signed cert + key as PEM.
func selfSignedPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "amqp-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

// testEnvRegistry is a minimal envapi.Registry shim backed by an in-memory map.
type testEnvRegistry struct {
	values map[string]string
}

func (r *testEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if v, ok := r.values[name]; ok {
		return v, nil
	}
	return "", envapi.ErrVariableNotFound
}

func (r *testEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	v, ok := r.values[name]
	if !ok {
		return "", false, envapi.ErrVariableNotFound
	}
	return v, true, nil
}

func (r *testEnvRegistry) Set(context.Context, string, string) error { return nil }
func (r *testEnvRegistry) All(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *testEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}
func (r *testEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) {}
func (r *testEnvRegistry) RegisterVariable(envapi.Variable) error      { return nil }
func (r *testEnvRegistry) UnregisterVariable(registry.ID)              {}

// jsonMapTranscoder unmarshals a Golang map payload into a struct via a JSON
// round trip, exercising real type coercion during decode.
type jsonMapTranscoder struct{}

func (jsonMapTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.New(p.Data()), nil
}

func (jsonMapTranscoder) Unmarshal(p payload.Payload, v any) error {
	m, ok := p.Data().(map[string]any)
	if !ok {
		return errors.New("payload is not a map")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// decodeAMQPConfig decodes a Config from an entry whose data map is data,
// exercising the central *_env resolution + validation path.
func decodeAMQPConfig(ctx context.Context, t *testing.T, data map[string]any) (*Config, error) {
	t.Helper()
	entry := registry.Entry{
		ID:   registry.NewID("test", "amqp"),
		Kind: Kind,
		Data: payload.New(data),
	}
	return entrycfg.DecodeEntryConfig[Config](ctx, jsonMapTranscoder{}, entry)
}

func envCtx(values map[string]string) context.Context {
	return envapi.WithRegistry(ctxapi.NewRootContext(), &testEnvRegistry{values: values})
}

// TestDecodeAMQPTLS_EnvCertKey: cert_env/key_env directives in the entry data
// resolve into cfg.TLS.Cert/Key at decode, and BuildTLSConfig loads the pair.
func TestDecodeAMQPTLS_EnvCertKey(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	ctx := envCtx(map[string]string{
		"app.env:amqp_cert": string(certPEM),
		"app.env:amqp_key":  string(keyPEM),
	})

	cfg, err := decodeAMQPConfig(ctx, t, map[string]any{
		"url": "amqp://localhost:5672/",
		"tls": map[string]any{
			"enabled":  true,
			"cert_env": "app.env:amqp_cert",
			"key_env":  "app.env:amqp_key",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg.TLS)
	assert.Equal(t, string(certPEM), cfg.TLS.Cert)
	assert.Equal(t, string(keyPEM), cfg.TLS.Key)

	out, err := cfg.TLS.BuildTLSConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Len(t, out.Certificates, 1)
}

// TestDecodeAMQPTLS_CAEnv: a ca_env directive resolves into cfg.TLS.CA and the
// CA pool is assembled by BuildTLSConfig.
func TestDecodeAMQPTLS_CAEnv(t *testing.T) {
	caPEM, _ := selfSignedPEM(t)
	ctx := envCtx(map[string]string{"app.env:amqp_ca": string(caPEM)})

	cfg, err := decodeAMQPConfig(ctx, t, map[string]any{
		"url": "amqp://localhost:5672/",
		"tls": map[string]any{
			"enabled": true,
			"ca_env":  "app.env:amqp_ca",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg.TLS)
	assert.Equal(t, string(caPEM), cfg.TLS.CA)

	out, err := cfg.TLS.BuildTLSConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, out.RootCAs)
}

// TestDecodeAMQPTLS_EnvLookupUnresolved: a directive naming a variable the
// registry does not resolve is left unapplied; the entry decodes with an empty
// cert rather than failing on a false "not found".
func TestDecodeAMQPTLS_EnvLookupUnresolved(t *testing.T) {
	ctx := envCtx(map[string]string{})
	cfg, err := decodeAMQPConfig(ctx, t, map[string]any{
		"url": "amqp://localhost:5672/",
		"tls": map[string]any{
			"enabled":  true,
			"cert_env": "app.env:missing",
			"key_env":  "app.env:missing2",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg.TLS)
	assert.True(t, cfg.TLS.Enabled)
	assert.Empty(t, cfg.TLS.Cert)
}

// TestDecodeAMQPTLS_EnvRegistryMissing: without an env registry the directive
// is left unapplied; the entry decodes with an empty cert.
func TestDecodeAMQPTLS_EnvRegistryMissing(t *testing.T) {
	cfg, err := decodeAMQPConfig(ctxapi.NewRootContext(), t, map[string]any{
		"url": "amqp://localhost:5672/",
		"tls": map[string]any{
			"enabled":  true,
			"cert_env": "app.env:amqp_cert",
			"key_env":  "app.env:amqp_key",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg.TLS)
	assert.True(t, cfg.TLS.Enabled)
	assert.Empty(t, cfg.TLS.Cert)
}
