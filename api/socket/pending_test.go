// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingCloser struct {
	done chan struct{}
	once sync.Once
	n    atomic.Int32
}

func newCountingCloser() *countingCloser {
	return &countingCloser{done: make(chan struct{})}
}

func (c *countingCloser) Close() error {
	c.n.Add(1)
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *countingCloser) closes() int32 { return c.n.Load() }

type quotaCloser struct {
	quota *atomic.Int32
	once  sync.Once
}

func (c *quotaCloser) Close() error {
	c.once.Do(func() { c.quota.Add(-1) })
	return nil
}

type gatedCloser struct {
	entered chan struct{}
	release chan struct{}
	quota   *atomic.Int32
	n       atomic.Int32
	enter   sync.Once
	quotaDo sync.Once
}

func (c *gatedCloser) Close() error {
	c.n.Add(1)
	c.enter.Do(func() { close(c.entered) })
	<-c.release
	c.quotaDo.Do(func() {
		if c.quota != nil {
			c.quota.Add(-1)
		}
	})
	return nil
}

func (c *gatedCloser) closes() int32 { return c.n.Load() }

func TestPendingOperationStartOnce(t *testing.T) {
	op := NewPendingOperation()
	ctx1, ok := op.Start(context.Background())
	require.True(t, ok)
	require.NotNil(t, ctx1)
	ctx2, ok := op.Start(context.Background())
	require.False(t, ok)
	require.Nil(t, ctx2)
	go func() {
		<-ctx1.Done()
		op.Complete(nil, ctx1.Err())
	}()
	require.NoError(t, op.Close())
}

func TestPendingOperationCloseBeforeStart(t *testing.T) {
	op := NewPendingOperation()
	done := make(chan struct{})
	go func() {
		defer close(done)
		require.NoError(t, op.Close())
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close before Start did not return promptly")
	}
	_, ok := op.Start(context.Background())
	require.False(t, ok)
	resource := newCountingCloser()
	op.Complete(resource, nil)
	require.Equal(t, int32(1), resource.closes())
}

func TestPendingOperationTakePendingAndDoubleTake(t *testing.T) {
	op := NewPendingOperation()
	_, ok := op.Start(context.Background())
	require.True(t, ok)

	value, err, ready := op.Take()
	require.False(t, ready)
	require.Nil(t, value)
	require.NoError(t, err)

	resource := newCountingCloser()
	op.Complete(resource, nil)
	require.True(t, op.Ready())

	first, err, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, err)
	require.Equal(t, resource, first)
	require.Equal(t, int32(0), resource.closes())

	second, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, second)
	require.ErrorIs(t, err, ErrAlreadyTaken)
	require.NoError(t, first.Close())
	require.Equal(t, int32(1), resource.closes())
	require.NoError(t, op.Close())
}

func TestPendingOperationCompleteNilSuccess(t *testing.T) {
	op := NewPendingOperation()
	_, ok := op.Start(context.Background())
	require.True(t, ok)
	op.Complete(nil, nil)
	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, err, ErrNoResult)
	require.NoError(t, op.Close())
}

func TestPendingOperationCompleteErrorAndResource(t *testing.T) {
	op := NewPendingOperation()
	_, ok := op.Start(context.Background())
	require.True(t, ok)
	resource := newCountingCloser()
	boom := errors.New("dial failed")
	op.Complete(resource, boom)
	require.Equal(t, int32(1), resource.closes())
	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, err, boom)
	require.NoError(t, op.Close())
}

func TestPendingOperationCompleteHonorsCanceledContext(t *testing.T) {
	op := NewPendingOperation()
	ctx, cancel := context.WithCancel(context.Background())
	opCtx, ok := op.Start(ctx)
	require.True(t, ok)
	cancel()
	<-opCtx.Done()
	resource := newCountingCloser()
	op.Complete(resource, nil)
	require.Equal(t, int32(1), resource.closes())
	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.Error(t, err)
	require.NoError(t, op.Close())
}

func TestPendingOperationCloseAfterStartJoinsAndClosesLateResult(t *testing.T) {
	op := NewPendingOperation()
	opCtx, ok := op.Start(context.Background())
	require.True(t, ok)
	entered := make(chan struct{})
	late := newCountingCloser()
	go func() {
		close(entered)
		<-opCtx.Done()
		op.Complete(late, nil)
	}()
	<-entered
	require.NoError(t, op.Close())
	require.Equal(t, int32(1), late.closes())
	_, err, ready := op.Take()
	require.True(t, ready)
	require.Error(t, err)
}

