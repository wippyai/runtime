// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// acknowledgeStrongResult releases only an observed terminal attempt. It is a
// bounded best-effort operation. Only definitive CAS refusal may be retried;
// ambiguous errors leave retention GC responsible without revising the result.
func (st *strongState) acknowledgeStrongResult(ctx context.Context, name string, owner pid.PID, attempt string) error {
	for retry := 0; retry < maxResolveRetries; retry++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		read := func(key string) (kvapi.Entry, error) { return st.svc.getContext(ctx, key) }
		entry, result, err := readStrongOutcome(read, attempt, name, owner.String(), st.resultPolicy.RecordBytes)
		if errors.Is(err, kvapi.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if result.Phase == strongResultPending {
			return nil
		}
		ops, err := releaseStrongResultOps(read, entry)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var committed bool
		if contextual, ok := st.svc.engine.(interface {
			TxnContext(context.Context, []kvapi.TxnOp) (bool, error)
		}); ok {
			committed, err = contextual.TxnContext(ctx, ops)
		} else {
			committed, err = st.svc.engine.Txn(ops)
		}
		if err != nil || committed {
			return err
		}
		// A definitive CAS refusal wrote nothing. Refresh BOTH result and
		// quota; never replay an ambiguous transaction error.
	}
	return nil // retention owns cleanup after bounded contention
}
