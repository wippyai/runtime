package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"go.temporal.io/sdk/temporal"
)

func TestToApplicationError_ApiError(t *testing.T) {
	apiErr := apierror.New(apierror.PermissionDenied, "access denied").
		WithRetryable(apierror.False)

	appErr := ToApplicationError(apiErr)
	require.NotNil(t, appErr)

	var tAppErr *temporal.ApplicationError
	require.True(t, errors.As(appErr, &tAppErr))

	assert.Equal(t, "access denied", tAppErr.Message())
	assert.Equal(t, "PermissionDenied", tAppErr.Type())
	assert.True(t, tAppErr.NonRetryable())

	var chain apierror.Chain
	err := tAppErr.Details(&chain)
	require.NoError(t, err)
	require.Len(t, chain.Errors, 1)

	assert.Equal(t, "access denied", chain.Errors[0].Message)
	assert.Equal(t, "PermissionDenied", chain.Errors[0].Kind)
	require.NotNil(t, chain.Errors[0].Retryable)
	assert.False(t, *chain.Errors[0].Retryable)
}

func TestToApplicationError_RichError(t *testing.T) {
	richErr := apierror.NewRich(apierror.Invalid, "validation failed").
		WithRetryable(apierror.False).
		WithDetails(map[string]any{"field": "email"}).
		WithStack([]string{"handler.go:42 (validate)", "main.go:10 (main)"})

	appErr := ToApplicationError(richErr)
	require.NotNil(t, appErr)

	var tAppErr *temporal.ApplicationError
	require.True(t, errors.As(appErr, &tAppErr))

	assert.Equal(t, "validation failed", tAppErr.Message())
	assert.Equal(t, "Invalid", tAppErr.Type())
	assert.True(t, tAppErr.NonRetryable())

	var chain apierror.Chain
	err := tAppErr.Details(&chain)
	require.NoError(t, err)
	require.Len(t, chain.Errors, 1)

	assert.Equal(t, "validation failed", chain.Errors[0].Message)
	assert.Equal(t, "Invalid", chain.Errors[0].Kind)
	assert.Equal(t, "email", chain.Errors[0].Details["field"])
	require.Len(t, chain.Errors[0].Stack, 2)
	assert.Equal(t, "handler.go:42 (validate)", chain.Errors[0].Stack[0])
}

func TestToApplicationError_WrappedChain(t *testing.T) {
	innerErr := apierror.NewRich(apierror.NotFound, "resource not found").
		WithRetryable(apierror.False)

	outerErr := apierror.NewRich(apierror.Internal, "operation failed").
		WithCause(innerErr)

	appErr := ToApplicationError(outerErr)
	require.NotNil(t, appErr)

	var tAppErr *temporal.ApplicationError
	require.True(t, errors.As(appErr, &tAppErr))

	var chain apierror.Chain
	err := tAppErr.Details(&chain)
	require.NoError(t, err)
	require.Len(t, chain.Errors, 2)

	assert.Equal(t, "operation failed", chain.Errors[0].Message)
	assert.Equal(t, "Internal", chain.Errors[0].Kind)

	assert.Equal(t, "resource not found", chain.Errors[1].Message)
	assert.Equal(t, "NotFound", chain.Errors[1].Kind)
}

func TestFromTemporalError_ReconstructChain(t *testing.T) {
	innerErr := apierror.NewRich(apierror.Unavailable, "database connection failed").
		WithRetryable(apierror.True)

	outerErr := apierror.NewRich(apierror.Internal, "query failed").
		WithCause(innerErr)

	appErr := ToApplicationError(outerErr)

	result := FromTemporalError(appErr)
	require.NotNil(t, result)

	richErr, ok := result.(*apierror.RichError)
	require.True(t, ok, "expected *apierror.RichError, got %T", result)

	assert.Equal(t, "query failed", richErr.Msg())
	assert.Equal(t, apierror.Internal, richErr.Kind())

	inner := richErr.Unwrap()
	require.NotNil(t, inner)

	innerRich := &apierror.RichError{}
	ok = errors.As(inner, &innerRich)
	require.True(t, ok)
	assert.Equal(t, "database connection failed", innerRich.Msg())
	assert.Equal(t, apierror.Unavailable, innerRich.Kind())
	assert.Equal(t, apierror.True, innerRich.Retryable())
}

