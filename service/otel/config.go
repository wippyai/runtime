package otel

import (
	"os"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.uber.org/zap"
)

// DefaultConfig returns OTEL configuration with safe defaults
func DefaultConfig() otelapi.Config {
	return otelapi.Config{
		Enabled:        false,
		Endpoint:       "localhost:4318",
		Protocol:       "http/protobuf",
		ServiceName:    "wippy-runtime",
		ServiceVersion: "",
		Insecure:       true,
		SampleRate:     1.0,
		Propagators:    []string{"tracecontext", "baggage"},
		TracesEnabled:  true,
		MetricsEnabled: false,
		HTTP: otelapi.HTTPConfig{
			Enabled:        true,
			ExtractHeaders: true,
			InjectHeaders:  true,
		},
		Process: otelapi.ProcessConfig{
			Enabled:        true,
			TraceLifecycle: true,
		},
		Interceptor: otelapi.InterceptorConfig{
			Enabled: true,
			Order:   100,
		},
		Queue: otelapi.QueueConfig{
			Enabled: true,
		},
	}
}

// LoadConfig loads OTEL configuration from boot config
func LoadConfig(bootCfg boot.Config) otelapi.Config {
	cfg := DefaultConfig()

	if bootCfg == nil {
		return cfg
	}

	otelCfg := bootCfg.Sub("otel")
	if otelCfg == nil {
		return cfg
	}

	cfg.Enabled = otelCfg.GetBool("enabled", cfg.Enabled)
	cfg.Endpoint = otelCfg.GetString("endpoint", cfg.Endpoint)
	cfg.Protocol = otelCfg.GetString("protocol", cfg.Protocol)
	cfg.ServiceName = otelCfg.GetString("service_name", cfg.ServiceName)
	cfg.ServiceVersion = otelCfg.GetString("service_version", cfg.ServiceVersion)
	cfg.Insecure = otelCfg.GetBool("insecure", cfg.Insecure)
	cfg.TracesEnabled = otelCfg.GetBool("traces_enabled", cfg.TracesEnabled)
	cfg.MetricsEnabled = otelCfg.GetBool("metrics_enabled", cfg.MetricsEnabled)

	// Sample rate (float64)
	if v, ok := otelCfg.Get("sample_rate"); ok {
		if rate, ok := v.(float64); ok {
			cfg.SampleRate = rate
		}
	}

	// Propagators ([]string)
	if v, ok := otelCfg.Get("propagators"); ok {
		if propagators, ok := v.([]string); ok && len(propagators) > 0 {
			cfg.Propagators = propagators
		} else if propagators, ok := v.([]interface{}); ok && len(propagators) > 0 {
			cfg.Propagators = make([]string, len(propagators))
			for i, p := range propagators {
				if s, ok := p.(string); ok {
					cfg.Propagators[i] = s
				}
			}
		}
	}

	// HTTP config
	if httpCfg := otelCfg.Sub("http"); httpCfg != nil {
		cfg.HTTP.Enabled = httpCfg.GetBool("enabled", cfg.HTTP.Enabled)
		cfg.HTTP.ExtractHeaders = httpCfg.GetBool("extract_headers", cfg.HTTP.ExtractHeaders)
		cfg.HTTP.InjectHeaders = httpCfg.GetBool("inject_headers", cfg.HTTP.InjectHeaders)
	}

	// Process config
	if procCfg := otelCfg.Sub("process"); procCfg != nil {
		cfg.Process.Enabled = procCfg.GetBool("enabled", cfg.Process.Enabled)
		cfg.Process.TraceLifecycle = procCfg.GetBool("trace_lifecycle", cfg.Process.TraceLifecycle)
	}

	// Interceptor config
	if intCfg := otelCfg.Sub("interceptor"); intCfg != nil {
		cfg.Interceptor.Enabled = intCfg.GetBool("enabled", cfg.Interceptor.Enabled)
		cfg.Interceptor.Order = intCfg.GetInt("order", cfg.Interceptor.Order)
	}

	// Queue config
	if queueCfg := otelCfg.Sub("queue"); queueCfg != nil {
		cfg.Queue.Enabled = queueCfg.GetBool("enabled", cfg.Queue.Enabled)
	}

	// Temporal config
	if temporalCfg := otelCfg.Sub("temporal"); temporalCfg != nil {
		cfg.Temporal.Enabled = temporalCfg.GetBool("enabled", cfg.Temporal.Enabled)
	}

	return cfg
}

