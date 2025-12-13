package retry

import (
	"context"
	"errors"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/backoff"
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
type Interceptor struct{}

// New creates a new retry interceptor.
func New() *Interceptor {
	return &Interceptor{}
}

// Handle implements the interceptor interface.
func (i *Interceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	result, err := next(ctx, task)

	checkErr := extractError(result, err)
	if checkErr == nil {
		return result, nil
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

	policy := parsePolicy(retryOpts)
	if policy.MaxAttempts <= 1 {
		return result, err
	}

	bag := runtime.Bag(retryOpts)
	retryKinds := bag.GetSlice(optionKeyRetryKinds)
	skipKinds := bag.GetSlice(optionKeySkipKinds)

	if !isRetryable(checkErr, retryKinds, skipKinds) {
		return result, err
	}

	calc := backoff.NewCalculator(policy)
	_ = calc.NextInterval() // consume first interval (attempt 0 already done)

	for attempt := 1; attempt < policy.MaxAttempts; attempt++ {
		interval := calc.NextInterval()
		if interval == 0 {
			break
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &runtime.Result{Error: ctx.Err()}, ctx.Err()
		case <-timer.C:
		}

		result, err = next(ctx, task)

		checkErr = extractError(result, err)
		if checkErr == nil {
			return result, nil
		}

		if !isRetryable(checkErr, retryKinds, skipKinds) {
			return result, err
		}
	}

	return result, err
}

// extractError returns the error from result or err, preferring err.
func extractError(result *runtime.Result, err error) error {
	if err != nil {
		return err
	}
	if result != nil && result.Error != nil {
		return result.Error
	}
	return nil
}

// parsePolicy extracts retry policy from options map.
func parsePolicy(opts map[string]any) supervisor.RetryPolicy {
	policy := supervisor.RetryPolicy{
		InitialDelay:  defaultInitialDelay,
		MaxDelay:      defaultMaxDelay,
		BackoffFactor: defaultBackoffFactor,
		Jitter:        defaultJitter,
		MaxAttempts:   0,
	}

	policy.MaxAttempts = getInt(opts, optionKeyMaxAttempts, 0)
	if d := getDuration(opts, optionKeyInitialDelay); d > 0 {
		policy.InitialDelay = d
	}
	if d := getDuration(opts, optionKeyMaxDelay); d > 0 {
		policy.MaxDelay = d
	}
	if f := getFloat(opts, optionKeyBackoffFactor); f > 0 {
		policy.BackoffFactor = f
	}
	if _, ok := opts[optionKeyJitter]; ok {
		policy.Jitter = getFloat(opts, optionKeyJitter)
	}

	return policy
}

func getInt(opts map[string]any, key string, def int) int {
	v, ok := opts[key]
	if !ok {
		return def
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	}
	return def
}

func getFloat(opts map[string]any, key string) float64 {
	v, ok := opts[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	}
	return 0
}

func getDuration(opts map[string]any, key string) time.Duration {
	v, ok := opts[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int:
		return time.Duration(val) * time.Millisecond
	case float64:
		return time.Duration(val) * time.Millisecond
	case string:
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return 0
}

func isRetryable(err error, retryKinds, skipKinds []string) bool {
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) {
		return isKindAllowed(apierror.KindUnknown, retryKinds, skipKinds)
	}

	if apiErr.Retryable() == apierror.False {
		return false
	}

	return isKindAllowed(apiErr.Kind(), retryKinds, skipKinds)
}

func isKindAllowed(kind apierror.Kind, retryKinds, skipKinds []string) bool {
	if len(skipKinds) > 0 && containsKind(skipKinds, kind) {
		return false
	}

	if len(retryKinds) > 0 {
		return containsKind(retryKinds, kind)
	}

	switch kind {
	case apierror.KindInvalid, apierror.KindPermissionDenied, apierror.KindInternal:
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
