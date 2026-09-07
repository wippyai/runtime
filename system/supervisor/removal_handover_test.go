// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestRemovalRetiresDependencyWithoutRestartingDependent(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()
	dependency, dependent := newStrictService(), newStrictService()
	registerInstanceWithDeps(h, "test:db", dependency, true, nil)
	registerInstanceWithDeps(h, "test:cdc", dependent, true, []string{"test:db"})
	awaitCondition(t, "dependent to start", dependent.isRunning)
	tx := newRegTx(h.sup.logger)
	tx.begin()
	require.NoError(t, tx.removeService("test:db"))
	require.NoError(t, h.sup.execute(context.Background(), tx))
	require.False(t, dependency.isRunning())
	require.False(t, dependent.isRunning())
	require.Equal(t, 1, dependent.stopCount())
	controllers := h.sup.snapshotControllers()
	require.NotContains(t, controllers, "test:db")
	require.Equal(t, supervisor.StatusStopped, controllers["test:cdc"].State().Status)
}
