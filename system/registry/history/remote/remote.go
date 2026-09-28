// SPDX-License-Identifier: MPL-2.0

// Package remote implements registry history on the Wippy History service.
package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	"github.com/wippyai/runtime/internal/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// MaxMessageBytes is the gRPC Go default receive limit.
const MaxMessageBytes = 4 << 20

const (
	listPageSize = 1000
	maxAttempts  = 3
	retryDelay   = 100 * time.Millisecond
)

type Config struct {
	Key             *historyv1.RegistryKey
	Timeout         time.Duration
	MaxMessageBytes int
}

// History is a registry.History backed by the History service. Each method
// returns after the service commits or rejects the operation.
type History struct {
	client          historyv1.HistoryServiceClient
	closer          io.Closer
	key             *historyv1.RegistryKey
	timeout         time.Duration
	maxMessageBytes int
}

var (
	_ registry.ResolutionHeadCASHistory = (*History)(nil)
	_ registry.ChangeSetReplayer        = (*History)(nil)
	_ registry.VersionLookup            = (*History)(nil)
	_ registry.VersionIDBounds          = (*History)(nil)
	_ registry.BaselineHistory          = (*History)(nil)
)

func New(connection grpc.ClientConnInterface, cfg Config) (*History, error) {
	if connection == nil || cfg.Key.GetTenantId() == "" || cfg.Key.GetEnvironmentId() == "" || cfg.Key.GetRegistryId() == "" {
		return nil, errors.New("history connection and registry identity are required")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("history timeout must be positive")
	}
	if cfg.MaxMessageBytes == 0 {
		cfg.MaxMessageBytes = MaxMessageBytes
	}
	if cfg.MaxMessageBytes < 0 {
		return nil, errors.New("history message size must be positive")
	}
	return &History{
		client:          historyv1.NewHistoryServiceClient(connection),
		key:             proto.Clone(cfg.Key).(*historyv1.RegistryKey),
		timeout:         cfg.Timeout,
		maxMessageBytes: cfg.MaxMessageBytes,
	}, nil
}

// Key returns the remote registry identity.
func (h *History) Key() *historyv1.RegistryKey {
	return proto.Clone(h.key).(*historyv1.RegistryKey)
}

func (h *History) Versions() ([]registry.Version, error) {
	versions := make(map[uint]registry.Version)
	list := make([]registry.Version, 0)
	request := &historyv1.ListVersionsRequest{Key: h.key, Limit: listPageSize}
	for {
		page, err := call(h, false, func(ctx context.Context, _ bool) (*historyv1.VersionList, error) {
			return h.client.ListVersions(ctx, request)
		})
		if err != nil {
			return nil, fmt.Errorf("list history versions: %w", err)
		}
		for _, node := range page.GetVersions() {
			id := uint(node.GetId())
			var stored registry.Version
			if node.ParentId == nil {
				if id != registry.RootVersion {
					return nil, fmt.Errorf("history version %d has no parent", id)
				}
				stored = version.New(id)
			} else {
				parent, ok := versions[uint(node.GetParentId())]
				if !ok {
					return nil, fmt.Errorf("history version %d references missing parent %d", id, node.GetParentId())
				}
				stored = version.FromParent(parent, id)
			}
			versions[id] = stored
			list = append(list, stored)
			last := node.GetId()
			request.AfterId = &last
		}
		if !page.GetHasMore() {
			return list, nil
		}
	}
}

func (h *History) MaxVersionID() (uint, error) {
	response, err := call(h, false, func(ctx context.Context, _ bool) (*historyv1.MaxVersionID, error) {
		return h.client.GetMaxVersionID(ctx, &historyv1.RegistryRequest{Key: h.key})
	})
	if err != nil {
		return 0, fmt.Errorf("read maximum history version: %w", err)
	}
	return uint(response.GetVersionId()), nil
}

func (h *History) GetVersion(id uint) (registry.Version, error) {
	lineage, err := call(h, false, func(ctx context.Context, _ bool) (*historyv1.Lineage, error) {
		return h.client.GetVersion(ctx, &historyv1.VersionRequest{Key: h.key, VersionId: uint64(id)})
	})
	if err != nil {
		return nil, versionError(id, err)
	}
	return fromLineage(id, lineage)
}

func (h *History) Head() (registry.Version, error) {
	lineage, err := call(h, false, func(ctx context.Context, _ bool) (*historyv1.Lineage, error) {
		return h.client.Head(ctx, &historyv1.RegistryRequest{Key: h.key})
	})
	if err != nil {
		return nil, fmt.Errorf("read history head: %w", err)
	}
	ids := lineage.GetIds()
	var id uint
	if len(ids) > 0 {
		id = uint(ids[len(ids)-1])
	}
	return fromLineage(id, lineage)
}

