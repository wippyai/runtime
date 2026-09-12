// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/pid"
)

// ErrMonitorDeliveryIncomplete reports retained obligations at forced shutdown.
// It does not assert process death or durable delivery.
var ErrMonitorDeliveryIncomplete = errors.New("monitor delivery remains incomplete")

var (
	errMonitorOutboxCapacity = errors.New("monitor terminal outbox capacity exhausted")
	errMonitorOutboxConflict = errors.New("monitor terminal outbox reservation conflict")
	errMonitorOutboxOversize = errors.New("monitor terminal notice exceeds reservation")
)

const (
	monitorOutboxMaxRefBytes = 64
)

// monitorOutbox retains the terminal obligations created by monitor admission.
// A reservation charges both record and byte capacity before a target can
// complete. The byte charge remains in place while the notice is filled, so
// completed obligations cannot outrun the admission bound.
//
// The outbox deliberately stores bytes rather than a runtime result. Callers
// must provide an immutable wire representation and choose its allowance at
// admission; this type does not invent a result encoding policy.
type monitorOutbox struct {
	stopped       bool
	stopErr       error
	mu            sync.Mutex
	maximum       int
	maximumBytes  int64
	reservedBytes int64
	records       map[monitorOutboxKey]monitorOutboxRecord
}

type monitorOutboxKey struct {
	targetNode, targetHost, targetID string
	callerNode, callerHost, callerID string
	reference                        string
}

type monitorOutboxRecord struct {
	allowance int64
	charge    int64
	notice    []byte
	filled    bool
}

func newMonitorOutbox(maximum int, maximumBytes int64) (*monitorOutbox, error) {
	if maximum <= 0 || maximumBytes <= 0 {
		return nil, errors.New("monitor terminal outbox bounds must be positive")
	}
	return &monitorOutbox{
		maximum:      maximum,
		maximumBytes: maximumBytes,
		records:      make(map[monitorOutboxKey]monitorOutboxRecord),
	}, nil
}

func monitorOutboxKeyOf(target, caller pid.PID, reference string) monitorOutboxKey {
	return monitorOutboxKey{
		targetNode: target.Node, targetHost: target.Host, targetID: target.UniqID,
		callerNode: caller.Node, callerHost: caller.Host, callerID: caller.UniqID,
		reference: reference,
	}
}

func validMonitorOutboxKey(key monitorOutboxKey) bool {
	return len(key.targetNode) > 0 && len(key.targetHost) > 0 && len(key.targetID) > 0 &&
		len(key.callerNode) > 0 && len(key.callerHost) > 0 &&
		len(key.reference) > 0 && len(key.reference) <= monitorOutboxMaxRefBytes
}

func monitorOutboxKeyBytes(key monitorOutboxKey) (int64, bool) {
	total := int64(0)
	for _, value := range []string{key.targetNode, key.targetHost, key.targetID, key.callerNode, key.callerHost, key.callerID, key.reference} {
		length := int64(len(value))
		if length > math.MaxInt64-total {
			return 0, false
		}
		total += length
	}
	return total, true
}

func cloneMonitorOutboxKey(key monitorOutboxKey) monitorOutboxKey {
	clone := strings.Clone
	return monitorOutboxKey{
		targetNode: clone(key.targetNode), targetHost: clone(key.targetHost), targetID: clone(key.targetID),
		callerNode: clone(key.callerNode), callerHost: clone(key.callerHost), callerID: clone(key.callerID),
		reference: clone(key.reference),
	}
}

// reserve installs one exact terminal obligation. Repeating the same key with
// the same allowance is idempotent. A different allowance cannot reinterpret
// an existing lifetime, and is rejected without changing the reservation.
func (o *monitorOutbox) reserve(target, caller pid.PID, reference string, allowance int64) error {
	if o == nil {
		return errors.New("nil monitor terminal outbox")
	}
	key := monitorOutboxKeyOf(target, caller, reference)
	keyBytes, keySizeOK := monitorOutboxKeyBytes(key)
	if !validMonitorOutboxKey(key) || !keySizeOK || allowance <= 0 {
		return errors.New("invalid monitor terminal outbox reservation")
	}
	if keyBytes > o.maximumBytes || allowance > o.maximumBytes-keyBytes {
		return errMonitorOutboxCapacity
	}
	charge := keyBytes + allowance

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return context.Canceled
	}
	if existing, ok := o.records[key]; ok {
		if existing.allowance != allowance {
			return errMonitorOutboxConflict
		}
		return nil
	}
	if len(o.records) >= o.maximum || charge > o.maximumBytes-o.reservedBytes {
		return errMonitorOutboxCapacity
	}
	o.records[cloneMonitorOutboxKey(key)] = monitorOutboxRecord{allowance: allowance, charge: charge}
	o.reservedBytes += charge
	return nil
}

