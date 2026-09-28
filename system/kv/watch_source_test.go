package kv

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func testWatchEvent(key, value string) kvapi.WatchEvent {
	return kvapi.WatchEvent{
		Type:    kvapi.WatchPut,
		Current: &kvapi.Entry{Key: key, Value: []byte(value)},
	}
}

func waitWatch(t *testing.T, ch <-chan kvapi.WatchEvent) kvapi.WatchEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("watch event timeout")
	}
	return kvapi.WatchEvent{}
}

func waitWatchState(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("watch state did not become ready")
}

func waitWatchDone(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("watch Done timeout")
	}
}

func TestWatchSourceOrderedPrefix(t *testing.T) {
	source, err := newWatchSource(watchLimits{maxWatchers: 2, maxEvents: 4, maxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := source.watch(context.Background(), "a/", nil)
	if err != nil {
		t.Fatal(err)
	}
	source.publish([]kvapi.WatchEvent{
		testWatchEvent("b/1", "ignored"),
		testWatchEvent("a/1", "one"),
		testWatchEvent("a/2", "two"),
	}, nil)
	if got := waitWatch(t, sub.Events()).Current.Key; got != "a/1" {
		t.Fatalf("first key = %q", got)
	}
	if got := waitWatch(t, sub.Events()).Current.Key; got != "a/2" {
		t.Fatalf("second key = %q", got)
	}
	if err := sub.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWatchSourceDetachesQueuedValueWithoutChargingUnusedBacking(t *testing.T) {
	source, err := newWatchSource(watchLimits{maxWatchers: 2, maxEvents: 2, maxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := source.watch(context.Background(), "k", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	largeBacking := make([]byte, 1, 1<<20)
	largeBacking[0] = 'x'
	source.publish([]kvapi.WatchEvent{{Type: kvapi.WatchPut, Current: &kvapi.Entry{Key: "key", Value: largeBacking}}}, nil)
	largeBacking[0] = 'y'
	event := waitWatch(t, sub.Events())
	if event.Current == nil || string(event.Current.Value) != "x" {
		t.Fatalf("queued event retained caller's mutable backing: %+v", event.Current)
	}
	if err := sub.Err(); err != nil {
		t.Fatalf("detached one-byte value exhausted the byte budget: %v", err)
	}
}

func TestWatchSourcePrefixesRespectGlobalByteBudget(t *testing.T) {
	source, err := newWatchSource(watchLimits{maxWatchers: 3, maxEvents: 2, maxBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	first, err := source.watch(context.Background(), "abcde", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := source.watch(context.Background(), "wxyz", nil); !errors.Is(err, errWatchLimit) {
			t.Fatalf("prefix exceeded byte budget: %v", err)
		}
	}
	if _, err := source.watch(context.Background(), "abc", nil); err != nil {
		t.Fatalf("rejected prefixes retained bytes: %v", err)
	}
	source.close()
	if got := first.Err(); !errors.Is(got, kvapi.ErrKVClosed) {
		t.Fatalf("source closure: %v", got)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.bytes != 0 {
		t.Fatalf("source retained %d prefix bytes after workers exited", source.bytes)
	}
}

func TestWatchSourceCountAndBytesOverflowBlocked(t *testing.T) {
	source, err := newWatchSource(watchLimits{maxWatchers: 1, maxEvents: 2, maxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := source.watch(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	source.publish([]kvapi.WatchEvent{testWatchEvent("one", "1")}, nil)
	waitWatchState(t, func() bool {
		source.mu.Lock()
		defer source.mu.Unlock()
		return sub.hasFlight
	})
	source.publish([]kvapi.WatchEvent{testWatchEvent("two", "2")}, nil)
	source.publish([]kvapi.WatchEvent{testWatchEvent("three", "3")}, nil)
	waitWatchDone(t, sub.Done())
	if !errors.Is(sub.Err(), errWatchOverflow) {
		t.Fatalf("Err = %v", sub.Err())
	}
	<-sub.worker
	source.mu.Lock()
	if source.bytes != 0 {
		t.Fatalf("retained bytes = %d", source.bytes)
	}
	source.mu.Unlock()
	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("event delivered after Done")
		}
	default:
		t.Fatal("events worker did not close")
	}

	byteSource, err := newWatchSource(watchLimits{maxWatchers: 1, maxEvents: 4, maxBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	byteSub, err := byteSource.watch(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	byteSource.publish([]kvapi.WatchEvent{testWatchEvent("long-key", "x")}, nil)
	<-byteSub.Done()
	if !errors.Is(byteSub.Err(), errWatchOverflow) {
		t.Fatalf("byte Err = %v", byteSub.Err())
	}
	<-byteSub.worker
}

func TestWatchSourceWatcherCapAndOwnerStop(t *testing.T) {
	source, err := newWatchSource(watchLimits{maxWatchers: 1, maxEvents: 2, maxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	owner := &watchOwner{}
	first, err := source.watch(context.Background(), "", owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.watch(context.Background(), "", nil); !errors.Is(err, errWatchLimit) {
		t.Fatalf("cap error = %v", err)
	}
	source.stopOwner(owner)
	if !errors.Is(first.Err(), kvapi.ErrKVClosed) {
		t.Fatalf("owner Err = %v", first.Err())
	}
	if _, err := source.watch(context.Background(), "", owner); !errors.Is(err, kvapi.ErrKVClosed) {
		t.Fatalf("stopped owner error = %v", err)
	}
	<-first.worker
}

func TestWatchSourceResetRegistrationOrdering(t *testing.T) {
	source, err := newWatchSource(watchLimits{maxWatchers: 2, maxEvents: 2, maxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	old, err := source.watch(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	resetDone := make(chan struct{})
	go func() {
		source.reset(func() {
			close(started)
			<-release
		})
		close(resetDone)
	}()
	<-started
	waitWatchDone(t, old.Done())
	newDone := make(chan *watchSubscription, 1)
	go func() {
		sub, watchErr := source.watch(context.Background(), "", nil)
		if watchErr != nil {
			t.Errorf("watch during reset: %v", watchErr)
			return
		}
		newDone <- sub
	}()
	runtime.Gosched()
	select {
	case <-newDone:
		t.Fatal("registration interleaved with reset commit")
	default:
	}
	close(release)
	<-resetDone
	newSub := <-newDone
	if !errors.Is(old.Err(), errWatchReset) {
		t.Fatalf("old Err = %v", old.Err())
	}
	_ = old.Close()
	_ = newSub.Close()
}

func TestWatchSourceCloseCancellationAndRepeatedClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source, err := newWatchSource(watchLimits{maxWatchers: 2, maxEvents: 2, maxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := source.watch(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	source.publish([]kvapi.WatchEvent{testWatchEvent("key", "value")}, nil)
	waitWatchState(t, func() bool {
		source.mu.Lock()
		defer source.mu.Unlock()
		return sub.hasFlight
	})
	cancel()
	<-sub.Done()
	if !errors.Is(sub.Err(), context.Canceled) {
		t.Fatalf("cancel Err = %v", sub.Err())
	}
	<-sub.worker
	if err := sub.Close(); err != nil {
		t.Fatal(err)
	}

	closeSource, err := newWatchSource(watchLimits{maxWatchers: 1, maxEvents: 2, maxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	closeSub, err := closeSource.watch(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	closeSource.publish([]kvapi.WatchEvent{testWatchEvent("key", "value")}, nil)
	waitWatchState(t, func() bool {
		closeSource.mu.Lock()
		defer closeSource.mu.Unlock()
		return closeSub.hasFlight
	})
	var closeWG sync.WaitGroup
	for i := 0; i < 8; i++ {
		closeWG.Add(1)
		go func() {
			defer closeWG.Done()
			closeSource.close()
		}()
	}
	closeWG.Wait()
	if !errors.Is(closeSub.Err(), kvapi.ErrKVClosed) {
		t.Fatalf("close Err = %v", closeSub.Err())
	}
	if _, err := closeSource.watch(context.Background(), "", nil); !errors.Is(err, kvapi.ErrKVClosed) {
		t.Fatalf("closed source error = %v", err)
	}
}

func TestWatchSourceCommitBeforeNotification(t *testing.T) {
	source, err := newWatchSource(watchLimits{maxWatchers: 2, maxEvents: 2, maxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := source.watch(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	committed := false
	source.publish([]kvapi.WatchEvent{testWatchEvent("key", "value")}, func() {
		mu.Lock()
		committed = true
		mu.Unlock()
	})
	got := waitWatch(t, sub.Events())
	mu.Lock()
	if !committed || got.Current == nil {
		t.Fatal("notification preceded commit")
	}
	mu.Unlock()
	_ = sub.Close()
}
