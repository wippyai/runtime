package config

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test config types

type BasicConfig struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type ConfigWithMeta struct {
	Name string            `json:"name"`
	Meta registry.Metadata `json:"-"`
}

type ConfigWithDefaults struct {
	Name    string `json:"name"`
	Timeout int    `json:"timeout"`
}

func (c *ConfigWithDefaults) InitDefaults() {
	if c.Timeout == 0 {
		c.Timeout = 30
	}
}

type ConfigWithValidation struct {
	Name string `json:"name"`
}

func (c *ConfigWithValidation) Validate() error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type ConfigWithAll struct {
	Name    string            `json:"name"`
	Timeout int               `json:"timeout"`
	Meta    registry.Metadata `json:"-"`
}

func (c *ConfigWithAll) InitDefaults() {
	if c.Timeout == 0 {
		c.Timeout = 60
	}
}

func (c *ConfigWithAll) Validate() error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	if c.Timeout < 0 {
		return errors.New("timeout must be positive")
	}
	return nil
}

// Mock transcoder

type mockTranscoder struct {
	unmarshalFunc func(payload.Payload, interface{}) error
}

func (m *mockTranscoder) Transcode(p payload.Payload, f payload.Format) (payload.Payload, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTranscoder) Marshal(v interface{}) (payload.Payload, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	if m.unmarshalFunc != nil {
		return m.unmarshalFunc(p, v)
	}
	return errors.New("not implemented")
}

// Tests

func TestDecodeAndInitConfig_NilData(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: nil,
	}

	cfg, err := DecodeAndInitConfig[BasicConfig](ctx, dtt, entry)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "configuration data is required")
}

func TestDecodeAndInitConfig_UnmarshalError(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			return errors.New("unmarshal failed")
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test"}),
	}

	cfg, err := DecodeAndInitConfig[BasicConfig](ctx, dtt, entry)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}

func TestDecodeAndInitConfig_BasicConfig(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*BasicConfig)
			cfg.Name = "test-name"
			cfg.Value = 42
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name", "value": 42}),
	}

	cfg, err := DecodeAndInitConfig[BasicConfig](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, 42, cfg.Value)
}

func TestDecodeAndInitConfig_WithMeta(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMeta)
			cfg.Name = "test-name"
			return nil
		},
	}
	meta := registry.Metadata{
		"server": "test-server",
		"port":   8080,
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeAndInitConfig[ConfigWithMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "test-server", cfg.Meta.StringValue("server"))
	assert.Equal(t, 8080, cfg.Meta["port"])
}

func TestDecodeAndInitConfig_WithDefaults(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithDefaults)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
	}

	cfg, err := DecodeAndInitConfig[ConfigWithDefaults](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, 30, cfg.Timeout)
}

func TestDecodeAndInitConfig_ValidationSuccess(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithValidation)
			cfg.Name = "valid-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "valid-name"}),
	}

	cfg, err := DecodeAndInitConfig[ConfigWithValidation](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "valid-name", cfg.Name)
}

func TestDecodeAndInitConfig_ValidationFailure(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{}),
	}

	cfg, err := DecodeAndInitConfig[ConfigWithValidation](ctx, dtt, entry)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid configuration")
	assert.Contains(t, err.Error(), "name is required")
}

func TestDecodeAndInitConfig_AllFeatures(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithAll)
			cfg.Name = "complete-config"
			return nil
		},
	}
	meta := registry.Metadata{
		"environment": "production",
		"region":      "us-east-1",
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "complete-config"}),
		Meta: meta,
	}

	cfg, err := DecodeAndInitConfig[ConfigWithAll](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "complete-config", cfg.Name)
	assert.Equal(t, 60, cfg.Timeout)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "production", cfg.Meta.StringValue("environment"))
	assert.Equal(t, "us-east-1", cfg.Meta.StringValue("region"))
}

