// SPDX-License-Identifier: MPL-2.0

package kvraft

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/kv"
	"github.com/wippyai/runtime/api/metrics"
	raftapi "github.com/wippyai/runtime/api/raft"
	"go.uber.org/zap"
)

// defaultApplyTimeout caps a single Raft Apply round-trip. Configurable via
// Config.ApplyTimeout.
const defaultApplyTimeout = 10 * time.Second

// reapInterval is how often the leader proposes a CmdReapTTL Apply.
const reapInterval = 5 * time.Second

// Config configures a Service.
type Config struct {
	// Raft is the Raft service this kvraft replicates against. Caller owns
	// the raft Node lifecycle.
	Raft raftapi.Service

	// FSM is the kvraft FSM that the Raft node was constructed against.
	// (kvraft.NewFSM should be passed to raft.NewNode by the boot wiring.)
	FSM *FSM

	MetricsCollector metrics.Collector
	Logger           *zap.Logger

	// ApplyTimeout caps a single Apply round-trip. Default 10s.
	ApplyTimeout time.Duration
}

// Service hosts kv.KV spaces backed by a single Raft replication group.
// All spaces share one FSM/raft instance; the namespace is encoded in the
// command.
type Service struct {
	logger    *zap.Logger
	tel       *telemetry
	hub       map[string]*watchHub // per-space watch hubs
	stopCh    chan struct{}
	cfg       Config
	hubMu     sync.RWMutex
	closeOnce sync.Once
	stopped   atomic.Bool
}

// NewService constructs a kvraft service. The caller is responsible for
// wiring cfg.Raft to a Raft node constructed against cfg.FSM.
func NewService(cfg Config) (*Service, error) {
	if cfg.Raft == nil {
		return nil, errors.New("kvraft: Raft service required")
	}
	if cfg.FSM == nil {
		return nil, errors.New("kvraft: FSM required")
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.ApplyTimeout <= 0 {
		cfg.ApplyTimeout = defaultApplyTimeout
	}
	s := &Service{
		cfg:    cfg,
		logger: cfg.Logger.Named("kvraft"),
		tel:    newTelemetry(cfg.MetricsCollector),
		hub:    make(map[string]*watchHub),
		stopCh: make(chan struct{}),
	}
	cfg.FSM.SetApplyCallback(s.onApply)
	return s, nil
}

// Start launches the periodic TTL reaper. Apply traffic is independent of
// Start (raft Node is started separately).
func (s *Service) Start(_ context.Context) error {
	go s.reapLoop()
	s.logger.Info("kvraft started",
		zap.Duration("apply_timeout", s.cfg.ApplyTimeout),
		zap.Duration("reap_interval", reapInterval))
	return nil
}

// Stop releases resources. Subsequent ops on returned KV handles return
// ErrSpaceClosed.
func (s *Service) Stop() error {
	s.closeOnce.Do(func() {
		s.stopped.Store(true)
		close(s.stopCh)
		s.hubMu.Lock()
		for _, h := range s.hub {
			h.Close()
		}
		s.hubMu.Unlock()
	})
	return nil
}

// Open returns the KV handle for `name`, creating a watch hub on demand.
// Callers receive the same KV instance for repeated calls.
func (s *Service) Open(name string) (kv.KV, error) {
	if s.stopped.Load() {
		return nil, kv.ErrSpaceClosed
	}
	if name == "" {
		return nil, errors.New("kvraft: space name cannot be empty")
	}
	return &spaceKV{svc: s, name: name}, nil
}

// reapLoop fires CmdReapTTL on the leader every reapInterval. Followers and
// non-leaders Apply the same command via raft replication, so all nodes
// converge on the same reaped set.
func (s *Service) reapLoop() {
	t := time.NewTicker(reapInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			if !s.cfg.Raft.IsLeader() {
				continue
			}
			cmd := &Command{Type: CmdReapTTL}
			data, err := EncodeCommand(cmd)
			if err != nil {
				continue
			}
			resp, err := s.cfg.Raft.Apply(data, s.cfg.ApplyTimeout)
			if err != nil {
				continue
			}
			if r, ok := resp.Response.(*Result); ok && r.Removed > 0 {
				s.tel.recordReap(r.Removed)
			}
		}
	}
}

// onApply is the FSM-callback path: every successful state mutation routes
// through here for watch fan-out.
func (s *Service) onApply(op kv.Op, space, key string, value []byte, version uint64) {
	hub := s.lookupHub(space, false)
	if hub == nil {
		return
	}
	hub.Publish(kv.Event{
		Op:  op,
		Key: key,
		Value: kv.Value{
			Data:    value,
			Version: version,
		},
	})
}

// lookupHub gets the watch hub for `space`. If `create` is true, allocates
// one on absence.
func (s *Service) lookupHub(space string, create bool) *watchHub {
	s.hubMu.RLock()
	h, ok := s.hub[space]
	s.hubMu.RUnlock()
	if ok || !create {
		return h
	}
	s.hubMu.Lock()
	defer s.hubMu.Unlock()
	if h, ok := s.hub[space]; ok {
		return h
	}
	h = newWatchHub(space, s.cfg.MetricsCollector)
	s.hub[space] = h
	return h
}

// applyCommand encodes and applies via Raft. Returns the FSM Result or err.
func (s *Service) applyCommand(cmd *Command) (*Result, error) {
	data, err := EncodeCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("kvraft: encode command: %w", err)
	}
	resp, err := s.cfg.Raft.Apply(data, s.cfg.ApplyTimeout)
	if err != nil {
		return nil, err
	}
	r, ok := resp.Response.(*Result)
	if !ok {
		return nil, fmt.Errorf("kvraft: unexpected apply response type: %T", resp.Response)
	}
	return r, r.Err
}

// readBarrier ensures linearizable reads by waiting for the local FSM to
// catch up to the leader's committed index. Skipped when this node is the
// leader (already up-to-date).
func (s *Service) readBarrier() error {
	if s.cfg.Raft.IsLeader() {
		return nil
	}
	return s.cfg.Raft.Barrier(s.cfg.ApplyTimeout)
}

// Provider returns a kv.ProviderRegistry that exposes only the raft side.
func (s *Service) Provider() kv.ProviderRegistry { return &providerAdapter{svc: s} }

type providerAdapter struct{ svc *Service }

func (p *providerAdapter) OpenRaft(name string) (kv.KV, error) {
	return p.svc.Open(name)
}

func (p *providerAdapter) OpenEventual(_ string) (kv.KV, error) {
	return nil, errors.New("kvraft: eventual mode not provided by this service")
}
