// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/metrics"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
)

type blockingCloseExporter struct {
	started  chan struct{}
	release  chan struct{}
	closeErr error
	once     sync.Once
}

func (e *blockingCloseExporter) Name() string { return "blocking-close" }

func (e *blockingCloseExporter) Record(string, api.MetricType, float64, api.Labels) error {
	return nil
}

func (e *blockingCloseExporter) Close() error {
	e.once.Do(func() { close(e.started) })
	<-e.release
	return e.closeErr
}

type closeErrorExporter struct {
	err error
}

func (e *closeErrorExporter) Name() string { return "close-error" }

func (e *closeErrorExporter) Record(string, api.MetricType, float64, api.Labels) error {
	return nil
}

func (e *closeErrorExporter) Close() error { return e.err }

type countingCloseExporter struct {
	closed atomic.Int32
}

func (e *countingCloseExporter) Name() string { return "counting-close" }

func (e *countingCloseExporter) Record(string, api.MetricType, float64, api.Labels) error {
	return nil
}

func (e *countingCloseExporter) Close() error {
	e.closed.Add(1)
	return nil
}

// Concurrent producers and shutdown must account for every metric: either it
// was admitted and flushed, or it was explicitly counted as dropped. In
// particular an atomic closed check alone cannot protect channel send/close.
func TestCollectorConcurrentCloseAccountsForAllRecords(t *testing.T) {
	for range 100 {
		cfg := apicfg.Config{}
		cfg.Buffer.Size = 64
		c := NewCollector(cfg).(*collector)
		exporter := &mockExporter{}
		require.NoError(t, c.RegisterExporter(exporter))
		start := make(chan struct{})
		var workers sync.WaitGroup
		const producers = 8
		const records = 1000
		for range producers {
			workers.Go(func() {
				<-start
				for range records {
					c.CounterInc("shutdown", nil)
				}
			})
		}
		stopped := make(chan error, 1)
		go func() { <-start; stopped <- c.Close() }()
		close(start)
		workers.Wait()
		require.NoError(t, <-stopped)
		require.EqualValues(t, producers*records, uint64(exporter.count())+c.Dropped())
		before := c.Dropped()
		c.CounterInc("after-close", nil)
		require.Equal(t, before+1, c.Dropped())
	}
}

func TestCollectorConcurrentCloseWaitsForExporterShutdown(t *testing.T) {
	c := NewCollector(apicfg.Config{}).(*collector)
	exporter := &blockingCloseExporter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	require.NoError(t, c.RegisterExporter(exporter))

	first := make(chan error, 1)
	go func() { first <- c.Close() }()
	<-exporter.started

	second := make(chan error, 1)
	go func() { second <- c.Close() }()
	select {
	case err := <-second:
		t.Fatalf("concurrent Close returned before exporter shutdown completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(exporter.release)
	require.NoError(t, <-first)
	require.NoError(t, <-second)
}

func TestCollectorRejectsExporterRegistrationAfterClose(t *testing.T) {
	c := NewCollector(apicfg.Config{}).(*collector)
	require.NoError(t, c.Close())
	require.ErrorIs(t, c.RegisterExporter(&mockExporter{}), errCollectorClosed)
}

func TestCollectorCloseJoinsExporterErrorsAndIsIdempotent(t *testing.T) {
	c := NewCollector(apicfg.Config{}).(*collector)
	firstErr := errors.New("first exporter close")
	secondErr := errors.New("second exporter close")
	require.NoError(t, c.RegisterExporter(&closeErrorExporter{err: firstErr}))
	require.NoError(t, c.RegisterExporter(&closeErrorExporter{err: secondErr}))

	err := c.Close()
	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.Equal(t, err, c.Close())
}

func TestCollectorRegisterExporterRaceWithCloseDoesNotLeak(t *testing.T) {
	for range 1000 {
		c := NewCollector(apicfg.Config{}).(*collector)
		exporter := &countingCloseExporter{}
		start := make(chan struct{})
		registered := make(chan error, 1)
		closed := make(chan error, 1)

		go func() {
			<-start
			registered <- c.RegisterExporter(exporter)
		}()
		go func() {
			<-start
			closed <- c.Close()
		}()
		close(start)

		regErr := <-registered
		require.NoError(t, <-closed)
		if regErr == nil {
			require.EqualValues(t, 1, exporter.closed.Load())
		} else {
			require.ErrorIs(t, regErr, errCollectorClosed)
			require.Zero(t, exporter.closed.Load())
		}
	}
}
