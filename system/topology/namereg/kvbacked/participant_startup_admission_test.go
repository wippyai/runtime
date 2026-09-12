// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestParticipantDoesNotAdmitWritesDuringBootstrap(t *testing.T) {
	inventory, engine := newParticipantTestInventory(t, 4)
	ctx := context.Background()
	require.NoError(t, inventory.enroll(ctx, "node", "one"))
	service := NewService(engine, "node", nil, nil)
	service.ConfigureStrong(StrongDeps{Incarnation: "one"})
	service.strong.participants = inventory
	entered, release := make(chan struct{}), make(chan struct{})
	started := make(chan error, 1)
	go func() {
		started <- service.startReconciler(ctx, func(context.Context) error {
			close(entered)
			<-release
			return nil
		}, nil)
	}()
	<-entered
	_, admissionErr := service.RegisterScope(ctx, "before-bootstrap", mkPID("node", "owner"), globalapi.Consistent)
	close(release)
	startErr := <-started
	t.Cleanup(func() { require.NoError(t, service.StopReconciler(ctx)) })
	require.NoError(t, startErr)
	require.ErrorIs(t, admissionErr, globalapi.ErrNotReady)
	_, err := engine.Get(activeKey("before-bootstrap"))
	require.ErrorIs(t, err, kvapi.ErrKeyNotFound)
	// Later refresh readiness loss must not turn Consistent into an all-node barrier.
	service.ready.Store(false)
	_, err = service.RegisterScope(ctx, "after-bootstrap", mkPID("node", "owner"), globalapi.Consistent)
	require.NoError(t, err)
}
