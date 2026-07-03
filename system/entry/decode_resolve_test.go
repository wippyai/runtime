// SPDX-License-Identifier: MPL-2.0

package entry

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
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

func TestEnvField_NotFoundFails(t *testing.T) {
	// A set *_env field whose variable is absent hard-fails the decode so a
	// misconfigured reference never silently falls back to the inline value.
	reg := &fakeEnvRegistry{}

	_, err := decodeServerEnv(t, reg, ServerEnvConfig{
		Host: "keep-me", HostEnv: "SRV_HOST",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be resolved")
	assert.Contains(t, err.Error(), "SRV_HOST")
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

func TestEnvField_NoRegistrySkips(t *testing.T) {
	// Without a registry in context the central pass leaves *_env fields for a
	// service that resolves them through an injected registry of its own.
	cfg, err := decodeServerEnv(t, nil, ServerEnvConfig{Host: "x", HostEnv: "SRV_HOST"})
	require.NoError(t, err)
	assert.Equal(t, "x", cfg.Host)
}

func TestEnvField_UnsetEnvFieldIgnored(t *testing.T) {
	// No *_env values set: registry absence is fine because nothing is resolved.
	cfg, err := decodeServerEnv(t, nil, ServerEnvConfig{Host: "plain", Port: 80})
	require.NoError(t, err)
	assert.Equal(t, "plain", cfg.Host)
	assert.Equal(t, 80, cfg.Port)
}

// SkipConfig marks its script field resolve:"-" so embedded code is exempt from
// placeholder resolution, including inside nested structs.
type SkipConfig struct {
	Name   string        `json:"name"`
	Script string        `json:"script" resolve:"-"`
	Inner  SkipInner     `json:"inner"`
	Items  []SkipElement `json:"items"`
}

type SkipInner struct {
	Body string `json:"body" resolve:"-"`
	Mode string `json:"mode"`
}

type SkipElement struct {
	Code string `json:"code" resolve:"-"`
	Tag  string `json:"tag"`
}

func TestDecodeEntryConfig_ResolveSkipTag(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "MODE", value: "fast"}, {name: "TAG", value: "v1"}}}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]any{
			"name":   "job",
			"script": "echo ${env:UNSET} ${ALSO_UNSET|x}",
			"inner": map[string]any{
				"body": "run ${env:UNSET}",
				"mode": "${env:MODE}",
			},
			"items": []any{
				map[string]any{"code": "${env:UNSET}", "tag": "${env:TAG}"},
			},
		}),
	}

	cfg, err := DecodeEntryConfig[SkipConfig](ctx, realTranscoder{}, entry)
	require.NoError(t, err)

	// Tagged fields keep placeholder-shaped spans byte-identical.
	assert.Equal(t, "echo ${env:UNSET} ${ALSO_UNSET|x}", cfg.Script)
	assert.Equal(t, "run ${env:UNSET}", cfg.Inner.Body)
	assert.Equal(t, "${env:UNSET}", cfg.Items[0].Code)

	// Untagged siblings resolve, slice elements included.
	assert.Equal(t, "fast", cfg.Inner.Mode)
	assert.Equal(t, "v1", cfg.Items[0].Tag)
}

// OptionsConfig exercises legacy _env resolution inside map fields, including a
// nested map[string]any bag.
type OptionsConfig struct {
	Name    string            `json:"name"`
	Options map[string]string `json:"options"`
	Extra   map[string]any    `json:"extra"`
}

func decodeOptions(t *testing.T, reg env.Registry, data map[string]any) (*OptionsConfig, error) {
	t.Helper()
	ctx := context.Background()
	if reg != nil {
		ctx = env.WithRegistry(ctxapi.WithAppContext(ctx, ctxapi.NewAppContext()), reg)
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "opts"),
		Kind: "test.opts",
		Data: payload.New(data),
	}
	return DecodeEntryConfig[OptionsConfig](ctx, realTranscoder{}, entry)
}

