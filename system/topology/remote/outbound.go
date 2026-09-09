// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"bytes"
	"errors"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

var ErrMonitorExpectation = errors.New("remote topology: no matching live monitor expectation")

// DeliverCompletion admits verified completion into a bounded local queue. It
// must not perform I/O, block or reenter Outbound. Error means nothing was
// admitted; a retry may invoke it again. Success transfers the copied value.
type DeliverCompletion func(Completion) error

// Outbound owns watcher-side expectations. The host registers an authorized
// request against the exact protected target connection before sending it.
// It performs no network I/O and never converts observation loss into an EXIT.
// Cancel/Close fence delivery callbacks. Owners cancel on watcher termination;
// expiry/connection loss are also checked on every admission and capacity sweep.
type Outbound struct {
	entries   map[Token]*expectation
	deliver   DeliverCompletion
	localNode pid.NodeID
	capacity  int
	mu        sync.Mutex
	closed    bool
}

type expectation struct {
	expires    time.Time
	pending    *Completion
	connection <-chan struct{}
	request    Control
	refusal    ControlKind
	installed  bool
	terminal   bool
	delivered  bool
	cancelled  bool
}

func NewOutbound(localNode pid.NodeID, capacity int, deliver DeliverCompletion) (*Outbound, error) {
	if !validComponent(localNode) || capacity <= 0 || deliver == nil {
		return nil, ErrInvalidGrantSpec
	}
	return &Outbound{localNode: localNode, capacity: capacity, deliver: deliver, entries: make(map[Token]*expectation)}, nil
}

// Track is a trusted host operation, not a remote-call surface. Enrollment or a
// payload PID alone must never be sufficient to invoke it. The host owns expiry
// and must not extend the target grant's lifetime. Identical retries are harmless.
func (o *Outbound) Track(request Control, ingress relay.IngressIdentity, expires time.Time) error {
	if request.Kind != MonitorControl || !request.valid() || request.Watcher.Node != o.localNode ||
		ingress.Node != request.Target.Node || !ingress.Authenticated || !ingress.IntegrityProtected ||
		connectionClosed(ingress.ConnectionClosed) || !expires.After(time.Now()) {
		return ErrMonitorExpectation
	}
	token, err := ParseToken(request.Grant)
	if err != nil {
		return err
	}
	request.Watcher = canonicalPID(request.Watcher)
	request.Target = canonicalPID(request.Target)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrMonitorExpectation
	}
	o.pruneLocked()
	if connectionClosed(ingress.ConnectionClosed) || !expires.After(time.Now()) {
		return ErrMonitorExpectation
	}
	if previous := o.entries[token]; previous != nil {
		if previous.cancelled {
			return ErrMonitorExpectation
		}
		if sameRequest(previous.request, request) && previous.connection == ingress.ConnectionClosed && previous.expires.Equal(expires) {
			return nil
		}
		return ErrControlConflict
	}
	if len(o.entries) >= o.capacity {
		return ErrGrantCapacity
	}
	o.entries[token] = &expectation{request: request, connection: ingress.ConnectionClosed, expires: expires}
	return nil
}

// Apply validates either a control receipt or completion envelope. Completion
// arriving before Installed is retained once and delivered only after Installed.
// Duplicate completion must be identical; successful delivery happens once.
// Apply borrows pkg on every outcome. The outer relay receiver owns package
// release after successful transport admission; only the copied Completion
// transfers to DeliverCompletion on callback success.
func (o *Outbound) Apply(pkg *relay.Package) error {
	var control Control
	var completion *Completion
	var err error
	if pkg != nil && len(pkg.Messages) == 1 && pkg.Messages[0] != nil && pkg.Messages[0].Topic == ExitTopic {
		var decoded Completion
		decoded, err = DecodeCompletion(pkg, o.localNode)
		if err == nil {
			control = decoded.Monitor
			completion = &decoded
		}
	} else {
		control, err = DecodeControl(pkg, o.localNode)
		if err == nil && control.Kind != InstalledControl && control.Kind != RejectedControl && control.Kind != MissingControl {
			err = ErrInvalidControl
		}
	}
	if err != nil {
		return err
	}
	token, err := ParseToken(control.Grant)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrMonitorExpectation
	}
	o.pruneLocked()
	entry := o.entries[token]
	if entry == nil || entry.cancelled || entry.connection != pkg.Ingress.ConnectionClosed || !sameRequest(entry.request, control) {
		return ErrMonitorExpectation
	}
	if completion != nil {
		if entry.terminal && !entry.delivered {
			return ErrControlConflict
		}
		if entry.pending != nil {
			left, _ := EncodeResult(entry.pending.Result)
			right, _ := EncodeResult(completion.Result)
			if !bytes.Equal(left, right) {
				return ErrControlConflict
			}
		} else {
			entry.pending = completion
		}
	} else {
		switch control.Kind {
		case InstalledControl:
			if entry.terminal && !entry.delivered {
				return ErrControlConflict
			}
			entry.installed = true
		case RejectedControl, MissingControl:
			if entry.installed || entry.pending != nil || (entry.refusal != "" && entry.refusal != control.Kind) {
				return ErrControlConflict
			}
			entry.terminal = true
			entry.refusal = control.Kind
		}
	}
	if entry.installed && entry.pending != nil && !entry.delivered {
		copied := *entry.pending
		copied.Result.Data = bytes.Clone(entry.pending.Result.Data)
		if err = o.deliver(copied); err != nil {
			return err
		}
		entry.delivered = true
		entry.terminal = true
	}
	return nil
}

func sameRequest(a, b Control) bool {
	return a.RequestID == b.RequestID && a.Grant == b.Grant && samePID(a.Watcher, b.Watcher) && samePID(a.Target, b.Target)
}

// Cancel retires an expectation and keeps a tombstone until expiry or connection
// loss. It still counts toward capacity so retries cannot revive a cancelled
// request or discard its replay fence. Reconnect needs a newly authorized grant.
func (o *Outbound) Cancel(token Token) {
	o.mu.Lock()
	if entry := o.entries[token]; entry != nil {
		entry.cancelled = true
		entry.pending = nil
	}
	o.mu.Unlock()
}

func (o *Outbound) Close() {
	o.mu.Lock()
	o.closed = true
	clear(o.entries)
	o.mu.Unlock()
}

func (o *Outbound) pruneLocked() {
	now := time.Now()
	for token, entry := range o.entries {
		if !entry.expires.After(now) || connectionClosed(entry.connection) {
			delete(o.entries, token)
		}
	}
}
