// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/pid"
)

var (
	errRemoteMonitorConflict = errors.New("remote monitor predecessor changed")
	errRemoteMonitorCapacity = errors.New("remote monitor record capacity exhausted")
	errRemoteMonitorClosed   = errors.New("remote monitor target lifetime ended")
)

type remoteMonitorRecord struct {
	reference string
	active    bool
}

// remoteMonitorSet belongs to one exact target process lifetime. Tombstones
// occupy the same bounded slots as active observers: forgetting a release would
// allow a delayed first request to resurrect it. Close is target-lifetime proof,
// never a consequence of gossip suspicion or connection loss.
// Native admission must authenticate callers before invoking these methods.
type remoteMonitorSet struct {
	retention *remoteMonitorRetention
	mu        sync.Mutex
	records   map[pid.PID]remoteMonitorRecord
	maximum   int
	closed    bool
}

func newRemoteMonitorSet(maximum int) (*remoteMonitorSet, error) {
	if maximum <= 0 {
		return nil, errors.New("remote monitor bound must be positive")
	}
	return &remoteMonitorSet{records: make(map[pid.PID]remoteMonitorRecord), maximum: maximum}, nil
}

// establish is a compare-and-replace transition. A caller must name its last
// observed reference to replace it, including after release. Retries of the
// installed active reference are idempotent; a released reference stays released.
func (s *remoteMonitorSet) establish(caller pid.PID, previous, next string) error {
	if next == "" || len(next) > 64 || len(previous) > 64 || previous == next {
		return errors.New("invalid remote monitor transition")
	}
	// PID's cachedString is an optimization, not part of identity/map equality.
	caller = pid.PID{Node: caller.Node, Host: caller.Host, UniqID: caller.UniqID}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errRemoteMonitorClosed
	}
	current, exists := s.records[caller]
	if exists && current.reference == next {
		if current.active {
			return nil
		}
		return errRemoteMonitorConflict
	}
	if (exists && current.reference != previous) || (!exists && previous != "") {
		return errRemoteMonitorConflict
	}
	if !exists && len(s.records) >= s.maximum {
		return errRemoteMonitorCapacity
	}
	if s.retention != nil {
		var err error
		if exists && current.active {
			err = s.retention.outbox.replace(s.retention.target, caller, current.reference, next, s.retention.allowance)
		} else {
			err = s.retention.outbox.reserve(s.retention.target, caller, next, s.retention.allowance)
		}
		if err != nil {
			if errors.Is(err, errMonitorOutboxCapacity) {
				return errRemoteMonitorCapacity
			}
			return err
		}
	}
	s.records[caller] = remoteMonitorRecord{reference: next, active: true}
	return nil
}

// release preserves the exact reference as a tombstone. An old release cannot
// affect a newer observer, and a duplicate release is harmless.
func (s *remoteMonitorSet) release(caller pid.PID, reference string) error {
	if reference == "" || len(reference) > 64 {
		return errors.New("invalid remote monitor release")
	}
	caller = pid.PID{Node: caller.Node, Host: caller.Host, UniqID: caller.UniqID}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errRemoteMonitorClosed
	}
	current, exists := s.records[caller]
	if !exists || current.reference != reference {
		return errRemoteMonitorConflict
	}
	if current.active && s.retention != nil {
		if !s.retention.outbox.release(s.retention.target, caller, reference) {
			return errRemoteMonitorConflict
		}
	}
	current.active = false
	s.records[caller] = current
	return nil
}

// close returns the observers owed target-exit notification and seals admission.
// Repeated closure returns no observers: the lifecycle owner handles delivery.
func (s *remoteMonitorSet) close() []remoteMonitorObserver {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var observers []remoteMonitorObserver
	for caller, record := range s.records {
		if record.active {
			observers = append(observers, remoteMonitorObserver{caller: caller, reference: record.reference})
		}
	}
	s.records = nil
	return observers
}

type remoteMonitorObserver struct {
	caller    pid.PID
	reference string
}