func TestDecodeAndInitConfig_SetMetaDoesNotOverwriteExisting(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMeta)
			cfg.Name = "test"
			cfg.Meta = registry.Metadata{"existing": "data"}
			return nil
		},
	}

	meta := registry.Metadata{"new": "data"}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test"}),
		Meta: meta,
	}

	cfg, err := DecodeAndInitConfig[ConfigWithMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, "data", cfg.Meta["existing"])
	assert.Nil(t, cfg.Meta["new"])
}

func TestDecodeAndInitConfig_EmptyMeta(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMeta)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: nil,
	}

	cfg, err := DecodeAndInitConfig[ConfigWithMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Nil(t, cfg.Meta)
}

// Reflection-based test config types

type ConfigWithID struct {
	ID   registry.ID `json:"-"`
	Name string      `json:"name"`
}

type ConfigWithIDAndMeta struct {
	ID   registry.ID       `json:"-"`
	Meta registry.Metadata `json:"-"`
	Name string            `json:"name"`
}

type ConfigWithMetaNoMethod struct {
	Name string            `json:"name"`
	Meta registry.Metadata `json:"-"`
}

type ConfigWithUnexportedFields struct {
	Name string `json:"name"`
	meta registry.Metadata
	id   registry.ID
}

// Reflection Tests

func TestDecodeAndInitConfig_ReflectionID(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithID)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
	}

	cfg, err := DecodeAndInitConfig[ConfigWithID](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, registry.ID{NS: "test", Name: "config"}, cfg.ID)
}

func TestDecodeAndInitConfig_ReflectionIDAndMeta(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithIDAndMeta)
			cfg.Name = "test-name"
			return nil
		},
	}
	meta := registry.Metadata{
		"server": "test-server",
		"port":   8080,
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeAndInitConfig[ConfigWithIDAndMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, registry.ID{NS: "test", Name: "config"}, cfg.ID)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "test-server", cfg.Meta.StringValue("server"))
	assert.Equal(t, 8080, cfg.Meta["port"])
}

func TestDecodeAndInitConfig_ReflectionMetaNoMethod(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMetaNoMethod)
			cfg.Name = "test-name"
			return nil
		},
	}
	meta := registry.Metadata{
		"key": "value",
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeAndInitConfig[ConfigWithMetaNoMethod](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "value", cfg.Meta.StringValue("key"))
}

func TestDecodeAndInitConfig_ReflectionDoesNotOverwriteExisting(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithIDAndMeta)
			cfg.Name = "test-name"
			cfg.ID = registry.ID{NS: "existing", Name: "id"}
			cfg.Meta = registry.Metadata{"existing": "data"}
			return nil
		},
	}
	meta := registry.Metadata{"new": "data"}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeAndInitConfig[ConfigWithIDAndMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, registry.ID{NS: "existing", Name: "id"}, cfg.ID)
	assert.Equal(t, "data", cfg.Meta["existing"])
	assert.Nil(t, cfg.Meta["new"])
}

func TestDecodeAndInitConfig_ReflectionUnexportedFields(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithUnexportedFields)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: registry.Metadata{"key": "value"},
	}

	cfg, err := DecodeAndInitConfig[ConfigWithUnexportedFields](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
}

func TestDecodeAndInitConfig_ReflectionCachingPerformance(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(p payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithIDAndMeta)
			cfg.Name = "test"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "config"},
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test"}),
		Meta: registry.Metadata{"key": "value"},
	}

	fieldCache = sync.Map{}

	start := time.Now()
	_, err := DecodeAndInitConfig[ConfigWithIDAndMeta](ctx, dtt, entry)
	require.NoError(t, err)
	firstCall := time.Since(start)

	start = time.Now()
	_, err = DecodeAndInitConfig[ConfigWithIDAndMeta](ctx, dtt, entry)
	require.NoError(t, err)
	secondCall := time.Since(start)

	t.Logf("First call: %v, Second call: %v", firstCall, secondCall)

	typ := reflect.TypeOf(ConfigWithIDAndMeta{})
	_, cached := fieldCache.Load(typ)
	assert.True(t, cached, "Type should be cached after first call")
}
