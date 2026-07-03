// SPDX-License-Identifier: MPL-2.0

package entry

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// realTranscoder unmarshals a Golang map payload into a struct via a JSON round trip
// so the end-to-end decode path exercises real type coercion.
type realTranscoder struct{}

func (realTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.New(p.Data()), nil
}

func (realTranscoder) Unmarshal(p payload.Payload, v any) error {
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

// ResolveConfig is decoded end to end through placeholder resolution.
type ResolveConfig struct {
	Name        string `json:"name"`
	Concurrency int    `json:"concurrency"`
}

func TestDecodeEntryConfig_WholeValuePlaceholderInt(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "WORKERS", value: "16"}}}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]any{
			"name":        "job",
			"concurrency": "${env:WORKERS|4}",
		}),
	}

	cfg, err := DecodeEntryConfig[ResolveConfig](ctx, realTranscoder{}, entry)
	require.NoError(t, err)
	assert.Equal(t, "job", cfg.Name)
	assert.Equal(t, 16, cfg.Concurrency)
}

func TestDecodeEntryConfig_WholeValuePlaceholderDefault(t *testing.T) {
	reg := &fakeEnvRegistry{}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]any{
			"name":        "job",
			"concurrency": "${env:WORKERS|4}",
		}),
	}

	cfg, err := DecodeEntryConfig[ResolveConfig](ctx, realTranscoder{}, entry)
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.Concurrency)
}

func TestDecodeEntryConfig_PlaceholderByEntryID(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "DB_PASSWORD", id: "app:db_password", value: "s3cret"}}}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]any{"name": "${env:app:db_password}"}),
	}

	cfg, err := DecodeEntryConfig[ResolveConfig](ctx, realTranscoder{}, entry)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", cfg.Name)
}

func TestDecodeEntryConfig_DoesNotMutateEntryData(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "WORKERS", value: "16"}}}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	original := map[string]any{"name": "job", "concurrency": "${env:WORKERS|4}"}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(original),
	}

	_, err := DecodeEntryConfig[ResolveConfig](ctx, realTranscoder{}, entry)
	require.NoError(t, err)
	assert.Equal(t, "${env:WORKERS|4}", original["concurrency"], "sealed entry data must stay unresolved")
}

func TestDecodeEntryConfigFromContext(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "WORKERS", value: "8"}}}
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)
	ctx = payload.WithTranscoder(ctx, realTranscoder{})
	ctx = env.WithRegistry(ctx, reg)

	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]any{"name": "job", "concurrency": "${env:WORKERS|4}"}),
	}

	cfg, err := DecodeEntryConfigFromContext[ResolveConfig](ctx, entry)
	require.NoError(t, err)
	assert.Equal(t, 8, cfg.Concurrency)
}

func TestDecodeEntryConfigFromContext_NoTranscoder(t *testing.T) {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]any{"name": "job"}),
	}

	_, err := DecodeEntryConfigFromContext[ResolveConfig](ctx, entry)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTranscoderMissing))
}

// Legacy *_env companion field configs.

type TLSConfig struct {
	CertFile    string `json:"cert_file"`
	Password    string `json:"password"`
	PasswordEnv string `json:"password_env"`
}

type ServerEnvConfig struct {
	Host     string    `json:"host"`
	HostEnv  string    `json:"host_env"`
	Port     int       `json:"port"`
	PortEnv  string    `json:"port_env"`
	Debug    bool      `json:"debug"`
	DebugEnv string    `json:"debug_env"`
	TLS      TLSConfig `json:"tls"`
}

// envDecodeTranscoder unmarshals a fixed config value, letting tests drive the
// struct contents directly while exercising the reflection env pass.
type envDecodeTranscoder struct {
	cfg ServerEnvConfig
}

func (e *envDecodeTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.New(p.Data()), nil
}

func (e *envDecodeTranscoder) Unmarshal(_ payload.Payload, v any) error {
	out, ok := v.(*ServerEnvConfig)
	if !ok {
		return errors.New("unexpected target type")
	}
	*out = e.cfg
	return nil
}

func decodeServerEnv(t *testing.T, reg env.Registry, cfg ServerEnvConfig) (*ServerEnvConfig, error) {
	t.Helper()
	ctx := context.Background()
	if reg != nil {
		ctx = env.WithRegistry(ctxapi.WithAppContext(ctx, ctxapi.NewAppContext()), reg)
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "server"),
		Kind: "test.server",
		Data: payload.New(map[string]any{"present": true}),
	}
	return DecodeEntryConfig[ServerEnvConfig](ctx, &envDecodeTranscoder{cfg: cfg}, entry)
}

func TestEnvField_StringIntBool(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{
		{name: "SRV_HOST", value: "prod.example.com"},
		{name: "SRV_PORT", value: "9443"},
		{name: "SRV_DEBUG", value: "true"},
	}}

	cfg, err := decodeServerEnv(t, reg, ServerEnvConfig{
		Host: "localhost", HostEnv: "SRV_HOST",
		Port: 8080, PortEnv: "SRV_PORT",
		Debug: false, DebugEnv: "SRV_DEBUG",
	})
	require.NoError(t, err)
	assert.Equal(t, "prod.example.com", cfg.Host)
	assert.Equal(t, 9443, cfg.Port)
	assert.True(t, cfg.Debug)
}

func TestEnvField_NestedStruct(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "TLS_PASS", value: "topsecret"}}}

	cfg, err := decodeServerEnv(t, reg, ServerEnvConfig{
		TLS: TLSConfig{Password: "inline", PasswordEnv: "TLS_PASS"},
	})
	require.NoError(t, err)
	assert.Equal(t, "topsecret", cfg.TLS.Password)
}

func TestEnvField_NotFoundKeepsInline(t *testing.T) {
	reg := &fakeEnvRegistry{}

	cfg, err := decodeServerEnv(t, reg, ServerEnvConfig{
		Host: "keep-me", HostEnv: "SRV_HOST",
		Port: 8080, PortEnv: "SRV_PORT",
	})
	require.NoError(t, err)
	assert.Equal(t, "keep-me", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
}

func TestEnvField_LookupErrorFails(t *testing.T) {
	reg := &fakeEnvRegistry{lookupErr: errors.New("storage unavailable")}

	_, err := decodeServerEnv(t, reg, ServerEnvConfig{Host: "x", HostEnv: "SRV_HOST"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage unavailable")
}

func TestEnvField_ConversionErrorFails(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "SRV_PORT", value: "not-a-port"}}}

	_, err := decodeServerEnv(t, reg, ServerEnvConfig{Port: 8080, PortEnv: "SRV_PORT"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "convert")
	assert.Contains(t, err.Error(), "SRV_PORT")
}

func TestEnvField_NoRegistryFails(t *testing.T) {
	_, err := decodeServerEnv(t, nil, ServerEnvConfig{Host: "x", HostEnv: "SRV_HOST"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment registry not available")
}

func TestEnvField_UnsetEnvFieldIgnored(t *testing.T) {
	// No *_env values set: registry absence is fine because nothing is resolved.
	cfg, err := decodeServerEnv(t, nil, ServerEnvConfig{Host: "plain", Port: 80})
	require.NoError(t, err)
	assert.Equal(t, "plain", cfg.Host)
	assert.Equal(t, 80, cfg.Port)
}
