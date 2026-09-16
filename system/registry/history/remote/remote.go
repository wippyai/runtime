package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/registry"
	entryencoding "github.com/wippyai/runtime/api/registry/history/encoding"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	"github.com/wippyai/runtime/internal/version"
	legacy "github.com/wippyai/runtime/system/registry/history/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const MaxMessageBytes = entryencoding.MaxEntryBytes

var ErrCommitUnknown = errors.New("history commit status is unknown; retry the same change")

type Config struct {
	Key             *historyv1.RegistryKey
	ReplicaID       string
	Timeout         time.Duration
	PollInterval    time.Duration
	MaxMessageBytes int
}

type History struct {
	key             *historyv1.RegistryKey
	pending         *historyv1.SubmitRequest
	pendingRestore  *historyv1.RestoreRequest
	pendingReport   *historyv1.AppliedRequest
	legacyDecoder   *legacy.LegacyDecoder
	closer          io.Closer
	client          historyv1.HistoryServiceClient
	replicaID       string
	timeout         time.Duration
	pollInterval    time.Duration
	maxMessageBytes int
	revision        uint64
	appliedRevision uint64
	reported        uint64
	mu              sync.Mutex
	reportMu        sync.Mutex
	legacyMu        sync.Mutex
}

var _ registry.PublishedHistory = (*History)(nil)

func New(connection grpc.ClientConnInterface, cfg Config) (*History, error) {
	if connection == nil || cfg.Key.GetTenantId() == "" || cfg.Key.GetEnvironmentId() == "" || cfg.Key.GetRegistryId() == "" {
		return nil, errors.New("history connection and registry identity are required")
	}
	if cfg.Timeout <= 0 || cfg.PollInterval <= 0 {
		return nil, errors.New("history timeout and poll interval must be positive")
	}
	if cfg.MaxMessageBytes == 0 {
		cfg.MaxMessageBytes = MaxMessageBytes
	}
	if cfg.MaxMessageBytes < 0 {
		return nil, errors.New("history message size must be positive")
	}
	if cfg.ReplicaID == "" {
		id, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("create history replica identity: %w", err)
		}
		cfg.ReplicaID = id.String()
	}
	return &History{client: historyv1.NewHistoryServiceClient(connection), key: proto.Clone(cfg.Key).(*historyv1.RegistryKey), replicaID: cfg.ReplicaID, timeout: cfg.Timeout, pollInterval: cfg.PollInterval, maxMessageBytes: cfg.MaxMessageBytes}, nil
}

func (h *History) SubmitChanges(ctx context.Context, changes registry.ChangeSet, resolution *registry.DependencyResolution) (*registry.HistoryReceipt, error) {
	mutations := make([]*historyv1.Mutation, 0, len(changes))
	positions := make(map[string]int, len(changes))
	for _, op := range changes {
		id := op.Entry.ID.Canonical()
		if id.Name == "" {
			return nil, errors.New("entry ID is required")
		}
		name := id.String()
		mutation := &historyv1.Mutation{EntryId: name}
		switch op.Kind {
		case registry.EntryCreate, registry.EntryUpdate:
			data, err := entryencoding.EncodeEntry(op.Entry)
			if err != nil {
				return nil, err
			}
			mutation.Value = data
		case registry.EntryDelete:
			mutation.Deleted = true
		default:
			return nil, errors.New("unsupported registry operation")
		}
		if index, exists := positions[name]; exists {
			mutations[index] = mutation
		} else {
			positions[name] = len(mutations)
			mutations = append(mutations, mutation)
		}
	}
	var graph []byte
	if resolution != nil {
		if !resolution.Valid() {
			return nil, registry.ErrInvalidDependencyResolution
		}
		var err error
		graph, err = json.Marshal(resolution.Canonical())
		if err != nil {
			return nil, err
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pendingRestore != nil {
		h.reconcilePendingLocked(ctx)
		if h.pendingRestore != nil {
			return nil, fmt.Errorf("%w: %s", ErrCommitUnknown, h.pendingRestore.RequestId)
		}
	}
	req := &historyv1.SubmitRequest{Mutations: mutations, Resolution: graph}
	if h.pending != nil {
		previous := &historyv1.SubmitRequest{Mutations: h.pending.Mutations, Resolution: h.pending.Resolution}
		matches := proto.Equal(previous, req)
		if receipt := h.reconcilePendingLocked(ctx); receipt != nil && matches {
			return convertReceipt(receipt), nil
		}
		if h.pending != nil && !matches {
			return nil, fmt.Errorf("%w: %s", ErrCommitUnknown, h.pending.RequestId)
		}
		if h.pending != nil {
			req = h.pending
		}
	}
	if h.pending == nil {
		expectedRevision := h.revision
		req.Key = h.key
		req.RequestId = uuid.NewString()
		req.ExpectedRevision = &expectedRevision
		if proto.Size(req) > h.maxMessageBytes {
			return nil, errors.New("changes exceed the message size limit")
		}
		h.pending = req
	}
	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	result, err := h.client.Submit(callCtx, req, grpc.MaxCallSendMsgSize(h.maxMessageBytes))
	cancel()
	if err != nil {
		if status.Code(err) == codes.Aborted {
			h.pending = nil
			return nil, fmt.Errorf("%w: %s: %w", registry.ErrHistoryConflict, req.RequestId, err)
		}
		if !retryable(err) {
			h.pending = nil
			return nil, err
		}
		receiptCtx, receiptCancel := context.WithTimeout(context.WithoutCancel(ctx), h.timeout)
		receipt, receiptErr := h.client.GetReceipt(receiptCtx, &historyv1.GetReceiptRequest{Key: h.key, RequestId: req.RequestId})
		receiptCancel()
		if receiptErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrCommitUnknown, req.RequestId, errors.Join(err, receiptErr))
		}
		if receipt == nil {
			return nil, fmt.Errorf("%w: %s: empty receipt", ErrCommitUnknown, req.RequestId)
		}
		h.revision = max(h.revision, receipt.Revision)
		h.pending = nil
		return convertReceipt(receipt), nil
	}
	if result.GetReceipt() == nil {
		return nil, fmt.Errorf("%w: %s: empty receipt", ErrCommitUnknown, req.RequestId)
	}
	h.revision = max(h.revision, result.Receipt.Revision)
	h.pending = nil
	return convertReceipt(result.Receipt), nil
}

