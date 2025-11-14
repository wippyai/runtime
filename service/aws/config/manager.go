package config

import (
	"context"
	"fmt"
	"sync"

	envapi "github.com/wippyai/runtime/api/env"

	serviceaws "github.com/wippyai/runtime/api/service/aws/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager handles S3 storage lifecycle and functions as a resource provider
type Manager struct {
	log     *zap.Logger
	dtt     payload.Transcoder
	bus     event.Bus
	mu      sync.RWMutex
	configs map[registry.ID]aws.Config
	env     envapi.Registry
}

// NewManager creates a new S3 storage manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
	envRegistry envapi.Registry,
) *Manager {
	return &Manager{
		log:     log,
		dtt:     dtt,
		bus:     bus,
		configs: make(map[registry.ID]aws.Config),
		env:     envRegistry,
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != serviceaws.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage already exists
	if _, exists := m.configs[entry.ID]; exists {
		return fmt.Errorf("storage %s already exists", entry.ID)
	}

	// Decode and initialize configuration
	cfg, err := entryutil.DecodeEntryConfig[serviceaws.Config](ctx, m.dtt, entry)
	if err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	// Create AWS config
	awsCfg, err := m.createAWSConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create AWS config: %w", err)
	}

	m.configs[entry.ID] = awsCfg

	// Register Manager as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID: entry.ID,
			Meta: map[string]any{
				"region": cfg.Region,
			},
			Provider: m, // Manager itself is the provider
		},
	})

	m.log.Info("added aws config",
		zap.String("id", entry.ID.String()),
		zap.String("region", cfg.Region))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != serviceaws.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage exists
	_, exists := m.configs[entry.ID]
	if !exists {
		return fmt.Errorf("storage %s not found", entry.ID)
	}

	// Decode and initialize updated configuration
	cfg, err := entryutil.DecodeEntryConfig[serviceaws.Config](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	// Create new AWS config
	awsCfg, err := m.createAWSConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create AWS config: %w", err)
	}

	m.configs[entry.ID] = awsCfg

	// Update resource provider metadata (provider remains the same)
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID: entry.ID,
			Meta: map[string]any{
				"region": cfg.Region,
			},
			Provider: m, // Manager itself is the provider
		},
	})

	m.log.Info("updated aws config",
		zap.String("id", entry.ID.String()),
		zap.String("region", cfg.Region))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != serviceaws.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if config exists
	_, exists := m.configs[entry.ID]
	if !exists {
		return fmt.Errorf("config %s not found", entry.ID)
	}

	// Unregister resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	delete(m.configs, entry.ID)

	m.log.Info("deleted aws config",
		zap.String("id", entry.ID.String()))

	return nil
}

// Acquire implements resource.Provider interface
func (m *Manager) Acquire(_ context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.configs[id]
	if !exists {
		return nil, fmt.Errorf("config %s not found", id)
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &configResource{
		manager: m,
		id:      id,
	}, nil
}

// configResource represents an acquired aws config resource
type configResource struct {
	manager *Manager
	id      registry.ID
	closed  bool
	mu      sync.Mutex
}

// Get implements resource.Resource interface
func (r *configResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	// Ensure storage still exists in manager
	r.manager.mu.RLock()
	c, exists := r.manager.configs[r.id]
	r.manager.mu.RUnlock()

	if !exists {
		return nil, resource.ErrResourceReleased
	}

	return c, nil
}

// Release implements resource.Resource interface
func (r *configResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	delete(r.manager.configs, r.id)
	r.closed = true
}

// createAWSConfig creates an AWS configuration from S3Config
func (m *Manager) createAWSConfig(ctx context.Context, cfg *serviceaws.Config) (aws.Config, error) {
	options := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	var accessKey, secretKey string

	// Only try to get credentials if environment variable names are provided
	if cfg.AccessKeyIDEnv != "" {
		accessKey, _ = m.env.Get(ctx, cfg.AccessKeyIDEnv)
	}

	if cfg.SecretAccessKeyEnv != "" {
		secretKey, _ = m.env.Get(ctx, cfg.SecretAccessKeyEnv)
	}

	// Add credentials if provided
	if accessKey != "" && secretKey != "" {
		options = append(options, config.WithCredentialsProvider(
			aws.CredentialsProviderFunc(func(_ context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     accessKey,
					SecretAccessKey: secretKey,
				}, nil
			}),
		))
	}

	// Load AWS configuration
	return config.LoadDefaultConfig(ctx, options...)
}