// replace atomically advances one exact unfilled reservation to a successor
// reference. The predecessor's count and byte charge are credited before the
// successor is checked, so replacement can succeed at a full bound. The
// predecessor must exist and be unfilled; a present successor is always a
// conflict, including when its allowance matches. This strict rule prevents a
// duplicate successor from bypassing predecessor fencing.
func (o *monitorOutbox) replace(target, caller pid.PID, previous, next string, allowance int64) error {
	if o == nil {
		return errors.New("nil monitor terminal outbox")
	}
	oldKey := monitorOutboxKeyOf(target, caller, previous)
	newKey := monitorOutboxKeyOf(target, caller, next)
	newKeyBytes, keySizeOK := monitorOutboxKeyBytes(newKey)
	if previous == next || !validMonitorOutboxKey(oldKey) || !validMonitorOutboxKey(newKey) || !keySizeOK || allowance <= 0 {
		return errors.New("invalid monitor terminal outbox replacement")
	}
	if newKeyBytes > o.maximumBytes || allowance > o.maximumBytes-newKeyBytes {
		return errMonitorOutboxCapacity
	}
	newCharge := newKeyBytes + allowance

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return context.Canceled
	}
	old, ok := o.records[oldKey]
	if !ok || old.filled {
		return errMonitorOutboxConflict
	}
	if _, exists := o.records[newKey]; exists {
		return errMonitorOutboxConflict
	}
	remaining := o.reservedBytes - old.charge
	if remaining < 0 || newCharge > o.maximumBytes-remaining {
		return errMonitorOutboxCapacity
	}
	delete(o.records, oldKey)
	o.records[cloneMonitorOutboxKey(newKey)] = monitorOutboxRecord{allowance: allowance, charge: newCharge}
	o.reservedBytes = remaining + newCharge
	return nil
}

// fill copies a bounded wire notice into an existing reservation. A filled
// notice is immutable: a duplicate with equal bytes is idempotent, while a
// different completion cannot replace the first terminal value.
func (o *monitorOutbox) fill(target, caller pid.PID, reference string, notice []byte) ([]byte, error) {
	if o == nil {
		return nil, errors.New("nil monitor terminal outbox")
	}
	key := monitorOutboxKeyOf(target, caller, reference)
	if !validMonitorOutboxKey(key) {
		return nil, errors.New("invalid monitor terminal outbox key")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return nil, context.Canceled
	}
	record, ok := o.records[key]
	if !ok {
		return nil, errMonitorOutboxConflict
	}
	if int64(len(notice)) > record.allowance {
		return nil, errMonitorOutboxOversize
	}
	if record.filled {
		if !bytes.Equal(record.notice, notice) {
			return nil, errMonitorOutboxConflict
		}
		return cloneMonitorOutboxBytes(record.notice), nil
	}
	record.notice = cloneMonitorOutboxBytes(notice)
	record.filled = true
	o.records[key] = record
	return cloneMonitorOutboxBytes(record.notice), nil
}

// notice returns an owned copy. Callers may mutate the returned bytes while a
// retry is in flight without changing the retained terminal value.
func (o *monitorOutbox) notice(target, caller pid.PID, reference string) ([]byte, bool) {
	if o == nil {
		return nil, false
	}
	key := monitorOutboxKeyOf(target, caller, reference)
	o.mu.Lock()
	defer o.mu.Unlock()
	record, ok := o.records[key]
	if !ok || !record.filled {
		return nil, false
	}
	return cloneMonitorOutboxBytes(record.notice), true
}

// release removes exactly one reservation. Unknown or stale references are
// harmless and cannot affect a replacement relationship.
func (o *monitorOutbox) release(target, caller pid.PID, reference string) bool {
	return o.remove(monitorOutboxKeyOf(target, caller, reference))
}

// ack has the same exact-key semantics as release, but only a filled notice
// can be acknowledged. A premature ACK cannot release an unfulfilled
// terminal obligation and its reserved capacity.
func (o *monitorOutbox) ack(target, caller pid.PID, reference string) bool {
	if o == nil {
		return false
	}
	key := monitorOutboxKeyOf(target, caller, reference)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return false
	}
	record, ok := o.records[key]
	if !ok || !record.filled {
		return false
	}
	delete(o.records, key)
	o.reservedBytes -= record.charge
	return true
}

func (o *monitorOutbox) remove(key monitorOutboxKey) bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return false
	}
	record, ok := o.records[key]
	if !ok {
		return false
	}
	delete(o.records, key)
	o.reservedBytes -= record.charge
	return true
}

func cloneMonitorOutboxBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	copyOf := make([]byte, len(value))
	copy(copyOf, value)
	return copyOf
}

// stop seals mutation admission and preserves unresolved identities/bytes for
// inspection. It never calls an unfinished notice delivered, and repeated stop
// reports the same outcome. Accepted work must drain before this forced stop.
func (o *monitorOutbox) stop() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.stopped {
		o.stopped = true
		if len(o.records) != 0 {
			completed := 0
			for _, record := range o.records {
				if record.filled {
					completed++
				}
			}
			o.stopErr = fmt.Errorf("%w: %d obligations (%d completed notices)", ErrMonitorDeliveryIncomplete, len(o.records), completed)
		}
	}
	return o.stopErr
}