func TestPendingOperationNotifyIsSingleChannel(t *testing.T) {
	op := NewPendingOperation()
	_, ok := op.Start(context.Background())
	require.True(t, ok)
	first := op.Notify()
	second := op.Notify()
	require.Equal(t, first, second)
	select {
	case <-first:
		t.Fatal("notify fired before completion")
	default:
	}
	op.Complete(nil, nil)
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("notify did not fire on owned completion")
	}
	require.Equal(t, first, op.Notify())
	require.NoError(t, op.Close())
}

func TestPendingOperationParentCancelAfterCompleteClosesResult(t *testing.T) {
	op := NewPendingOperation()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, ok := op.Start(parent)
	require.True(t, ok)
	resource := newCountingCloser()
	op.Complete(resource, nil)
	require.Equal(t, int32(0), resource.closes())
	cancel()
	select {
	case <-resource.done:
	case <-time.After(time.Second):
		t.Fatal("parent cancel left the completed resource open")
	}
	require.Equal(t, int32(1), resource.closes())
	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.Error(t, err)
	require.NoError(t, op.Close())
}

func TestPendingOperationTakeStopsParentCancelHook(t *testing.T) {
	op := NewPendingOperation()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, ok := op.Start(parent)
	require.True(t, ok)
	resource := newCountingCloser()
	op.Complete(resource, nil)
	value, err, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, err)
	require.Equal(t, resource, value)
	cancel()
	select {
	case <-resource.done:
		t.Fatal("Take did not stop parent-cancel cleanup")
	case <-time.After(50 * time.Millisecond):
	}
	require.Equal(t, int32(0), resource.closes())
	require.NoError(t, value.Close())
	require.Equal(t, int32(1), resource.closes())
	require.NoError(t, op.Close())
}

func TestPendingOperationPreservesQuotaUntilPhysicalClose(t *testing.T) {
	op := NewPendingOperation()
	_, ok := op.Start(context.Background())
	require.True(t, ok)
	quota := &atomic.Int32{}
	quota.Store(1)
	resource := &quotaCloser{quota: quota}
	op.Complete(resource, nil)
	require.Equal(t, int32(1), quota.Load())
	require.NoError(t, op.Close())
	require.Equal(t, int32(0), quota.Load())
}

func TestPendingOperationCompleteTwiceClosesExtraOnce(t *testing.T) {
	op := NewPendingOperation()
	_, ok := op.Start(context.Background())
	require.True(t, ok)
	first := newCountingCloser()
	second := newCountingCloser()
	op.Complete(first, nil)
	op.Complete(second, nil)
	require.Equal(t, int32(0), first.closes())
	require.Equal(t, int32(1), second.closes())
	value, err, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, err)
	require.Equal(t, first, value)
	require.NoError(t, value.Close())
	require.NoError(t, op.Close())
}

func TestPendingOperationCloseCompleteRaceClosesOnce(t *testing.T) {
	for i := 0; i < 50; i++ {
		op := NewPendingOperation()
		_, ok := op.Start(context.Background())
		require.True(t, ok)
		resource := newCountingCloser()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			op.Complete(resource, nil)
		}()
		go func() {
			defer wg.Done()
			_ = op.Close()
		}()
		wg.Wait()
		require.Equal(t, int32(1), resource.closes())
		_, _, ready := op.Take()
		require.True(t, ready)
	}
}

func TestPendingOperationTakeParentCancelRaceOwnership(t *testing.T) {
	for i := 0; i < 50; i++ {
		op := NewPendingOperation()
		parent, cancel := context.WithCancel(context.Background())
		_, ok := op.Start(parent)
		require.True(t, ok)
		resource := newCountingCloser()
		op.Complete(resource, nil)
		var taken io.Closer
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			value, _, ready := op.Take()
			if ready {
				taken = value
			}
		}()
		go func() {
			defer wg.Done()
			cancel()
		}()
		wg.Wait()
		_ = op.Close()
		if taken != nil {
			require.Equal(t, int32(0), resource.closes())
			require.NoError(t, taken.Close())
		}
		require.Equal(t, int32(1), resource.closes())
	}
}

