// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
)

// transitionRunner drives Reg with a caller-supplied Transition body so tests
// can hold a transition open and observe what the rest of the registry can
// still do while entry-kind handlers are working.
type transitionRunner struct {
	fn func(ctx context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error)
}

func (t *transitionRunner) Transition(ctx context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
	return t.fn(ctx, state, changes)
}

func appendTransition(_ context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
	next := append(registry.State(nil), state...)
	for _, change := range changes {
		switch change.Kind {
		case registry.EntryCreate, registry.EntryUpdate:
			replaced := false
			for i := range next {
				if next[i].ID == change.Entry.ID {
					next[i] = change.Entry
					replaced = true
					break
				}
			}
			if !replaced {
				next = append(next, change.Entry)
			}
		case registry.EntryDelete:
			for i := range next {
				if next[i].ID == change.Entry.ID {
					next = append(next[:i], next[i+1:]...)
					break
				}
			}
		}
	}
	return next, nil
}

func newTransitionLockRegistry(t *testing.T, runner registry.Runner) *Reg {
	t.Helper()
	hist := historymem.New()
	require.NoError(t, hist.Save(version.New(registry.RootVersion), registry.ChangeSet{}, true))
	return NewRegistry(hist, runner, topology.NewStateBuilder(zap.NewNop(), nil), topology.NewResolver(), zap.NewNop())
}

func createOp(id registry.ID, data string) registry.ChangeSet {
	return registry.ChangeSet{{
		Kind:  registry.EntryCreate,
		Entry: registry.Entry{ID: id, Kind: "test", Data: payload.New(data)},
	}}
}

// TestReadsDoNotBlockDuringTransition holds a transition open and requires the
// read surface to keep answering: handler work is not a reason for readers to
// wait.
func TestReadsDoNotBlockDuringTransition(t *testing.T) {
	seeded := registry.NewID("app", "seeded")
	runner := &transitionRunner{fn: appendTransition}
	reg := newTransitionLockRegistry(t, runner)

	_, err := reg.Apply(context.Background(), createOp(seeded, "seed"))
	require.NoError(t, err)

	entered := make(chan struct{})
	release := make(chan struct{})
	runner.fn = func(ctx context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
		close(entered)
		<-release
		return appendTransition(ctx, state, changes)
	}

	applied := make(chan error, 1)
	go func() {
		_, applyErr := reg.Apply(context.Background(), createOp(registry.NewID("app", "slow"), "slow"))
		applied <- applyErr
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("transition never started")
	}

	reads := make(chan error, 1)
	go func() {
		if _, readErr := reg.GetEntry(seeded); readErr != nil {
			reads <- readErr
			return
		}
		if _, readErr := reg.GetAllEntries(); readErr != nil {
			reads <- readErr
			return
		}
		if _, readErr := reg.Current(); readErr != nil {
			reads <- readErr
			return
		}
		reads <- nil
	}()

	select {
	case readErr := <-reads:
		require.NoError(t, readErr)
	case <-time.After(500 * time.Millisecond):
		close(release)
		t.Fatal("reads blocked while a transition was running")
	}

	close(release)
	select {
	case applyErr := <-applied:
		require.NoError(t, applyErr)
	case <-time.After(5 * time.Second):
		t.Fatal("apply never completed")
	}

	entries, err := reg.GetAllEntries()
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	_, err = reg.GetEntry(registry.NewID("app", "slow"))
	require.NoError(t, err)
}

// TestHandlerMayReadRegistryDuringTransition covers the handler that consults
// the registry while accepting a change. The read resolves against the state
// the transition started from and the apply completes.
func TestHandlerMayReadRegistryDuringTransition(t *testing.T) {
	seeded := registry.NewID("app", "seeded")
	runner := &transitionRunner{fn: appendTransition}
	reg := newTransitionLockRegistry(t, runner)

	_, err := reg.Apply(context.Background(), createOp(seeded, "seed"))
	require.NoError(t, err)

	var (
		observed registry.Entry
		readErr  error
	)
	runner.fn = func(ctx context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
		observed, readErr = reg.GetEntry(seeded)
		return appendTransition(ctx, state, changes)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, applyErr := reg.Apply(ctx, createOp(registry.NewID("app", "reader"), "reader"))
		done <- applyErr
	}()

	select {
	case applyErr := <-done:
		require.NoError(t, applyErr)
	case <-ctx.Done():
		t.Fatal("handler read of the registry deadlocked the apply")
	}

	require.NoError(t, readErr)
	assert.Equal(t, seeded, observed.ID)
}

// TestConcurrentApplyStillSerialized proves applyMu remains the single writer
// gate: a second apply does not reach the runner until the first has published.
func TestConcurrentApplyStillSerialized(t *testing.T) {
	runner := &transitionRunner{}
	reg := newTransitionLockRegistry(t, runner)

	var (
		mu     sync.Mutex
		events []string
	)
	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}

	firstEntered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runner.fn = func(ctx context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
		first := false
		once.Do(func() {
			first = true
		})
		if first {
			record("first:transition")
			close(firstEntered)
			<-release
			return appendTransition(ctx, state, changes)
		}
		record("second:transition")
		return appendTransition(ctx, state, changes)
	}

	errs := make(chan error, 2)
	go func() {
		_, err := reg.Apply(context.Background(), createOp(registry.NewID("app", "first"), "first"))
		record("first:published")
		errs <- err
	}()

	select {
	case <-firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first transition never started")
	}

	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		_, err := reg.Apply(context.Background(), createOp(registry.NewID("app", "second"), "second"))
		record("second:published")
		errs <- err
	}()

	<-secondStarted
	// Give the second apply room to reach the runner if applyMu no longer holds it back.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	duringFirst := append([]string(nil), events...)
	mu.Unlock()
	assert.Equal(t, []string{"first:transition"}, duringFirst)

	close(release)
	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("applies never completed")
		}
	}

	mu.Lock()
	final := append([]string(nil), events...)
	mu.Unlock()
	require.Len(t, final, 4)
	assert.Equal(t, "first:transition", final[0])
	assert.Equal(t, "first:published", final[1])
	assert.Equal(t, "second:transition", final[2])
	assert.Equal(t, "second:published", final[3])

	entries, err := reg.GetAllEntries()
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

// TestPublishRejectsStaleTransitionBase covers the invariant publish states:
// applyMu is the only writer gate, so registry state must still be the slice the
// transition started from. A writer that reached state without the serializer
// surfaces as an error instead of overwriting the newer state.
func TestPublishRejectsStaleTransitionBase(t *testing.T) {
	reg := newTransitionLockRegistry(t, &transitionRunner{fn: appendTransition})

	_, err := reg.Apply(context.Background(), createOp(registry.NewID("app", "one"), "one"))
	require.NoError(t, err)

	stale := transitionBase{state: registry.State{{ID: registry.NewID("app", "gone"), Kind: "test"}}}

	installed := false
	publishErr := reg.publish(stale, func() { installed = true })
	require.Error(t, publishErr)
	assert.False(t, installed)

	var richErr apierror.Error
	require.True(t, errors.As(publishErr, &richErr))
	assert.Equal(t, apierror.Internal, richErr.Kind())

	entries, err := reg.GetAllEntries()
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}
