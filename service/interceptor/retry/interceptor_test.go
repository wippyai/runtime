package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
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

func (e *testError) Details() map[string]any {
	return nil
}

func TestInterceptor_NoOptions(t *testing.T) {
	interceptor := New()
	ctx := context.Background()

	called := false
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		called = true
		return &runtime.Result{}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestInterceptor_NoRetryOption(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	_ = apiinterceptor.SetOptions(ctx, opts)

	called := false
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		called = true
		return &runtime.Result{}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestInterceptor_MaxAttemptsZero(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 0})
	_ = apiinterceptor.SetOptions(ctx, opts)

	called := false
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		called = true
		return &runtime.Result{}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestInterceptor_SuccessFirstAttempt(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		return &runtime.Result{}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 1, attempts)
}

func TestInterceptor_RetryableError(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		if attempts < 3 {
			return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, ctx
		}
		return &runtime.Result{}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 3, attempts)
}

func TestInterceptor_MaxAttemptsReached(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	testErr := &testError{kind: apierror.KindUnavailable}
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		return &runtime.Result{Error: testErr}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

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
			rootCtx := ctxapi.NewRootContext()
			ctx, _ := ctxapi.OpenFrameContext(rootCtx)

			opts := apiinterceptor.NewBag()
			opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
			_ = apiinterceptor.SetOptions(ctx, opts)

			attempts := 0
			testErr := &testError{kind: tc.kind}
			next := func(ctx context.Context) (*runtime.Result, context.Context) {
				attempts++
				return &runtime.Result{Error: testErr}, ctx
			}

			result, _ := interceptor.Handle(ctx, next)

			assert.NotNil(t, result)
			assert.Equal(t, testErr, result.Error)
			assert.Equal(t, 1, attempts, "Should not retry non-retryable error")
		})
	}
}

func TestInterceptor_RetryableFlagFalse(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	testErr := &testError{kind: apierror.KindUnavailable, retryable: apierror.False}
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		return &runtime.Result{Error: testErr}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.NotNil(t, result)
	assert.Equal(t, testErr, result.Error)
	assert.Equal(t, 1, attempts, "Should not retry when Retryable is False")
}

func TestInterceptor_UnknownError(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 1})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	testErr := errors.New("unknown error")
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		if attempts < 2 {
			return &runtime.Result{Error: testErr}, ctx
		}
		return &runtime.Result{}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.NotNil(t, result)
	assert.Nil(t, result.Error)
	assert.Equal(t, 2, attempts, "Unknown errors should be retryable")
}

func TestInterceptor_ExponentialBackoff(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3, BackoffMs: 50})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	timestamps := []time.Time{}
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, ctx
	}

	start := time.Now()
	interceptor.Handle(ctx, next)
	duration := time.Since(start)

	assert.Equal(t, 3, attempts)
	require.Len(t, timestamps, 3)

	delay1 := timestamps[1].Sub(timestamps[0])
	delay2 := timestamps[2].Sub(timestamps[1])

	assert.GreaterOrEqual(t, delay1.Milliseconds(), int64(50), "First retry should wait at least 50ms")
	assert.GreaterOrEqual(t, delay2.Milliseconds(), int64(100), "Second retry should wait at least 100ms")

	expectedTotal := 50 + 100
	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(expectedTotal), "Total duration should include backoff")
}

func TestInterceptor_DefaultBackoff(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 2, BackoffMs: 0})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	timestamps := []time.Time{}
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		timestamps = append(timestamps, time.Now())
		return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, ctx
	}

	start := time.Now()
	interceptor.Handle(ctx, next)
	duration := time.Since(start)

	assert.Equal(t, 2, attempts)
	require.Len(t, timestamps, 2)

	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(100), "Should use default 100ms backoff")
}

func TestInterceptor_ContextCancellation(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	ctx, cancel := context.WithCancel(ctx)

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 10, BackoffMs: 100})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		if attempts == 2 {
			cancel()
		}
		return &runtime.Result{Error: &testError{kind: apierror.KindUnavailable}}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.NotNil(t, result)
	assert.Error(t, result.Error)
	assert.Equal(t, context.Canceled, result.Error)
	assert.LessOrEqual(t, attempts, 3, "Should stop retrying after context cancellation")
}

func TestInterceptor_ContextCancelledBeforeStart(t *testing.T) {
	interceptor := New()
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	opts := apiinterceptor.NewBag()
	opts.Set("retry", &retryapi.Options{MaxAttempts: 3})
	_ = apiinterceptor.SetOptions(ctx, opts)

	attempts := 0
	next := func(ctx context.Context) (*runtime.Result, context.Context) {
		attempts++
		return &runtime.Result{}, ctx
	}

	result, _ := interceptor.Handle(ctx, next)

	assert.NotNil(t, result)
	assert.Error(t, result.Error)
	assert.Equal(t, context.Canceled, result.Error)
	assert.Equal(t, 0, attempts, "Should not call next if context already cancelled")
}
