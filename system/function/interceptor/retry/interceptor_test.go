// SPDX-License-Identifier: MPL-2.0

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
		ID:      registry.NewID("", "test"),
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
	opts.Set("retry", map[string]any{"max_attempts": 0})
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
	opts.Set("retry", map[string]any{"max_attempts": 3})
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
	opts.Set("retry", map[string]any{"max_attempts": 3, "initial_delay": 1, "jitter": 0.0})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts < 3 {
			return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
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
	opts.Set("retry", map[string]any{"max_attempts": 3, "initial_delay": 1, "jitter": 0.0})
	task := makeTask(opts)

	attempts := 0
	testErr := &testError{kind: apierror.Unavailable}
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
		{"Invalid", apierror.Invalid},
		{"PermissionDenied", apierror.PermissionDenied},
		{"Internal", apierror.Internal},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			interceptor := New()
			ctx := context.Background()

			opts := runtime.Bag{}
			opts.Set("retry", map[string]any{"max_attempts": 3, "initial_delay": 1})
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
	opts.Set("retry", map[string]any{"max_attempts": 3, "initial_delay": 1})
	task := makeTask(opts)

	attempts := 0
	testErr := &testError{kind: apierror.Unavailable, retryable: apierror.False}
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
	opts.Set("retry", map[string]any{"max_attempts": 3, "initial_delay": 1, "jitter": 0.0})
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

func TestInterceptor_ExponentialBackoff(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{
		"max_attempts":   4,
		"initial_delay":  50,
		"backoff_factor": 2.0,
		"jitter":         0.0,
	})
	task := makeTask(opts)

	attempts := 0
	var timestamps []time.Time
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
	}

	start := time.Now()
	_, _ = interceptor.Handle(ctx, task, next)
	duration := time.Since(start)

	assert.Equal(t, 4, attempts)
	require.Len(t, timestamps, 4)

	delay1 := timestamps[1].Sub(timestamps[0])
	delay2 := timestamps[2].Sub(timestamps[1])
	delay3 := timestamps[3].Sub(timestamps[2])

	assert.GreaterOrEqual(t, delay1.Milliseconds(), int64(45), "First retry delay")
	assert.GreaterOrEqual(t, delay2.Milliseconds(), int64(90), "Second retry delay (2x)")
	assert.GreaterOrEqual(t, delay3.Milliseconds(), int64(180), "Third retry delay (4x)")

	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(300), "Total duration should include backoff")
}

func TestInterceptor_MaxDelayLimit(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{
		"max_attempts":   4,
		"initial_delay":  50,
		"max_delay":      100,
		"backoff_factor": 10.0,
		"jitter":         0.0,
	})
	task := makeTask(opts)

	attempts := 0
	var timestamps []time.Time
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
	}

	_, _ = interceptor.Handle(ctx, task, next)

	assert.Equal(t, 4, attempts)
	require.Len(t, timestamps, 4)

	delay2 := timestamps[2].Sub(timestamps[1])
	delay3 := timestamps[3].Sub(timestamps[2])

	assert.LessOrEqual(t, delay2.Milliseconds(), int64(120), "Delay should be capped at max_delay")
	assert.LessOrEqual(t, delay3.Milliseconds(), int64(120), "Delay should be capped at max_delay")
}

func TestInterceptor_DefaultBackoff(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{"max_attempts": 2, "jitter": 0.0})
	task := makeTask(opts)

	attempts := 0
	var timestamps []time.Time
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
	}

	start := time.Now()
	_, _ = interceptor.Handle(ctx, task, next)
	duration := time.Since(start)

	assert.Equal(t, 2, attempts)
	require.Len(t, timestamps, 2)

	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(90), "Should use default 100ms initial delay")
}

func TestInterceptor_DurationStringParsing(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{
		"max_attempts":  2,
		"initial_delay": "50ms",
		"max_delay":     "1s",
		"jitter":        0.0,
	})
	task := makeTask(opts)

	attempts := 0
	var timestamps []time.Time
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
	}

	start := time.Now()
	_, _ = interceptor.Handle(ctx, task, next)
	duration := time.Since(start)

	assert.Equal(t, 2, attempts)
	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(45), "Should parse duration string")
}

