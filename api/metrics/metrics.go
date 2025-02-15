package metrics

import (
	"context"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

const (
	KindRegistry registry.Kind = "metrics.registry"
	// KindCounter identifies a counter metric.
	KindCounter registry.Kind = "metrics.counter"
	// KindGauge identifies a gauge metric.
	KindGauge registry.Kind = "metrics.gauge"
	// KindHistogram identifies a histogram metric.
	KindHistogram registry.Kind = "metrics.histogram"
	// KindSummary identifies a summary metric.
	KindSummary registry.Kind = "metrics.summary"

	// RegistryID is key used to identify metrics registry.
	RegistryID string = "registry"
)

type (
	// Registry manages a collection of metrics
	Registry interface {
		// Counter creates or gets an existing counter
		Counter(name, help string, labels ...string) Counter

		// Gauge creates or gets an existing gauge
		Gauge(name, help string, labels ...string) Gauge

		// Histogram creates or gets an existing histogram
		Histogram(name, help string, buckets []float64, labels ...string) Histogram

		// Summary creates or gets an existing summary
		Summary(name, help string, objectives map[float64]float64, labels ...string) Summary
	}

	RegistryManager interface {
		// Collectors returns all registered collectors
		Collectors() []Collector

		// Names returns all registered metric names
		Names() []string

		// Get returns a collector by name
		Get(name string) (Collector, error)
	}

	RegistryConfig struct {
		// Prefix for all metrics in this registry
		Prefix string `json:"prefix"`

		// Default labels applied to all metrics
		Labels map[string]string `json:"labels"`

		// IsDefault marks this registry as a default metrics provider
		IsDefault bool `json:"is_default"`

		// Address to expose prometheus metrics on
		Address string `json:"address"`

		// Path to expose metrics on
		Path string `json:"path" default:"/metrics"`

		// Lifecycle configuration
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
	}

	// MetricConfig is used to declare a metric ahead of time.
	MetricConfig struct {
		Meta registry.Metadata `json:"meta"`
		// Name is the metric name.
		Name string `json:"name"`
		// Kind is the metric kind.
		Kind registry.Kind
		// Help is the help text describing the metric.
		Help string `json:"help"`
		// Labels defines the label names for the metric.
		Labels []string `json:"labels,omitempty"`
		// Buckets is used for histogram metrics.
		Buckets []float64 `json:"buckets,omitempty"`
		// Objectives is used for summary metrics.
		Objectives map[float64]float64 `json:"objectives,omitempty"`
	}

	Manager interface {
		Get(id registry.ID) (Registry, error)
		GetDefault() (Registry, error)
	}
)

func GetMetrics(ctx context.Context) Manager {
	return ctx.Value(contextapi.MetricsCtx).(Manager)
}

func (c *RegistryConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()
}

func (c *RegistryConfig) Validate() error {
	if c.Prefix == "" {
		return fmt.Errorf("prefix cannot be empty")
	}

	if c.Address == "" {
		return fmt.Errorf("address cannot be empty")
	}

	if c.Path == "" {
		c.Path = "/metrics"
	}

	return nil
}

// Validate checks if the MetricConfig is valid.
func (mc *MetricConfig) Validate() error {
	if mc.Meta == nil {
		return fmt.Errorf("metadata cannot be nil")
	}

	serverID := mc.Meta.StringValue(RegistryID)
	if serverID == "" {
		return fmt.Errorf("registry in metadata cannot be empty")
	}

	if mc.Name == "" {
		return fmt.Errorf("metric name cannot be empty")
	}
	if mc.Help == "" {
		return fmt.Errorf("metric help text cannot be empty")
	}

	return nil
}