func TestPendingOperationTakeRechecksParentCancelBeforeTransfer(t *testing.T) {
	for i := 0; i < 50; i++ {
		op := NewPendingOperation()
		parent, cancel := context.WithCancel(context.Background())
		child, ok := op.Start(parent)
		require.True(t, ok)
		resource := newCountingCloser()
		op.Complete(resource, nil)
		cancel()
		<-child.Done()
		value, err, ready := op.Take()
		require.True(t, ready)
		require.Nil(t, value)
		require.Error(t, err)
		select {
		case <-resource.done:
		case <-time.After(time.Second):
			t.Fatal("canceled result was not closed")
		}
		require.Equal(t, int32(1), resource.closes())
		require.NoError(t, op.Close())
	}
}

func TestPendingOperationCloseJoinsRejectedGatedDispose(t *testing.T) {
	op := NewPendingOperation()
	_, ok := op.Start(context.Background())
	require.True(t, ok)
	closer := &gatedCloser{entered: make(chan struct{}), release: make(chan struct{})}
	go op.Complete(closer, errors.New("rejected result"))
	select {
	case <-closer.entered:
	case <-time.After(time.Second):
		t.Fatal("rejected result not closed")
	}
	closed := make(chan struct{})
	go func() {
		_ = op.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before rejected dispose finished")
	case <-time.After(50 * time.Millisecond):
	}
	require.False(t, op.Ready())
	close(closer.release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not join rejected dispose")
	}
}

func TestPendingOperationGatedParentCancelConcurrentTakeClose(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := NewPendingOperation()
	_, ok := op.Start(parent)
	require.True(t, ok)
	quota := &atomic.Int32{}
	quota.Store(1)
	closer := &gatedCloser{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		quota:   quota,
	}
	op.Complete(closer, nil)
	require.True(t, op.Ready())
	require.Equal(t, int32(1), quota.Load())
	cancel()
	select {
	case <-closer.entered:
	case <-time.After(time.Second):
		t.Fatal("parent cancel did not start dispose")
	}
	require.Equal(t, int32(1), quota.Load())

	var taken io.Closer
	var takeErr error
	var takeReady bool
	takeDone := make(chan struct{})
	closeDone := make(chan struct{})
	go func() {
		taken, takeErr, takeReady = op.Take()
		close(takeDone)
	}()
	go func() {
		_ = op.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		t.Fatal("Close returned before physical dispose finished")
	case <-time.After(50 * time.Millisecond):
	}
	require.Equal(t, int32(1), quota.Load())

	select {
	case <-takeDone:
		require.True(t, takeReady)
		require.Nil(t, taken)
		require.Error(t, takeErr)
	case <-time.After(time.Second):
		t.Fatal("Take did not return a canceled completion")
	}

	select {
	case <-closeDone:
		t.Fatal("Close returned before physical dispose finished")
	default:
	}

	close(closer.release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close did not join physical dispose")
	}
	require.Equal(t, int32(0), quota.Load())
	require.Equal(t, int32(1), closer.closes())
}

func TestPendingOperationCloseJoinsHookAfterTake(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := NewPendingOperation()
	_, ok := op.Start(parent)
	require.True(t, ok)
	quota := &atomic.Int32{}
	quota.Store(1)
	closer := &gatedCloser{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		quota:   quota,
	}
	op.Complete(closer, nil)
	cancel()
	select {
	case <-closer.entered:
	case <-time.After(time.Second):
		t.Fatal("parent cancel did not start dispose")
	}

	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.Error(t, err)

	closed := make(chan struct{})
	go func() {
		_ = op.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before hook dispose finished")
	case <-time.After(50 * time.Millisecond):
	}
	require.Equal(t, int32(1), quota.Load())
	close(closer.release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not join hook dispose after Take")
	}
	require.Equal(t, int32(0), quota.Load())
}

func TestPendingOperationNilReceiver(t *testing.T) {
	var op *PendingOperation
	_, ok := op.Start(context.Background())
	require.False(t, ok)
	op.Complete(newCountingCloser(), nil)
	value, err, ready := op.Take()
	require.False(t, ready)
	require.Nil(t, value)
	require.NoError(t, err)
	require.NoError(t, op.Close())
	assert.False(t, op.Ready())
}
