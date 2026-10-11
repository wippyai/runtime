// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestSnapshotFrameContextOnPreservesInheritanceWithoutJoining(t *testing.T) {
	completed := 0
	sourceCtx, source := ForkFrameContextWithCompletion(context.Background(), func() { completed++ })
	defer ReleaseFrameContext(source)
	values := NewValues()
	values.Set("origin", "source")
	inheritKey := &Key{Name: "inherited", Inherit: true}
	executionKey := &Key{Name: "execution", Execution: true}
	closed := 0
	require.NoError(t, source.SetMultiple(
		ValuesPair(values),
		Pair{Key: inheritKey, Value: "metadata"},
		Pair{Key: executionKey, Value: CloserFunc(func() error { closed++; return nil })},
		Pair{Key: "untyped", Value: "not inherited"},
	))
	ctx, snapshot := SnapshotFrameContextOn(context.Background(), sourceCtx)
	defer ReleaseFrameContext(snapshot)
	require.True(t, source.IsSealed())
	require.False(t, snapshot.IsSealed())
	require.Nil(t, snapshot.(*frameContextRef).frame.parent)
	require.Nil(t, snapshot.(*frameContextRef).frame.onComplete)
	require.Nil(t, snapshot.(*frameContextRef).frame.execution)
	require.False(t, snapshot.Has(executionKey))
	require.False(t, snapshot.Has("untyped"))
	ReleaseFrameContext(source)
	require.Equal(t, 1, completed)
	require.Equal(t, 1, closed)
	value, ok := snapshot.Get(inheritKey)
	require.True(t, ok)
	require.Equal(t, "metadata", value)
	inheritedValues := GetValues(ctx)
	require.NotNil(t, inheritedValues)
	inheritedValues.Set("origin", "snapshot")
	value, _ = values.Get("origin")
	require.Equal(t, "source", value)
	ReleaseFrameContext(snapshot)
	require.Equal(t, 1, completed, "snapshot teardown must not repeat source completion")
	require.Equal(t, 1, closed)
}

type blockingSnapshotCloner struct {
	copying chan struct{}
	resume  chan struct{}
}

func (v blockingSnapshotCloner) Clone() any {
	close(v.copying)
	<-v.resume
	return "copied"
}

func TestSnapshotFrameContextOnRetainsSourceOnlyWhileCopying(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		completed := make(chan struct{})
		sourceCtx, source := ForkFrameContextWithCompletion(context.Background(), func() { close(completed) })
		defer ReleaseFrameContext(source)
		cloner := blockingSnapshotCloner{copying: make(chan struct{}), resume: make(chan struct{})}
		key := &Key{Name: "clone", Inherit: true}
		require.NoError(t, source.Set(key, cloner))
		snapshots := make(chan FrameContext, 1)
		go func() {
			_, snapshot := SnapshotFrameContextOn(context.Background(), sourceCtx)
			snapshots <- snapshot
		}()
		<-cloner.copying
		ReleaseFrameContext(source)
		synctest.Wait()
		select {
		case <-completed:
			t.Error("source must stay alive until its inheritance snapshot has been copied")
		default:
		}
		close(cloner.resume)
		snapshot := <-snapshots
		defer ReleaseFrameContext(snapshot)
		synctest.Wait()
		select {
		case <-completed:
		default:
			t.Error("completed snapshot must not retain the source")
		}
		value, ok := snapshot.Get(key)
		require.True(t, ok)
		require.Equal(t, "copied", value)
	})
}

func TestSnapshotFrameContextOnEmptyOrReleasedSource(t *testing.T) {
	releasedCtx, released := OpenFrameContext(context.Background())
	ReleaseFrameContext(released)
	for _, sourceCtx := range []context.Context{context.Background(), releasedCtx} {
		ctx, snapshot := SnapshotFrameContextOn(context.Background(), sourceCtx)
		require.NotNil(t, FrameFromContext(ctx))
		require.Nil(t, snapshot.(*frameContextRef).frame.parent)
		require.NoError(t, snapshot.Set("fresh", "value"))
		ReleaseFrameContext(snapshot)
	}
}

func TestOpenFrameContextOnStillJoinsSourceLifetime(t *testing.T) {
	completed := false
	sourceCtx, source := ForkFrameContextWithCompletion(context.Background(), func() { completed = true })
	_, child := OpenFrameContextOn(context.Background(), sourceCtx)
	defer ReleaseFrameContext(child)
	ReleaseFrameContext(source)
	require.False(t, completed, "ordinary frame forks must still retain their parent")
	ReleaseFrameContext(child)
	require.True(t, completed)
}

func BenchmarkSnapshotFrameContextOn(b *testing.B) {
	ctx, source := OpenFrameContext(context.Background())
	defer ReleaseFrameContext(source)
	_ = SetValues(ctx, NewValues())
	source.Seal()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, snapshot := SnapshotFrameContextOn(context.Background(), ctx)
		ReleaseFrameContext(snapshot)
	}
}