func (h *History) AwaitPublished(ctx context.Context, receipt *registry.HistoryReceipt) (*registry.PublishedState, error) {
	if receipt == nil || receipt.RequestID == "" {
		return nil, errors.New("history receipt is required")
	}
	current := *receipt
	delay := h.pollInterval
	for {
		switch current.Status {
		case "published":
			if current.PublishedRevision == 0 {
				return nil, errors.New("published receipt has no version")
			}
			return h.ReadPublished(ctx, current.PublishedRevision)
		case "conflicted":
			return nil, fmt.Errorf("%w: %s: %s", registry.ErrHistoryConflict, current.RequestID, current.Message)
		case "rejected":
			return nil, fmt.Errorf("%w: %s: %s", registry.ErrHistoryRejected, current.RequestID, current.Message)
		case "stored", "superseded":
		default:
			return nil, fmt.Errorf("invalid history receipt status: %s", current.Status)
		}
		if err := wait(ctx, delay); err != nil {
			return nil, fmt.Errorf("wait for history request %s: %w", current.RequestID, err)
		}
		callCtx, cancel := context.WithTimeout(ctx, h.timeout)
		result, err := h.client.GetReceipt(callCtx, &historyv1.GetReceiptRequest{Key: h.key, RequestId: receipt.RequestID})
		cancel()
		if err != nil {
			if !retryable(err) {
				return nil, err
			}
		} else {
			current = *convertReceipt(result)
		}
	}
}

func (h *History) ReadPublished(ctx context.Context, revision uint64) (*registry.PublishedState, error) {
	raw, err := h.readVersion(ctx, revision, false)
	if err != nil {
		return nil, err
	}
	return decodeVersion(raw)
}

