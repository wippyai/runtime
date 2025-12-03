package s3

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	services3 "github.com/wippyai/runtime/api/service/aws/s3"
	entryutil "github.com/wippyai/runtime/internal/entry"
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
		return newUnsupportedKindError(string(entry.Kind))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage already exists
	if _, exists := m.storages[entry.ID]; exists {
		return newStorageAlreadyExistsError(entry.ID.String())
	}

	meta, err := m.set(ctx, entry)
	if err != nil {
		return newAddEntryError(err)
	}

	// Register Manager as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Meta:     meta,
			Provider: m, // Manager itself is the provider
		},
	})

	m.log.Info("added S3 storage",
		zap.String("id", entry.ID.String()),
		zap.String("bucket", meta.GetString("bucket", "")),
	)

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != services3.Kind {
		return newUnsupportedKindError(string(entry.Kind))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage already exists
	if _, exists := m.storages[entry.ID]; !exists {
		return newStorageNotFoundError(entry.ID.String())
	}

	meta, err := m.set(ctx, entry)
	if err != nil {
		return newUpdateEntryError(err)
	}

	// Update resource provider metadata (provider remains the same)
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Meta:     meta,
			Provider: m, // Manager itself is the provider
		},
	})

	m.log.Info("updated S3 storage",
		zap.String("id", entry.ID.String()),
		zap.String("bucket", meta.GetString("bucket", "")),
	)

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != services3.Kind {
		return newUnsupportedKindError(string(entry.Kind))
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if storage exists
	_, exists := m.storages[entry.ID]
	if !exists {
		return newStorageNotFoundError(entry.ID.String())
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
		return nil, newStorageNotFoundError(id.String())
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

func (m *Manager) set(ctx context.Context, entry registry.Entry) (registry.Metadata, error) {
	// Decode and initialize configuration
	cfg, err := entryutil.DecodeEntryConfig[services3.Config](ctx, m.dtt, entry)
	if err != nil {
		return nil, newDecodeConfigError(err)
	}

	resourceRegistry := resource.GetRegistry(ctx)
	rsc, err := resourceRegistry.Acquire(ctx, registry.ParseID(cfg.AWSConfig), resource.ModeNormal)
	if err != nil {
		return nil, newAcquireResourceError(err)
	}

	gotConfig, err := rsc.Get()
	if err != nil {
		return nil, newGetConfigError(err)
	}
	awsCfg, ok := gotConfig.(aws.Config)
	if !ok {
		return nil, newAWSConfigNotConfigError()
	}

	// Create S3 client
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.UsePathStyle = true
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	// Create S3 storage
	storage := NewStorage(client, cfg.Bucket, cfg, m.log)
	m.storages[entry.ID] = storage
	return map[string]any{
		"bucket": cfg.Bucket,
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
func (r *s3Resource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}
