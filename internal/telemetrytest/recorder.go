// SPDX-License-Identifier: MPL-2.0

// Package telemetrytest provides in-memory test doubles for metric and trace
// recording, used by subsystem telemetry_test.go files.
package telemetrytest

import (
	"sort"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/metrics"
)

type sample struct {
	value float64
	count uint64
}

type Recorder struct {
	samples map[string]map[string]*sample
	mu      sync.Mutex
}

func NewRecorder() *Recorder {
	return &Recorder{samples: make(map[string]map[string]*sample)}
}

func labelKey(labels metrics.Labels) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

func (r *Recorder) bucket(name, key string) *sample {
	m, ok := r.samples[name]
	if !ok {
		m = make(map[string]*sample)
		r.samples[name] = m
	}
	s, ok := m[key]
	if !ok {
		s = &sample{}
		m[key] = s
	}
	return s
}

func (r *Recorder) CounterInc(name string, labels metrics.Labels) {
	r.CounterAdd(name, 1, labels)
}

func (r *Recorder) CounterAdd(name string, delta float64, labels metrics.Labels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.bucket(name, labelKey(labels))
	s.value += delta
}

func (r *Recorder) GaugeSet(name string, value float64, labels metrics.Labels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.bucket(name, labelKey(labels))
	s.value = value
}

func (r *Recorder) GaugeInc(name string, labels metrics.Labels) {
	r.CounterAdd(name, 1, labels)
}

func (r *Recorder) GaugeDec(name string, labels metrics.Labels) {
	r.CounterAdd(name, -1, labels)
}

func (r *Recorder) HistogramObserve(name string, value float64, labels metrics.Labels) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.bucket(name, labelKey(labels))
	s.value += value
	s.count++
}

func (r *Recorder) RegisterExporter(_ metrics.Exporter) error { return nil }

func (r *Recorder) Close() error { return nil }

func (r *Recorder) CounterValue(name string, labels metrics.Labels) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.samples[name]; ok {
		if s, ok := m[labelKey(labels)]; ok {
			return s.value
		}
	}
	return 0
}

func (r *Recorder) GaugeValue(name string, labels metrics.Labels) float64 {
	return r.CounterValue(name, labels)
}

func (r *Recorder) HistogramCount(name string, labels metrics.Labels) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.samples[name]; ok {
		if s, ok := m[labelKey(labels)]; ok {
			return s.count
		}
	}
	return 0
}

// Names returns all metric names recorded so far (sorted).
func (r *Recorder) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.samples))
	for k := range r.samples {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var _ metrics.Collector = (*Recorder)(nil)
