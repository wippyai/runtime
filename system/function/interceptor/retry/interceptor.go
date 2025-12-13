package retry

import (
	"context"
	"errors"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/backoff"
	"go.uber.org/zap"
)

const (
	optionKeyMaxAttempts   = "max_attempts"
	optionKeyInitialDelay  = "initial_delay"
	optionKeyMaxDelay      = "max_delay"
	optionKeyBackoffFactor = "backoff_factor"
	optionKeyJitter        = "jitter"
	optionKeyRetryKinds    = "retry_kinds"
	optionKeySkipKinds     = "skip_kinds"

	defaultInitialDelay  = 100 * time.Millisecond
	defaultMaxDelay      = 10 * time.Second
	defaultBackoffFactor = 2.0
	defaultJitter        = 0.1
)

// Interceptor implements retry functionality with exponential backoff.
type Interceptor struct {
	logger *zap.Logger
}

// New creates a new retry interceptor.
func New() *Interceptor {
	return &Interceptor{}
}

// NewWithLogger creates a new retry interceptor with logger.
func NewWithLogger(logger *zap.Logger) *Interceptor {
	return &Interceptor{logger: logger}
}

// Handle implements the interceptor interface.
func (i *Interceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	result, err := next(ctx, task)

	if err == nil && (result == nil || result.Error == nil) {
		return result, nil
	}

	checkErr := err
	if checkErr == nil && result != nil {
		checkErr = result.Error
	}

	if errors.Is(checkErr, context.Canceled) || errors.Is(checkErr, context.DeadlineExceeded) {
		return result, err
	}

	var apiErr apierror.Error
	if errors.As(checkErr, &apiErr) && apiErr.Retryable() == apierror.False {
		return result, err
	}

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

	retryOpts, ok := val.(map[string]any)
	if !ok {
		return result, err
	}

	policy := i.parsePolicy(retryOpts)
	if policy.MaxAttempts <= 1 {
		return result, err
	}

	bag := runtime.Bag(retryOpts)
	retryKinds := bag.GetSlice(optionKeyRetryKinds)
	skipKinds := bag.GetSlice(optionKeySkipKinds)

	if !i.isRetryable(checkErr, retryKinds, skipKinds) {
		return result, err
	}

	calc := backoff.NewCalculator(policy)
	// We already did attempt 0, so consume the first interval
	_ = calc.NextInterval()

	for attempt := 1; attempt < policy.MaxAttempts; attempt++ {
		interval := calc.NextInterval()
		if interval == 0 {
			break
		}

		select {
		case <-ctx.Done():
			return &runtime.Result{Error: ctx.Err()}, ctx.Err()
		case <-time.After(interval):
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

func (i *Interceptor) parsePolicy(opts map[string]any) supervisor.RetryPolicy {
	policy := supervisor.RetryPolicy{
		InitialDelay:  defaultInitialDelay,
		MaxDelay:      defaultMaxDelay,
		BackoffFactor: defaultBackoffFactor,
		Jitter:        defaultJitter,
		MaxAttempts:   0,
	}

	if ma, ok := opts[optionKeyMaxAttempts]; ok {
		switch v := ma.(type) {
		case int:
			policy.MaxAttempts = v
		case float64:
			policy.MaxAttempts = int(v)
		}
	}

	if id, ok := opts[optionKeyInitialDelay]; ok {
		switch v := id.(type) {
		case int:
			policy.InitialDelay = time.Duration(v) * time.Millisecond
		case float64:
			policy.InitialDelay = time.Duration(v) * time.Millisecond
		case string:
			if d, err := time.ParseDuration(v); err == nil {
				policy.InitialDelay = d
			}
		}
	}

	if md, ok := opts[optionKeyMaxDelay]; ok {
		switch v := md.(type) {
		case int:
			policy.MaxDelay = time.Duration(v) * time.Millisecond
		case float64:
			policy.MaxDelay = time.Duration(v) * time.Millisecond
		case string:
			if d, err := time.ParseDuration(v); err == nil {
				policy.MaxDelay = d
			}
		}
	}

	if bf, ok := opts[optionKeyBackoffFactor]; ok {
		switch v := bf.(type) {
		case float64:
			policy.BackoffFactor = v
		case int:
			policy.BackoffFactor = float64(v)
		}
	}

	if j, ok := opts[optionKeyJitter]; ok {
		switch v := j.(type) {
		case float64:
			policy.Jitter = v
		case int:
			policy.Jitter = float64(v)
		}
	}

	return policy
}

func (i *Interceptor) isRetryable(err error, retryKinds, skipKinds []string) bool {
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		if len(skipKinds) > 0 {
			return !containsKind(skipKinds, apierror.KindUnknown)
		}
		if len(retryKinds) > 0 {
			return containsKind(retryKinds, apierror.KindUnknown)
		}
		return true
	}

	if apiErr.Retryable() == apierror.False {
		return false
	}

	kind := apiErr.Kind()

	if len(skipKinds) > 0 && containsKind(skipKinds, kind) {
		return false
	}

	if len(retryKinds) > 0 {
		return containsKind(retryKinds, kind)
	}

	switch kind {
	case apierror.KindInvalid,
		apierror.KindPermissionDenied,
		apierror.KindInternal:
		return false
	default:
		return true
	}
}

func containsKind(kinds []string, target apierror.Kind) bool {
	targetStr := string(target)
	for _, k := range kinds {
		if k == targetStr {
			return true
		}
	}
	return false
}
