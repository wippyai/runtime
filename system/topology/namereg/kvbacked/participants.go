// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

const participantsKey = registryPrefix + "participants"

var (
	// ErrParticipantIncarnationConflict means a node already has a different
	// committed incarnation. Retirement and replacement are separate protocol
	// transitions and are not implicit in enrollment.
	ErrParticipantIncarnationConflict = errors.New("participant incarnation conflict")
	// ErrParticipantInventoryBusy means the inventory CAS loop reached its retry
	// bound under contention. No write outcome is inferred from this error.
	ErrParticipantInventoryBusy = errors.New("participant inventory busy")
	// ErrParticipantInventorySaturated means the configured participant bound
	// would be exceeded by enrollment.
	ErrParticipantInventorySaturated = errors.New("participant inventory saturated")
)

// participantInventory is the committed naming-participant inventory. The
// record is deliberately separate from discovery membership: retirement is explicit and never falls back to a discovery view.
type participantInventory struct {
	engine          kvapi.Engine
	read            func(string) (kvapi.Entry, error)
	maxParticipants int
}

// newParticipantInventory constructs an inventory over engine. read is the
// caller's authoritative read function; Service supplies s.get so follower
// reads can use its leader-read surface when available.
func newParticipantInventory(engine kvapi.Engine, read func(string) (kvapi.Entry, error), maxParticipants int) (*participantInventory, error) {
	if engine == nil {
		return nil, errors.New("participant inventory requires a kv engine")
	}
	if read == nil {
		return nil, errors.New("participant inventory requires a read function")
	}
	if maxParticipants <= 0 {
		return nil, errors.New("participant inventory maxParticipants must be positive")
	}
	return &participantInventory{
		engine:          engine,
		read:            read,
		maxParticipants: maxParticipants,
	}, nil
}

// enroll commits node's exact incarnation into the inventory. A repeated
// enrollment of the same pair is idempotent. A different incarnation for an
// existing node is rejected. Retired nodes require explicit replacement admission.
func (p *participantInventory) enroll(ctx context.Context, node pid.NodeID, incarnation string) error {
	if err := validateParticipantIdentity(node, incarnation); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("participant enrollment requires a context")
	}

	for attempt := 0; attempt < maxResolveRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		record, version, exists, err := p.readRecord()
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current, ok := record[node]; ok {
			if current == incarnation {
				return nil
			}
			return fmt.Errorf("%w: node %q already has incarnation %q", ErrParticipantIncarnationConflict, node, current)
		}
		retired, retiredCheck, err := p.retiredRecord()
		if err != nil {
			return err
		}
		if _, ok := retired[node]; ok {
			return ErrParticipantRetired
		}
		if len(record)+len(retired) >= p.maxParticipants {
			return fmt.Errorf("%w: limit %d", ErrParticipantInventorySaturated, p.maxParticipants)
		}

		record[node] = incarnation
		value, err := encode(record)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		ops := []kvapi.TxnOp{
			retiredCheck,
			{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: participantsKey, Value: value},
		}
		if exists {
			ops = append([]kvapi.TxnOp{{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: participantsKey, Expect: version}}, ops...)
		} else {
			ops = append([]kvapi.TxnOp{{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: participantsKey}}, ops...)
		}
		committed, err := p.engine.Txn(ops)
		if err != nil {
			// An error is an uncertain transaction outcome. Do not retry or infer
			// whether the write committed.
			return err
		}
		if committed {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	return ErrParticipantInventoryBusy
}

// readSnapshot returns an independent decoded inventory snapshot and the
// version check that must be included in a reservation transaction. A missing
// or malformed record is an error; callers must never substitute discovery
// membership or an empty map.
func (p *participantInventory) readSnapshot() (map[pid.NodeID]string, kvapi.TxnOp, error) {
	record, version, exists, err := p.readRecord()
	if err != nil {
		return nil, kvapi.TxnOp{}, err
	}
	if !exists {
		return nil, kvapi.TxnOp{}, kvapi.ErrKeyNotFound
	}

	return record, kvapi.TxnOp{
		Kind:   kvapi.TxnCheck,
		Cond:   kvapi.CondVersion,
		Key:    participantsKey,
		Expect: version,
	}, nil
}

func (p *participantInventory) readRecord() (map[pid.NodeID]string, kvapi.Version, bool, error) {
	entry, err := p.read(participantsKey)
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return make(map[pid.NodeID]string), 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if entry.Version == 0 {
		return nil, 0, false, errors.New("participant inventory record has zero version")
	}

	record, err := decodeParticipantRecord(entry.Value)
	if err != nil {
		return nil, 0, false, err
	}
	if len(record) > p.maxParticipants {
		return nil, 0, false, fmt.Errorf("%w: record has %d participants, limit %d", ErrParticipantInventorySaturated, len(record), p.maxParticipants)
	}
	return record, entry.Version, true, nil
}

func decodeParticipantRecord(value []byte) (map[pid.NodeID]string, error) {
	var decoded map[pid.NodeID]string
	if err := decodeInto(value, &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		return nil, errors.New("participant inventory record is nil")
	}
	for node, incarnation := range decoded {
		if err := validateParticipantIdentity(node, incarnation); err != nil {
			return nil, err
		}
	}
	return decoded, nil
}

func validateParticipantIdentity(node pid.NodeID, incarnation string) error {
	if strings.TrimSpace(string(node)) == "" {
		return errors.New("participant node identity must be nonempty")
	}
	if strings.TrimSpace(incarnation) == "" {
		return errors.New("participant incarnation must be nonempty")
	}
	return nil
}
