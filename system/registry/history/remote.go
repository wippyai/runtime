package history

import (
	"connectrpc.com/connect"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/wippyai/cloudhistory-poc/history"
	"google.golang.org/protobuf/types/known/structpb"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"git.spiralscout.com/estimation-engine/cloudsync/proto/gen/wippy/cloudsync/history/v1"
	"git.spiralscout.com/estimation-engine/cloudsync/proto/gen/wippy/cloudsync/history/v1/historyv1connect"
	payloadapi "github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
	"github.com/ponyruntime/pony/system/payload"
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
	inner                   registry.History
	applicationTokenService historyv1connect.ApplicationTokenServiceClient
	applicationService      historyv1connect.ApplicationServiceClient
	versionService          historyv1connect.VersionServiceClient
	appID                   string
	log                     *zap.Logger

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
		inner:          inner,
		versionService: versionService,
		appID:          appID,
		log:            log,
		queue:          make(chan toSave, 1),
		workerWg:       sync.WaitGroup{},
		shutdownCh:     make(chan struct{}),
		shutdownOnce:   sync.Once{},
		enabled:        atomic.Bool{},
	}

	// Start background worker
	rs.workerWg.Add(1)
	go rs.worker()

	return rs
}

// InitializeFromCloud loads the latest state from cloud
func (rs *RemoteStorage) InitializeFromCloud(ctx context.Context, toVersion int) error {
	//var application *historyv1.Application
	//{
	//	// todo: move to init
	//	resp1, _ := rs.applicationTokenService.GetTokenApplication(ctx, connect.NewRequest(&historyv1.GetTokenApplicationRequest{}))
	//	applicationId := resp1.Msg.GetApplicationId()
	//
	//	resp, err := rs.applicationService.ListApplications(ctx, connect.NewRequest(&historyv1.ListApplicationsRequest{}))
	//	if err != nil {
	//		return fmt.Errorf("list applications: %w", err)
	//	}
	//
	//	applications := resp.Msg.GetApplications()
	//	for _, app := range applications {
	//		if app.GetId() == applicationId {
	//			application = app
	//		}
	//	}
	//	if application == nil {
	//		return fmt.Errorf("application not found: %s", applicationId)
	//	}
	//
	//	rs.appID = application.Id
	//}

	if toVersion == 0 {
		return nil
	}

	// Get full history from cloud
	resp, err := rs.versionService.GetVersionSequence(ctx, withAuth(connect.NewRequest(&historyv1.GetVersionSequenceRequest{
		ApplicationId: rs.appID,
		Hash:          strconv.Itoa(toVersion),
	})))
	if err != nil {
		return fmt.Errorf("get version sequence: %w", err)
	}

	versions := resp.Msg.GetVersions()

	if len(versions) == 0 {
		// No cloud state exists yet
		return nil
	}

	innerVersions, err := rs.inner.Versions()
	if err != nil {
		return fmt.Errorf("get inner versions: %w", err)
	}

	lastVersion := innerVersions[len(innerVersions)-1]
	rs.log.Debug("remote history last inner version", zap.Uint("id", lastVersion.ID()))

	if toVersion == 0 {
		hash := versions[len(versions)-1].GetVersionLog().GetHash()
		parsed, _ := strconv.ParseUint(hash, 10, 64)
		toVersion = int(parsed)
	}

	// Reconstruct versions in order
	registryVersions := make([]registry.Version, 0, len(versions))
	for i := 0; i < toVersion; i++ {
		hash := versions[len(versions)-1].GetVersionLog().GetHash()
		parsed, _ := strconv.ParseUint(hash, 10, 64)
		ver := version.FromParent(lastVersion, uint(parsed))
		registryVersions = append(registryVersions, ver)
		lastVersion = ver
	}

	for i := range toVersion {
		changeSet, err := rs.convertCloudOperationsToChangeSet(versions[i].GetOperations())
		if err != nil {
			return fmt.Errorf("convert cloud operations for version %s: %w", versions[i].GetVersionLog().GetHash(), err)
		}

		isHead := false
		if i == toVersion-1 {
			isHead = true
		}

		// Save to inner history
		if err := rs.inner.Save(registryVersions[i], changeSet, isHead); err != nil {
			return fmt.Errorf("restore version %s to inner history: %w", versions[i].GetVersionLog().GetHash(), err)
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

	if v.ID() == 0 {
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
//func (rs *RemoteStorage) SetHead(v registry.Version) error {
//	if err := rs.inner.SetHead(v); err != nil {
//		return fmt.Errorf("set head for inner history: %w", err)
//	}
//
//	// TODO: rpc call to set head version
//
//	return nil
//}

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

	if _, err := rs.versionService.CreateVersion(ctx, withAuth(connect.NewRequest(cloudVersion))); err != nil {
		return fmt.Errorf("create history version: %w", err)
	}

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
		ApplicationId: rs.appID,
		Hash:          strconv.FormatUint(uint64(item.version.ID()), 10),
		PreviousHash:  strconv.FormatUint(uint64(item.version.Previous().ID()), 10),
		Operations:    operations,
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

func withAuth[E any](req *connect.Request[E]) *connect.Request[E] {
	req.Header().Set("Authorization", basicAuth("x-ddf7bea9-665f-44d4-87cc-fa64b763cf1e@mail.com", "pass1234"))
	return req
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}
