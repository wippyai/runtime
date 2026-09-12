// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

var (
	errParticipantAuthorityBusy = errors.New("participant authority busy")
	errParticipantPeerMismatch  = errors.New("participant request peer mismatch")
)

// participantAuthority bounds and owns enrollment/snapshot work independently
// of transport queues. The relay adapter must also bound its payloads and replies.
type participantAuthority struct {
	inventory       *participantInventory
	source          participantSnapshotSource
	limits          participantSnapshotLimits
	self            pid.NodeID
	lifetime        string
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	active, maximum int
	stopping        bool
	done            chan struct{}
}

func newParticipantAuthority(ctx context.Context, self pid.NodeID, inventory *participantInventory, source participantSnapshotSource, limits participantSnapshotLimits, maximum int) (*participantAuthority, error) {
	if self == "" || inventory == nil || source == nil || maximum <= 0 || limits.MaxEntries <= 0 || limits.MaxValueBytes <= 0 {
		return nil, fmt.Errorf("invalid participant authority configuration")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owned, cancel := context.WithCancel(ctx)
	return &participantAuthority{lifetime: rand.Text(), inventory: inventory, source: source, limits: limits, self: self, ctx: owned, cancel: cancel, maximum: maximum, done: make(chan struct{})}, nil
}

// snapshotForPackage derives the entering node solely from native immediate-peer
// metadata. Neither a claimed node nor a forwarded origin can substitute for it.
// It borrows pkg; relay ownership/release remains with the calling adapter.
func (a *participantAuthority) snapshotForPackage(ctx context.Context, pkg *relay.Package, incarnation string) (*participantSnapshot, error) {
	return a.snapshotForPackageSince(ctx, pkg, incarnation, "", 0)
}

// snapshotForPackageSince validates identity/enrollment before conditional
// capture. Unknown authority lifetime forces a full response, including after
// restart or leader change. It never treats a caller's revision as evidence.
func (a *participantAuthority) snapshotForPackageSince(ctx context.Context, pkg *relay.Package, incarnation, lifetime string, known uint64) (*participantSnapshot, error) {
	if pkg == nil || pkg.ReceivedFrom == "" || pkg.Source.Node != pkg.ReceivedFrom || pkg.Source.Host != RegistryHostID || pkg.Target.Node != a.self || pkg.Target.Host != RegistryHostID {
		return nil, errParticipantPeerMismatch
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.stopping || a.ctx.Err() != nil {
		a.mu.Unlock()
		return nil, context.Canceled
	}
	if a.active >= a.maximum {
		a.mu.Unlock()
		return nil, errParticipantAuthorityBusy
	}
	a.active++
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.active--
		if a.stopping && a.active == 0 {
			close(a.done)
		}
		a.mu.Unlock()
	}()
	request, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	defer func() { stop(); cancel() }()
	// Redirect a known follower before enrollment. Leadership can still change
	// after this hint; the capture barrier remains the authority check, and an
	// enrollment error can leave the same incarnation committed remotely.
	if source, ok := a.source.(interface{ SnapshotAuthority() (string, error) }); ok {
		peer, err := source.SnapshotAuthority()
		if err != nil {
			return nil, err
		}
		if peer == "" {
			return nil, errParticipantUnavailable
		}
		if peer != a.self {
			return nil, &participantRedirectError{Peer: peer}
		}
	}
	if err := a.inventory.enroll(request, pkg.ReceivedFrom, incarnation); err != nil {
		return nil, err
	}
	if lifetime == "" || lifetime != a.lifetime {
		known = 0
	}
	snapshot, err := a.inventory.captureSnapshotSince(request, a.source, pkg.ReceivedFrom, incarnation, a.limits, known)
	if snapshot != nil {
		snapshot.Lifetime = a.lifetime
	}
	if stopped := a.ctx.Err(); stopped != nil {
		return nil, stopped
	}
	return snapshot, err
}

// stop closes admission and joins all accepted captures. Timeout bounds only
// this wait; call again to join a synchronous source that has not returned yet.
func (a *participantAuthority) stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.stopping {
		a.stopping = true
		a.cancel()
		if a.active == 0 {
			close(a.done)
		}
	}
	a.mu.Unlock()
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
