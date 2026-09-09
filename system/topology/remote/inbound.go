// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

var (
	ErrInboundClosed   = errors.New("remote topology: monitor receiver closed")
	ErrReceiptCapacity = errors.New("remote topology: monitor receipt capacity reached")
	ErrControlConflict = errors.New("remote topology: monitor request identity conflict")
)

// InstallTarget establishes local observation of target and returns its exact
// release. Inbound shares one installation across grants for the same target.
// It must use a local service observer, not directly register an
// untrusted remote watcher with ordinary topology. Calls and releases must be
// bounded, non-reentrant native operations with no I/O. If an error is returned
// with a non-nil release, Inbound invokes it to clean partial installation.
type InstallTarget func(target pid.PID) (release func(), err error)

// Inbound owns target-side monitor admission and receipt state. It performs no
// network I/O. Apply borrows the incoming package and returns a correlated
// control reply; the outer native receiver owns sending and package release.
// This component does not yet forward target EXITs.
type Inbound struct {
	authority    *Authority
	install      InstallTarget
	entries      map[Token]*inboundGrant
	observations map[pid.PID]*localObservation
	done         chan struct{}
	maxReceipts  int
	receipts     int
	reserved     int
	mu           sync.Mutex
	workers      sync.WaitGroup
	closed       bool
}

type localObservation struct {
	release func()
	claims  int
}

type inboundGrant struct {
	lease           *Lease
	release         func()
	replies         map[string]inboundReceipt
	monitor         Control
	terminal        bool
	releaseReserved bool
}

type inboundReceipt struct {
	request ControlKind
	result  ControlKind
}

func NewInbound(authority *Authority, install InstallTarget, maxReceipts int) (*Inbound, error) {
	if authority == nil || install == nil || maxReceipts <= 0 {
		return nil, ErrInvalidGrantSpec
	}
	return &Inbound{authority: authority, install: install, maxReceipts: maxReceipts, entries: make(map[Token]*inboundGrant), observations: make(map[pid.PID]*localObservation), done: make(chan struct{})}, nil
}

// Apply validates the complete envelope and then the exact owner grant. Identical
// retries replay a stored result; reusing an ID for a different operation fails.
// Capacity is checked before installing or removing a local observer.
func (in *Inbound) Apply(pkg *relay.Package) (Control, error) {
	control, err := DecodeControl(pkg, in.authority.localNode)
	if err != nil {
		return Control{}, err
	}
	if !control.request() {
		return Control{}, ErrInvalidControl
	}
	token, err := ParseToken(control.Grant)
	if err != nil {
		return Control{}, err
	}
	lease, err := in.authority.Acquire(token, pkg.Ingress, control.Watcher, control.Target)
	if err != nil {
		return Control{}, err
	}
	response := control
	err = lease.Use(func() error {
		in.mu.Lock()
		defer in.mu.Unlock()
		if in.closed {
			return ErrInboundClosed
		}
		in.pruneLocked()
		entry := in.entries[token]
		if entry != nil {
			if receipt, ok := entry.replies[control.RequestID]; ok {
				if receipt.request != control.Kind {
					return ErrControlConflict
				}
				response.Kind = receipt.result
				return nil
			}
			// New IDs cannot create additional installation identities or grow
			// receipt retention for a relationship that is already terminal.
			// These results are deterministic and do not need stored receipts.
			if control.Kind == MonitorControl {
				response.Kind = RejectedControl
				return nil
			}
			if entry.terminal {
				response.Kind = ReleasedControl
				return nil
			}
		}
		reserveRelease := entry == nil && control.Kind == MonitorControl
		needed := 1
		if reserveRelease {
			needed++
		}
		if entry != nil && control.Kind == ReleaseControl && entry.releaseReserved {
			needed--
		}
		if in.receipts+in.reserved+needed > in.maxReceipts {
			return ErrReceiptCapacity
		}

		if entry == nil {
			if len(in.entries) >= in.authority.capacity {
				return ErrGrantCapacity
			}
			entry = &inboundGrant{lease: lease, replies: make(map[string]inboundReceipt), releaseReserved: reserveRelease}
			if reserveRelease {
				in.reserved++
			}
			in.entries[token] = entry
			in.workers.Add(1)
			go in.watch(token, entry)
		}
		response.Kind = RejectedControl
		switch control.Kind {
		case MonitorControl:
			if !entry.terminal && entry.release == nil {
				release, installErr := in.observeLocked(control.Target)
				if installErr == nil && release != nil {
					entry.release = release
					entry.monitor = control
					response.Kind = InstalledControl
				} else {
					if release != nil {
						release()
					}
					entry.terminal = true
					in.releaseReservationLocked(entry)
					if errors.Is(installErr, topology.ErrPIDNotRegistered) {
						response.Kind = MissingControl
					}
				}
			}
		case ReleaseControl:
			if entry.release != nil {
				entry.release()
				entry.release = nil
			}
			entry.terminal = true
			in.releaseReservationLocked(entry)
			response.Kind = ReleasedControl
		}
		entry.replies[control.RequestID] = inboundReceipt{request: control.Kind, result: response.Kind}
		in.receipts++
		return nil
	})
	if err != nil {
		return Control{}, err
	}
	return response, nil
}

