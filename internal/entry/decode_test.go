package entry

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// Test config types

type BasicConfig struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type ConfigWithMeta struct {
	Name string    `json:"name"`
	Meta attrs.Bag `json:"-"`
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
	Name    string    `json:"name"`
	Timeout int       `json:"timeout"`
	Meta    attrs.Bag `json:"-"`
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

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.New(p.Data()), nil
}

func (m *mockTranscoder) Marshal(_ interface{}) (payload.Payload, error) {
	return nil, errors.New("not implemented")
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	if m.unmarshalFunc != nil {
		return m.unmarshalFunc(p, v)
	}
	return errors.New("not implemented")
}

// Tests

func TestDecodeEntryConfig_NilData(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: nil,
	}

	cfg, err := DecodeEntryConfig[BasicConfig](ctx, dtt, entry)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "configuration data is required")
}

func TestDecodeEntryConfig_UnmarshalError(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, _ interface{}) error {
			return errors.New("unmarshal failed")
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test"}),
	}

	cfg, err := DecodeEntryConfig[BasicConfig](ctx, dtt, entry)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}

func TestDecodeEntryConfig_BasicConfig(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*BasicConfig)
			cfg.Name = "test-name"
			cfg.Value = 42
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name", "value": 42}),
	}

	cfg, err := DecodeEntryConfig[BasicConfig](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, 42, cfg.Value)
}

func TestDecodeEntryConfig_WithMeta(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMeta)
			cfg.Name = "test-name"
			return nil
		},
	}
	meta := attrs.Bag{
		"server": "test-server",
		"port":   8080,
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeEntryConfig[ConfigWithMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "test-server", cfg.Meta.GetString("server", ""))
	assert.Equal(t, 8080, cfg.Meta["port"])
}

func TestDecodeEntryConfig_WithDefaults(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithDefaults)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
	}

	cfg, err := DecodeEntryConfig[ConfigWithDefaults](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, 30, cfg.Timeout)
}

func TestDecodeEntryConfig_ValidationSuccess(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithValidation)
			cfg.Name = "valid-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "valid-name"}),
	}

	cfg, err := DecodeEntryConfig[ConfigWithValidation](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "valid-name", cfg.Name)
}

func TestDecodeEntryConfig_ValidationFailure(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, _ interface{}) error {
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{}),
	}

	cfg, err := DecodeEntryConfig[ConfigWithValidation](ctx, dtt, entry)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid configuration")
	// Validation error is wrapped as cause
	var entryErr *Error
	if errors.As(err, &entryErr) && errors.Unwrap(entryErr) != nil {
		assert.Contains(t, errors.Unwrap(entryErr).Error(), "name is required")
	}
}

func TestDecodeEntryConfig_AllFeatures(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithAll)
			cfg.Name = "complete-config"
			return nil
		},
	}
	meta := attrs.Bag{
		"environment": "production",
		"region":      "us-east-1",
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "complete-config"}),
		Meta: meta,
	}

	cfg, err := DecodeEntryConfig[ConfigWithAll](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "complete-config", cfg.Name)
	assert.Equal(t, 60, cfg.Timeout)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "production", cfg.Meta.GetString("environment", ""))
	assert.Equal(t, "us-east-1", cfg.Meta.GetString("region", ""))
}

func TestDecodeEntryConfig_SetMetaDoesNotOverwriteExisting(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMeta)
			cfg.Name = "test"
			cfg.Meta = attrs.Bag{"existing": "data"}
			return nil
		},
	}

	meta := attrs.Bag{"new": "data"}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test"}),
		Meta: meta,
	}

	cfg, err := DecodeEntryConfig[ConfigWithMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, "data", cfg.Meta["existing"])
	assert.Nil(t, cfg.Meta["new"])
}

func TestDecodeEntryConfig_EmptyMeta(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMeta)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: nil,
	}

	cfg, err := DecodeEntryConfig[ConfigWithMeta](ctx, dtt, entry)

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
	ID   registry.ID `json:"-"`
	Meta attrs.Bag   `json:"-"`
	Name string      `json:"name"`
}

type ConfigWithMetaNoMethod struct {
	Name string    `json:"name"`
	Meta attrs.Bag `json:"-"`
}

type ConfigWithUnexportedFields struct {
	Name string `json:"name"`
}

// Reflection Tests

func TestDecodeEntryConfig_ReflectionID(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithID)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
	}

	cfg, err := DecodeEntryConfig[ConfigWithID](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, registry.NewID("test", "config"), cfg.ID)
}

func TestDecodeEntryConfig_ReflectionIDAndMeta(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithIDAndMeta)
			cfg.Name = "test-name"
			return nil
		},
	}
	meta := attrs.Bag{
		"server": "test-server",
		"port":   8080,
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeEntryConfig[ConfigWithIDAndMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.Equal(t, registry.NewID("test", "config"), cfg.ID)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "test-server", cfg.Meta.GetString("server", ""))
	assert.Equal(t, 8080, cfg.Meta["port"])
}

func TestDecodeEntryConfig_ReflectionMetaNoMethod(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithMetaNoMethod)
			cfg.Name = "test-name"
			return nil
		},
	}
	meta := attrs.Bag{
		"key": "value",
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeEntryConfig[ConfigWithMetaNoMethod](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
	assert.NotNil(t, cfg.Meta)
	assert.Equal(t, "value", cfg.Meta.GetString("key", ""))
}

func TestDecodeEntryConfig_ReflectionDoesNotOverwriteExisting(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithIDAndMeta)
			cfg.Name = "test-name"
			cfg.ID = registry.NewID("existing", "id")
			cfg.Meta = attrs.Bag{"existing": "data"}
			return nil
		},
	}
	meta := attrs.Bag{"new": "data"}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: meta,
	}

	cfg, err := DecodeEntryConfig[ConfigWithIDAndMeta](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, registry.NewID("existing", "id"), cfg.ID)
	assert.Equal(t, "data", cfg.Meta["existing"])
	assert.Nil(t, cfg.Meta["new"])
}

func TestDecodeEntryConfig_ReflectionUnexportedFields(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithUnexportedFields)
			cfg.Name = "test-name"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test-name"}),
		Meta: attrs.Bag{"key": "value"},
	}

	cfg, err := DecodeEntryConfig[ConfigWithUnexportedFields](ctx, dtt, entry)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "test-name", cfg.Name)
}

func TestDecodeEntryConfig_ReflectionCachingPerformance(t *testing.T) {
	ctx := context.Background()
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v interface{}) error {
			cfg := v.(*ConfigWithIDAndMeta)
			cfg.Name = "test"
			return nil
		},
	}
	entry := registry.Entry{
		ID:   registry.NewID("test", "config"),
		Kind: "test.config",
		Data: payload.New(map[string]interface{}{"name": "test"}),
		Meta: attrs.Bag{"key": "value"},
	}

	fieldCache = sync.Map{}

	start := time.Now()
	_, err := DecodeEntryConfig[ConfigWithIDAndMeta](ctx, dtt, entry)
	require.NoError(t, err)
	firstCall := time.Since(start)

	start = time.Now()
	_, err = DecodeEntryConfig[ConfigWithIDAndMeta](ctx, dtt, entry)
	require.NoError(t, err)
	secondCall := time.Since(start)

	t.Logf("First call: %v, Second call: %v", firstCall, secondCall)

	typ := reflect.TypeOf(ConfigWithIDAndMeta{})
	_, cached := fieldCache.Load(typ)
	assert.True(t, cached, "Type should be cached after first call")
}
