// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestControllerRestartTimeoutDoesNotDoubleStart(t *testing.T) {
	status := make(chan any, 1)
	var starts atomic.Int32
	svc := &mockService{
		startFunc: func(context.Context) (<-chan any, error) { starts.Add(1); return status, nil },
		stopFunc:  func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
	}
	ctrl := NewController(t.Context(), svc, supervisor.LifecycleConfig{
		StartTimeout: time.Second, StopTimeout: 50 * time.Millisecond,
		RetryPolicy: supervisor.RetryPolicy{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxAttempts: 1},
	}, nil)
	defer ctrl.close()
	require.NoError(t, ctrl.Start())
	status <- supervisor.Restart{Graceful: true}
	require.Eventually(t, func() bool { return ctrl.State().Status == supervisor.StatusFailed }, time.Second, time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int32(1), starts.Load(), "replacement must not start before old incarnation stops")
}
