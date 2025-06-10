package history

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
)

// VersionOperation represents a single operation within a version
type VersionOperation struct {
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Metadata  map[string]any `json:"metadata"`
	Payload   []byte         `json:"payload"`
}

// CloudVersion represents a version to be sent to the cloud
type CloudVersion struct {
	Version    uint               `json:"version"`
	Head       bool               `json:"head"`
	Operations []VersionOperation `json:"operations"`
}

// CloudHistory represents the full history from cloud
type CloudHistory []CloudVersion

// CloudHistoryClient defines the interface for remote cloud operations
type CloudHistoryClient interface {
	CreateHistoryVersion(ctx context.Context, id string, version *CloudVersion) error
	GetHistory(ctx context.Context, id string) (CloudHistory, error)
}

// toSave represents an item to be queued for remote persistence
type toSave struct {
	version   registry.Version
	changeSet registry.ChangeSet
	head      bool
	timestamp time.Time
}

// RemoteStorage provides cloud-backed history storage with local caching
type RemoteStorage struct {
	// Core components
	inner  registry.History
	client CloudHistoryClient
	appID  string
	log    *zap.Logger

	// Channel-based queue
	queue chan toSave

	// Worker management
	workerWg sync.WaitGroup

	// Shutdown management
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}

// NewRemoteStorage creates a new RemoteStorage instance
func NewRemoteStorage(inner registry.History, client CloudHistoryClient, appID string, log *zap.Logger) *RemoteStorage {
	rs := &RemoteStorage{
		inner:        inner,
		client:       client,
		appID:        appID,
		log:          log,
		queue:        make(chan toSave, 1),
		workerWg:     sync.WaitGroup{},
		shutdownCh:   make(chan struct{}),
		shutdownOnce: sync.Once{},
	}

	// Start background worker
	rs.workerWg.Add(1)
	go rs.worker()

	return rs
}

// InitializeFromCloud loads the latest state from cloud
func (rs *RemoteStorage) InitializeFromCloud(ctx context.Context) error {
	// Get full history from cloud
	cloudHistory, err := rs.client.GetHistory(ctx, rs.appID)
	if err != nil {
		return fmt.Errorf("failed to get history from cloud: %w", err)
	}

	if len(cloudHistory) == 0 {
		// No cloud state exists yet
		return nil
	}

	// Reconstruct versions in order
	registryVersions := make([]registry.Version, 0, len(cloudHistory))
	for i := 0; i < len(cloudHistory); i++ {
		if i == 0 {
			ver := version.New(cloudHistory[i].Version)
			registryVersions = append(registryVersions, ver)
			continue
		}
		ver := version.FromParent(registryVersions[i-1], cloudHistory[i].Version)
		registryVersions = append(registryVersions, ver)
	}

	for i, cloudVersion := range cloudHistory {
		changeSet, err := rs.convertCloudOperationsToChangeSet(cloudVersion.Operations)
		if err != nil {
			return fmt.Errorf("convert cloud operations for version %d: %w", cloudVersion.Version, err)
		}

		// Save to inner history
		if err := rs.inner.Save(registryVersions[i], changeSet, cloudVersion.Head); err != nil {
			return fmt.Errorf("restore version %d to inner history: %w", cloudVersion.Version, err)
		}
	}

	return nil
}

// Versions returns all versions from the inner history
func (rs *RemoteStorage) Versions() ([]registry.Version, error) {
	return rs.inner.Versions()
}

// Get retrieves a changeset for a specific version from inner history
func (rs *RemoteStorage) Get(version registry.Version) (registry.ChangeSet, error) {
	return rs.inner.Get(version)
}

// Save persists a version locally and queues it for remote persistence
func (rs *RemoteStorage) Save(v registry.Version, cs registry.ChangeSet, head bool) error {
	// Save to inner history first (local persistence)
	if err := rs.inner.Save(v, cs, head); err != nil {
		return fmt.Errorf("save history: %w", err)
	}

	// Queue for remote persistence
	item := toSave{
		version:   v,
		changeSet: cs,
		head:      head,
		timestamp: time.Now(),
	}

	// Send to queue (blocking if full)
	select {
	case rs.queue <- item:
		// Successfully queued
	case <-rs.shutdownCh:
		return fmt.Errorf("storage is shutting down")
	}

	return nil
}

// Head returns the current head version from inner history
func (rs *RemoteStorage) Head() (registry.Version, error) {
	return rs.inner.Head()
}

// Shutdown gracefully shuts down the remote storage
func (rs *RemoteStorage) Shutdown(ctx context.Context) error {
	rs.shutdownOnce.Do(func() {
		close(rs.shutdownCh)
	})

	// Close the queue to signal no more items will be added
	close(rs.queue)

	// Wait for worker to finish processing remaining items
	done := make(chan struct{})
	go func() {
		rs.workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		rs.log.Info("worker shutdown completed")
	case <-ctx.Done():
		rs.log.Error("shutdown context canceled, worker may not have finished gracefully")
	}

	return nil
}

// worker is the background goroutine that processes the queue
func (rs *RemoteStorage) worker() {
	defer rs.workerWg.Done()

	for item := range rs.queue {
		if err := rs.sendHistoryVersion(item); err != nil {
			rs.log.Error("send history version to cloud", zap.Error(err))
		}
	}
}

// sendHistoryVersion sends a single item to the cloud
func (rs *RemoteStorage) sendHistoryVersion(item toSave) error {
	const timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Convert to cloud format
	cloudVersion, err := rs.convertToCloudVersion(item)
	if err != nil {
		return fmt.Errorf("convert to cloud version: %w", err)
	}

	if err := rs.client.CreateHistoryVersion(ctx, rs.appID, cloudVersion); err != nil {
		return fmt.Errorf("create history version: %w", err)
	}

	return nil
}

// convertToCloudVersion converts internal types to cloud API format
func (rs *RemoteStorage) convertToCloudVersion(item toSave) (*CloudVersion, error) { //nolint:unparam // for unmarshal error
	operations := make([]VersionOperation, len(item.changeSet))

	for i, op := range item.changeSet {
		// Convert Entry to JSON payload
		// todo: understand how to marshal payload
		payload := []byte(op.Entry.Data.Format())

		operations[i] = VersionOperation{
			Kind:      op.Kind,
			Name:      op.Entry.ID.Name,
			Namespace: op.Entry.ID.NS,
			Metadata:  op.Entry.Meta,
			Payload:   payload,
		}
	}

	return &CloudVersion{
		Version:    item.version.ID(),
		Head:       item.head,
		Operations: operations,
	}, nil
}

// convertCloudOperationsToChangeSet converts cloud operations back to ChangeSet
func (rs *RemoteStorage) convertCloudOperationsToChangeSet(ops []VersionOperation) (registry.ChangeSet, error) { //nolint:unparam // for unmarshal error
	changeSet := make(registry.ChangeSet, len(ops))

	for i, op := range ops {
		changeSet[i] = registry.Operation{
			Kind: op.Kind,
			Entry: registry.Entry{
				ID: registry.ID{
					NS:   op.Namespace,
					Name: op.Name,
				},
				Kind: "",
				Meta: op.Metadata,
				//Data: op.Payload,
			},
		}
	}

	return changeSet, nil
}
