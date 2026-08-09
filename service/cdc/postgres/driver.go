// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	cdcservice "github.com/wippyai/runtime/service/cdc"
	entryutil "github.com/wippyai/runtime/system/entry"
	"go.uber.org/zap"
)

// Driver wires PostgreSQL CDC into the driver-neutral CDC manager. It only
// constructs a source; registry visibility, replacement, and supervisor
// lifecycle remain owned by service/cdc.
type Driver struct{}

func NewDriver() cdcservice.Driver { return Driver{} }

func (Driver) Kind() registry.Kind { return config.Postgres }

func (Driver) Create(ctx context.Context, entry registry.Entry, deps cdcservice.Dependencies) (cdcservice.ManagedSource, error) {
	if deps.Transcoder == nil {
		return nil, ErrTranscoderRequired
	}
	cfg, err := entryutil.DecodeEntryConfig[config.Config](ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, NewInvalidConfigError(err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, NewInvalidConfigError(err)
	}
	standby, _ := cfg.StandbyDuration()
	status, _ := cfg.StatusDuration()
	replDSN, adminDSN, err := buildDSNs(cfg)
	if err != nil {
		return nil, err
	}
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}
	opts := SourceOptions{
		ReplDSN:               replDSN,
		AdminDSN:              adminDSN,
		Name:                  entry.ID.String(),
		Slot:                  cfg.SlotName,
		Publication:           cfg.Publication,
		Tables:                cfg.Tables,
		Temporary:             cfg.Temporary,
		Snapshot:              cfg.Snapshot,
		Streaming:             cfg.Streaming,
		Failover:              cfg.Failover,
		StandbyInterval:       standby,
		StatusInterval:        status,
		SnapshotFetchSize:     cfg.SnapshotFetchSize,
		MaxTransactionChanges: cfg.MaxTransactionChanges,
		MaxTransactionBytes:   cfg.MaxTransactionBytes,
		Log:                   log.With(zap.String("id", entry.ID.String())),
	}
	return &sourceAdapter{
		source:       NewSource(opts),
		opts:         opts,
		lifecycle:    cfg.Lifecycle,
		exclusiveKey: postgresExclusiveKey(cfg),
	}, nil
}

// sourceAdapter preserves the existing PostgreSQL source implementation while
// exposing the common context-aware CDC contract. The old source stream API
// remains private to this adapter, so no driver-specific shape leaks into the
// common manager or dispatcher.
type sourceAdapter struct {
	mu           sync.RWMutex
	source       *Source
	opts         SourceOptions
	lifecycle    supervisor.LifecycleConfig
	exclusiveKey string
}

func (s *sourceAdapter) Info() config.SourceInfo {
	s.mu.RLock()
	source := s.source
	s.mu.RUnlock()
	source.mu.Lock()
	state := source.state
	source.mu.Unlock()

	info := config.SourceInfo{
		Kind:        config.Postgres,
		Name:        source.name,
		Slot:        source.slot,
		Publication: source.publication,
		Tables:      append([]string(nil), source.tables...),
		Streaming:   state == sourceRunning,
		Failover:    source.failover,
		Temporary:   source.temporary,
		Snapshot:    source.snapshot,
		State:       postgresSourceState(state),
		Capabilities: config.Capabilities{
			Snapshot:               source.snapshot,
			Durable:                true,
			Replayable:             true,
			CapturesExternalWrites: true,
			BeforeImages:           false,
		},
	}
	if state == sourceFailed {
		info.Faulted = true
	}
	return info
}

func (s *sourceAdapter) Subscribe(ctx context.Context, opts config.StreamOptions) (config.Stream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.After != "" {
		return nil, config.ErrUnsupported
	}
	if opts.Snapshot {
		return nil, fmt.Errorf("%w: snapshot is configured on the source", config.ErrUnsupported)
	}

	s.mu.RLock()
	source := s.source
	source.mu.Lock()
	state := source.state
	source.mu.Unlock()
	if state != sourceRunning {
		s.mu.RUnlock()
		return nil, config.ErrSourceNotReady
	}
	stream := source.Subscribe(opts)
	s.mu.RUnlock()
	return stream, nil
}

func (s *sourceAdapter) Start(ctx context.Context) (<-chan any, error) {
	s.mu.RLock()
	source := s.source
	opts := s.opts
	s.mu.RUnlock()
	status, err := source.Start(ctx)
	if !errors.Is(err, ErrSourceClosed) {
		return status, err
	}

	// Source deliberately makes a stopped generation terminal so a stale
	// replication connection can never be reused. The stable manager slot can
	// still restart the logical generation by constructing a fresh source with
	// the same immutable configuration and checkpoint identity.
	fresh := NewSource(opts)
	s.mu.Lock()
	if s.source == source {
		s.source = fresh
	}
	source = s.source
	s.mu.Unlock()
	return source.Start(ctx)
}

func (s *sourceAdapter) Stop(ctx context.Context) error {
	s.mu.RLock()
	source := s.source
	s.mu.RUnlock()
	return source.Stop(ctx)
}

// Dispose is used only for a committed dynamic delete. The generic manager
// keeps a non-subscribable tombstone until this completes, so retries can
// finish cleanup. Ordinary Stop/replacement never drops the slot.
func (s *sourceAdapter) Dispose(ctx context.Context) error {
	s.mu.RLock()
	source := s.source
	s.mu.RUnlock()
	source.MarkForSlotDrop()
	stopErr := source.Stop(ctx)
	source.mu.Lock()
	temporary := source.temporary
	source.mu.Unlock()
	if temporary {
		return stopErr
	}
	cleanupErr := source.dropSlotAndCheckpoint(ctx)
	return errors.Join(stopErr, cleanupErr)
}

func (s *sourceAdapter) LifecycleConfig() supervisor.LifecycleConfig {
	return s.lifecycle
}

// PostgreSQL replication slots are exclusive resources. The stable manager
// slot uses this key to perform a stop/start handoff for updates that retain
// the same slot, avoiding a concurrent replication-slot ownership error.
func (s *sourceAdapter) ExclusiveResourceKey() string {
	s.mu.RLock()
	key := s.exclusiveKey
	s.mu.RUnlock()
	return key
}

func postgresExclusiveKey(cfg *config.Config) string {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(cfg.Host), "."))
	endpoint := net.JoinHostPort(host, strconv.Itoa(cfg.Port))
	return "postgres/" + endpoint + "/" + cfg.Database + "/slot/" + cfg.SlotName
}

func postgresSourceState(state sourceState) config.SourceState {
	switch state {
	case sourceStarting:
		return config.SourceStateStarting
	case sourceRunning:
		return config.SourceStateRunning
	case sourceFailed:
		return config.SourceStateFaulted
	case sourceStopped:
		return config.SourceStateStopped
	default:
		return config.SourceStateUnknown
	}
}

var _ cdcservice.ManagedSource = (*sourceAdapter)(nil)
var _ cdcservice.ExclusiveResource = (*sourceAdapter)(nil)
var _ cdcservice.Disposable = (*sourceAdapter)(nil)
