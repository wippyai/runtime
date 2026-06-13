//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/metrics"
)

type fakeCollector struct {
	gauges   map[string]float64
	counters map[string]int
	mu       sync.Mutex
}

func (f *fakeCollector) GaugeSet(name string, value float64, _ metrics.Labels) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gauges[name] = value
}

func (f *fakeCollector) has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.gauges[name]
	return ok
}

func (f *fakeCollector) counter(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counters[name]
}

func (f *fakeCollector) CounterInc(name string, _ metrics.Labels) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counters[name]++
}
func (f *fakeCollector) CounterAdd(string, float64, metrics.Labels)       {}
func (f *fakeCollector) GaugeInc(string, metrics.Labels)                  {}
func (f *fakeCollector) GaugeDec(string, metrics.Labels)                  {}
func (f *fakeCollector) HistogramObserve(string, float64, metrics.Labels) {}
func (f *fakeCollector) RegisterExporter(metrics.Exporter) error          { return nil }
func (f *fakeCollector) Close() error                                     { return nil }

func TestReportLagRecordsGauge(t *testing.T) {
	repl, admin := dsns(t)
	db, err := sql.Open("postgres", admin)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	setupSchema(t, db)
	dropSlot(t, repl)
	defer dropSlot(t, repl)

	_, err = db.Exec(`DELETE FROM accounts`)
	require.NoError(t, err)

	fc := &fakeCollector{gauges: map[string]float64{}, counters: map[string]int{}}
	base := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	base = metrics.WithCollector(base, fc)

	src := NewSource(SourceOptions{
		ReplDSN: repl, AdminDSN: admin, Slot: itSlot, Publication: "wippy_cdc_pub",
		Bus: newCaptureBus(), StandbyInterval: 200 * time.Millisecond, StatusInterval: 200 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	_, err = src.Start(ctx)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return fc.has(retainedWALGauge)
	}, 5*time.Second, 100*time.Millisecond, "retained WAL gauge must be recorded for slot-lag monitoring")

	_, err = db.Exec(`INSERT INTO accounts (email, balance) VALUES ('metric@w.ai', 1)`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return fc.counter(changesCounter) >= 1
	}, 10*time.Second, 100*time.Millisecond, "changes counter must increment on a streamed change")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	require.NoError(t, src.Stop(stopCtx))
	stopCancel()
}
