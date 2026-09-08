// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

func TestCanceledProposalRetainsByteBudgetUntilCompletion(t *testing.T) {
	waits := &proposalWaits{slots: make(chan struct{}, 2), maxBytes: 4}
	stop := make(chan struct{})
	started, finish := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(finish) })
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := waits.run(ctx, stop, []byte("full"), func([]byte) (*raftapi.ApplyResponse, error) { close(started); <-finish; return nil, nil })
		done <- err
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	waits.mu.Lock()
	require.Equal(t, 4, waits.bytes)
	waits.mu.Unlock()
	// A count slot is free, but the canceled proposal still owns all byte credit.
	short, cancelShort := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelShort()
	_, err := waits.run(short, stop, []byte("x"), func([]byte) (*raftapi.ApplyResponse, error) { t.Error("byte budget exceeded"); return nil, nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	release()
	later, cancelLater := context.WithTimeout(t.Context(), time.Second)
	defer cancelLater()
	_, err = waits.run(later, stop, []byte("next"), func([]byte) (*raftapi.ApplyResponse, error) { return nil, nil })
	require.NoError(t, err, "real completion must restore byte capacity")
}

func TestOversizedProposalDoesNotCopyOrSubmit(t *testing.T) {
	waits := &proposalWaits{slots: make(chan struct{}, 1), maxBytes: 1}
	_, err := waits.run(t.Context(), make(chan struct{}), []byte("too large"), func([]byte) (*raftapi.ApplyResponse, error) { t.Error("oversized proposal submitted"); return nil, nil })
	require.ErrorContains(t, err, "max_pending_apply_bytes")
	require.Empty(t, waits.slots)
	require.Zero(t, waits.bytes)
	waits.slots <- struct{}{}
	defer func() { <-waits.slots }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = waits.run(ctx, make(chan struct{}), []byte("too large"), func([]byte) (*raftapi.ApplyResponse, error) { t.Error("oversized proposal submitted"); return nil, nil })
	require.ErrorContains(t, err, "max_pending_apply_bytes", "oversized work must be rejected before waiting for a count slot")
}

func TestStoppedProposalByteWaitReturnsCapacity(t *testing.T) {
	waits := &proposalWaits{slots: make(chan struct{}, 1), maxBytes: 1}
	stop := make(chan struct{})
	require.NoError(t, waits.reserveBytes(t.Context(), stop, 1))
	defer waits.releaseBytes(1)
	done := make(chan error, 1)
	go func() {
		_, err := waits.run(t.Context(), stop, []byte("x"), func([]byte) (*raftapi.ApplyResponse, error) { t.Error("submitted after stop"); return nil, nil })
		done <- err
	}()
	require.Eventually(t, func() bool {
		waits.mu.Lock()
		defer waits.mu.Unlock()
		return waits.changed != nil
	}, time.Second, time.Millisecond, "proposal must be waiting for byte capacity before shutdown")
	close(stop)
	select {
	case err := <-done:
		require.ErrorIs(t, err, raftapi.ErrNotRunning)
	case <-time.After(time.Second):
		t.Fatal("byte admission ignored shutdown")
	}
	require.Empty(t, waits.slots)
}

type observedProposalContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedProposalContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.observed) })
	return err
}

func TestProposalByteAdmissionRechecksCancellationAfterContention(t *testing.T) {
	waits := &proposalWaits{slots: make(chan struct{}, 1), maxBytes: 1}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observed := &observedProposalContext{Context: ctx, observed: make(chan struct{})}
	waits.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- waits.reserveBytes(observed, make(chan struct{}), 1) }()
	// The first cancellation check observed a live caller. Hold the ownership
	// mutex until cancellation, so acquisition must revalidate that observation.
	<-observed.observed
	cancel()
	waits.mu.Unlock()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Zero(t, waits.bytes, "canceled admission must not consume byte credit")
}