func (h *History) Get(v registry.Version) (registry.ChangeSet, error) {
	changes, err := call(h, false, func(ctx context.Context, _ bool) (*historyv1.ChangeSet, error) {
		return h.client.GetChangeSet(ctx, &historyv1.VersionRequest{Key: h.key, VersionId: uint64(v.ID())})
	})
	if err != nil {
		return nil, versionError(v.ID(), err)
	}
	decoded, err := decodeChangeSet(changes.GetData())
	if err != nil {
		return nil, fmt.Errorf("decode history version %d: %w", v.ID(), err)
	}
	return decoded, nil
}

// ReplayChanges reads the whole lineage before it calls apply, so apply can
// call back into the history.
func (h *History) ReplayChanges(ctx context.Context, target registry.Version, apply func(registry.ChangeSet) error) error {
	var encoded [][]byte
	_, err := call(h, false, func(callCtx context.Context, _ bool) (struct{}, error) {
		encoded = encoded[:0]
		stream, err := h.client.ReplayChanges(mergeContext(callCtx, ctx), &historyv1.VersionRequest{Key: h.key, VersionId: uint64(target.ID())})
		if err != nil {
			return struct{}{}, err
		}
		for {
			changes, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return struct{}{}, nil
			}
			if err != nil {
				return struct{}{}, err
			}
			encoded = append(encoded, changes.GetData())
		}
	})
	if err != nil {
		return versionError(target.ID(), err)
	}
	for _, data := range encoded {
		if err := ctx.Err(); err != nil {
			return err
		}
		changes, err := decodeChangeSet(data)
		if err != nil {
			return fmt.Errorf("decode history lineage of version %d: %w", target.ID(), err)
		}
		if err := apply(changes); err != nil {
			return err
		}
	}
	return nil
}

func (h *History) Save(v registry.Version, changes registry.ChangeSet, head bool) error {
	return h.SaveWithDependencyResolution(v, changes, nil, head)
}

func (h *History) SaveWithDependencyResolution(v registry.Version, changes registry.ChangeSet, resolution *registry.DependencyResolution, head bool) error {
	if v.ID() == registry.RootVersion {
		if len(changes) == 0 && resolution == nil {
			return nil
		}
		return fmt.Errorf("version %d already exists", v.ID())
	}
	if v.Previous() == nil {
		return fmt.Errorf("non-root version %d has no parent", v.ID())
	}
	graph, err := encodeResolution(resolution)
	if err != nil {
		return err
	}
	data, err := encodeChangeSet(changes)
	if err != nil {
		return fmt.Errorf("encode history version %d: %w", v.ID(), err)
	}
	request := &historyv1.SaveRequest{Key: h.key, VersionId: uint64(v.ID()), ParentId: uint64(v.Previous().ID()), Changeset: data, Resolution: graph, Head: head}
	_, err = call(h, true, func(ctx context.Context, retry bool) (*historyv1.Empty, error) {
		request.Retry = retry
		return h.client.Save(ctx, request)
	})
	if err != nil {
		return fmt.Errorf("save history version %d: %w", v.ID(), err)
	}
	return nil
}

func (h *History) SetHead(v registry.Version) error {
	_, err := call(h, true, func(ctx context.Context, _ bool) (*historyv1.Empty, error) {
		return h.client.SetHead(ctx, &historyv1.SetHeadRequest{Key: h.key, VersionId: uint64(v.ID())})
	})
	if err != nil {
		return versionError(v.ID(), err)
	}
	return nil
}

func (h *History) CompareAndSetHead(expected, target registry.Version) error {
	return h.compareAndSetHead(expected, target, nil)
}

func (h *History) CompareAndSetHeadWithDependencyResolution(expected, target registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	graph, err := encodeResolution(resolution)
	if err != nil {
		return err
	}
	return h.compareAndSetHead(expected, target, graph)
}

func (h *History) compareAndSetHead(expected, target registry.Version, graph []byte) error {
	request := &historyv1.CompareAndSetHeadRequest{Key: h.key, ExpectedId: uint64(expected.ID()), TargetId: uint64(target.ID()), Resolution: graph}
	_, err := call(h, true, func(ctx context.Context, retry bool) (*historyv1.Empty, error) {
		request.Retry = retry
		return h.client.CompareAndSetHead(ctx, request)
	})
	if err != nil {
		return versionError(target.ID(), err)
	}
	return nil
}

