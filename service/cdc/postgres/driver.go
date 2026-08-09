// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
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
	if _, err := quoteReplicationSlotName(cfg.SlotName); err != nil {
		return nil, NewInvalidConfigError(err)
	}
	if cfg.Publication != "" {
		if err := validatePostgresIdentifier(cfg.Publication, "publication"); err != nil {
			return nil, NewInvalidConfigError(err)
		}
	} else {
		for _, table := range cfg.Tables {
			if _, err := quoteQualifiedIdent(table); err != nil {
				return nil, NewInvalidConfigError(err)
			}
		}
		if _, err := quotePostgresIdentifier(cfg.SlotName+"_pub", "publication"); err != nil {
			return nil, NewInvalidConfigError(err)
		}
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
	lifecycle    supervisor.LifecycleConfig
	exclusiveKey string
}

func (s *sourceAdapter) Info() config.SourceInfo {
	s.mu.RLock()
	source := s.source
	s.mu.RUnlock()
	source.mu.Lock()
	state := source.state
	sourceErr := source.sourceErr
	source.mu.Unlock()

	info := config.SourceInfo{
		Kind:        config.Postgres,
		Name:        source.name,
		Slot:        source.slot,
		Publication: source.publication,
		Tables:      append([]string(nil), source.tables...),
		// Streaming is a legacy field describing the configured pgoutput
		// protocol mode, not the current lifecycle state. State is exposed by
		// SourceState above.
		Streaming: source.streaming,
		Failover:  source.failover,
		Temporary: source.temporary,
		Snapshot:  source.snapshot,
		State:     postgresSourceState(state),
		Capabilities: config.Capabilities{
			// Snapshot is a source-start bootstrap operation. Subscribe rejects
			// per-consumer snapshot requests, so it is not a common API
			// capability of this adapter.
			Snapshot:               false,
			Durable:                !source.temporary,
			Replayable:             false,
			CapturesExternalWrites: true,
			BeforeImages:           false,
		},
	}
	if state == sourceFailed {
		info.Faulted = true
	}
	if sourceErr != nil {
		info.Error = sourceErr.Error()
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
	s.mu.RUnlock()
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
	return source.Stop(ctx)
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
	return "postgres/" + endpoint + "/slot/" + cfg.SlotName
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