func TestFromTemporalError_CanceledError(t *testing.T) {
	canceledErr := temporal.NewCanceledError("user canceled")

	result := FromTemporalError(canceledErr)
	require.NotNil(t, result)

	richErr, ok := result.(*apierror.RichError)
	require.True(t, ok)
	assert.Equal(t, apierror.Canceled, richErr.Kind())
	assert.Equal(t, apierror.False, richErr.Retryable())
}

func TestFromTemporalError_TimeoutError(t *testing.T) {
	timeoutErr := temporal.NewTimeoutError(1, nil)

	result := FromTemporalError(timeoutErr)
	require.NotNil(t, result)

	richErr, ok := result.(*apierror.RichError)
	require.True(t, ok)
	assert.Equal(t, apierror.Timeout, richErr.Kind())
	assert.Equal(t, apierror.False, richErr.Retryable())

	details := richErr.Details()
	require.NotNil(t, details)
	assert.Equal(t, "start_to_close", details["timeout_type"])
}

func TestFromTemporalError_NoChainInDetails(t *testing.T) {
	appErr := temporal.NewApplicationError("simple error", "CustomType")

	result := FromTemporalError(appErr)
	require.NotNil(t, result)

	richErr, ok := result.(*apierror.RichError)
	require.True(t, ok)
	assert.Equal(t, "simple error", richErr.Error())
	assert.Equal(t, apierror.Unknown, richErr.Kind())
	assert.Equal(t, apierror.True, richErr.Retryable())
}

func TestRoundTrip_PreservesAllMetadata(t *testing.T) {
	original := apierror.NewRich(apierror.Conflict, "version mismatch").
		WithRetryable(apierror.False).
		WithDetails(map[string]any{
			"entity_id": "123",
			"version":   42,
		}).
		WithStack([]string{"store.go:100 (save)", "handler.go:50 (update)"})

	appErr := ToApplicationError(original)
	result := FromTemporalError(appErr)

	richErr, ok := result.(*apierror.RichError)
	require.True(t, ok)

	assert.Equal(t, "version mismatch", richErr.Error())
	assert.Equal(t, apierror.Conflict, richErr.Kind())
	assert.Equal(t, apierror.False, richErr.Retryable())

	details := richErr.Details()
	assert.Equal(t, "123", details["entity_id"])
	switch v := details["version"].(type) {
	case int:
		assert.Equal(t, 42, v)
	case float64:
		assert.Equal(t, float64(42), v)
	default:
		t.Errorf("unexpected version type: %T", details["version"])
	}

	stack := richErr.StackFrames()
	require.Len(t, stack, 2)
	assert.Equal(t, "store.go:100 (save)", stack[0])
	assert.Equal(t, "handler.go:50 (update)", stack[1])
}

func TestToApplicationError_Nil(t *testing.T) {
	result := ToApplicationError(nil)
	assert.Nil(t, result)
}

func TestFromTemporalError_Nil(t *testing.T) {
	result := FromTemporalError(nil)
	assert.Nil(t, result)
}

func TestFromTemporalError_UnknownError(t *testing.T) {
	unknownErr := errors.New("some unknown error")

	result := FromTemporalError(unknownErr)
	require.NotNil(t, result)

	richErr, ok := result.(*apierror.RichError)
	require.True(t, ok)
	assert.Equal(t, apierror.Internal, richErr.Kind())
}

func TestFromTemporalError_UnwrapsUnknownWrapper(t *testing.T) {
	appErr := temporal.NewApplicationError("child workflow intentional error", "NotFound")
	wrapped := errors.New("outer wrapper: " + appErr.Error())
	err := fmt.Errorf("%w: %w", wrapped, appErr)

	result := FromTemporalError(err)
	require.NotNil(t, result)

	richErr, ok := result.(*apierror.RichError)
	require.True(t, ok)
	assert.Equal(t, apierror.NotFound, richErr.Kind())
	assert.Contains(t, richErr.Error(), "intentional error")
}

func TestMapTypeToKind(t *testing.T) {
	tests := []struct {
		errType  string
		expected apierror.Kind
	}{
		{"NotFound", apierror.NotFound},
		{"AlreadyExists", apierror.AlreadyExists},
		{"Invalid", apierror.Invalid},
		{"PermissionDenied", apierror.PermissionDenied},
		{"Unavailable", apierror.Unavailable},
		{"Internal", apierror.Internal},
		{"Canceled", apierror.Canceled},
		{"Conflict", apierror.Conflict},
		{"Timeout", apierror.Timeout},
		{"RateLimited", apierror.RateLimited},
		{"UnknownType", apierror.Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.errType, func(t *testing.T) {
			result := mapTypeToKind(tt.errType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
