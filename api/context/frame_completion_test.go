// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestFrameCompletionWaitsForDescendantClosers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parentCtx, parent := OpenFrameContext(context.Background())
		defer ReleaseFrameContext(parent)
		parentClosed := false
		require.NoError(t, parent.Set(&Key{Name: "parent"}, CloserFunc(func() error {
			parentClosed = true
			return nil
		})))
		events := make(chan string, 4)
		ctx, frame := ForkFrameContextWithCompletion(parentCtx, func() { events <- "complete" })
		require.NoError(t, frame.Set(&Key{Name: "frame"}, CloserFunc(func() error {
			events <- "frame"
			return nil
		})))
		childCtx, child := ForkFrameContext(ctx)
		require.NoError(t, child.Set(&Key{Name: "child"}, CloserFunc(func() error {
			events <- "child"
			return nil
		})))
		_, descendant := ForkFrameContext(childCtx)
		closing := make(chan struct{})
		release := make(chan struct{})
		require.NoError(t, descendant.Set(&Key{Name: "descendant"}, CloserFunc(func() error {
			close(closing)
			<-release
			events <- "descendant"
			return nil
		})))

		ReleaseFrameContext(frame)
		ReleaseFrameContext(child)
		go ReleaseFrameContext(descendant)
		<-closing
		synctest.Wait()
		early := len(events)
		close(release)
		synctest.Wait()
		require.Zero(t, early, "neither frame cleanup nor completion may overtake a descendant closer")
		for _, expected := range []string{"descendant", "child", "frame", "complete"} {
			require.Equal(t, expected, <-events)
		}
		require.False(t, parentClosed, "completion must not wait for unrelated ancestor ownership")
		ReleaseFrameContext(frame)
		ReleaseFrameContext(child)
		ReleaseFrameContext(descendant)
		require.Empty(t, events, "duplicate releases must not repeat completion")
	})
}

func TestFrameCompletionWaitsForEveryChild(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		completed := make(chan struct{}, 1)
		ctx, frame := ForkFrameContextWithCompletion(context.Background(), func() { completed <- struct{}{} })
		_, first := ForkFrameContext(ctx)
		_, second := ForkFrameContext(ctx)
		ReleaseFrameContext(frame)
		go ReleaseFrameContext(first)
		synctest.Wait()
		require.Empty(t, completed)
		go ReleaseFrameContext(second)
		synctest.Wait()
		require.Len(t, completed, 1)
	})
}

func TestFrameCompletionRunsBeforeAncestorCompletion(t *testing.T) {
	events := make(chan string, 2)
	ctx, frame := ForkFrameContextWithCompletion(context.Background(), func() { events <- "parent" })
	_, child := ForkFrameContextWithCompletion(ctx, func() { events <- "child" })
	ReleaseFrameContext(frame)
	ReleaseFrameContext(child)
	require.Equal(t, "child", <-events)
	require.Equal(t, "parent", <-events)
}

func TestFrameCompletionClearsPooledReferences(t *testing.T) {
	completions := 0
	ctx, frame := ForkFrameContextWithCompletion(context.Background(), func() { completions++ })
	_, child := ForkFrameContext(ctx)
	_, continuation, err := ContinueFrameContext(ctx)
	require.NoError(t, err)
	// Ordinary forks and continuations must never inherit a completion hook.
	require.Nil(t, child.(*frameContextRef).frame.onComplete)
	require.Nil(t, continuation.(*frameContextRef).frame.onComplete)
	ReleaseFrameContext(child)
	ReleaseFrameContext(continuation)
	require.Zero(t, completions)
	pooled := frame.(*frameContextRef).frame
	ReleaseFrameContext(frame)
	require.Equal(t, 1, completions)
	require.Nil(t, pooled.onComplete, "the pool must not retain the callback or captured call state")
	require.Nil(t, pooled.parent)
	// Duplicate release remains harmless even after the frame is pooled.
	ReleaseFrameContext(frame)
	require.Equal(t, 1, completions)
	_, ordinary := OpenFrameContext(context.Background())
	require.Nil(t, ordinary.(*frameContextRef).frame.onComplete)
	ReleaseFrameContext(ordinary)
	_, fork := ForkFrameContext(context.Background())
	require.Nil(t, fork.(*frameContextRef).frame.onComplete)
	ReleaseFrameContext(fork)
	require.Equal(t, 1, completions)
}

func TestFrameCompletionNilPreservesFrameLifetime(t *testing.T) {
	ctx, parent := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(parent)
	key := &Key{Name: "inherited", Inherit: true}
	require.NoError(t, parent.Set(key, "value"))
	_, frame := ForkFrameContextWithCompletion(ctx, nil)
	require.True(t, parent.IsSealed())
	value, ok := frame.Get(key)
	require.True(t, ok)
	require.Equal(t, "value", value)
	ReleaseFrameContext(frame)
	value, ok = parent.Get(key)
	require.True(t, ok, "releasing a nil-completion fork must not release its parent's owner")
	require.Equal(t, "value", value)
}
