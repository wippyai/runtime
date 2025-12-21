package otel

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.uber.org/zap"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.Enabled)
	assert.Equal(t, "localhost:4318", cfg.Endpoint)
	assert.Equal(t, "http/protobuf", cfg.Protocol)
	assert.Equal(t, "wippy-runtime", cfg.ServiceName)
	assert.True(t, cfg.Insecure)
	assert.Equal(t, 1.0, cfg.SampleRate)
	assert.Equal(t, []string{"tracecontext", "baggage"}, cfg.Propagators)
	assert.True(t, cfg.TracesEnabled)
	assert.False(t, cfg.MetricsEnabled)

	assert.True(t, cfg.HTTP.Enabled)
	assert.True(t, cfg.HTTP.ExtractHeaders)
	assert.True(t, cfg.HTTP.InjectHeaders)

	assert.True(t, cfg.Process.Enabled)
	assert.True(t, cfg.Process.TraceLifecycle)

	assert.True(t, cfg.Interceptor.Enabled)
	assert.Equal(t, 100, cfg.Interceptor.Order)

	assert.True(t, cfg.Queue.Enabled)
}

func TestLoadConfig_NilBootConfig(t *testing.T) {
	cfg := LoadConfig(nil)

	assert.Equal(t, DefaultConfig(), cfg)
}

type mockBootConfig struct {
	data map[string]any
}

func (m *mockBootConfig) Sub(key string) mockSubConfig {
	if v, ok := m.data[key]; ok {
		if sub, ok := v.(map[string]any); ok {
			return &mockSubConfigImpl{data: sub}
		}
	}
	return nil
}

type mockSubConfig interface {
	GetBool(key string, defaultVal bool) bool
	GetString(key string, defaultVal string) string
	GetInt(key string, defaultVal int) int
	Get(key string) (any, bool)
	Sub(key string) mockSubConfig
}

type mockSubConfigImpl struct {
	data map[string]any
}

func (m *mockSubConfigImpl) GetBool(key string, defaultVal bool) bool {
	if v, ok := m.data[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func (m *mockSubConfigImpl) GetString(key string, defaultVal string) string {
	if v, ok := m.data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func (m *mockSubConfigImpl) GetInt(key string, defaultVal int) int {
	if v, ok := m.data[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return defaultVal
}

func (m *mockSubConfigImpl) Get(key string) (any, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *mockSubConfigImpl) Sub(key string) mockSubConfig {
	if v, ok := m.data[key]; ok {
		if sub, ok := v.(map[string]any); ok {
			return &mockSubConfigImpl{data: sub}
		}
	}
	return nil
}

func TestApplyEnvOverrides_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_SDK_DISABLED", "true")

	ApplyEnvOverrides(&cfg, logger)

	assert.False(t, cfg.Enabled)
}

func TestApplyEnvOverrides_Endpoint(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, "collector:4318", cfg.Endpoint)
}

func TestApplyEnvOverrides_EndpointHTTPS(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://secure-collector:4318")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, "secure-collector:4318", cfg.Endpoint)
}

func TestApplyEnvOverrides_Protocol(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, "grpc", cfg.Protocol)
}

func TestApplyEnvOverrides_ServiceName(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_SERVICE_NAME", "my-service")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, "my-service", cfg.ServiceName)
}

func TestApplyEnvOverrides_ServiceVersion(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_SERVICE_VERSION", "1.2.3")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, "1.2.3", cfg.ServiceVersion)
}

func TestApplyEnvOverrides_SampleRate(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.5")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, 0.5, cfg.SampleRate)
}

func TestApplyEnvOverrides_SampleRateInvalid(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()
	originalRate := cfg.SampleRate

	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "invalid")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, originalRate, cfg.SampleRate)
}

func TestApplyEnvOverrides_Propagators(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_PROPAGATORS", "tracecontext, b3, jaeger")

	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, []string{"tracecontext", "b3", "jaeger"}, cfg.Propagators)
}

func TestApplyEnvOverrides_NoEnvVars(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	os.Unsetenv("OTEL_SDK_DISABLED")
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	os.Unsetenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	os.Unsetenv("OTEL_SERVICE_NAME")
	os.Unsetenv("OTEL_SERVICE_VERSION")
	os.Unsetenv("OTEL_TRACES_SAMPLER_ARG")
	os.Unsetenv("OTEL_PROPAGATORS")

	original := DefaultConfig()
	ApplyEnvOverrides(&cfg, logger)

	assert.Equal(t, original, cfg)
}

func TestApplyEnvOverrides_DisabledFalse(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	t.Setenv("OTEL_SDK_DISABLED", "false")

	ApplyEnvOverrides(&cfg, logger)

	assert.True(t, cfg.Enabled)
}

func TestLogConfigSources(t *testing.T) {
	cfg := otelapi.Config{
		Endpoint:    "localhost:4318",
		Protocol:    "http/protobuf",
		ServiceName: "test-service",
		SampleRate:  0.5,
		Propagators: []string{"tracecontext"},
	}
	logger := zap.NewNop()

	require.NotPanics(t, func() {
		LogConfigSources(cfg, logger)
	})
}
