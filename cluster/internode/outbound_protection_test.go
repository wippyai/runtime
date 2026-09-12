// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestOutboundProtectionPreservesControlAndTotalBounds(t *testing.T) {
	for _, pressure := range []string{"entries", "bytes"} {
		t.Run(pressure, func(t *testing.T) {
			root, err := newOutboundBudget(4, 16)
			require.NoError(t, err)
			require.NoError(t, root.protect(1, 4))
			peer, err := root.child(4, 16)
			require.NoError(t, err)
			require.NoError(t, peer.protect(1, 4))
			var frames []*outboundReservation
			defer func() {
				for _, r := range frames {
					r.release()
				}
			}()
			sizes := []uint64{12}
			if pressure == "entries" {
				sizes = []uint64{1, 1, 1}
			}
			for _, size := range sizes {
				r, err := peer.reserve(context.Background(), size, false)
				require.NoError(t, err)
				frames = append(frames, r)
			}
			_, err = peer.reserve(context.Background(), 1, false)
			require.ErrorIs(t, err, ErrQueueFull)
			size := uint64(4)
			if pressure == "entries" {
				size = 1
			}
			control, err := peer.reserveTraffic(context.Background(), size, false, true)
			require.NoError(t, err)
			frames = append(frames, control)
			_, err = peer.reserveTraffic(context.Background(), 1, false, true)
			require.ErrorIs(t, err, ErrQueueFull, "control must respect hard total limits")
			for _, r := range frames {
				r.release()
				r.release()
			}
			require.Zero(t, root.entries)
			require.Zero(t, root.bytes)
			require.Zero(t, root.ordinaryEntries)
			require.Zero(t, root.ordinaryBytes)
		})
	}
}

func TestOutboundProtectionAggregateAndBorrowing(t *testing.T) {
	root, err := newOutboundBudget(4, 16)
	require.NoError(t, err)
	require.NoError(t, root.protect(1, 4))
	a, _ := root.child(4, 16)
	b, _ := root.child(4, 16)
	ordinary, err := a.reserve(context.Background(), 12, false)
	require.NoError(t, err)
	defer ordinary.release()
	_, err = b.reserve(context.Background(), 1, false)
	require.ErrorIs(t, err, ErrQueueFull)
	control, err := b.reserveTraffic(context.Background(), 4, false, true)
	require.NoError(t, err)
	control.release()
	ordinary.release()
	// A control burst can borrow unused ordinary capacity, but cannot exceed it.
	borrowed, err := b.reserveTraffic(context.Background(), 16, false, true)
	require.NoError(t, err)
	defer borrowed.release()
	_, err = a.reserve(context.Background(), 1, false)
	require.ErrorIs(t, err, ErrQueueFull)
	_, err = b.reserve(context.Background(), 13, true)
	require.Error(t, err, "an impossible ordinary frame must not wait forever")
}

func TestOutboundProtectionWaitAndShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, _ := newOutboundBudget(4, 16)
		require.NoError(t, root.protect(1, 4))
		peer, _ := root.child(4, 16)
		ordinary, err := peer.reserve(context.Background(), 12, false)
		require.NoError(t, err)
		defer ordinary.release()
		done := make(chan error, 1)
		go func() {
			r, err := peer.reserve(context.Background(), 1, true)
			r.release()
			done <- err
		}()
		synctest.Wait()
		require.Equal(t, 1, root.waiters)
		control, err := peer.reserveTraffic(context.Background(), 4, false, true)
		require.NoError(t, err)
		defer control.release()
		root.close()
		require.ErrorIs(t, <-done, ErrNodeNotManaged)
		require.Equal(t, uint64(16), root.bytes, "shutdown must retain writer-owned credit")
	})
}

func TestOutboundProtectionRejectsNoOrdinaryCapacity(t *testing.T) {
	b, _ := newOutboundBudget(4, 16)
	require.Error(t, b.protect(4, 1))
	require.Error(t, b.protect(1, 16))
	require.Zero(t, b.protectedEntries)
	require.Zero(t, b.protectedBytes)
	require.NoError(t, b.protect(0, 0))
}
