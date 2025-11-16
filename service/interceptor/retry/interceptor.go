package retry

import (
	"context"
	"errors"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/runtime"
	retryapi "github.com/wippyai/runtime/api/service/interceptor/retry"
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
	// Get options from task
	if task.Options == nil {
		return next(ctx, task)
	}

	opts, ok := task.Options.(runtime.Bag)
	if !ok {
		return next(ctx, task)
	}

	val, ok := opts.Get("retry")
	if !ok {
		return next(ctx, task)
	}

	// Parse retry options from map or struct
	var maxAttempts int
	var backoff time.Duration
	var retryKinds []string
	var skipKinds []string

	switch v := val.(type) {
	case *retryapi.Options:
		maxAttempts = v.MaxAttempts
		if maxAttempts == 0 {
			return next(ctx, task)
		}
		backoffMs := v.BackoffMs
		if backoffMs == 0 {
			backoffMs = defaultBackoffMs
		}
		backoff = time.Duration(backoffMs) * time.Millisecond
		retryKinds = v.RetryKinds
		skipKinds = v.SkipKinds

	case map[string]any:
		if ma, ok := v[optionKeyMaxAttempts]; ok {
			if maInt, ok := ma.(int); ok {
				maxAttempts = maInt
			} else if maFloat, ok := ma.(float64); ok {
				maxAttempts = int(maFloat)
			}
		}
		if maxAttempts == 0 {
			return next(ctx, task)
		}
		backoffMs := defaultBackoffMs
		if bm, ok := v[optionKeyBackoffMs]; ok {
			if bmInt, ok := bm.(int); ok {
				backoffMs = bmInt
			} else if bmFloat, ok := bm.(float64); ok {
				backoffMs = int(bmFloat)
			}
		}
		backoff = time.Duration(backoffMs) * time.Millisecond

		bag := runtime.Bag(v)
		retryKinds = bag.GetSlice(optionKeyRetryKinds)
		skipKinds = bag.GetSlice(optionKeySkipKinds)

	default:
		return next(ctx, task)
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return &runtime.Result{Error: ctx.Err()}, ctx.Err()
		default:
			result, err := next(ctx, task)

			if err == nil && (result == nil || result.Error == nil) {
				return result, nil
			}

			// Determine the error to check
			checkErr := err
			if checkErr == nil && result != nil {
				checkErr = result.Error
			}

			// Check if error is retryable
			if !i.isRetryable(checkErr, retryKinds, skipKinds) {
				return result, err
			}

			// If this was the last attempt, return the error
			if attempt >= maxAttempts-1 {
				return result, err
			}

			select {
			case <-ctx.Done():
				return &runtime.Result{Error: ctx.Err()}, ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}
	}

	// Should never reach here, but return error if we do
	return &runtime.Result{Error: errors.New("max retry attempts exceeded")}, errors.New("max retry attempts exceeded")
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
	targetStr := target.String()
	for _, k := range kinds {
		if k == targetStr {
			return true
		}
	}
	return false
}
