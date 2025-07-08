package history

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	payloadapi "github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"github.com/ponyruntime/pony/system/payload"
	"github.com/wippyai/cloudhistory-poc/history"
)

// CloudHistoryClient defines the interface for remote cloud operations
type CloudHistoryClient interface {
	CreateHistoryVersion(ctx context.Context, id string, version *history.CloudVersion) error
	GetHistory(ctx context.Context, id string) (history.CloudHistory, error)
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

	enabled atomic.Bool
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
func (rs *RemoteStorage) InitializeFromCloud(ctx context.Context, toVersion int) error {
	// Get full history from cloud
	cloudHistory, err := rs.client.GetHistory(ctx, rs.appID)
	if err != nil {
		return fmt.Errorf("get history from cloud: %w", err)
	}

	if len(cloudHistory) == 0 {
		// No cloud state exists yet
		return nil
	}

	innerVersions, err := rs.inner.Versions()
	if err != nil {
		return fmt.Errorf("get inner versions: %w", err)
	}

	lastVersion := innerVersions[len(innerVersions)-1]
	rs.log.Debug("remote history inner version", zap.String("id", lastVersion.ID()))

	if toVersion == 0 {
		toVersion = len(cloudHistory)
	}

	// Reconstruct versions in order
	registryVersions := make([]registry.Version, 0, len(cloudHistory))
	for i := 0; i < toVersion; i++ {
		ver := version.FromParent(lastVersion, strconv.Itoa(int(cloudHistory[i].Version)))
		registryVersions = append(registryVersions, ver)
		lastVersion = ver
	}

	for i := range toVersion {
		changeSet, err := rs.convertCloudOperationsToChangeSet(cloudHistory[i].Operations)
		if err != nil {
			return fmt.Errorf("convert cloud operations for version %d: %w", cloudHistory[i].Version, err)
		}

		// Save to inner history
		if err := rs.inner.Save(registryVersions[i], changeSet, cloudHistory[i].Head); err != nil {
			return fmt.Errorf("restore version %d to inner history: %w", cloudHistory[i].Version, err)
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

	if !rs.enabled.Load() {
		return nil
	}

	if v.ID() == "" {
		rs.log.Info("zero version ignored in remote storage")
		return nil
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

// SetHead sets head version.
func (rs *RemoteStorage) SetHead(v registry.Version) error {
	if err := rs.inner.SetHead(v); err != nil {
		return fmt.Errorf("set head for inner history: %w", err)
	}

	// TODO: rpc call to set head version

	return nil
}

func (rs *RemoteStorage) EnableSync() {
	rs.enabled.Store(true)
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
func (rs *RemoteStorage) convertToCloudVersion(item toSave) (*history.CloudVersion, error) {
	operations := make([]history.VersionOperation, len(item.changeSet))

	for i, op := range item.changeSet {
		targetFormat := payloadapi.JSON
		transcoded, err := payload.GlobalTranscoder().Transcode(op.Entry.Data, targetFormat)
		if err != nil {
			return nil, fmt.Errorf("transcode operation %s into %s: %w", op.Entry.ID, targetFormat, err)
		}
		data, ok := transcoded.Data().([]byte)
		if !ok {
			return nil, fmt.Errorf("transcoded data is not of type []byte")
		}

		operations[i] = history.VersionOperation{
			Kind:          op.Kind,
			EntryKind:     op.Entry.Kind,
			Name:          op.Entry.ID.Name,
			Namespace:     op.Entry.ID.NS,
			Metadata:      op.Entry.Meta,
			Payload:       data,
			PayloadFormat: string(op.Entry.Data.Format()),
		}
	}

	return &history.CloudVersion{
		//Version:    item.version.ID(),
		Head:       item.head,
		Operations: operations,
	}, nil
}

// convertCloudOperationsToChangeSet converts cloud operations back to ChangeSet
func (rs *RemoteStorage) convertCloudOperationsToChangeSet(ops []history.VersionOperation) (registry.ChangeSet, error) {
	changeSet := make(registry.ChangeSet, len(ops))

	for i, op := range ops {
		pld := payloadapi.NewPayload(op.Payload, payloadapi.JSON)
		if payloadapi.Format(op.PayloadFormat) != payloadapi.JSON {
			transcoded, err := payload.GlobalTranscoder().Transcode(pld, payloadapi.Format(op.PayloadFormat))
			if err != nil {
				return nil, fmt.Errorf("transcode cloud payload operation %d: %w", i, err)
			}
			pld = transcoded
		}

		changeSet[i] = registry.Operation{
			Kind: op.Kind,
			Entry: registry.Entry{
				ID: registry.ID{
					NS:   op.Namespace,
					Name: op.Name,
				},
				Kind: op.EntryKind,
				Meta: op.Metadata,
				Data: pld,
			},
		}
	}

	return changeSet, nil
}
