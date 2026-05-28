// SPDX-License-Identifier: MPL-2.0

package logs

import (
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/metrics"
	"go.uber.org/zap/zapcore"
)

type recordingCollector struct {
	metrics.Collector
	counts map[string]map[string]int
	mu     sync.Mutex
}

func newRecordingCollector() *recordingCollector {
	return &recordingCollector{counts: make(map[string]map[string]int)}
}

func (r *recordingCollector) CounterInc(name string, labels metrics.Labels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts[name] == nil {
		r.counts[name] = make(map[string]int)
	}
	r.counts[name][labels["level"]+":"+labels["component"]]++
}

func (r *recordingCollector) get(name, level, component string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[name][level+":"+component]
}

func TestTopLevelComponent(t *testing.T) {
	cases := map[string]string{
		"":               "unnamed",
		"raft":           "raft",
		"raft.node":      "raft",
		"pg.pg.app:pg":   "pg",
		"cluster.gossip": "cluster",
		"a.b.c.d":        "a",
	}
	for input, want := range cases {
		if got := topLevelComponent(input); got != want {
			t.Errorf("topLevelComponent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEmissionHookNoCollector(t *testing.T) {
	// Reset to nil and confirm hook is a clean no-op.
	SetEmissionCollector(nil)
	t.Cleanup(func() { SetEmissionCollector(nil) })

	if err := EmissionHook(zapcore.Entry{
		Level:      zapcore.WarnLevel,
		LoggerName: "raft.node",
	}); err != nil {
		t.Fatalf("hook returned error with no collector: %v", err)
	}
}

func TestEmissionHookIncrements(t *testing.T) {
	rec := newRecordingCollector()
	SetEmissionCollector(rec)
	t.Cleanup(func() { SetEmissionCollector(nil) })

	entries := []zapcore.Entry{
		{Level: zapcore.WarnLevel, LoggerName: "raft.node"},
		{Level: zapcore.WarnLevel, LoggerName: "raft.transport"},
		{Level: zapcore.ErrorLevel, LoggerName: "pg.pg.app:pg"},
		{Level: zapcore.InfoLevel, LoggerName: ""},
	}
	for _, e := range entries {
		if err := EmissionHook(e); err != nil {
			t.Fatalf("hook error for %+v: %v", e, err)
		}
	}

	if got := rec.get(EmissionMetricName, "warn", "raft"); got != 2 {
		t.Errorf("raft warns: got %d, want 2", got)
	}
	if got := rec.get(EmissionMetricName, "error", "pg"); got != 1 {
		t.Errorf("pg errors: got %d, want 1", got)
	}
	if got := rec.get(EmissionMetricName, "info", "unnamed"); got != 1 {
		t.Errorf("unnamed info: got %d, want 1", got)
	}
}

func TestSetEmissionCollectorDetach(t *testing.T) {
	rec := newRecordingCollector()
	SetEmissionCollector(rec)

	if err := EmissionHook(zapcore.Entry{Level: zapcore.WarnLevel, LoggerName: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := rec.get(EmissionMetricName, "warn", "x"); got != 1 {
		t.Fatalf("baseline: got %d, want 1", got)
	}

	SetEmissionCollector(nil)
	t.Cleanup(func() { SetEmissionCollector(nil) })

	if err := EmissionHook(zapcore.Entry{Level: zapcore.WarnLevel, LoggerName: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := rec.get(EmissionMetricName, "warn", "x"); got != 1 {
		t.Errorf("post-detach: got %d, want 1 (no further increments)", got)
	}
}
