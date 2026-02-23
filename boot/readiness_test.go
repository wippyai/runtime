// SPDX-License-Identifier: MPL-2.0

package boot

import (
	"context"
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