func (h *History) GetDependencyResolution(v registry.Version) (*registry.DependencyResolution, error) {
	response, err := call(h, false, func(ctx context.Context, _ bool) (*historyv1.Resolution, error) {
		return h.client.GetResolution(ctx, &historyv1.VersionRequest{Key: h.key, VersionId: uint64(v.ID())})
	})
	if status.Code(err) == codes.NotFound {
		return nil, registry.ErrDependencyResolutionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read dependency resolution of version %d: %w", v.ID(), err)
	}
	return decodeResolution(response.GetData())
}

func (h *History) CheckpointDependencyResolution(v registry.Version, resolution *registry.DependencyResolution) error {
	if resolution == nil {
		return registry.ErrDependencyResolutionNotFound
	}
	graph, err := encodeResolution(resolution)
	if err != nil {
		return err
	}
	_, err = call(h, true, func(ctx context.Context, _ bool) (*historyv1.Empty, error) {
		return h.client.CheckpointResolution(ctx, &historyv1.CheckpointResolutionRequest{Key: h.key, VersionId: uint64(v.ID()), Resolution: graph})
	})
	if err != nil {
		return versionError(v.ID(), err)
	}
	return nil
}

func (h *History) Baseline() (registry.State, error) {
	baseline, err := h.baseline()
	if err != nil {
		return nil, err
	}
	state, err := decodeState(baseline.GetState())
	if err != nil {
		return nil, fmt.Errorf("decode history baseline: %w", err)
	}
	return state, nil
}

// SaveBaseline replaces the stored baseline when its digest changed.
func (h *History) SaveBaseline(state registry.State) error {
	data, sum, err := encodeState(state)
	if err != nil {
		return fmt.Errorf("encode history baseline: %w", err)
	}
	var expected string
	current, err := h.baseline()
	switch {
	case err == nil:
		expected = current.GetDigest()
	case !errors.Is(err, registry.ErrBaselineNotFound):
		return err
	}
	if expected == sum {
		return nil
	}
	_, err = call(h, true, func(ctx context.Context, _ bool) (*historyv1.Empty, error) {
		return h.client.SetBaseline(ctx, &historyv1.SetBaselineRequest{Key: h.key, ExpectedDigest: expected, Baseline: &historyv1.Baseline{State: data, Digest: sum}})
	})
	if err != nil {
		return fmt.Errorf("save history baseline: %w", err)
	}
	return nil
}

func (h *History) baseline() (*historyv1.Baseline, error) {
	baseline, err := call(h, false, func(ctx context.Context, _ bool) (*historyv1.Baseline, error) {
		return h.client.GetBaseline(ctx, &historyv1.RegistryRequest{Key: h.key})
	})
	if status.Code(err) == codes.NotFound {
		return nil, registry.ErrBaselineNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read history baseline: %w", err)
	}
	return baseline, nil
}

func (h *History) Close() error {
	if h.closer != nil {
		return h.closer.Close()
	}
	return nil
}

// call runs one request with the configured timeout. It resends the request
// when the result is unknown. retry tells the request that it is a resend.
func call[T any](h *History, write bool, run func(context.Context, bool) (T, error)) (T, error) {
	var result T
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay << (attempt - 1))
		}
		ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
		result, err = run(ctx, attempt > 0)
		cancel()
		if !unknownResult(err, write) {
			return result, err
		}
	}
	return result, err
}

func unknownResult(err error, write bool) bool {
	switch status.Code(err) {
	case codes.Unavailable:
		return true
	case codes.DeadlineExceeded, codes.Unknown:
		return write
	}
	return false
}

func mergeContext(callCtx, parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(callCtx)
	stop := context.AfterFunc(parent, cancel)
	context.AfterFunc(ctx, func() { stop(); cancel() })
	return ctx
}

func fromLineage(id uint, lineage *historyv1.Lineage) (registry.Version, error) {
	current := version.New(registry.RootVersion)
	for _, next := range lineage.GetIds() {
		if uint(next) <= current.ID() {
			return nil, fmt.Errorf("history version %d has an invalid lineage", id)
		}
		current = version.FromParent(current, uint(next))
	}
	if current.ID() != id {
		return nil, fmt.Errorf("history version %d has an invalid lineage", id)
	}
	return current, nil
}

func versionError(id uint, err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("version %d not found: %s", id, status.Convert(err).Message())
	case codes.Aborted, codes.FailedPrecondition, codes.AlreadyExists, codes.InvalidArgument:
		return errors.New(status.Convert(err).Message())
	}
	return err
}
