package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	retryapi "github.com/wippyai/runtime/api/service/interceptor/retry"
)

type testError struct {
	kind      apierror.Kind
	retryable apierror.Ternary
}

func (e *testError) Error() string {
	return "test error"
}

func (e *testError) Kind() apierror.Kind {
	return e.kind
}

func (e *testError) Retryable() apierror.Ternary {
	return e.retryable
}

func (e *testError) Details() attrs.Attributes {
	return nil
}

func makeTask(options runtime.Options) runtime.Task {
	return runtime.Task{
		ID:      registry.ID{Name: "test"},
		Options: options,
	}
}

func TestInterceptor_NoOptions(t *testing.T) {
	interceptor := New()
	ctx := context.Background()
	task := makeTask(nil)

	called := false
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestInterceptor_NoRetryOption(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	task := makeTask(opts)

	called := false
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestInterceptor_MaxAttemptsZero(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 0})
	task := makeTask(opts)

	called := false
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestInterceptor_SuccessFirstAttempt(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 1, attempts)
}

func TestInterceptor_RetryableError(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts < 3 {
			return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, nil
		}
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 3, attempts)
}

func TestInterceptor_MaxAttemptsReached(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	task := makeTask(opts)

	attempts := 0
	testErr := &testError{kind: apierror.KindUnavailable}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{Error: testErr}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.Equal(t, testErr, result.Error)
	assert.Equal(t, 3, attempts)
}

func TestInterceptor_NonRetryableErrorKind(t *testing.T) {
	testCases := []struct {
		name string
		kind apierror.Kind
	}{
		{"Invalid", apierror.KindInvalid},
		{"PermissionDenied", apierror.KindPermissionDenied},
		{"Internal", apierror.KindInternal},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			interceptor := New()
			ctx := context.Background()

			opts := runtime.Bag{}
			opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
			task := makeTask(opts)

			attempts := 0
			testErr := &testError{kind: tc.kind}
			next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
				attempts++
				return &runtime.Result{Error: testErr}, nil
			}

			result, _ := interceptor.Handle(ctx, task, next)

			assert.NotNil(t, result)
			assert.Equal(t, testErr, result.Error)
			assert.Equal(t, 1, attempts, "Should not retry non-retryable error")
		})
	}
}

func TestInterceptor_RetryableFlagFalse(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	task := makeTask(opts)

	attempts := 0
	testErr := &testError{kind: apierror.KindUnavailable, retryable: apierror.False}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{Error: testErr}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.Equal(t, testErr, result.Error)
	assert.Equal(t, 1, attempts, "Should not retry when Retryable is False")
}

func TestInterceptor_UnknownError(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	task := makeTask(opts)

	attempts := 0
	testErr := errors.New("unknown error")
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts < 2 {
			return &runtime.Result{Error: testErr}, nil
		}
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 2, attempts, "Unknown errors should be retryable")
}

func TestInterceptor_FixedBackoff(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 50})
	task := makeTask(opts)

	attempts := 0
	var timestamps []time.Time
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, nil
	}

	start := time.Now()
	_, _ = interceptor.Handle(ctx, task, next)
	duration := time.Since(start)

	assert.Equal(t, 3, attempts)
	require.Len(t, timestamps, 3)

	delay1 := timestamps[1].Sub(timestamps[0])
	delay2 := timestamps[2].Sub(timestamps[1])

	assert.GreaterOrEqual(t, delay1.Milliseconds(), int64(50), "First retry should wait at least 50ms")
	assert.GreaterOrEqual(t, delay2.Milliseconds(), int64(50), "Second retry should wait at least 50ms")

	expectedTotal := 100
	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(expectedTotal), "Total duration should include backoff")
}

func TestInterceptor_DefaultBackoff(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 2, BackoffMs: 0})
	task := makeTask(opts)

	attempts := 0
	var timestamps []time.Time
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, nil
	}

	start := time.Now()
	_, _ = interceptor.Handle(ctx, task, next)
	duration := time.Since(start)

	assert.Equal(t, 2, attempts)
	require.Len(t, timestamps, 2)

	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(100), "Should use default 100ms backoff")
}

func TestInterceptor_ContextCancellation(t *testing.T) {
	interceptor := New()
	ctx, cancel := context.WithCancel(context.Background())

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 10, BackoffMs: 100})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts == 2 {
			cancel()
		}
		return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.Error(t, result.Error)
	assert.Equal(t, context.Canceled, result.Error)
	assert.LessOrEqual(t, attempts, 3, "Should stop retrying after context cancellation")
}

func TestInterceptor_ContextCancelledBeforeStart(t *testing.T) {
	interceptor := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := runtime.Bag{}
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.Error(t, result.Error)
	assert.Equal(t, context.Canceled, result.Error)
	assert.Equal(t, 0, attempts, "Should not call next if context already canceled")
}

func TestInterceptor_WithRetryKinds(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{
		"max_attempts": 3,
		"backoff_ms":   10,
		"retry_kinds":  []string{"Unavailable", "Timeout"},
	})
	task := makeTask(opts)

	// Test that Unavailable is retried
	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts < 2 {
			return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, nil
		}
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)
	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 2, attempts, "Unavailable should be retried")

	// Test that Invalid is NOT retried (not in retry_kinds)
	attempts = 0
	testErr := &testError{kind: apierror.KindInvalid}
	next = func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{Error: testErr}, nil
	}

	result, _ = interceptor.Handle(ctx, task, next)
	assert.NotNil(t, result)
	assert.Equal(t, testErr, result.Error)
	assert.Equal(t, 1, attempts, "Invalid should not be retried when not in retry_kinds")
}

func TestInterceptor_WithSkipKinds(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{
		"max_attempts": 3,
		"backoff_ms":   10,
		"skip_kinds":   []string{"Timeout"},
	})
	task := makeTask(opts)

	// Test that Unavailable is retried (not in skip_kinds)
	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts < 2 {
			return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, nil
		}
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)
	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 2, attempts, "Unavailable should be retried")

	// Test that Timeout is NOT retried (in skip_kinds)
	attempts = 0
	testErr := &testError{kind: apierror.KindTimeout}
	next = func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{Error: testErr}, nil
	}

	result, _ = interceptor.Handle(ctx, task, next)
	assert.NotNil(t, result)
	assert.Equal(t, testErr, result.Error)
	assert.Equal(t, 1, attempts, "Timeout should not be retried when in skip_kinds")
}
