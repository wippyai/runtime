package s3

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ponyruntime/pony/api/cloudstorage"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	services3 "github.com/ponyruntime/pony/api/service/s3"
	internalconfig "github.com/ponyruntime/pony/internal/config"
	"go.uber.org/zap"
)

// Manager handles S3 storage lifecycle and functions as a resource provider
type Manager struct {
	log      *zap.Logger
	dtt      payload.Transcoder
	bus      event.Bus
	mu       sync.RWMutex
	storages map[registry.ID]*Storage
}

// NewManager creates a new S3 storage manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:      log,
		dtt:      dtt,
		bus:      bus,
		storages: make(map[registry.ID]*Storage),
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != services3.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage already exists
	if _, exists := m.storages[entry.ID]; exists {
		return fmt.Errorf("storage %s already exists", entry.ID)
	}

	// Decode and initialize configuration
	cfg, err := internalconfig.DecodeAndInitConfig[services3.Config](m.dtt, entry)
	if err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	// Create AWS config
	awsCfg, err := m.createAWSConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create AWS config: %w", err)
	}

	// Create S3 client
	client := s3.NewFromConfig(awsCfg)

	// Create S3 storage
	storage := NewStorage(client, cfg.Bucket, cfg, m.log)
	m.storages[entry.ID] = storage

	// Register Manager as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID: entry.ID,
			Meta: map[string]any{
				"bucket": cfg.Bucket,
				"region": cfg.Region,
			},
			Provider: m, // Manager itself is the provider
		},
	})

	m.log.Info("added S3 storage",
		zap.String("id", entry.ID.String()),
		zap.String("bucket", cfg.Bucket),
		zap.String("region", cfg.Region))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != services3.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage exists
	_, exists := m.storages[entry.ID]
	if !exists {
		return fmt.Errorf("storage %s not found", entry.ID)
	}

	// Decode and initialize updated configuration
	cfg, err := internalconfig.DecodeAndInitConfig[services3.Config](m.dtt, entry)
	if err != nil {
		return err
	}

	// Create AWS config
	awsCfg, err := m.createAWSConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create AWS config: %w", err)
	}

	// Create new S3 client
	client := s3.NewFromConfig(awsCfg)

	// Create new storage with updated config
	newStorage := NewStorage(client, cfg.Bucket, cfg, m.log)
	m.storages[entry.ID] = newStorage

	// Update resource provider metadata (provider remains the same)
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID: entry.ID,
			Meta: map[string]any{
				"bucket": cfg.Bucket,
				"region": cfg.Region,
			},
			Provider: m, // Manager itself is the provider
		},
	})

	m.log.Info("updated S3 storage",
		zap.String("id", entry.ID.String()),
		zap.String("bucket", cfg.Bucket),
		zap.String("region", cfg.Region))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != services3.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage exists
	_, exists := m.storages[entry.ID]
	if !exists {
		return fmt.Errorf("storage %s not found", entry.ID)
	}

	// Unregister resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	delete(m.storages, entry.ID)

	m.log.Info("deleted S3 storage",
		zap.String("id", entry.ID.String()))

	return nil
}

// Acquire implements resource.Provider interface
func (m *Manager) Acquire(_ context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.storages[id]
	if !exists {
		return nil, fmt.Errorf("storage %s not found", id)
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &s3Resource{
		manager: m,
		id:      id,
	}, nil
}

// s3Resource represents an acquired S3 storage resource
type s3Resource struct {
	manager *Manager
	id      registry.ID
	closed  bool
	mu      sync.Mutex
}

// Get implements resource.Resource interface
func (r *s3Resource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	// Ensure storage still exists in manager
	r.manager.mu.RLock()
	storage, exists := r.manager.storages[r.id]
	r.manager.mu.RUnlock()

	if !exists {
		return nil, resource.ErrResourceReleased
	}

	return cloudstorage.Storage(storage), nil
}

// Release implements resource.Resource interface
func (r *s3Resource) Release() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true
	return nil
}

// createAWSConfig creates an AWS configuration from S3Config
func (m *Manager) createAWSConfig(ctx context.Context, cfg *services3.Config) (aws.Config, error) {
	options := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	// Add credentials if provided
	if cfg.AccessKeyIDEnv != "" && cfg.SecretAccessKeyEnv != "" {
		options = append(options, config.WithCredentialsProvider(
			aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     os.Getenv(cfg.AccessKeyIDEnv),
					SecretAccessKey: os.Getenv(cfg.SecretAccessKeyEnv),
				}, nil
			}),
		))
	}

	// Load AWS configuration
	return config.LoadDefaultConfig(ctx, options...)
}