func (h *History) RestoreChanges(ctx context.Context, revision uint64) (*registry.HistoryReceipt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending != nil {
		h.reconcilePendingLocked(ctx)
		if h.pending != nil {
			return nil, fmt.Errorf("%w: %s", ErrCommitUnknown, h.pending.RequestId)
		}
	}
	request := h.pendingRestore
	if request != nil {
		matches := request.TargetRevision == revision
		if receipt := h.reconcilePendingLocked(ctx); receipt != nil && matches {
			return convertReceipt(receipt), nil
		}
		request = h.pendingRestore
		if request != nil && !matches {
			return nil, fmt.Errorf("%w: %s", ErrCommitUnknown, request.RequestId)
		}
	}
	if request == nil {
		expectedRevision := h.revision
		request = &historyv1.RestoreRequest{Key: h.key, RequestId: uuid.NewString(), TargetRevision: revision, ExpectedRevision: &expectedRevision}
		h.pendingRestore = request
	}
	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	result, err := h.client.Restore(callCtx, request)
	cancel()
	if err != nil {
		if status.Code(err) == codes.Aborted {
			h.pendingRestore = nil
			return nil, fmt.Errorf("%w: restore request %s: %w", registry.ErrHistoryConflict, request.RequestId, err)
		}
		if !retryable(err) {
			h.pendingRestore = nil
			return nil, err
		}
		receiptCtx, receiptCancel := context.WithTimeout(context.WithoutCancel(ctx), h.timeout)
		receipt, receiptErr := h.client.GetReceipt(receiptCtx, &historyv1.GetReceiptRequest{Key: h.key, RequestId: request.RequestId})
		receiptCancel()
		if receiptErr != nil {
			return nil, fmt.Errorf("%w: restore request %s: %w", ErrCommitUnknown, request.RequestId, errors.Join(err, receiptErr))
		}
		if receipt == nil {
			return nil, fmt.Errorf("%w: restore request %s: empty receipt", ErrCommitUnknown, request.RequestId)
		}
		h.revision = max(h.revision, receipt.Revision)
		h.pendingRestore = nil
		return convertReceipt(receipt), nil
	}
	if result.GetReceipt() == nil {
		return nil, fmt.Errorf("%w: %s: empty restore receipt", ErrCommitUnknown, request.RequestId)
	}
	h.revision = max(h.revision, result.GetReceipt().Revision)
	h.pendingRestore = nil
	return convertReceipt(result.GetReceipt()), nil
}

