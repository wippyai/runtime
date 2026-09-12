// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// ParticipantEndpointConfig bounds snapshot work separately from transport
// framing. Every limit is explicit; RefreshInterval is the client's backstop,
// not an expiry lease or failure detector.
type ParticipantEndpointConfig struct {
	Cleanup       CleanupConfig
	MaxEntries    int
	MaxValueBytes int
	MaxWireBytes  int
	// MaxConcurrentRequests bounds captures through reply admission. The same
	// bound separately limits busy replies; saturation retains at most twice
	// this many accepted requests, with no additional pending queue.
	MaxConcurrentRequests int
	MaxRedirects          int
	RequestTimeout        time.Duration
	RefreshInterval       time.Duration
}

// ParticipantEndpoint owns the existing sysreg host's snapshot endpoints and
// naming reconciliation. Construct after ConfigureParticipation, register it as
// RegistryHostID before Start, and Stop before shutting down relay or KV.
// Construction/configuration must be serialized with all service startup.
type ParticipantEndpoint struct {
	mu                 sync.Mutex
	operations         int
	stopping           bool
	started            bool
	topologyRegistered bool
	topologyCleanup    sync.Once
	done               chan struct{}
	ctx                context.Context
	cancel             context.CancelFunc
	service            *Service
	host               *participantHost
	client             *participantClient
	receiver           *participantReceiver
	source             participantSnapshotSource
	config             ParticipantEndpointConfig
	member             bool
}

func NewParticipantEndpoint(ctx context.Context, s *Service, router relay.ContextSender, resolve func(context.Context) (pid.NodeID, error), config ParticipantEndpointConfig) (*ParticipantEndpoint, error) {
	if s == nil || s.strong == nil || s.strong.participants == nil || router == nil || resolve == nil {
		return nil, fmt.Errorf("participant endpoint requires configured service and mesh routing")
	}
	if s.reconciler.Load() != nil || s.ready.Load() {
		return nil, fmt.Errorf("participant endpoint requires unstarted service")
	}
	config.Cleanup.initDefaults()
	if err := config.Cleanup.validate(); err != nil {
		return nil, err
	}
	if config.MaxEntries <= 0 || config.MaxValueBytes <= 0 || config.MaxWireBytes <= 0 || config.MaxConcurrentRequests <= 0 || config.MaxRedirects < 0 || config.RequestTimeout <= 0 || config.RefreshInterval <= 0 {
		return nil, fmt.Errorf("invalid participant endpoint limits")
	}
	ctx, cancel := context.WithCancel(ctx)
	client, err := newParticipantClient(ctx, s.selfNode, s.strong.incarnation, resolve, router, config.MaxWireBytes, config.MaxEntries, config.RequestTimeout)
	if err != nil {
		cancel()
		return nil, err
	}
	client.maxRedirects = config.MaxRedirects
	client.maxParticipants = s.strong.participants.maxParticipants
	p := &ParticipantEndpoint{done: make(chan struct{}), ctx: ctx, cancel: cancel, service: s, client: client, config: config, source: client, member: s.nonMember == nil || !s.nonMember()}
	p.host = &participantHost{registry: s, client: client, cleanup: newCleanupQueue(s, config.Cleanup)}
	if p.member {
		source, ok := s.engine.(participantSnapshotSource)
		if !ok {
			cancel()
			_ = client.stop(context.Background())
			return nil, fmt.Errorf("participant member requires authority snapshot source")
		}
		limits := participantSnapshotLimits{MaxEntries: config.MaxEntries, MaxValueBytes: config.MaxValueBytes}
		authority, err := newParticipantAuthority(ctx, s.selfNode, s.strong.participants, source, limits, config.MaxConcurrentRequests)
		if err != nil {
			cancel()
			_ = client.stop(context.Background())
			return nil, err
		}
		receiver, err := newParticipantReceiver(authority, router, config.MaxWireBytes, config.MaxEntries, config.RequestTimeout)
		if err != nil {
			_ = authority.stop(context.Background())
			cancel()
			_ = client.stop(context.Background())
			return nil, err
		}
		p.receiver = receiver
		p.host.authority = receiver
		p.source = &participantMemberSource{service: s, local: source, remote: client}
		s.strong.recovery = &participantRecovery{interval: config.RefreshInterval, refresh: func(ctx context.Context) error {
			_, err := s.refreshParticipant(ctx, s.strong.participants, p.source, limits)
			return err
		}}
	}
	// Reuse the bounded, authority-validated snapshot and its conditional cache.
	// Cleanup and refresh serialize through the same single-flight source gate.
	s.cleanupSnapshot = func(ctx context.Context) (*participantSnapshot, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		finishPriority := s.prioritizeParticipantCleanup()
		defer finishPriority()
		release, err := s.strong.participantRefresh.LockContext(ctx, "")
		if err != nil {
			return nil, err
		}
		defer release()
		return s.strong.participants.captureSnapshot(ctx, p.source, s.selfNode, s.strong.incarnation, participantSnapshotLimits{MaxEntries: config.MaxEntries, MaxValueBytes: config.MaxValueBytes})
	}
	return p, nil
}