func TestEnvMap_ResolvesAndDropsDirective(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "DB_SSLMODE", value: "require"}}}

	cfg, err := decodeOptions(t, reg, map[string]any{
		"name": "db",
		"options": map[string]any{
			"sslmode_env": "DB_SSLMODE",
			"timeout":     "30",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "require", cfg.Options["sslmode"])
	assert.Equal(t, "30", cfg.Options["timeout"])
	_, present := cfg.Options["sslmode_env"]
	assert.False(t, present, "directive key must be dropped")
}

func TestEnvMap_NotFoundFails(t *testing.T) {
	reg := &fakeEnvRegistry{}

	_, err := decodeOptions(t, reg, map[string]any{
		"options": map[string]any{"sslmode_env": "MISSING"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be resolved")
	assert.Contains(t, err.Error(), "MISSING")
}

func TestEnvMap_EmptyDirectiveDropped(t *testing.T) {
	reg := &fakeEnvRegistry{}

	cfg, err := decodeOptions(t, reg, map[string]any{
		"options": map[string]any{"sslmode_env": ""},
	})
	require.NoError(t, err)
	_, present := cfg.Options["sslmode_env"]
	assert.False(t, present)
	_, base := cfg.Options["sslmode"]
	assert.False(t, base)
}

func TestEnvMap_NestedBagResolves(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "TOK", value: "secret"}}}

	cfg, err := decodeOptions(t, reg, map[string]any{
		"extra": map[string]any{
			"inner": map[string]any{"token_env": "TOK"},
		},
	})
	require.NoError(t, err)
	inner, ok := cfg.Extra["inner"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "secret", inner["token"])
	_, present := inner["token_env"]
	assert.False(t, present)
}

// MetaBagConfig has an attrs.Bag meta field to prove meta _env keys are left as
// dependency references rather than resolved as variables.
type MetaBagConfig struct {
	Name string    `json:"name"`
	Meta attrs.Bag `json:"meta"`
}

func TestEnvMap_MetaBagNotResolved(t *testing.T) {
	// A missing variable would hard-fail if meta were resolved; it must not be.
	reg := &fakeEnvRegistry{}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)

	entry := registry.Entry{
		ID:   registry.NewID("test", "meta"),
		Kind: "test.meta",
		Data: payload.New(map[string]any{"name": "x"}),
		Meta: attrs.Bag{"storage_env": "app:some_storage"},
	}

	cfg, err := DecodeEntryConfig[MetaBagConfig](ctx, realTranscoder{}, entry)
	require.NoError(t, err)
	assert.Equal(t, "app:some_storage", cfg.Meta["storage_env"], "meta _env keys stay as dependency references")
}

// TypedEnvConfig covers the integer/float bit-size branches of assignEnvValue.
type TypedEnvConfig struct {
	Small    int8    `json:"small"`
	SmallEnv string  `json:"small_env"`
	Count    uint32  `json:"count"`
	CountEnv string  `json:"count_env"`
	Ratio    float32 `json:"ratio"`
	RatioEnv string  `json:"ratio_env"`
	Big      int64   `json:"big"`
	BigEnv   string  `json:"big_env"`
}

func decodeTyped(t *testing.T, reg env.Registry, data map[string]any) (*TypedEnvConfig, error) {
	t.Helper()
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)
	entry := registry.Entry{ID: registry.NewID("test", "typed"), Kind: "test.typed", Data: payload.New(data)}
	return DecodeEntryConfig[TypedEnvConfig](ctx, realTranscoder{}, entry)
}

func TestEnvField_TypedBitSizes(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{
		{name: "S", value: "7"}, {name: "C", value: "4000000000"},
		{name: "R", value: "1.5"}, {name: "B", value: "9000000000"},
	}}
	cfg, err := decodeTyped(t, reg, map[string]any{
		"small_env": "S", "count_env": "C", "ratio_env": "R", "big_env": "B",
	})
	require.NoError(t, err)
	assert.Equal(t, int8(7), cfg.Small)
	assert.Equal(t, uint32(4000000000), cfg.Count)
	assert.Equal(t, float32(1.5), cfg.Ratio)
	assert.Equal(t, int64(9000000000), cfg.Big)
}

func TestEnvField_TypedOverflowFails(t *testing.T) {
	// 300 does not fit int8; conversion must fail rather than silently wrap.
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "S", value: "300"}}}
	_, err := decodeTyped(t, reg, map[string]any{"small_env": "S"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert")
}

func TestPlaceholder_WholeValueInt64AndFloat(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "N", value: "9000000000"}}}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)
	entry := registry.Entry{
		ID:   registry.NewID("test", "s"),
		Kind: "test.s",
		Data: payload.New(map[string]any{"big": "${env:N|1}"}),
	}
	type C struct {
		Big int64 `json:"big"`
	}
	cfg, err := DecodeEntryConfig[C](ctx, realTranscoder{}, entry)
	require.NoError(t, err)
	assert.Equal(t, int64(9000000000), cfg.Big)
}

func TestPlaceholder_WhitespaceInterpolatesNotTyped(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "N", value: "16"}}}
	ctx := env.WithRegistry(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), reg)
	entry := registry.Entry{
		ID:   registry.NewID("test", "s"),
		Kind: "test.s",
		Data: payload.New(map[string]any{"name": " ${env:N} "}),
	}
	cfg, err := DecodeEntryConfig[ResolveConfig](ctx, realTranscoder{}, entry)
	require.NoError(t, err)
	// Surrounding whitespace forces string interpolation, preserving the spaces.
	assert.Equal(t, " 16 ", cfg.Name)
}
