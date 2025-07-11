package history

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/ponyruntime/pony/system/registry/history/authmanager"
	"google.golang.org/protobuf/types/known/structpb"

	"go.uber.org/zap"

	historyv1 "git.spiralscout.com/estimation-engine/cloudsync/proto/gen/wippy/cloudsync/history/v1"
	"git.spiralscout.com/estimation-engine/cloudsync/proto/gen/wippy/cloudsync/history/v1/historyv1connect"
	payloadapi "github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"github.com/ponyruntime/pony/system/payload"
)

const timeout = 5 * time.Second

// AuthManager sets auth to http header.
type AuthManager interface {
	SetAuth(http.Header)
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
	inner          registry.History
	versionService historyv1connect.VersionServiceClient
	appID          string
	log            *zap.Logger
	authManager    AuthManager

	remoteVersionLogs map[uint]*historyv1.VersionLog

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
func NewRemoteStorage(
	inner registry.History,
	versionService historyv1connect.VersionServiceClient,
	appID string,
	log *zap.Logger,
) *RemoteStorage {
	rs := &RemoteStorage{
		inner:             inner,
		versionService:    versionService,
		appID:             appID,
		log:               log,
		authManager:       authmanager.Noop{},
		remoteVersionLogs: make(map[uint]*historyv1.VersionLog),
		queue:             make(chan toSave, 1),
		workerWg:          sync.WaitGroup{},
		shutdownCh:        make(chan struct{}),
		shutdownOnce:      sync.Once{},
		enabled:           atomic.Bool{},
	}

	// Start background worker
	rs.workerWg.Add(1)
	go rs.worker()

	return rs
}

// SetAuthManager sets new auth manager for remote storage.
func (rs *RemoteStorage) SetAuthManager(am AuthManager) {
	rs.authManager = am
}

func (rs *RemoteStorage) LatestSequence(ctx context.Context) (uint64, error) {
	req := connect.NewRequest(&historyv1.ListVersionLogsRequest{ApplicationId: rs.appID})
	rs.authManager.SetAuth(req.Header())
	resp, err := rs.versionService.ListVersionLogs(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("list remote history version logs: %w", err)
	}

	versionLogs := resp.Msg.GetVersionLogs()
	if len(versionLogs) == 0 {
		return 0, nil
	}

	maxSequence := slices.MaxFunc(resp.Msg.GetVersionLogs(), func(a, b *historyv1.VersionLog) int {
		return max(int(a.GetSequence()), int(b.GetSequence())) //nolint:gosec //G101: Potential hardcoded credentials
	})
	return maxSequence.GetSequence(), nil
}

// InitializeFromCloud loads the latest state from cloud
func (rs *RemoteStorage) InitializeFromCloud(ctx context.Context, toVersion int) error {
	request := &historyv1.GetVersionSequenceRequest{
		ApplicationId: rs.appID,
	}

	if toVersion == 0 {
		request.Source = &historyv1.GetVersionSequenceRequest_Head{Head: true}
	} else {
		request.Source = &historyv1.GetVersionSequenceRequest_Sequence{Sequence: uint64(toVersion)} //nolint:gosec //G115 is ok
	}
	req := connect.NewRequest(request)
	rs.authManager.SetAuth(req.Header())

	// Get full history from cloud
	resp, err := rs.versionService.GetVersionSequence(ctx, req)
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			rs.log.Warn("version sequence not found, ignore initialization", zap.Error(err))
			return nil
		}
		return fmt.Errorf("get version sequence: %w", err)
	}

	versions := resp.Msg.GetVersions()

	if len(versions) == 0 {
		// No cloud state exists yet
		return nil
	}

	slices.SortFunc(versions, func(a, b *historyv1.Version) int {
		return cmp.Compare(a.VersionLog.Sequence, b.VersionLog.Sequence)
	})

	innerVersions, err := rs.inner.Versions()
	if err != nil {
		return fmt.Errorf("get inner versions: %w", err)
	}

	lastVersion := innerVersions[len(innerVersions)-1]
	rs.log.Debug("remote history last inner version", zap.Uint("id", lastVersion.ID()))

	// Reconstruct versions in order

	chain := make([]string, 0, len(versions))
	for _, remoteVersion := range versions {
		versionLog := remoteVersion.GetVersionLog()
		seq := versionLog.GetSequence()
		ver := version.FromParent(lastVersion, uint(seq))
		lastVersion = ver
		chain = append(chain, strconv.Itoa(int(ver.ID()))) //nolint:gosec //G115: integer overflow conversion uint -> int

		changeSet, err := rs.convertCloudOperationsToChangeSet(remoteVersion.GetOperations())
		if err != nil {
			return fmt.Errorf("convert cloud operations for version %d: %w", versionLog.GetSequence(), err)
		}

		// Save to inner history
		if err := rs.inner.Save(ver, changeSet, versionLog.GetIsHead()); err != nil {
			return fmt.Errorf("restore version %d to inner history: %w", versionLog.GetSequence(), err)
		}

		rs.remoteVersionLogs[ver.ID()] = versionLog
	}

	rs.log.Info(fmt.Sprintf("history version chain: %s", strings.Join(chain, "->")))

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

	if v.ID() == 0 {
		rs.log.Info("zero version ignored in remote storage")
		return nil
	}

	rs.log.Info("save version to remote", zap.String("version", v.String()), zap.Bool("head", head))

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
	head, err := rs.inner.Head()
	if err != nil {
		return fmt.Errorf("get head: %w", err)
	}

	if head.ID() == v.ID() {
		return nil
	}

	if err := rs.inner.SetHead(v); err != nil {
		return fmt.Errorf("set head for inner history: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &historyv1.SetHeadVersionRequest{
		ApplicationId: rs.appID,
		VersionId:     rs.remoteVersionLogs[v.ID()].GetId(),
	}
	request := connect.NewRequest(req)
	rs.authManager.SetAuth(request.Header())
	_, err = rs.versionService.SetHeadVersion(ctx, request)
	if err != nil {
		return fmt.Errorf("set version %d in remote cloud: %w", v.ID(), err)
	}

	rs.log.Info(fmt.Sprintf("set head for remote history: %d", v.ID()))

	return nil
}

