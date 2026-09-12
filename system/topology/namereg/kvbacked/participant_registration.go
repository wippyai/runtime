// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// createPending is the reservation transaction used by Strong registration.
// The inventory path is configured before startup as part of the coordinated
// enrollment cutover. KV boot configures this inventory for members and clients.
func (st *strongState) createPending(ctx context.Context, header pendingHeader) (bool, error) {
	if header.AttemptID == "" {
		header.AttemptID = rand.Text()
	}
	for attempt := 0; attempt < maxResolveRetries; attempt++ {
		var ops []kvapi.TxnOp
		var err error
		if st.participants == nil {
			header.RequiredNodes = st.requiredNodes()
			body, encodeErr := encode(header)
			if encodeErr != nil {
				return false, encodeErr
			}
			ops = []kvapi.TxnOp{
				{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: activeKey(header.Name)},
				{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey(header.Name), Value: body},
			}
		} else {
			ops, err = st.participants.reservationOps(ctx, header)
			if err != nil {
				return false, err
			}
		}
		// Derive the budget from the exact header committed by inventory,
		// including its required participants, never from a stale caller copy.
		var admitted pendingHeader
		found := false
		for _, op := range ops {
			if op.Kind == kvapi.TxnPut && op.Key == pendingKey(header.Name) {
				admitted, err = decodePending(op.Value)
				if err != nil {
					return false, err
				}
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Errorf("missing Strong pending admission")
		}
		capacity, initial, err := planStrongResultCapacity(admitted, st.resultPolicy.RecordBytes)
		if err != nil {
			return st.resultAdmissionFailure(ctx, header.Name, err)
		}
		resultOps, err := reserveStrongResultOps(func(key string) (kvapi.Entry, error) { return st.svc.getContext(ctx, key) }, strongResultLimits{Entries: st.resultPolicy.Entries, Bytes: st.resultPolicy.Bytes}, admitted.AttemptID, capacity, initial)
		if err != nil {
			return st.resultAdmissionFailure(ctx, header.Name, err)
		}
		ops = append(ops, resultOps...)
		if err := ctx.Err(); err != nil {
			return false, err
		}
		committed, err := st.svc.engine.Txn(ops)
		if err != nil || committed {
			return committed, err
		}
		// A rejected transaction wrote nothing. An extant claim is a conflict;
		// otherwise refresh inventory and quota before retrying concurrent writes.
		for _, key := range []string{activeKey(header.Name), pendingKey(header.Name)} {
			if _, err := st.svc.get(key); err == nil {
				return false, nil
			} else if !errors.Is(err, kvapi.ErrKeyNotFound) {
				return false, err
			}
		}
	}
	return false, ErrParticipantInventoryBusy
}

// Resource limits govern new admissions, not existing ownership. Resolve an
// extant claim through the normal idempotence/conflict path even when retained
// history fills the quota. Only the exceptional capacity path adds these reads.
func (st *strongState) resultAdmissionFailure(ctx context.Context, name string, cause error) (bool, error) {
	if !errors.Is(cause, errStrongResultCapacity) && !errors.Is(cause, errStrongResultTooLarge) {
		return false, cause
	}
	for _, key := range []string{activeKey(name), pendingKey(name)} {
		if _, err := st.svc.getContext(ctx, key); err == nil {
			return false, nil
		} else if !errors.Is(err, kvapi.ErrKeyNotFound) {
			return false, err
		}
	}
	return false, cause
}
