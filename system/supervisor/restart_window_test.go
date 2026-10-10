// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	api "github.com/wippyai/runtime/api/supervisor"
)

func TestRestartWindowBoundaryAndBoundedStorage(t *testing.T) {
	w := restartWindow{policy: &api.RestartIntensity{MaxRestarts: 2, Window: 10 * time.Second}}
	base := time.Unix(100, 0)
	require.True(t, w.admit(base))
	require.True(t, w.admit(base.Add(time.Second)))
	require.False(t, w.admit(base.Add(10*time.Second-time.Nanosecond)))
	require.True(t, w.admit(base.Add(10*time.Second)), "left boundary is excluded")
	require.False(t, w.admit(base.Add(10*time.Second)))
	require.True(t, w.admit(base.Add(11*time.Second)))
	for i := 1; i <= 1000; i++ {
		require.True(t, w.admit(base.Add(time.Duration(i+2)*20*time.Second)))
	}
	require.Len(t, w.times, 2)
	allocs := testing.AllocsPerRun(100, func() { w.admit(base.Add(24 * time.Hour)) })
	require.Zero(t, allocs, "a full ring does not allocate on admission or rejection")
}

func TestRestartWindowDisabledDoesNotAllocate(t *testing.T) {
	w := restartWindow{}
	allocs := testing.AllocsPerRun(1000, func() {
		if !w.admit(time.Time{}) {
			t.Fatal("disabled intensity refused a restart")
		}
	})
	require.Zero(t, allocs)
	require.Nil(t, w.times)
}

func TestControllerIntensitySurvivesStableCounterReset(t *testing.T) {
	var starts atomic.Int32
	svc := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			starts.Add(1)
			ch := make(chan any)
			close(ch)
			return ch, nil
		},
		stopFunc: func(context.Context) error { return nil },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: time.Second, StopTimeout: time.Second, StableThreshold: 0,
		RetryPolicy: api.RetryPolicy{InitialDelay: time.Millisecond, BackoffFactor: 1,
			Intensity: &api.RestartIntensity{MaxRestarts: 2, Window: time.Hour}},
	}, nil)
	defer c.close()
	_ = c.Start()
	require.Eventually(t, func() bool { return c.State().Status == api.StatusExited }, time.Second, time.Millisecond)
	require.Equal(t, int32(3), starts.Load(), "initial start plus exactly two failure-driven restarts")
	var err apierror.Error
	require.ErrorAs(t, c.State().Details.(error), &err)
	require.Equal(t, apierror.False, err.Retryable())
	require.Contains(t, err.Error(), "restart intensity exceeded")
	require.Equal(t, api.StatusRunning, c.State().Desired)
	require.NoError(t, c.Stop())
}

func TestControllerNoIntensityKeepsUnlimitedRestarts(t *testing.T) {
	var starts atomic.Int32
	final := make(chan any)
	svc := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			if starts.Add(1) >= 9 {
				return final, nil
			}
			ch := make(chan any)
			close(ch)
			return ch, nil
		},
		stopFunc: func(context.Context) error { close(final); return nil },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: time.Second, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{InitialDelay: time.Millisecond, BackoffFactor: 1},
	}, nil)
	defer c.close()
	_ = c.Start()
	require.Eventually(t, func() bool { return starts.Load() == 9 && c.State().Status == api.StatusRunning }, time.Second, time.Millisecond)
	require.NoError(t, c.Stop())
}

func TestControllerPlannedRestartsDoNotConsumeIntensity(t *testing.T) {
	var starts atomic.Int32
	final := make(chan any)
	svc := &mockService{
		startFunc: func(context.Context) (<-chan any, error) {
			if starts.Add(1) >= 4 {
				return final, nil
			}
			ch := make(chan any, 1)
			ch <- api.Restart{}
			return ch, nil
		},
		stopFunc: func(context.Context) error { close(final); return nil },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: time.Second, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{InitialDelay: time.Millisecond, BackoffFactor: 2,
			Intensity: &api.RestartIntensity{MaxRestarts: 1, Window: time.Hour}},
	}, nil)
	defer c.close()
	_ = c.Start()
	require.Eventually(t, func() bool { return starts.Load() == 4 && c.State().Status == api.StatusRunning }, time.Second, time.Millisecond)
	require.NoError(t, c.Stop())
}
