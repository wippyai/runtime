// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

// participantSnapshotSource must capture one immutable authority view. The
// Raft engine's ScanAtIndex barriers on the leader before capturing that view.
// An ordinary Scan or a client-local cache cannot implement this contract.
type participantSnapshotSource interface {
	ScanAtIndex(string, func(kvapi.Entry) bool) (uint64, error)
}

// participantContextSnapshotSource allows native request adapters to bind
// authority waits to feed cancellation. Sources without this capability retain
// synchronous ownership: Stop must still join their capture before completing.
type participantContextSnapshotSource interface {
	ScanAtIndexContext(context.Context, string, func(kvapi.Entry) bool) (uint64, error)
}

type participantSnapshotLimits struct {
	MaxEntries    int
	MaxValueBytes int // sum of retained key and value bytes; wire framing is separate
}

type participantSnapshot struct {
	Revision   uint64
	Lifetime   string
	Unchanged  bool
	Entries    map[string]kvapi.Entry
	exclusions []participantSnapshotExclusion
}

// Only validated Strong records need reconciliation. Consistent records are
// still validated and bounded, but are not decoded a second time by refresh.
type participantSnapshotExclusion struct {
	attemptID   string
	name, owner string
	epoch       uint64
	pending     *pendingHeader // nil denotes an active Strong claim
}

// captureSnapshot obtains membership and all exclusions in the SAME view,
// proving that the entering incarnation was enrolled before its bootstrap
// snapshot. It does not open admission or authorize a wire requester.
func (p *participantInventory) captureSnapshot(ctx context.Context, source participantSnapshotSource, node pid.NodeID, incarnation string, limits participantSnapshotLimits) (*participantSnapshot, error) {
	return p.captureSnapshotSince(ctx, source, node, incarnation, limits, 0)
}

// captureSnapshotSince is for an already-enrolled, authority-lifetime-bound
// request. Only the authority may offer known after validating that binding.
func (p *participantInventory) captureSnapshotSince(ctx context.Context, source participantSnapshotSource, node pid.NodeID, incarnation string, limits participantSnapshotLimits, known uint64) (*participantSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source == nil || limits.MaxEntries <= 0 || limits.MaxValueBytes <= 0 {
		return nil, fmt.Errorf("invalid participant snapshot source or limits")
	}
	if err := validateParticipantIdentity(node, incarnation); err != nil {
		return nil, err
	}
	snapshot := &participantSnapshot{Entries: make(map[string]kvapi.Entry)}
	bytesLeft := limits.MaxValueBytes
	var members map[pid.NodeID]string
	var captureErr error
	scan := source.ScanAtIndex
	if contextual, ok := source.(participantContextSnapshotSource); ok {
		scan = func(prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
			return contextual.ScanAtIndexContext(ctx, prefix, visit)
		}
	}
	unchanged := false
	if conditional, ok := source.(kvapi.ConditionalSnapshotReader); ok && known != 0 {
		scan = func(prefix string, visit func(kvapi.Entry) bool) (uint64, error) {
			revision, equal, err := conditional.ScanAtIndexSince(prefix, known, visit)
			unchanged = equal
			return revision, err
		}
	}
	visited := false
	revision, err := scan(registryPrefix, func(entry kvapi.Entry) bool {
		visited = true
		if captureErr = ctx.Err(); captureErr != nil {
			return false
		}
		relevant := entry.Key == participantsKey || strings.HasPrefix(entry.Key, activePrefix) || strings.HasPrefix(entry.Key, pendingPrefix)
		if !relevant {
			return true
		}
		if len(snapshot.Entries) >= limits.MaxEntries || len(entry.Key) > bytesLeft || len(entry.Value) > bytesLeft-len(entry.Key) {
			captureErr = fmt.Errorf("participant snapshot exceeds configured limits")
			return false
		}
		if entry.Version == 0 {
			captureErr = fmt.Errorf("participant snapshot record %q has zero version", entry.Key)
			return false
		}
		if _, duplicate := snapshot.Entries[entry.Key]; duplicate {
			captureErr = fmt.Errorf("duplicate participant snapshot key")
			return false
		}
		bytesLeft -= len(entry.Key) + len(entry.Value)
		switch {
		case entry.Key == participantsKey:
			members, captureErr = decodeParticipantRecord(entry.Value)
			if captureErr == nil && len(members) > p.maxParticipants {
				captureErr = fmt.Errorf("invalid participant inventory revision or size")
			}
		case strings.HasPrefix(entry.Key, activePrefix):
			var active activeValue
			active, captureErr = decodeActive(entry.Value)
			if captureErr == nil {
				captureErr = validateNamingRecord(entry.Key, activePrefix, active.Name, active.PID)
			}
			if captureErr == nil {
				captureErr = validateRequiredIncarnations(pendingHeader{RequiredNodes: active.RequiredNodes, RequiredIncarnations: active.RequiredIncarnations})
			}
			if captureErr == nil && !active.Strong && active.RequiredIncarnations != nil {
				captureErr = fmt.Errorf("consistent record carries Strong participation")
			}
			if captureErr == nil && active.Strong {
				snapshot.exclusions = append(snapshot.exclusions, participantSnapshotExclusion{attemptID: active.AttemptID, name: active.Name, owner: active.PID, epoch: entry.Epoch})
			}
		case strings.HasPrefix(entry.Key, pendingPrefix):
			var pending pendingHeader
			pending, captureErr = decodePending(entry.Value)
			if captureErr == nil {
				captureErr = validateNamingRecord(entry.Key, pendingPrefix, pending.Name, pending.PID)
			}
			if captureErr == nil {
				snapshot.exclusions = append(snapshot.exclusions, participantSnapshotExclusion{name: pending.Name, owner: pending.PID, epoch: entry.Epoch, pending: &pending})
			}
		}
		if captureErr != nil {
			return false
		}
		snapshot.Entries[entry.Key] = entry
		return true
	})
	if err != nil {
		return nil, err
	}
	if captureErr != nil {
		return nil, captureErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if revision == 0 {
		return nil, fmt.Errorf("participant snapshot has zero authority revision")
	}
	if unchanged {
		if known == 0 || revision != known || visited {
			return nil, fmt.Errorf("invalid unchanged participant snapshot")
		}
		snapshot.Revision, snapshot.Unchanged = revision, true
		snapshot.Entries = nil
		return snapshot, nil
	}
	if members == nil {
		return nil, globalapi.ErrNotAvailable
	}
	registered, present := members[node]
	if !present {
		return nil, globalapi.ErrNotAvailable
	}
	if registered != incarnation {
		return nil, ErrParticipantIncarnationConflict
	}
	for key, entry := range snapshot.Entries {
		// An exclusion cannot originate after the snapshot that claims it.
		// Otherwise even later valid absence may be unable to retire it.
		if entry.Epoch > revision {
			return nil, fmt.Errorf("participant snapshot record %q epoch exceeds authority revision", key)
		}
		if name, ok := strings.CutPrefix(key, pendingPrefix); ok {
			if _, active := snapshot.Entries[activeKey(name)]; active {
				return nil, fmt.Errorf("participant snapshot contains active and pending binding for %q", name)
			}
		}
	}
	snapshot.Revision = revision
	return snapshot, nil
}
