// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestCanceledStartStillRequiresBoundedCleanup(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan error, 1)
	startCtx, cancelStart := context.WithCancel(t.Context())
	defer cancelStart()
	controller := NewController(t.Context(), &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		stopFunc: func(ctx context.Context) error {
			stopped <- ctx.Err()
			return nil
		},
	}, supervisor.LifecycleConfig{StartTimeout: time.Second, StopTimeout: time.Second}, nil)
	defer controller.close()
	done := make(chan error, 1)
	go func() { done <- controller.startContext(startCtx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("start did not begin")
	}
	cancelStart()
	require.ErrorIs(t, <-done, context.Canceled)
	stopCtx, cancelStop := context.WithTimeout(t.Context(), time.Second)
	defer cancelStop()
	require.NoError(t, controller.StopContext(stopCtx))
	select {
	case err := <-stopped:
		require.NoError(t, err, "cleanup inherited the canceled start context")
	case <-time.After(time.Second):
		t.Fatal("canceled start skipped service cleanup")
	}
}
