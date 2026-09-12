// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestOutboundBudgetWaitCancellationDoesNotTakeOwnership(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, err := newOutboundBudget(1, 8)
		require.NoError(t, err)
		accepted, err := b.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			r, err := b.reserve(ctx, 1, true)
			if r != nil {
				r.release()
			}
			done <- err
		}()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Equal(t, uint64(1), b.entries)
		require.Equal(t, uint64(8), b.bytes)
		accepted.release()
		require.Zero(t, b.entries)
		require.Zero(t, b.bytes)
	})
}

func TestOutboundBudgetReservationSurvivesRetryUntilDisposed(t *testing.T) {
	b, err := newOutboundBudget(2, 8)
	require.NoError(t, err)
	first, err := b.reserve(context.Background(), 6, false)
	require.NoError(t, err)
	_, err = b.reserve(context.Background(), 3, false)
	require.ErrorIs(t, err, ErrQueueFull)
	second, err := b.reserve(context.Background(), 2, false)
	require.NoError(t, err)
	// Moving accepted work between queue and writer consumes the same credit.
	_, err = b.reserve(context.Background(), 0, false)
	require.ErrorIs(t, err, ErrQueueFull)
	first.release()
	next, err := b.reserve(context.Background(), 6, false)
	require.NoError(t, err)
	first.release() // late duplicate completion cannot free the replacement
	_, err = b.reserve(context.Background(), 1, false)
	require.ErrorIs(t, err, ErrQueueFull)
	second.release()
	next.release()
	require.Zero(t, b.entries)
	require.Zero(t, b.bytes)
}

func TestOutboundBudgetReleaseWakesWaitingAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, err := newOutboundBudget(1, 8)
		require.NoError(t, err)
		first, err := b.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		done := make(chan *outboundReservation, 1)
		go func() {
			next, err := b.reserve(context.Background(), 8, true)
			if err != nil {
				t.Error(err)
			}
			done <- next
		}()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("admitted above capacity")
		default:
		}
		first.release()
		next := <-done
		require.NotNil(t, next)
		require.Equal(t, uint64(8), b.bytes)
		next.release()
	})
}

func TestOutboundBudgetBlockedPeerDoesNotHoardAggregate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, err := newOutboundBudget(2, 16)
		require.NoError(t, err)
		firstPeer, err := root.child(1, 8)
		require.NoError(t, err)
		otherPeer, err := root.child(1, 8)
		require.NoError(t, err)
		first, err := firstPeer.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			r, err := firstPeer.reserve(ctx, 8, true)
			r.release()
			done <- err
		}()
		synctest.Wait()
		other, err := otherPeer.reserve(context.Background(), 8, false)
		require.NoError(t, err, "blocked peer must not reserve aggregate credit")
		require.Equal(t, uint64(2), root.entries)
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Equal(t, uint64(16), root.bytes)
		first.release()
		other.release()
		require.Zero(t, root.entries)
		require.Zero(t, root.bytes)
	})
}

func TestOutboundBudgetAncestorLimitAndSiblingWake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, err := newOutboundBudget(3, 16)
		require.NoError(t, err)
		class, err := root.child(2, 8)
		require.NoError(t, err)
		a, err := class.child(2, 16)
		require.NoError(t, err)
		b, err := class.child(2, 16)
		require.NoError(t, err)
		r, err := a.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		_, err = b.reserve(context.Background(), 9, true)
		require.Error(t, err, "impossible ancestor admission must fail without waiting")
		_, err = b.reserve(context.Background(), 1, false)
		require.ErrorIs(t, err, ErrQueueFull)
		require.Zero(t, b.entries, "ancestor refusal must leave child uncharged")
		done := make(chan *outboundReservation, 1)
		go func() {
			next, err := b.reserve(context.Background(), 8, true)
			if err != nil {
				t.Error(err)
			}
			done <- next
		}()
		synctest.Wait()
		r.release()
		next := <-done
		require.NotNil(t, next)
		r.release()
		require.Equal(t, uint64(8), root.bytes)
		require.Equal(t, uint64(8), class.bytes)
		require.Zero(t, a.bytes)
		require.Equal(t, uint64(8), b.bytes)
		next.release()
		require.Zero(t, root.bytes)
		require.Zero(t, class.bytes)
		require.Zero(t, b.bytes)
	})
}

func TestOutboundBudgetRetirementCancelsWaitersButRetainsWriterCredit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, err := newOutboundBudget(2, 16)
		require.NoError(t, err)
		old, err := root.child(1, 8)
		require.NoError(t, err)
		writer, err := old.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() {
			r, err := old.reserve(context.Background(), 8, true)
			r.release()
			done <- err
		}()
		synctest.Wait()
		old.close()
		old.close()
		require.ErrorIs(t, <-done, ErrNodeNotManaged)
		require.Equal(t, uint64(8), root.bytes, "retirement cannot credit writer-owned payload")
		replacement, err := root.child(1, 8)
		require.NoError(t, err)
		next, err := replacement.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		writer.release()
		require.Equal(t, uint64(8), root.bytes)
		_, err = old.reserve(context.Background(), 8, false)
		require.ErrorIs(t, err, ErrNodeNotManaged, "freed capacity cannot revive a retired scope")
		next.release()
		require.Zero(t, root.bytes)
	})
}

func TestOutboundBudgetAncestorShutdownCancelsFullDescendant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, err := newOutboundBudget(2, 16)
		require.NoError(t, err)
		peer, err := root.child(1, 8)
		require.NoError(t, err)
		writer, err := peer.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() {
			r, err := peer.reserve(context.Background(), 8, true)
			r.release()
			done <- err
		}()
		synctest.Wait()
		root.close()
		require.ErrorIs(t, <-done, ErrNodeNotManaged, "full leaf must still observe closed ancestor")
		require.Equal(t, uint64(8), root.bytes)
		writer.release()
		require.Zero(t, root.bytes)
		late, err := root.child(1, 8)
		require.NoError(t, err)
		_, err = late.reserve(context.Background(), 8, false)
		require.ErrorIs(t, err, ErrNodeNotManaged, "child created after shutdown cannot admit")
	})
}