// ApplyEnvOverrides applies standard OTEL environment variables to config
func ApplyEnvOverrides(cfg *otelapi.Config, logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	// OTEL_SDK_DISABLED
	if disabled := os.Getenv("OTEL_SDK_DISABLED"); disabled != "" {
		if strings.ToLower(disabled) == "true" {
			logger.Debug("SDK disabled via env",
				zap.String("var", "OTEL_SDK_DISABLED"),
				zap.String("value", disabled))
			cfg.Enabled = false
		} else if strings.ToLower(disabled) == "false" {
			logger.Debug("SDK enabled via env",
				zap.String("var", "OTEL_SDK_DISABLED"),
				zap.String("value", disabled))
			cfg.Enabled = true
		}
	}

	// OTEL_EXPORTER_OTLP_ENDPOINT
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		endpoint = strings.TrimPrefix(endpoint, "https://")
		logger.Debug("using endpoint from env",
			zap.String("var", "OTEL_EXPORTER_OTLP_ENDPOINT"),
			zap.String("value", endpoint))
		cfg.Endpoint = endpoint
	}

	// OTEL_EXPORTER_OTLP_PROTOCOL
	if protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"); protocol != "" {
		logger.Debug("using protocol from env",
			zap.String("var", "OTEL_EXPORTER_OTLP_PROTOCOL"),
			zap.String("value", protocol))
		cfg.Protocol = protocol
	}

	// OTEL_SERVICE_NAME
	if serviceName := os.Getenv("OTEL_SERVICE_NAME"); serviceName != "" {
		logger.Debug("using service name from env",
			zap.String("var", "OTEL_SERVICE_NAME"),
			zap.String("value", serviceName))
		cfg.ServiceName = serviceName
	}

	// OTEL_SERVICE_VERSION
	if serviceVersion := os.Getenv("OTEL_SERVICE_VERSION"); serviceVersion != "" {
		logger.Debug("using service version from env",
			zap.String("var", "OTEL_SERVICE_VERSION"),
			zap.String("value", serviceVersion))
		cfg.ServiceVersion = serviceVersion
	}

	// OTEL_TRACES_SAMPLER_ARG (sample rate)
	if samplerArg := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); samplerArg != "" {
		if rate, err := strconv.ParseFloat(samplerArg, 64); err == nil {
			logger.Debug("using sample rate from env",
				zap.String("var", "OTEL_TRACES_SAMPLER_ARG"),
				zap.Float64("value", rate))
			cfg.SampleRate = rate
		}
	}

	// OTEL_PROPAGATORS
	if propagators := os.Getenv("OTEL_PROPAGATORS"); propagators != "" {
		list := strings.Split(propagators, ",")
		for i := range list {
			list[i] = strings.TrimSpace(list[i])
		}
		logger.Debug("using propagators from env",
			zap.String("var", "OTEL_PROPAGATORS"),
			zap.Strings("value", list))
		cfg.Propagators = list
	}
}

// LogConfigSources logs where configuration values came from
func LogConfigSources(cfg otelapi.Config, logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	fields := []zap.Field{
		zap.String("endpoint", cfg.Endpoint),
		zap.String("protocol", cfg.Protocol),
		zap.String("service_name", cfg.ServiceName),
		zap.Float64("sample_rate", cfg.SampleRate),
		zap.Strings("propagators", cfg.Propagators),
	}

	logger.Info("OTEL configuration loaded", fields...)
}
