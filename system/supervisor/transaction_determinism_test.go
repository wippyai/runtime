// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/testutil"
)

// TestCommit_StableOrder is the regression test for transaction.commit()
// invoking removeFn/registerFn in deterministic order. Pre-fix the callbacks
// fired in Go map iteration order, which is hash-seed randomized, so any
// downstream consumer (the supervisor itself) inherited that randomness.
func TestCommit_StableOrder(t *testing.T) {
	ids := make([]string, 30)
	for i := range ids {
		ids[i] = fmt.Sprintf("svc-%03d", i)
	}

	expectedRemoves := append([]string(nil), ids[:10]...)
	expectedRegisters := append([]string(nil), ids[10:]...)
	sort.Strings(expectedRemoves)
	sort.Strings(expectedRegisters)

	for seed := uint64(0); seed < 1000; seed++ {
		removeOrder := testutil.ShuffleSlice(ids[:10], seed)
		registerOrder := testutil.ShuffleSlice(ids[10:], seed^0xDEADBEEF)

		th := newRegTx(noopLogger())
		th.begin()
		for _, id := range removeOrder {
			require.NoError(t, th.removeService(id))
		}
		for _, id := range registerOrder {
			require.NoError(t, th.registerService(id, &supervisor.Entry{}))
		}

		var gotRemoves, gotRegisters []string
		removeFn := func(id string) error {
			gotRemoves = append(gotRemoves, id)
			return nil
		}
		registerFn := func(id string, _ *supervisor.Entry) error {
			gotRegisters = append(gotRegisters, id)
			return nil
		}

		require.NoError(t, th.commit(removeFn, registerFn))
		require.Equal(t, expectedRemoves, gotRemoves, "seed=%d: remove callbacks must fire in sorted order", seed)
		require.Equal(t, expectedRegisters, gotRegisters, "seed=%d: register callbacks must fire in sorted order", seed)
	}
}

// TestCommit_RemoveBeforeRegister keeps the documented ordering invariant
// (remove first, then register) — guards against future refactors swapping
// the loops while landing the determinism fix.
func TestCommit_RemoveBeforeRegister(t *testing.T) {
	th := newRegTx(noopLogger())
	th.begin()
	require.NoError(t, th.removeService("a"))
	require.NoError(t, th.registerService("b", &supervisor.Entry{}))

	var sequence []string
	require.NoError(t, th.commit(
		func(id string) error {
			sequence = append(sequence, "remove:"+id)
			return nil
		},
		func(id string, _ *supervisor.Entry) error {
			sequence = append(sequence, "register:"+id)
			return nil
		},
	))

	require.Equal(t, []string{"remove:a", "register:b"}, sequence)
}