func (in *Inbound) watch(token Token, entry *inboundGrant) {
	defer in.workers.Done()
	select {
	case <-entry.lease.Done():
		in.mu.Lock()
		in.retireLocked(token, entry)
		in.mu.Unlock()
	case <-in.done:
	}
}

func (in *Inbound) retireLocked(token Token, entry *inboundGrant) {
	if in.entries[token] != entry {
		return
	}
	if entry.release != nil {
		entry.release()
		entry.release = nil
	}
	in.releaseReservationLocked(entry)
	in.receipts -= len(entry.replies)
	delete(in.entries, token)
}

func (in *Inbound) pruneLocked() {
	for token, entry := range in.entries {
		select {
		case <-entry.lease.Done():
			in.retireLocked(token, entry)
		default:
		}
	}
}

// Close fences admission, releases all local observers and joins cleanup
// workers. It does not close the separately owned grant authority.
func (in *Inbound) Close() {
	in.mu.Lock()
	if !in.closed {
		in.closed = true
		close(in.done)
		for token, entry := range in.entries {
			in.retireLocked(token, entry)
		}
	}
	in.mu.Unlock()
	in.workers.Wait()
}

// TargetExit is local verified completion evidence ready for the outer native
// service's bounded outbound queue. Monitor identifies the installed relationship;
// Result is the actual local task result. This is not a wire representation.
type TargetExit struct {
	Result  *runtime.Result
	Monitor Control
}

// TargetExited consumes a local topology observation, not a remote claim. The
// caller must authenticate that local event before invoking this method. New
// monitor IDs cannot resurrect the completed relationship; historical admission
// retries still replay their historical receipt.
func (in *Inbound) TargetExited(target pid.PID, result *runtime.Result) ([]TargetExit, error) {
	if !validActorPID(target) || target.Node != in.authority.localNode || result == nil {
		return nil, ErrInvalidControl
	}
	in.mu.Lock()
	if in.closed {
		in.mu.Unlock()
		return nil, ErrInboundClosed
	}
	candidates := make([]*inboundGrant, 0, len(in.entries))
	for _, entry := range in.entries {
		if entry.release != nil && samePID(entry.monitor.Target, target) {
			candidates = append(candidates, entry)
		}
	}
	in.mu.Unlock()
	exits := make([]TargetExit, 0, len(candidates))
	for _, entry := range candidates {
		// Keep the same grant-then-receiver lock order as Apply.
		_ = entry.lease.Use(func() error {
			in.mu.Lock()
			defer in.mu.Unlock()
			if in.closed || entry.terminal || entry.release == nil {
				return nil
			}
			entry.release()
			entry.release = nil
			entry.terminal = true
			in.releaseReservationLocked(entry)
			exits = append(exits, TargetExit{Monitor: entry.monitor, Result: &runtime.Result{Value: payload.Snapshot(result.Value), Error: result.Error}})
			return nil
		})
	}
	return exits, nil
}

func (in *Inbound) releaseReservationLocked(entry *inboundGrant) {
	if entry.releaseReserved {
		in.reserved--
		entry.releaseReserved = false
	}
}

// observeLocked multiplexes one local topology registration. Every grant owns
// a distinct release claim; only the last claim removes the registration.
func (in *Inbound) observeLocked(target pid.PID) (func(), error) {
	target = canonicalPID(target)
	observation := in.observations[target]
	if observation == nil {
		release, err := in.install(target)
		if err != nil || release == nil {
			return release, err
		}
		observation = &localObservation{release: release}
		in.observations[target] = observation
	}
	observation.claims++
	released := false
	return func() {
		if released {
			return
		}
		released = true
		observation.claims--
		if observation.claims == 0 {
			observation.release()
			delete(in.observations, target)
		}
	}, nil
}
