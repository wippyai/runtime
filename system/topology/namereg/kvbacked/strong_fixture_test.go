// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// seedStrongPending preserves a deliberately chosen participant snapshot while
// admitting the same atomic outcome slot required by production completion.
func seedStrongPending(t *testing.T, r *Service, header pendingHeader) (pendingHeader, kvapi.Entry) {
	t.Helper()
	if header.AttemptID == "" {
		header.AttemptID = rand.Text()
	}
	capacity, initial, err := planStrongResultCapacity(header, r.strong.resultPolicy.RecordBytes)
	require.NoError(t, err)
	ops, err := reserveStrongResultOps(r.engine.Get, strongResultLimits{Entries: r.strong.resultPolicy.Entries, Bytes: r.strong.resultPolicy.Bytes}, header.AttemptID, capacity, initial)
	require.NoError(t, err)
	body, err := encode(header)
	require.NoError(t, err)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey(header.Name), Value: body})
	committed, err := r.engine.Txn(ops)
	require.NoError(t, err)
	require.True(t, committed)
	entry, err := r.engine.Get(pendingKey(header.Name))
	require.NoError(t, err)
	return header, entry
}
