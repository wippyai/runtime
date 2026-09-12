// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

const retiredParticipantsKey = registryPrefix + "participants_retired"

var ErrParticipantRetired = errors.New("participant requires explicit replacement admission")

// retiredRecord retains one terminal incarnation per stable node, not a history
// per restart. Together with the active inventory it consumes maxParticipants
// identity slots. Slots are deliberately not reclaimed by gossip or a timer.
func (p *participantInventory) retiredRecord() (map[pid.NodeID]string, kvapi.TxnOp, error) {
	entry, err := p.read(retiredParticipantsKey)
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return make(map[pid.NodeID]string), kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: retiredParticipantsKey}, nil
	}
	if err != nil {
		return nil, kvapi.TxnOp{}, err
	}
	if entry.Version == 0 {
		return nil, kvapi.TxnOp{}, errors.New("retired participant record has zero version")
	}
	record, err := decodeParticipantRecord(entry.Value)
	if err != nil {
		return nil, kvapi.TxnOp{}, err
	}
	if len(record) > p.maxParticipants {
		return nil, kvapi.TxnOp{}, ErrParticipantInventorySaturated
	}
	return record, kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: retiredParticipantsKey, Expect: entry.Version}, nil
}

// retire is a committed inventory transition, NOT proof that a node is fenced.
// Only a lifecycle owner that has sealed/joined admission and established weaker
// binding withdrawal may invoke it. No endpoint or boot path exposes it yet.
func (p *participantInventory) retire(ctx context.Context, node pid.NodeID, incarnation string) error {
	return p.transitionParticipant(ctx, node, incarnation, "")
}

// replace admits a fresh incarnation after explicit retirement. Snapshot/enroll
// requests never call this. The lifecycle owner must authorize the replacement;
// knowing the previous incarnation is not itself an authorization credential.
func (p *participantInventory) replace(ctx context.Context, node pid.NodeID, previous, next string) error {
	if err := validateParticipantIdentity(node, next); err != nil {
		return err
	}
	if previous == next {
		return errors.New("replacement requires a fresh incarnation")
	}
	return p.transitionParticipant(ctx, node, previous, next)
}

func (p *participantInventory) transitionParticipant(ctx context.Context, node pid.NodeID, previous, next string) error {
	if ctx == nil {
		return errors.New("participant transition requires a context")
	}
	if err := validateParticipantIdentity(node, previous); err != nil {
		return err
	}
	for attempt := 0; attempt < maxResolveRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		active, version, exists, err := p.readRecord()
		if err != nil {
			return err
		}
		retired, retiredCheck, err := p.retiredRecord()
		if err != nil {
			return err
		}
		if _, ok := active[node]; ok && retired[node] != "" {
			return errors.New("participant is both active and retired")
		}
		if next == "" {
			if active[node] == "" && retired[node] == previous {
				return nil
			}
			if active[node] != previous {
				return fmt.Errorf("%w: retirement owner changed", ErrParticipantIncarnationConflict)
			}
			delete(active, node)
			retired[node] = previous
		} else {
			// A retry after uncertain commit may observe the replacement already live.
			if active[node] == next && retired[node] == "" {
				return nil
			}
			if active[node] != "" || retired[node] != previous {
				return fmt.Errorf("%w: replacement predecessor changed", ErrParticipantIncarnationConflict)
			}
			delete(retired, node)
			active[node] = next
		}
		if len(active)+len(retired) > p.maxParticipants {
			return ErrParticipantInventorySaturated
		}
		activeBody, err := encode(active)
		if err != nil {
			return err
		}
		retiredBody, err := encode(retired)
		if err != nil {
			return err
		}
		check := kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: participantsKey}
		if exists {
			check.Cond = kvapi.CondVersion
			check.Expect = version
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		committed, err := p.engine.Txn([]kvapi.TxnOp{
			check, retiredCheck,
			{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: participantsKey, Value: activeBody},
			{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: retiredParticipantsKey, Value: retiredBody},
		})
		if err != nil || committed {
			return err
		}
		runtime.Gosched()
	}
	return ErrParticipantInventoryBusy
}
