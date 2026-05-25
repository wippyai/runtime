// SPDX-License-Identifier: MPL-2.0

package config

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	awsconfigapi "github.com/wippyai/runtime/api/service/aws/config"
	entryutil "github.com/wippyai/runtime/internal/entry"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

// Manager handles AWS config lifecycle and functions as a resource provider
type Manager struct {
	dtt     payload.Transcoder
	bus     event.Bus
	env     envapi.Registry
	log     *zap.Logger
	configs map[registry.ID]aws.Config
	mu      sync.RWMutex
}

// NewManager creates a new AWS config manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
	envRegistry envapi.Registry,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
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
	if entry.Kind != awsconfigapi.Kind {
		return NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if config already exists
	if _, exists := m.configs[entry.ID]; exists {
		return NewConfigAlreadyExistsError(entry.ID.String())
	}

	// Decode and initialize configuration
	cfg, err := entryutil.DecodeEntryConfig[awsconfigapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeConfigError(err)
	}

	// Create AWS config
	awsCfg, err := m.createAWSConfig(ctx, cfg)
	if err != nil {
		return NewCreateAWSConfigError(err)
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
	if entry.Kind != awsconfigapi.Kind {
		return NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if config exists
	_, exists := m.configs[entry.ID]
	if !exists {
		return NewConfigNotFoundError(entry.ID.String())
	}

	// Decode and initialize updated configuration
	cfg, err := entryutil.DecodeEntryConfig[awsconfigapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeConfigError(err)
	}

	// Create new AWS config
	awsCfg, err := m.createAWSConfig(ctx, cfg)
	if err != nil {
		return NewCreateAWSConfigError(err)
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
	if entry.Kind != awsconfigapi.Kind {
		return NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if config exists
	_, exists := m.configs[entry.ID]
	if !exists {
		return NewConfigNotFoundError(entry.ID.String())
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
		return nil, NewConfigNotFoundError(id.String())
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, systemresource.ErrLocked
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
		return nil, resource.ErrReleased
	}

	// Ensure config still exists in manager
	r.manager.mu.RLock()
	c, exists := r.manager.configs[r.id]
	r.manager.mu.RUnlock()

	if !exists {
		return nil, resource.ErrReleased
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

	r.closed = true
}

// createAWSConfig creates an AWS configuration from Config
func (m *Manager) createAWSConfig(ctx context.Context, cfg *awsconfigapi.Config) (aws.Config, error) {
	if cfg.RegionEnv != "" {
		region, found, err := m.getEnvValue(ctx, cfg.RegionEnv, "region")
		if err != nil {
			if cfg.Region == "" {
				return aws.Config{}, err
			}
		} else if found {
			cfg.Region = region
		}
	}

	options := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	var accessKey, secretKey string

	// Only try to get credentials if environment variable names are provided
	if cfg.AccessKeyIDEnv != "" {
		if m.env != nil {
			accessKey, _ = m.env.Get(ctx, cfg.AccessKeyIDEnv)
		}
	}

	if cfg.SecretAccessKeyEnv != "" {
		if m.env != nil {
			secretKey, _ = m.env.Get(ctx, cfg.SecretAccessKeyEnv)
		}
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

func (m *Manager) getEnvValue(ctx context.Context, envName, field string) (string, bool, error) {
	if envName == "" {
		return "", false, nil
	}
	if m.env == nil {
		return "", false, fmt.Errorf("%s_env %q requested but env registry is unavailable", field, envName)
	}
	value, err := m.env.Get(ctx, envName)
	if err != nil {
		return "", false, fmt.Errorf("lookup %s_env %q: %w", field, envName, err)
	}
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}