func TestInterceptor_ContextCancellation(t *testing.T) {
	interceptor := New()
	ctx, cancel := context.WithCancel(context.Background())

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{"max_attempts": 10, "initial_delay": 100})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts == 2 {
			cancel()
		}
		return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
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
	opts.Set("retry", map[string]any{"max_attempts": 3})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.NoError(t, result.Error)
	assert.Equal(t, 1, attempts, "Should execute first call regardless of context")
}

func TestInterceptor_WithRetryKinds(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	opts := runtime.Bag{}
	opts.Set("retry", map[string]any{
		"max_attempts":  3,
		"initial_delay": 1,
		"jitter":        0.0,
		"retry_kinds":   []string{"Unavailable", "Timeout"},
	})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts < 2 {
			return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
		}
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)
	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 2, attempts, "Unavailable should be retried")

	attempts = 0
	testErr := &testError{kind: apierror.Invalid}
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
		"max_attempts":  3,
		"initial_delay": 1,
		"jitter":        0.0,
		"skip_kinds":    []string{"Timeout"},
	})
	task := makeTask(opts)

	attempts := 0
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		if attempts < 2 {
			return &runtime.Result{Error: &testError{kind: apierror.Unavailable}}, nil
		}
		return &runtime.Result{}, nil
	}

	result, _ := interceptor.Handle(ctx, task, next)
	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 2, attempts, "Unavailable should be retried")

	attempts = 0
	testErr := &testError{kind: apierror.Timeout}
	next = func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		attempts++
		return &runtime.Result{Error: testErr}, nil
	}

	result, _ = interceptor.Handle(ctx, task, next)
	assert.NotNil(t, result)
	assert.Equal(t, testErr, result.Error)
	assert.Equal(t, 1, attempts, "Timeout should not be retried when in skip_kinds")
}

func TestInterceptor_ParsePolicy(t *testing.T) {
	tests := []struct {
		opts     map[string]any
		name     string
		expected struct {
			maxAttempts   int
			initialDelay  time.Duration
			maxDelay      time.Duration
			backoffFactor float64
			jitter        float64
		}
	}{
		{
			name: "all defaults",
			opts: map[string]any{},
			expected: struct {
				maxAttempts   int
				initialDelay  time.Duration
				maxDelay      time.Duration
				backoffFactor float64
				jitter        float64
			}{0, defaultInitialDelay, defaultMaxDelay, defaultBackoffFactor, defaultJitter},
		},
		{
			name: "int values",
			opts: map[string]any{
				"max_attempts":   5,
				"initial_delay":  200,
				"max_delay":      5000,
				"backoff_factor": 3,
				"jitter":         0,
			},
			expected: struct {
				maxAttempts   int
				initialDelay  time.Duration
				maxDelay      time.Duration
				backoffFactor float64
				jitter        float64
			}{5, 200 * time.Millisecond, 5000 * time.Millisecond, 3.0, 0.0},
		},
		{
			name: "float64 values",
			opts: map[string]any{
				"max_attempts":   float64(4),
				"initial_delay":  float64(150),
				"max_delay":      float64(3000),
				"backoff_factor": 2.5,
				"jitter":         0.2,
			},
			expected: struct {
				maxAttempts   int
				initialDelay  time.Duration
				maxDelay      time.Duration
				backoffFactor float64
				jitter        float64
			}{4, 150 * time.Millisecond, 3000 * time.Millisecond, 2.5, 0.2},
		},
		{
			name: "string duration values",
			opts: map[string]any{
				"max_attempts":  3,
				"initial_delay": "500ms",
				"max_delay":     "30s",
			},
			expected: struct {
				maxAttempts   int
				initialDelay  time.Duration
				maxDelay      time.Duration
				backoffFactor float64
				jitter        float64
			}{3, 500 * time.Millisecond, 30 * time.Second, defaultBackoffFactor, defaultJitter},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := parsePolicy(tt.opts)
			assert.Equal(t, tt.expected.maxAttempts, policy.MaxAttempts)
			assert.Equal(t, tt.expected.initialDelay, policy.InitialDelay)
			assert.Equal(t, tt.expected.maxDelay, policy.MaxDelay)
			assert.Equal(t, tt.expected.backoffFactor, policy.BackoffFactor)
			assert.Equal(t, tt.expected.jitter, policy.Jitter)
		})
	}
}
