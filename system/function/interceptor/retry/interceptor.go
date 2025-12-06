package retry

import (
	"context"
	"errors"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/runtime"
	"go.uber.org/zap"
)

const (
	defaultBackoffMs = 100

	optionKeyMaxAttempts = "max_attempts"
	optionKeyBackoffMs   = "backoff_ms"
	optionKeyRetryKinds  = "retry_kinds"
	optionKeySkipKinds   = "skip_kinds"
)

// Interceptor implements retry functionality
type Interceptor struct {
	logger *zap.Logger
}

// New creates a new retry interceptor
func New() *Interceptor {
	return &Interceptor{}
}

// NewWithLogger creates a new retry interceptor with logger
func NewWithLogger(logger *zap.Logger) *Interceptor {
	return &Interceptor{logger: logger}
}

// Handle implements the interceptor interface
func (i *Interceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	// Execute first - only check retry config if there's an error
	result, err := next(ctx, task)

	// Fast path: no error = no retry needed
	if err == nil && (result == nil || result.Error == nil) {
		return result, nil
	}

	// Check if error is explicitly non-retryable before parsing options
	checkErr := err
	if checkErr == nil && result != nil {
		checkErr = result.Error
	}

	// Context errors are never retryable
	if errors.Is(checkErr, context.Canceled) || errors.Is(checkErr, context.DeadlineExceeded) {
		return result, err
	}

	// API errors marked non-retryable skip option parsing
	var apiErr apierror.Error
	if errors.As(checkErr, &apiErr) && apiErr.Retryable() == apierror.False {
		return result, err
	}

	// Now check if retry is configured
	if task.Options == nil {
		return result, err
	}

	opts, ok := task.Options.(runtime.Bag)
	if !ok {
		return result, err
	}

	val, ok := opts["retry"]
	if !ok {
		return result, err
	}

	// Parse retry options
	retryOpts, ok := val.(map[string]any)
	if !ok {
		return result, err
	}

	var maxAttempts int
	if ma, ok := retryOpts[optionKeyMaxAttempts]; ok {
		if maInt, ok := ma.(int); ok {
			maxAttempts = maInt
		} else if maFloat, ok := ma.(float64); ok {
			maxAttempts = int(maFloat)
		}
	}
	if maxAttempts <= 1 {
		return result, err
	}

	backoffMs := defaultBackoffMs
	if bm, ok := retryOpts[optionKeyBackoffMs]; ok {
		if bmInt, ok := bm.(int); ok && bmInt > 0 {
			backoffMs = bmInt
		} else if bmFloat, ok := bm.(float64); ok && bmFloat > 0 {
			backoffMs = int(bmFloat)
		}
	}
	backoff := time.Duration(backoffMs) * time.Millisecond

	bag := runtime.Bag(retryOpts)
	retryKinds := bag.GetSlice(optionKeyRetryKinds)
	skipKinds := bag.GetSlice(optionKeySkipKinds)

	// Check if error is retryable
	if !i.isRetryable(checkErr, retryKinds, skipKinds) {
		return result, err
	}

	// Retry loop (we already did attempt 0 above)
	for attempt := 1; attempt < maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return &runtime.Result{Error: ctx.Err()}, ctx.Err()
		case <-time.After(backoff):
		}

		result, err = next(ctx, task)

		if err == nil && (result == nil || result.Error == nil) {
			return result, nil
		}

		checkErr = err
		if checkErr == nil && result != nil {
			checkErr = result.Error
		}

		if !i.isRetryable(checkErr, retryKinds, skipKinds) {
			return result, err
		}
	}

	return result, err
}

// isRetryable checks if an error should be retried
func (i *Interceptor) isRetryable(err error, retryKinds, skipKinds []string) bool {
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		// Unknown errors are retryable unless skip_kinds includes "Unknown"
		if len(skipKinds) > 0 {
			return !i.containsKind(skipKinds, apierror.KindUnknown)
		}
		// If retry_kinds is specified, unknown errors are only retryable if "Unknown" is in the list
		if len(retryKinds) > 0 {
			return i.containsKind(retryKinds, apierror.KindUnknown)
		}
		return true
	}

	// Check Retryable flag first
	if apiErr.Retryable() == apierror.False {
		return false
	}

	kind := apiErr.Kind()

	// If skip_kinds is specified, check if this kind should be skipped
	if len(skipKinds) > 0 && i.containsKind(skipKinds, kind) {
		return false
	}

	// If retry_kinds is specified, only retry if kind is in the list
	if len(retryKinds) > 0 {
		return i.containsKind(retryKinds, kind)
	}

	// Default behavior: skip certain error kinds
	switch kind {
	case apierror.KindInvalid,
		apierror.KindPermissionDenied,
		apierror.KindInternal:
		return false
	case apierror.KindUnknown,
		apierror.KindNotFound,
		apierror.KindAlreadyExists,
		apierror.KindUnavailable,
		apierror.KindCanceled,
		apierror.KindConflict,
		apierror.KindTimeout,
		apierror.KindRateLimited:
		return true
	default:
		return true
	}
}

// containsKind checks if a kind is in the list
func (i *Interceptor) containsKind(kinds []string, target apierror.Kind) bool {
	targetStr := string(target)
	for _, k := range kinds {
		if k == targetStr {
			return true
		}
	}
	return false
}
