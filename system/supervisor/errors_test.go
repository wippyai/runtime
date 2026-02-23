// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"errors"
	"testing"
	"time"

	apierror "github.com/wippyai/runtime/api/error"

	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	cause := errors.New("test cause")

	t.Run("NewSubscriberError", func(t *testing.T) {
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create event subscriber")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.Equal(t, apierror.True, err.Retryable())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("NewDependencyResolveError", func(t *testing.T) {
		err := NewDependencyResolveError("my-service", cause)
		assert.Contains(t, err.Error(), "resolve dependencies")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.True(t, errors.Is(err, cause))
		serviceID, _ := err.Details().Get("service_id")
		assert.Equal(t, "my-service", serviceID)
	})

	t.Run("NewStartOperationsError", func(t *testing.T) {
		err := NewStartOperationsError(cause)
		assert.Contains(t, err.Error(), "start operations")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewTransitionError", func(t *testing.T) {
		err := NewTransitionError(cause)
		assert.Contains(t, err.Error(), "transitions")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewStopError", func(t *testing.T) {
		err := NewStopError(cause)
		assert.Contains(t, err.Error(), "stop service")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewSupervisorStoppedError", func(t *testing.T) {
		err := NewSupervisorStoppedError(cause)
		assert.Contains(t, err.Error(), "supervisor is stopped")
		assert.Equal(t, apierror.Unavailable, err.Kind())
	})

	t.Run("NewStopTimeoutError", func(t *testing.T) {
		err := NewStopTimeoutError(5 * time.Second)
		assert.Contains(t, err.Error(), "timed out")
		assert.Equal(t, apierror.Timeout, err.Kind())
		timeout, _ := err.Details().Get("timeout")
		assert.Equal(t, "5s", timeout)
	})

	t.Run("NewServiceStartError", func(t *testing.T) {
		err := NewServiceStartError("my-service", cause)
		assert.Contains(t, err.Error(), "start service")
		assert.Equal(t, apierror.Internal, err.Kind())
		assert.Equal(t, apierror.True, err.Retryable())
		serviceID, _ := err.Details().Get("service_id")
		assert.Equal(t, "my-service", serviceID)
	})

	t.Run("NewServiceStopError", func(t *testing.T) {
		err := NewServiceStopError("my-service", cause)
		assert.Contains(t, err.Error(), "stop service")
		assert.Equal(t, apierror.Internal, err.Kind())
		serviceID, _ := err.Details().Get("service_id")
		assert.Equal(t, "my-service", serviceID)
	})

	t.Run("NewStartSequenceError", func(t *testing.T) {
		err := NewStartSequenceError(cause)
		assert.Contains(t, err.Error(), "start sequence")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewStopSequenceError", func(t *testing.T) {
		err := NewStopSequenceError(cause)
		assert.Contains(t, err.Error(), "stop sequence")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewDependencyLevelsError", func(t *testing.T) {
		err := NewDependencyLevelsError("start", cause)
		assert.Contains(t, err.Error(), "start")
		assert.Contains(t, err.Error(), "dependency levels")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewMultiStartError", func(t *testing.T) {
		err := NewMultiStartError(3, cause)
		assert.Contains(t, err.Error(), "multiple services")
		assert.Equal(t, apierror.Internal, err.Kind())
		val, ok := err.Details().Get("failed_count")
		assert.True(t, ok)
		assert.Equal(t, 3, val)
	})

	t.Run("NewMultiStopError", func(t *testing.T) {
		err := NewMultiStopError(2, cause)
		assert.Contains(t, err.Error(), "multiple services")
		assert.Equal(t, apierror.Internal, err.Kind())
	})

	t.Run("NewCommitRemoveError", func(t *testing.T) {
		err := NewCommitRemoveError("my-service", cause)
		assert.Contains(t, err.Error(), "remove")
		assert.Equal(t, apierror.Internal, err.Kind())
		serviceID, _ := err.Details().Get("service_id")
		assert.Equal(t, "my-service", serviceID)
	})

	t.Run("NewCommitRegisterError", func(t *testing.T) {
		err := NewCommitRegisterError("my-service", cause)
		assert.Contains(t, err.Error(), "register")
		assert.Equal(t, apierror.Internal, err.Kind())
		serviceID, _ := err.Details().Get("service_id")
		assert.Equal(t, "my-service", serviceID)
	})
}
