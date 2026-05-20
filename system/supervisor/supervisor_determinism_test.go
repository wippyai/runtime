// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/testutil"
	"go.uber.org/zap"
)

// TestSortedRegisterIDs_DeterministicAcrossPermutations builds a tx.register
// map by inserting entries in 1000 different shuffled orders and asserts the
// helper always returns the same lexicographic sequence. Locks in the contract
// that supervisor.execute() processes registrations in stable order regardless
// of the Go map hash seed.
func TestSortedRegisterIDs_DeterministicAcrossPermutations(t *testing.T) {
	canonicalIDs := make([]string, 50)
	for i := range canonicalIDs {
		canonicalIDs[i] = fmt.Sprintf("svc-%03d", i)
	}
	expected := make([]string, len(canonicalIDs))
	copy(expected, canonicalIDs)
	sort.Strings(expected)

	for seed := uint64(0); seed < 1000; seed++ {
		shuffled := testutil.ShuffleSlice(canonicalIDs, seed)
		m := make(map[string]*supervisor.Entry, len(shuffled))
		for _, id := range shuffled {
			m[id] = &supervisor.Entry{}
		}

		got := sortedRegisterIDs(m)
		require.Equal(t, expected, got, "seed=%d", seed)
	}
}

// TestSortedRemoveIDs_DeterministicAcrossPermutations is the analog for the
// tx.remove set used in execute() lines 568 and 605.
func TestSortedRemoveIDs_DeterministicAcrossPermutations(t *testing.T) {
	canonicalIDs := make([]string, 50)
	for i := range canonicalIDs {
		canonicalIDs[i] = fmt.Sprintf("svc-%03d", i)
	}
	expected := make([]string, len(canonicalIDs))
	copy(expected, canonicalIDs)
	sort.Strings(expected)

	for seed := uint64(0); seed < 1000; seed++ {
		shuffled := testutil.ShuffleSlice(canonicalIDs, seed)
		m := make(map[string]struct{}, len(shuffled))
		for _, id := range shuffled {
			m[id] = struct{}{}
		}

		got := sortedRemoveIDs(m)
		require.Equal(t, expected, got, "seed=%d", seed)
	}
}

// TestSortedControllerIDs_DeterministicAcrossPermutations covers
// buildStopOperations() iterating over s.controllers.
func TestSortedControllerIDs_DeterministicAcrossPermutations(t *testing.T) {
	canonicalIDs := make([]string, 50)
	for i := range canonicalIDs {
		canonicalIDs[i] = fmt.Sprintf("svc-%03d", i)
	}
	expected := make([]string, len(canonicalIDs))
	copy(expected, canonicalIDs)
	sort.Strings(expected)

	for seed := uint64(0); seed < 1000; seed++ {
		shuffled := testutil.ShuffleSlice(canonicalIDs, seed)
		m := make(map[string]*Controller, len(shuffled))
		for _, id := range shuffled {
			m[id] = &Controller{}
		}

		got := sortedControllerIDs(m)
		require.Equal(t, expected, got, "seed=%d", seed)
	}
}

// TestSortedRegisterIDs_EmptyAndSingleton verifies edge cases.
func TestSortedRegisterIDs_EmptyAndSingleton(t *testing.T) {
	assert.Empty(t, sortedRegisterIDs(map[string]*supervisor.Entry{}))
	assert.Empty(t, sortedRemoveIDs(map[string]struct{}{}))
	assert.Empty(t, sortedControllerIDs(map[string]*Controller{}))

	single := map[string]*supervisor.Entry{"only": {}}
	assert.Equal(t, []string{"only"}, sortedRegisterIDs(single))
}

// TestBuildStartOperationsForRoots_DeterministicAcrossRootPermutations is the
// integration-level regression test for the boot symptom. It directly exercises
// the operation-building path used by execute(): build a controllers map
// representing N independent services, derive roots through sortedRegisterIDs,
// and call buildStartOperationsForRoots. Across 1000 random insertion orders
// of the underlying tx.register map, the produced operations slice (and thus
// the order the sequencer will schedule services in) must be identical. Pre-fix
// the slice depended on Go map iteration, which is hash-seed randomized.
func TestBuildStartOperationsForRoots_DeterministicAcrossRootPermutations(t *testing.T) {
	serviceIDs := []string{
		"app.fs:cache", "app.fs:state", "app.fs:uploads",
		"app.process:queue", "app.process:worker",
		"keeper.components:git_static", "keeper.mcp:auth", "keeper.mcp:tokens",
		"userspace.uploads:process_queue",
	}

	expectedSorted := make([]string, len(serviceIDs))
	copy(expectedSorted, serviceIDs)
	sort.Strings(expectedSorted)

	var baseline []string

	for seed := uint64(0); seed < 1000; seed++ {
		shuffled := testutil.ShuffleSlice(serviceIDs, seed)
		got := buildOperationOrderFromRegistration(t, shuffled)

		if seed == 0 {
			baseline = got
			require.Equal(t, expectedSorted, got,
				"operations slice should mirror lexicographic registration order")
			continue
		}
		require.Equal(t, baseline, got, "operation order diverged for seed=%d", seed)
	}
}

// buildOperationOrderFromRegistration simulates the deterministic prefix of
// supervisor.execute() — populating tx.register and then deriving the roots
// slice via sortedRegisterIDs — and runs buildStartOperationsForRoots against
// an independent-controller universe. Returns the IDs in the order the
// resulting operations slice will be fed to the sequencer.
func buildOperationOrderFromRegistration(t *testing.T, registrationOrder []string) []string {
	t.Helper()

	tx := newRegTx(testLogger(t))
	tx.begin()
	for _, id := range registrationOrder {
		require.NoError(t, tx.registerService(id, &supervisor.Entry{
			Service: newTestService(),
			Config: supervisor.LifecycleConfig{
				AutoStart:    true,
				StartTimeout: 2 * time.Second,
				StopTimeout:  2 * time.Second,
			},
		}))
	}

	registerIDs := sortedRegisterIDs(tx.register)

	controllers := make(map[string]*Controller, len(registerIDs))
	for _, id := range registerIDs {
		entry := tx.register[id]
		controllers[id] = NewController(context.Background(), entry.Service, entry.Config, func(_ supervisor.Status, _ any) {})
	}

	roots := make([]startRoot, 0, len(registerIDs))
	for _, id := range registerIDs {
		entry := tx.register[id]
		roots = append(roots, startRoot{
			id:       id,
			required: entry.Config.StartupRequired(),
		})
	}

	sup := &Supervisor{}
	ops, err := sup.buildStartOperationsForRoots(controllers, roots)
	require.NoError(t, err)

	got := make([]string, 0, len(ops))
	for _, op := range ops {
		got = append(got, op.id)
	}
	return got
}

func testLogger(_ testing.TB) *zap.Logger { return zap.NewNop() }