func (h *History) FollowPublished(ctx context.Context, after uint64, apply func(*registry.PublishedState) error) error {
	if apply == nil {
		return errors.New("publication callback is required")
	}
	delay := backoff.DefaultConfig.BaseDelay
	for {
		if err := h.flushApplied(ctx); err != nil {
			if !retryable(err) {
				return err
			}
			if err := wait(ctx, delay); err != nil {
				return err
			}
			delay = min(time.Duration(float64(delay)*backoff.DefaultConfig.Multiplier), backoff.DefaultConfig.MaxDelay)
			continue
		}
		stream, err := h.client.Watch(ctx, &historyv1.ReadRequest{Key: h.key, AfterRevision: after, Limit: 0}, grpc.MaxCallRecvMsgSize(h.maxMessageBytes))
		if err == nil {
			for {
				next, receiveErr := stream.Recv()
				if receiveErr != nil {
					err = receiveErr
					break
				}
				if next.Revision <= after {
					continue
				}
				published, decodeErr := decodeVersion(next)
				if decodeErr != nil {
					return decodeErr
				}
				if applyErr := apply(published); applyErr != nil {
					return applyErr
				}
				after = next.Revision
				delay = backoff.DefaultConfig.BaseDelay
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !retryable(err) && !errors.Is(err, io.EOF) {
			return err
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
		delay = min(time.Duration(float64(delay)*backoff.DefaultConfig.Multiplier), backoff.DefaultConfig.MaxDelay)
	}
}

func (h *History) ReportApplied(ctx context.Context, published *registry.PublishedState, applicationErr error) error {
	if published == nil || published.Version == nil {
		return errors.New("published version is required")
	}
	revision := uint64(published.Version.ID())
	if applicationErr == nil {
		h.mu.Lock()
		h.revision = max(h.revision, revision)
		h.appliedRevision = max(h.appliedRevision, revision)
		hasPending := h.pending != nil || h.pendingRestore != nil
		h.mu.Unlock()
		if hasPending {
			h.reconcilePending(ctx)
		}
	}
	request := &historyv1.AppliedRequest{Key: h.key, ReplicaId: h.replicaID, Revision: revision}
	if applicationErr != nil {
		request.Error = applicationErr.Error()
	}
	h.reportMu.Lock()
	defer h.reportMu.Unlock()
	if request.Revision < h.reported {
		return h.flushAppliedLocked(ctx)
	}
	if h.pendingReport == nil || request.Revision >= h.pendingReport.Revision {
		h.pendingReport = request
	}
	return h.flushAppliedLocked(ctx)
}

func (h *History) reconcilePending(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reconcilePendingLocked(ctx)
}

func (h *History) reconcilePendingLocked(ctx context.Context) *historyv1.Receipt {
	if h.appliedRevision == 0 {
		return nil
	}
	var requestID string
	if h.pending != nil {
		requestID = h.pending.RequestId
	} else if h.pendingRestore != nil {
		requestID = h.pendingRestore.RequestId
	} else {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	receipt, err := h.client.GetReceipt(callCtx, &historyv1.GetReceiptRequest{Key: h.key, RequestId: requestID})
	cancel()
	if err != nil || receipt == nil || receipt.RequestId != requestID || receipt.Status != "published" || receipt.PublishedRevision == 0 || receipt.PublishedRevision > h.appliedRevision {
		return nil
	}
	h.revision = max(h.revision, receipt.Revision, receipt.PublishedRevision)
	if h.pending != nil && h.pending.RequestId == requestID {
		h.pending = nil
	}
	if h.pendingRestore != nil && h.pendingRestore.RequestId == requestID {
		h.pendingRestore = nil
	}
	return receipt
}

func (h *History) flushApplied(ctx context.Context) error {
	h.reportMu.Lock()
	defer h.reportMu.Unlock()
	return h.flushAppliedLocked(ctx)
}

func (h *History) flushAppliedLocked(ctx context.Context) error {
	if h.pendingReport == nil {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	if _, err := h.client.ReportApplied(callCtx, h.pendingReport); err != nil {
		return err
	}
	h.reported = h.pendingReport.Revision
	h.pendingReport = nil
	return nil
}

func decodeVersion(value *historyv1.Version) (*registry.PublishedState, error) {
	if value == nil || uint64(uint(value.Revision)) != value.Revision {
		return nil, errors.New("invalid published version")
	}
	published := &registry.PublishedState{Version: version.New(uint(value.Revision)), Changes: make(registry.ChangeSet, len(value.Entries))}
	seen := make(map[registry.ID]struct{}, len(value.Entries))
	for i, mutation := range value.Entries {
		if mutation == nil {
			return nil, errors.New("nil published entry")
		}
		id := registry.ParseID(mutation.EntryId).Canonical()
		if id.Name == "" || id.String() != mutation.EntryId {
			return nil, errors.New("invalid published entry ID")
		}
		if _, ok := seen[id]; ok {
			return nil, errors.New("duplicate published entry")
		}
		seen[id] = struct{}{}
		if mutation.Deleted {
			if len(mutation.Value) != 0 {
				return nil, errors.New("deleted entry has a value")
			}
			published.Changes[i] = registry.Operation{Kind: registry.EntryDelete, Entry: registry.Entry{ID: id}}
		} else {
			entry, err := entryencoding.DecodeEntry(mutation.Value)
			if err != nil {
				return nil, err
			}
			if entry.ID != id {
				return nil, errors.New("published entry ID does not match its value")
			}
			published.Changes[i] = registry.Operation{Kind: registry.EntryUpdate, Entry: entry}
		}
	}
	if len(value.Resolution) > 0 {
		if err := json.Unmarshal(value.Resolution, &published.Resolution); err != nil {
			return nil, err
		}
		if !published.Resolution.Valid() {
			return nil, registry.ErrInvalidDependencyResolution
		}
	}
	return published, nil
}

func convertReceipt(value *historyv1.Receipt) *registry.HistoryReceipt {
	if value == nil {
		return nil
	}
	return &registry.HistoryReceipt{RequestID: value.RequestId, Revision: value.Revision, PublishedRevision: value.PublishedRevision, Status: value.Status, Message: value.Message}
}

func retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Unknown, codes.Aborted:
		return true
	default:
		return false
	}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (h *History) Save(registry.Version, registry.ChangeSet, bool) error {
	return registry.ErrHistoryOperationUnsupported
}
func (h *History) SetHead(registry.Version) error { return registry.ErrHistoryOperationUnsupported }
func (h *History) Head() (registry.Version, error) {
	published, err := h.ReadPublished(context.Background(), 0)
	if err != nil {
		return nil, err
	}
	return published.Version, nil
}
func (h *History) MaxVersionID() (uint, error) {
	head, err := h.Head()
	if err != nil {
		return 0, err
	}
	return head.ID(), nil
}
func (h *History) GetDependencyResolution(target registry.Version) (*registry.DependencyResolution, error) {
	if target == nil {
		return nil, errors.New("target version is required")
	}
	raw, err := h.readVersion(context.Background(), uint64(target.ID()), true)
	if err != nil {
		return nil, err
	}
	published, err := decodeVersion(raw)
	if err != nil {
		return nil, err
	}
	if published.Resolution == nil {
		return nil, registry.ErrDependencyResolutionNotFound
	}
	return published.Resolution, nil
}
func (h *History) SaveWithDependencyResolution(registry.Version, registry.ChangeSet, *registry.DependencyResolution, bool) error {
	return registry.ErrHistoryOperationUnsupported
}
func (h *History) CheckpointDependencyResolution(registry.Version, *registry.DependencyResolution) error {
	return registry.ErrHistoryOperationUnsupported
}
