// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"fmt"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// StrongResultPolicy bounds retained attempt evidence independently of names.
// Retention limits recovery history; it never controls claim ownership.
// Configure before starting or publishing the registry.
type StrongResultPolicy struct {
	Entries      uint64
	Bytes        uint64
	RecordBytes  uint64
	Retention    time.Duration
	ReclaimBatch int
}

func DefaultStrongResultPolicy() StrongResultPolicy {
	return StrongResultPolicy{Entries: 4096, Bytes: 16 << 20, RecordBytes: 1 << 20, Retention: time.Minute, ReclaimBatch: 64}
}

func (s *Service) ConfigureStrongResults(policy StrongResultPolicy) error {
	if s.strong == nil {
		return fmt.Errorf("Strong results require Strong configuration")
	}
	if policy.Entries == 0 || policy.Bytes == 0 || policy.RecordBytes == 0 || policy.RecordBytes > policy.Bytes || policy.Retention <= 0 || policy.ReclaimBatch <= 0 || uint64(policy.ReclaimBatch) > policy.Entries {
		return fmt.Errorf("invalid Strong result limits, retention or reclaim batch")
	}
	s.strong.resultPolicy = policy
	return nil
}

// reclaimStrongResults performs at most one transaction. The quota fences the
// entire scan against concurrent admissions/releases; every deletion also fences
// its exact terminal version. A rejected transaction is retried by a later sweep.
// Decoded result count is bounded by admission; writes by ReclaimBatch.
// The current KV Scan still traverses unrelated keys when filtering its prefix.
func (st *strongState) reclaimStrongResults(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !st.isLeader() {
		return nil
	}
	policy := st.resultPolicy
	usage, check, err := readStrongResultUsage(st.svc.engine.Get)
	if err != nil || usage.Entries == 0 {
		return err
	}
	ops := []kvapi.TxnOp{check}
	var scanned, releasedBytes uint64
	released := 0
	now := st.clock().UnixNano()
	var scanErr error
	err = st.svc.engine.Scan(strongResultPrefix, func(entry kvapi.Entry) bool {
		if scanErr = ctx.Err(); scanErr != nil {
			return false
		}
		scanned++
		// A lowered configuration may leave more admitted records than its new
		// limit. The persisted quota remains the bound while those drain.
		if scanned > usage.Entries {
			scanErr = fmt.Errorf("Strong result scan exceeded admitted count")
			return false
		}
		outcome, decodeErr := decodeStrongOutcome(entry, policy.RecordBytes)
		if decodeErr != nil {
			scanErr = decodeErr
			return false
		}
		if outcome.Phase == strongResultPending || outcome.RetainUntil > now {
			return true
		}
		var reservation strongResultReservation
		if scanErr = decodeInto(entry.Value, &reservation); scanErr != nil {
			return false
		}
		if releasedBytes > usage.Bytes || reservation.Capacity > usage.Bytes-releasedBytes {
			scanErr = fmt.Errorf("Strong result usage underflow")
			return false
		}
		releasedBytes += reservation.Capacity
		released++
		ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondVersion, Key: entry.Key, Expect: entry.Version})
		return released < policy.ReclaimBatch
	})
	if err != nil {
		return err
	}
	if scanErr != nil {
		return scanErr
	}
	if released == 0 {
		return nil
	}
	usage.Entries -= uint64(released)
	usage.Bytes -= releasedBytes
	if usage.Entries == 0 && usage.Bytes != 0 {
		return fmt.Errorf("inconsistent Strong result usage")
	}
	body, err := encode(usage)
	if err != nil {
		return err
	}
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: strongResultUsageKey, Value: body})
	if err := ctx.Err(); err != nil {
		return err
	}
	if contextual, ok := st.svc.engine.(interface {
		TxnContext(context.Context, []kvapi.TxnOp) (bool, error)
	}); ok {
		_, err = contextual.TxnContext(ctx, ops)
	} else {
		_, err = st.svc.engine.Txn(ops)
	}
	return err
}
