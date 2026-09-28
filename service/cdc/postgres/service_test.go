// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSourceDefaults(t *testing.T) {
	s := NewSource(SourceOptions{})
	assert.Equal(t, defaultStandbyInterval, s.standbyInterval)
	assert.Equal(t, defaultStatusInterval, s.statusInterval)
	assert.Equal(t, defaultSnapshotFetchSize, s.snapshotFetchSize)
	assert.Equal(t, defaultMaxTransactionChanges, s.maxTransactionChanges)
	assert.Equal(t, int64(defaultMaxTransactionBytes), s.maxTransactionBytes)
	assert.Equal(t, defaultMaxInflightChanges, s.maxInflightChanges)
	assert.Equal(t, int64(defaultMaxInflightBytes), s.maxInflightBytes)
	assert.NotNil(t, s.log)
}

func TestNewSourceHonorsOverrides(t *testing.T) {
	s := NewSource(SourceOptions{
		StandbyInterval:       1 * time.Second,
		StatusInterval:        2 * time.Second,
		SnapshotFetchSize:     4096,
		MaxTransactionChanges: 123,
		MaxTransactionBytes:   456,
		MaxInflightChanges:    789,
		MaxInflightBytes:      101112,
	})
	assert.Equal(t, 1*time.Second, s.standbyInterval)
	assert.Equal(t, 2*time.Second, s.statusInterval)
	assert.Equal(t, 4096, s.snapshotFetchSize)
	assert.Equal(t, 123, s.maxTransactionChanges)
	assert.Equal(t, int64(456), s.maxTransactionBytes)
	assert.Equal(t, 789, s.maxInflightChanges)
	assert.Equal(t, int64(101112), s.maxInflightBytes)
}

func TestStopBeforeStartIsSafe(t *testing.T) {
	s := NewSource(SourceOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, s.Stop(ctx))
	require.NoError(t, s.Stop(ctx))
}

func TestFailedSourceCanBeStoppedAndRetried(t *testing.T) {
	s := NewSource(SourceOptions{})
	s.mu.Lock()
	s.state = sourceFailed
	s.mu.Unlock()

	require.NoError(t, s.Stop(context.Background()))

	// A canceled start fails during setup, but it must not be rejected as a
	// permanently closed source. Supervisors use this path after a fault.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Start(ctx)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrSourceClosed)
}

func TestFailedSourceStopWaitsForRunCleanup(t *testing.T) {
	source := NewSource(SourceOptions{})
	runDone := make(chan struct{})
	cancelCalled := make(chan struct{})
	releaseRun := make(chan struct{})
	source.mu.Lock()
	source.state = sourceFailed
	source.done = runDone
	source.cancel = func() { close(cancelCalled) }
	source.mu.Unlock()
	go func() {
		<-cancelCalled
		<-releaseRun
		close(runDone)
	}()

	stopDone := make(chan error, 1)
	go func() { stopDone <- source.Stop(context.Background()) }()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before failed run cleanup: %v", err)
	case <-cancelCalled:
	}
	close(releaseRun)
	require.NoError(t, <-stopDone)

	source.mu.Lock()
	assert.Equal(t, sourceStopped, source.state)
	source.mu.Unlock()
}

func TestFailedSourceStopCancellationIsRetryableAndIsolated(t *testing.T) {
	first := NewSource(SourceOptions{Name: "db-one"})
	firstDone := make(chan struct{})
	first.mu.Lock()
	first.state = sourceFailed
	first.done = firstDone
	first.mu.Unlock()

	second := NewSource(SourceOptions{Name: "db-two"})
	second.mu.Lock()
	second.state = sourceFailed
	second.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, first.Stop(ctx), context.DeadlineExceeded)
	first.mu.Lock()
	assert.Equal(t, sourceStopping, first.state)
	first.mu.Unlock()

	// A blocked source must not hold lifecycle state for an independent
	// database/source instance.
	require.NoError(t, second.Stop(context.Background()))
	close(firstDone)
	require.NoError(t, first.Stop(context.Background()))
}

func TestClosePermanentlyRetiresSource(t *testing.T) {
	s := NewSource(SourceOptions{})
	require.NoError(t, s.Close(context.Background()))
	_, err := s.Start(context.Background())
	require.ErrorIs(t, err, ErrSourceClosed)
}
