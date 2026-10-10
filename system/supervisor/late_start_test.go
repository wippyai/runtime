// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	api "github.com/wippyai/runtime/api/supervisor"
)

func TestControllerLateStartIsStoppedBeforeRetry(t *testing.T) {
	allowFirst := make(chan struct{})
	firstCanceled := make(chan struct{})
	cleanupEntered := make(chan struct{})
	allowCleanup := make(chan struct{})
	secondStarted := make(chan struct{})
	var starts atomic.Int32
	var stops atomic.Int32
	details := make(chan any)
	svc := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			if starts.Add(1) == 1 {
				<-ctx.Done()
				close(firstCanceled)
				<-allowFirst
				return make(chan any), nil
			}
			close(secondStarted)
			return details, nil
		},
		stopFunc: func(ctx context.Context) error {
			if stops.Add(1) == 1 {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				close(cleanupEntered)
				<-allowCleanup
				return nil
			}
			close(details)
			return nil
		},
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: 20 * time.Millisecond, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{InitialDelay: time.Millisecond, MaxAttempts: 3},
	}, nil)
	defer c.close()
	started := make(chan error, 1)
	go func() { started <- c.Start() }()
	<-firstCanceled
	select {
	case <-secondStarted:
		t.Error("retry started while the first startup was still in flight")
	case <-time.After(40 * time.Millisecond):
	}
	close(allowFirst)
	select {
	case <-cleanupEntered:
	case <-time.After(time.Second):
		t.Error("late successful startup was not stopped")
	}
	select {
	case <-secondStarted:
		t.Error("retry started before late-start cleanup completed")
	default:
	}
	close(allowCleanup)
	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("retry did not resume after cleanup")
	}
	require.NoError(t, c.Stop())
}

func TestControllerLateCleanupFailureBlocksRetryUntilStop(t *testing.T) {
	allowFirst := make(chan struct{})
	firstCanceled := make(chan struct{})
	details := make(chan any)
	var starts, stops atomic.Int32
	cleanupErr := errors.New("child did not stop")
	svc := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			if starts.Add(1) == 1 {
				<-ctx.Done()
				close(firstCanceled)
				<-allowFirst
			}
			return details, nil
		},
		stopFunc: func(context.Context) error {
			switch stops.Add(1) {
			case 1:
				return cleanupErr
			case 2:
				return nil // explicit Stop resolves the abandoned first startup
			default:
				close(details)
				return nil
			}
		},
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: 20 * time.Millisecond, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{InitialDelay: time.Millisecond, MaxAttempts: 3},
	}, nil)
	defer c.close()
	started := make(chan error, 1)
	go func() { started <- c.Start() }()
	<-firstCanceled
	close(allowFirst)
	select {
	case err := <-started:
		require.ErrorIs(t, err, cleanupErr)
	case <-time.After(time.Second):
		t.Fatal("cleanup failure was not reported to startup")
	}
	require.Equal(t, api.StatusFailed, c.State().Status, "failed cleanup is not proof of exit")
	require.False(t, c.startMayCompleteInBackground())
	require.ErrorIs(t, c.Start(), cleanupErr)
	require.Equal(t, int32(1), starts.Load(), "failed cleanup must block duplicate incarnations")
	require.NoError(t, c.Stop())
	require.NoError(t, c.Start())
	require.Equal(t, int32(2), starts.Load())
	require.NoError(t, c.Stop())
}

func TestControllerLateCleanupFailureReportedWithoutRetries(t *testing.T) {
	allowFirst := make(chan struct{})
	firstCanceled := make(chan struct{})
	cleanupErr := errors.New("late cleanup failed")
	svc := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			<-ctx.Done()
			close(firstCanceled)
			<-allowFirst
			return make(chan any), nil
		},
		stopFunc: func(context.Context) error { return cleanupErr },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: 10 * time.Millisecond, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{MaxAttempts: 1},
	}, nil)
	defer c.close()
	require.Error(t, c.Start())
	<-firstCanceled
	close(allowFirst)
	require.Eventually(t, func() bool {
		err, ok := c.State().Details.(error)
		return ok && errors.Is(err, cleanupErr)
	}, time.Second, time.Millisecond)
	require.Equal(t, api.StatusFailed, c.State().Status)
}

func TestControllerStopBoundedWhileStartupIgnoresCancellation(t *testing.T) {
	allowStart := make(chan struct{})
	canceled := make(chan struct{})
	cleaned := make(chan struct{})
	svc := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			<-ctx.Done()
			close(canceled)
			<-allowStart
			return make(chan any), nil
		},
		stopFunc: func(context.Context) error { close(cleaned); return nil },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: 10 * time.Millisecond, StopTimeout: 20 * time.Millisecond,
		RetryPolicy: api.RetryPolicy{MaxAttempts: 1},
	}, nil)
	defer c.close()
	require.Error(t, c.Start())
	<-canceled
	stopped := make(chan error, 1)
	go func() { stopped <- c.Stop() }()
	select {
	case err := <-stopped:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Error("Stop lost its deadline waiting for Start")
	}
	close(allowStart)
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("late startup did not retain cleanup ownership")
	}
	require.NoError(t, c.Stop())
}

func TestControllerLateCleanupRetainsFrameAfterClose(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	key := &ctxapi.Key{Name: "late-start-owner", Inherit: true}
	require.NoError(t, frame.Set(key, "retained"))
	allowStart := make(chan struct{})
	canceled := make(chan struct{})
	cleaned := make(chan any, 1)
	svc := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			<-ctx.Done()
			close(canceled)
			<-allowStart
			return make(chan any), nil
		},
		stopFunc: func(ctx context.Context) error {
			value, _ := ctxapi.FrameFromContext(ctx).Get(key)
			cleaned <- value
			return ctx.Err()
		},
	}
	c := NewController(ctx, svc, api.LifecycleConfig{
		StartTimeout: 10 * time.Millisecond, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{MaxAttempts: 1},
	}, nil)
	require.Error(t, c.Start())
	<-canceled
	c.close()
	close(allowStart)
	select {
	case value := <-cleaned:
		require.Equal(t, "retained", value)
	case <-time.After(time.Second):
		t.Fatal("closed controller abandoned the late startup")
	}
}

func TestControllerCanceledQueuedStartDoesNotResurrect(t *testing.T) {
	allowStart := make(chan struct{})
	canceled := make(chan struct{})
	cleaned := make(chan struct{})
	var starts atomic.Int32
	svc := &mockService{
		startFunc: func(ctx context.Context) (<-chan any, error) {
			starts.Add(1)
			<-ctx.Done()
			close(canceled)
			<-allowStart
			return make(chan any), nil
		},
		stopFunc: func(context.Context) error { close(cleaned); return nil },
	}
	c := NewController(context.Background(), svc, api.LifecycleConfig{
		StartTimeout: 10 * time.Millisecond, StopTimeout: time.Second,
		RetryPolicy: api.RetryPolicy{MaxAttempts: 1},
	}, nil)
	defer c.close()
	require.Error(t, c.Start())
	<-canceled
	queued, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, c.startContext(queued), context.DeadlineExceeded)
	close(allowStart)
	<-cleaned
	require.NoError(t, c.Stop())
	require.Equal(t, int32(1), starts.Load())
}
