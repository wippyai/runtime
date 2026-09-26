// SPDX-License-Identifier: MPL-2.0

package boot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestReadiness_AddDoneWait(t *testing.T) {
	r := NewReadiness()
	require.NotNil(t, r)

	r.Add(2)
	assert.Equal(t, int64(2), r.Pending())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- r.Wait(context.Background())
	}()

	// Wait should still block with one pending item.
	r.Done()
	assert.Equal(t, int64(1), r.Pending())
	select {
	case <-doneCh:
		t.Fatal("wait returned before all readiness tasks completed")
	default:
	}

	r.Done()
	assert.Equal(t, int64(0), r.Pending())

	select {
	case err := <-doneCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("wait did not return after readiness reached zero")
	}
}

func TestReadiness_Track(t *testing.T) {
	r := NewReadiness()
	release := r.Track()
	assert.Equal(t, int64(1), r.Pending())
	release()
	assert.Equal(t, int64(0), r.Pending())
}

func TestReadiness_WaitCanceled(t *testing.T) {
	r := NewReadiness()
	r.Add(1)
	defer r.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := r.Wait(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestReadinessContext(t *testing.T) {
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)

	r := NewReadiness()
	ctx = WithReadiness(ctx, r)

	got := GetReadiness(ctx)
	require.NotNil(t, got)
	assert.Equal(t, r, got)
}

func TestReadiness_GateReady(t *testing.T) {
	r := NewReadiness()
	gate := r.RegisterGate("app:bootloader")
	require.NotNil(t, gate)
	assert.Equal(t, int64(1), r.Pending())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- r.Wait(context.Background())
	}()

	select {
	case <-doneCh:
		t.Fatal("wait returned while gate was still pending")
	default:
	}

	gate.Ready()
	assert.Equal(t, int64(0), r.Pending())

	select {
	case err := <-doneCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("wait timed out after gate became ready")
	}

	// Repeated Ready calls must be no-op and not double-decrement
	gate.Ready()
	assert.Equal(t, int64(0), r.Pending())
}

func TestReadiness_GateFail(t *testing.T) {
	r := NewReadiness()
	gate := r.RegisterGate("app:bootloader")
	require.NotNil(t, gate)
	assert.Equal(t, int64(1), r.Pending())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- r.Wait(context.Background())
	}()

	underlyingErr := assert.AnError
	gate.Fail(underlyingErr)
	assert.Equal(t, int64(0), r.Pending())

	select {
	case err := <-doneCh:
		require.Error(t, err)
		var gateErr *GateError
		require.True(t, errors.As(err, &gateErr), "error must be typed GateError")
		assert.Equal(t, "app:bootloader", gateErr.Service)
		assert.ErrorIs(t, err, underlyingErr)
	case <-time.After(time.Second):
		t.Fatal("wait timed out after gate failed")
	}

	// Repeated Fail calls must be no-op and not double-decrement
	gate.Fail(underlyingErr)
	assert.Equal(t, int64(0), r.Pending())
}

func TestReadiness_RestartNeverPassesFailedGate(t *testing.T) {
	r := NewReadiness()
	gate := r.RegisterGate("app:bootloader")
	require.NotNil(t, gate)

	// Initial run fails
	gate.Fail(assert.AnError)
	assert.Equal(t, int64(0), r.Pending())

	err := r.Wait(context.Background())
	require.Error(t, err)

	// Supervisor restarts the service and the retried run succeeds:
	// Gate state MUST remain Failed; Ready() must be a no-op!
	gate.Ready()
	assert.Equal(t, int64(0), r.Pending())

	// Wait still returns the original failure
	errAfterRetry := r.Wait(context.Background())
	require.Error(t, errAfterRetry)
	var gateErr *GateError
	require.True(t, errors.As(errAfterRetry, &gateErr))
	assert.Equal(t, "app:bootloader", gateErr.Service)
}

func TestReadiness_NoGatingServicesUnchanged(t *testing.T) {
	r := NewReadiness()
	assert.Equal(t, int64(0), r.Pending())
	// Wait must return nil immediately without hanging
	err := r.Wait(context.Background())
	require.NoError(t, err)
}