func (p *ParticipantEndpoint) Send(pkg *relay.Package) error { return p.host.Send(pkg) }
func (p *ParticipantEndpoint) SendContext(ctx context.Context, pkg *relay.Package) error {
	return p.host.SendContext(ctx, pkg)
}
func (p *ParticipantEndpoint) Start(ctx context.Context) error { return p.start(ctx, false) }

// StartAfterRetirement authorizes this fresh native endpoint to succeed its own
// committed retired predecessor. The trusted lifecycle owner must have joined
// the prior runtime. It neither fences an active predecessor nor removes that
// runtime's process claims. Ordinary mesh snapshots never invoke replacement.
func (p *ParticipantEndpoint) StartAfterRetirement(ctx context.Context) error {
	return p.start(ctx, true)
}

func (p *ParticipantEndpoint) start(ctx context.Context, recoverRetired bool) (err error) {
	if ctx == nil {
		return fmt.Errorf("participant startup requires a context")
	}

	p.mu.Lock()
	if p.stopping || p.ctx.Err() != nil {
		p.mu.Unlock()
		return context.Canceled
	}
	if p.started {
		p.mu.Unlock()
		return fmt.Errorf("participant endpoint already started; use a fresh endpoint")
	}
	p.started = true
	p.operations++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.operations--
		if p.stopping && p.operations == 0 {
			close(p.done)
		}
		p.mu.Unlock()
	}()
	if err := p.host.cleanup.start(p.ctx); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	context.AfterFunc(runCtx, func() { stop() })
	defer func() {
		if err != nil {
			cancel()
		}
	}()
	ctx = runCtx
	if p.service.topo != nil {
		if err := p.service.topo.Register(p.service.self); err != nil {
			return fmt.Errorf("register naming monitor identity: %w", err)
		}
		p.topologyRegistered = true
	}
	if recoverRetired {
		inventory := p.service.strong.participants
		retired, _, readErr := inventory.retiredRecord()
		if readErr != nil {
			return fmt.Errorf("naming startup requires authority for predecessor recovery: %w", readErr)
		}
		if previous := retired[p.service.selfNode]; previous != "" {
			if err := inventory.replace(ctx, p.service.selfNode, previous, p.service.strong.incarnation); err != nil {
				return err
			}
		}
	}
	limits := participantSnapshotLimits{MaxEntries: p.config.MaxEntries, MaxValueBytes: p.config.MaxValueBytes}
	if p.member {
		return p.service.startParticipantMember(ctx, p.service.strong.participants, p.source, limits)
	}
	return p.service.startParticipantFeed(ctx, p.service.strong.participants, p.source, limits, p.config.RefreshInterval)
}
func (p *ParticipantEndpoint) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.stopping {
		p.stopping = true
		p.cancel()
		if p.operations == 0 {
			close(p.done)
		}
	}
	p.mu.Unlock()
	cleanupErr := p.host.cleanup.stop(ctx)
	// Close source admission first: cancel a blocked bootstrap/capture before
	// joining the feed. Accepted authority work is canceled/joined separately.
	clientErr := p.client.stop(ctx)
	var receiverErr error
	if p.receiver != nil {
		receiverErr = p.receiver.stop(ctx)
	}
	serviceErr := p.service.StopReconciler(ctx)
	stopErr := errors.Join(cleanupErr, clientErr, receiverErr, serviceErr)
	select {
	case <-p.done:
		p.topologyCleanup.Do(func() {
			if p.topologyRegistered {
				p.service.topo.Remove(p.service.self)
			}
		})
		return stopErr
	case <-ctx.Done():
		return errors.Join(stopErr, ctx.Err())
	}
}

// participantMemberSource uses the local leader's barriered view or a direct
// authenticated mesh request. Each capture enrolls the same incarnation;
// leadership observation is only a hint and never replaces the barrier.
type participantMemberSource struct {
	service *Service
	local   participantSnapshotSource
	remote  *participantClient
}

func (s *participantMemberSource) participantIdentity() (pid.NodeID, string) {
	return s.remote.self, s.remote.incarnation
}
func (s *participantMemberSource) ScanAtIndex(prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	return s.ScanAtIndexContext(context.Background(), prefix, visit)
}
func (s *participantMemberSource) ScanAtIndexContext(ctx context.Context, prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
	if s.service.strong.isLeader() {
		if err := s.service.strong.participants.enroll(ctx, s.service.selfNode, s.service.strong.incarnation); err != nil {
			return 0, err
		}
		if source, ok := s.local.(participantContextSnapshotSource); ok {
			return source.ScanAtIndexContext(ctx, prefix, visit)
		}
		return s.local.ScanAtIndex(prefix, visit)
	}
	return s.remote.ScanAtIndexContext(ctx, prefix, visit)
}