func (rs *RemoteStorage) Sync(enabled bool) {
	if enabled {
		rs.log.Info("enabled sync with remote storage")
	}
	rs.enabled.Store(enabled)
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
		err := rs.retryWithBackoff(100, func() error {
			return rs.sendHistoryVersion(item)
		})
		if err != nil {
			rs.log.Error("send history version to cloud", zap.Error(err))
		}
	}
}

// sendHistoryVersion sends a single item to the cloud
func (rs *RemoteStorage) sendHistoryVersion(item toSave) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Convert to cloud format
	cloudVersion, err := rs.convertToCloudVersion(item)
	if err != nil {
		return fmt.Errorf("convert to cloud version: %w", err)
	}

	req := connect.NewRequest(cloudVersion)
	rs.authManager.SetAuth(req.Header())
	resp, err := rs.versionService.CreateVersion(ctx, req)
	if err != nil {
		return fmt.Errorf("create history version: %w", err)
	}
	rs.log.Info("created new history version in cloud",
		zap.Uint("id", item.version.ID()),
		zap.String("remote_id", resp.Msg.GetVersionLog().GetId()))

	rs.remoteVersionLogs[item.version.ID()] = resp.Msg.GetVersionLog()

	return nil
}

// convertToCloudVersion converts internal types to cloud API format
func (rs *RemoteStorage) convertToCloudVersion(item toSave) (*historyv1.CreateVersionRequest, error) {
	operations := &historyv1.Operations{Operations: make([]*historyv1.Operation, 0, len(item.changeSet))}

	for _, op := range item.changeSet {
		targetFormat := payloadapi.JSON
		transcoded, err := payload.GlobalTranscoder().Transcode(op.Entry.Data, targetFormat)
		if err != nil {
			return nil, fmt.Errorf("transcode operation %s into %s: %w", op.Entry.ID, targetFormat, err)
		}
		data, ok := transcoded.Data().([]byte)
		if !ok {
			return nil, fmt.Errorf("transcoded data is not of type []byte")
		}

		meta, err := structpb.NewStruct(op.Entry.Meta)
		if err != nil {
			return nil, fmt.Errorf("transcode metadata: %w", err)
		}

		operations.Operations = append(operations.Operations, &historyv1.Operation{
			Kind: op.Kind,
			Entry: &historyv1.Operation_Entry{
				Kind:      op.Entry.Kind,
				Name:      op.Entry.ID.Name,
				Namespace: op.Entry.ID.NS,
				Metadata:  meta,
				Payload:   data,
				Format:    string(targetFormat),
			},
		})
	}

	return &historyv1.CreateVersionRequest{
		ApplicationId:    rs.appID,
		Sequence:         uint64(item.version.ID()),
		PreviousSequence: uint64(item.version.Previous().ID()),
		Operations:       operations,
	}, nil
}

// convertCloudOperationsToChangeSet converts cloud operations back to ChangeSet
func (rs *RemoteStorage) convertCloudOperationsToChangeSet(ops []*historyv1.Operation) (registry.ChangeSet, error) {
	changeSet := make(registry.ChangeSet, len(ops))

	for i, op := range ops {
		entry := op.Entry
		pld := payloadapi.NewPayload(entry.GetPayload(), payloadapi.JSON)
		if payloadapi.Format(entry.GetFormat()) != payloadapi.JSON {
			transcoded, err := payload.GlobalTranscoder().Transcode(pld, payloadapi.Format(entry.GetFormat()))
			if err != nil {
				return nil, fmt.Errorf("transcode cloud payload operation %d: %w", i, err)
			}
			pld = transcoded
		}

		changeSet[i] = registry.Operation{
			Kind: op.Kind,
			Entry: registry.Entry{
				ID: registry.ID{
					NS:   entry.GetNamespace(),
					Name: entry.GetName(),
				},
				Kind: entry.GetKind(),
				Meta: entry.GetMetadata().AsMap(),
				Data: pld,
			},
		}
	}

	return changeSet, nil
}

func (rs *RemoteStorage) retryWithBackoff(maxAttempts int, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't sleep after last attempt
		if attempt == maxAttempts-1 {
			break
		}

		// Exponential backoff: 100ms, 200ms, 400ms, 800ms, etc.
		backoff := time.Duration(100*math.Pow(2, float64(attempt))) * time.Millisecond

		rs.log.Warn("retrying after error",
			zap.Error(lastErr),
			zap.Int("attempt", attempt+1),
			zap.Duration("backoff", backoff))

		select {
		case <-rs.shutdownCh:
			return lastErr
		case <-time.After(backoff):
			continue
		}
	}

	return lastErr
}
