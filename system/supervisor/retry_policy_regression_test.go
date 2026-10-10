// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/supervisor"
)

func TestControllerRetryDelayGrowsAndCaps(t *testing.T) {
	starts := make(chan time.Time, 3)
	details := make(chan any)
	var attempt atomic.Int32
	svc := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			starts <- time.Now()
			if attempt.Add(1) < 3 {
				return nil, errors.New("temporary startup failure")
			}
			return details, nil
		},
		stopFunc: func(context.Context) error { close(details); return nil },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: time.Second, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{InitialDelay: 30 * time.Millisecond, MaxDelay: 80 * time.Millisecond,
			BackoffFactor: 3, MaxAttempts: 4},
	}, nil)
	defer c.close()
	require.NoError(t, c.Start())
	first, second, third := <-starts, <-starts, <-starts
	require.GreaterOrEqual(t, second.Sub(first), 30*time.Millisecond)
	require.GreaterOrEqual(t, third.Sub(second), 80*time.Millisecond, "second retry must grow to the cap")
	require.NoError(t, c.Stop())
}

func TestControllerRetryDelayResetsAfterStableRun(t *testing.T) {
	starts := make(chan time.Time, 4)
	details, final := make(chan any), make(chan any)
	var attempt atomic.Int32
	svc := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			starts <- time.Now()
			switch attempt.Add(1) {
			case 1, 2:
				return nil, errors.New("temporary startup failure")
			case 3:
				return details, nil
			default:
				return final, nil
			}
		},
		stopFunc: func(context.Context) error { close(final); return nil },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: time.Second, StopTimeout: time.Second, StableThreshold: 10 * time.Millisecond,
		RetryPolicy: api.RetryPolicy{InitialDelay: 20 * time.Millisecond, MaxDelay: 300 * time.Millisecond,
			BackoffFactor: 20, MaxAttempts: 4},
	}, nil)
	defer c.close()
	require.NoError(t, c.Start())
	first, second, third := <-starts, <-starts, <-starts
	require.GreaterOrEqual(t, second.Sub(first), 20*time.Millisecond)
	require.GreaterOrEqual(t, third.Sub(second), 300*time.Millisecond)
	time.Sleep(20 * time.Millisecond) // hold the successful run beyond stable_threshold
	exited := time.Now()
	close(details)
	select {
	case fourth := <-starts:
		require.GreaterOrEqual(t, fourth.Sub(exited), 20*time.Millisecond)
		require.Less(t, fourth.Sub(exited), 250*time.Millisecond, "stable run must reset the 300ms failure backoff")
	case <-time.After(time.Second):
		t.Fatal("stable service did not restart")
	}
	require.NoError(t, c.Stop())
}
