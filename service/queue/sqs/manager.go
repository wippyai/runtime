// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqsapi "github.com/wippyai/runtime/api/service/queue/sqs"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	queuesvc "github.com/wippyai/runtime/service/queue"
	"go.uber.org/zap"
)

// Manager handles lifecycle of SQS driver instances.
type Manager struct {
	dtt     payload.Transcoder
	bus     event.Bus
	log     *zap.Logger
	drivers map[registry.ID]*Driver
	mu      sync.RWMutex
}

// NewManager creates a new SQS driver manager.
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{
		log:     log,
		dtt:     dtt,
		bus:     bus,
		drivers: make(map[registry.ID]*Driver),
	}
}

// loadAWSConfig resolves an aws.Config from the driver configuration.
//
// Resolution order:
//  1. AWSConfig set → acquire shared config.aws resource
//  2. Otherwise → build from inline fields (region, credentials, profile, HTTP, retry)
func (m *Manager) loadAWSConfig(ctx context.Context, cfg *sqsapi.Config) (aws.Config, error) {
	// Path 1: shared config.aws resource
	if cfg.AWSConfig != "" {
		return m.loadSharedAWSConfig(ctx, cfg)
	}

	// Path 2: inline configuration
	return m.loadInlineAWSConfig(ctx, cfg)
}

// loadSharedAWSConfig acquires an aws.Config from the shared resource registry,
// then applies SQS-specific overrides (endpoint).
func (m *Manager) loadSharedAWSConfig(ctx context.Context, cfg *sqsapi.Config) (aws.Config, error) {
	resourceRegistry := resource.GetRegistry(ctx)
	rsc, err := resourceRegistry.Acquire(ctx, registry.ParseID(cfg.AWSConfig), resource.ModeNormal)
	if err != nil {
		return aws.Config{}, fmt.Errorf("sqs: acquire aws config resource %q: %w", cfg.AWSConfig, err)
	}

	gotConfig, err := rsc.Get()
	if err != nil {
		return aws.Config{}, fmt.Errorf("sqs: get aws config resource: %w", err)
	}

	awsCfg, ok := gotConfig.(aws.Config)
	if !ok {
		return aws.Config{}, fmt.Errorf("sqs: aws config resource is not aws.Config")
	}

	// Apply SQS-specific overrides on top of the shared config
	if cfg.Endpoint != "" {
		awsCfg.BaseEndpoint = aws.String(cfg.Endpoint)
	}

	// Apply custom HTTP client if configured
	httpClient, ok, err := cfg.BuildHTTPClient()
	if err != nil {
		return aws.Config{}, fmt.Errorf("sqs: build http client: %w", err)
	}
	if ok {
		awsCfg.HTTPClient = httpClient
	}

	return awsCfg, nil
}

// loadInlineAWSConfig builds an aws.Config from the driver's inline fields.
func (m *Manager) loadInlineAWSConfig(ctx context.Context, cfg *sqsapi.Config) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.Profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}

	// Static credentials
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			aws.CredentialsProviderFunc(func(_ context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     cfg.AccessKeyID,
					SecretAccessKey: cfg.SecretAccessKey,
					SessionToken:    cfg.SessionToken,
				}, nil
			}),
		))
	}

	// Retry
	if cfg.RetryMaxAttempts > 0 {
		opts = append(opts, awsconfig.WithRetryMaxAttempts(cfg.RetryMaxAttempts))
	}
	switch cfg.RetryMode {
	case "standard":
		opts = append(opts, awsconfig.WithRetryMode(aws.RetryModeStandard))
	case "adaptive":
		opts = append(opts, awsconfig.WithRetryMode(aws.RetryModeAdaptive))
	}

	// Custom HTTP client
	httpClient, ok, err := cfg.BuildHTTPClient()
	if err != nil {
		return aws.Config{}, fmt.Errorf("sqs: build http client: %w", err)
	}
	if ok {
		opts = append(opts, awsconfig.WithHTTPClient(httpClient))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("aws config: %w", err)
	}

	// Endpoint (set on the resolved config, not via load options)
	if cfg.Endpoint != "" {
		awsCfg.BaseEndpoint = aws.String(cfg.Endpoint)
	}

	return awsCfg, nil
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqsapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; exists {
		return queuesvc.NewDriverExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[sqsapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	awsCfg, err := m.loadAWSConfig(ctx, cfg)
	if err != nil {
		return queuesvc.NewConfigError("failed to load aws config", err)
	}

	driver := NewDriver(entry.ID, cfg, awsCfg, m.log)
	m.drivers[entry.ID] = driver

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: driver,
			Config:  cfg.Lifecycle,
		},
	})

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverRegister,
		Path:   entry.ID.String(),
		Data:   driver,
	})

	m.log.Info("added sqs driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqsapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.RLock()
	driver, exists := m.drivers[entry.ID]
	m.mu.RUnlock()

	if !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[sqsapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: driver,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("updated sqs driver", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != sqsapi.Kind {
		return queuesvc.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.drivers[entry.ID]; !exists {
		return queuesvc.NewDriverNotFoundError(entry.ID)
	}

	delete(m.drivers, entry.ID)

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	m.bus.Send(ctx, event.Event{
		System: queueapi.System,
		Kind:   queueapi.DriverDelete,
		Path:   entry.ID.String(),
	})

	m.log.Info("deleted sqs driver", zap.String("id", entry.ID.String()))

	return nil
}
