// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"errors"
	"maps"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// writeVote binds incarnation votes to the live pending version. A delayed
// write cannot leave an orphan vote after the reservation was removed.
func (st *strongState) writeVote(name string, epoch uint64, hdr pendingHeader, key string, value []byte) error {
	if hdr.RequiredIncarnations == nil {
		_, _, err := st.svc.engine.SetIfAbsent(key, value)
		return err
	}
	read := st.svc.engine.Get
	if st.svc.nonMember != nil && st.svc.nonMember() {
		read = st.svc.get
	}
	// Members may observe a stale replica: the transaction's version check
	// prevents a stale observation from writing into a replacement reservation.
	entry, err := read(pendingKey(name))
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		// The reservation finished between observation and vote preparation.
		// Keep the exclusion until ordinary authoritative reconciliation retires it.
		return nil
	}
	if err != nil {
		return err
	}
	current, err := decodePending(entry.Value)
	if err != nil {
		return err
	}
	if entry.Epoch != epoch || current.Name != name || current.PID != hdr.PID || !maps.Equal(current.RequiredIncarnations, hdr.RequiredIncarnations) {
		return nil // superseded reservation: no vote is needed for this observation
	}
	committed, err := st.svc.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: entry.Version},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: key, Value: value},
	})
	if err != nil {
		return err
	}
	if committed {
		return nil
	}
	// Another evaluator may have committed this same incarnation's vote.
	if _, err := read(key); err == nil {
		return nil
	} else if !errors.Is(err, kvapi.ErrKeyNotFound) {
		return err
	}
	return nil // superseded reservation: no vote is needed for this observation
}
