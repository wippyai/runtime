// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apisupervisor "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestY02FailedSupervisorCommitRemovesController(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	assertCanceledCommitRemovesController(canceledCtx, t, "canceled-service")

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	assertCanceledCommitRemovesController(deadlineCtx, t, "deadline-service")
}

func assertCanceledCommitRemovesController(ctx context.Context, t *testing.T, serviceID string) {
	t.Helper()
	bus := eventbus.NewBus()
	defer bus.Stop()
	sup := NewSupervisor(bus, zap.NewNop())
	sup.ctx = ctx
	tx := newRegTx(zap.NewNop())
	tx.open = true
	tx.register[serviceID] = &apisupervisor.Entry{
		Service: newTestService(),
		Config: apisupervisor.LifecycleConfig{
			AutoStart:    true,
			StartTimeout: time.Second,
			StopTimeout:  time.Second,
		},
	}

	err := sup.execute(ctx, tx)

	require.Error(t, err)
	_, stateErr := sup.GetState(serviceID)
	require.Error(t, stateErr)
	require.Empty(t, sup.GetAllStates())
}
