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

func TestControllerRestartBackoffRetiresPendingStart(t *testing.T) {
	for _, operation := range []string{"stop", "start"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := make(chan any, 1)
			second := make(chan any, 1)
			var starts atomic.Int32
			svc := &mockService{
				startFunc: func(context.Context) (<-chan any, error) {
					if starts.Add(1) == 1 {
						return first, nil
					}
					return second, nil
				},
				stopFunc: func(context.Context) error { return nil },
			}
			const delay = 300 * time.Millisecond
			ctrl := NewController(ctx, svc, supervisor.LifecycleConfig{StartTimeout: time.Second, StopTimeout: time.Second,
				RetryPolicy: supervisor.RetryPolicy{InitialDelay: delay, MaxDelay: delay, MaxAttempts: 1}}, nil)
			require.NoError(t, ctrl.Start())
			first <- supervisor.Restart{}
			require.Eventually(t, func() bool { return ctrl.State().Status == supervisor.StatusExited }, time.Second, time.Millisecond)
			want := int32(1)
			if operation == "stop" {
				require.NoError(t, ctrl.Stop())
			} else {
				require.NoError(t, ctrl.Start())
				second <- supervisor.ErrExit
				require.Eventually(t, func() bool { return ctrl.State().Status == supervisor.StatusExited }, time.Second, time.Millisecond)
				want = 2
			}
			time.Sleep(2 * delay)
			require.Equal(t, want, starts.Load(), "a superseded restart timer cannot start another run")
		})
	}
}
