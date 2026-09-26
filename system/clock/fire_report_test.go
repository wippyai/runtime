// SPDX-License-Identifier: MPL-2.0

package clock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// failingNode rejects every delivery with err.
type failingNode struct {
	mockNode
	err error
}

func (n *failingNode) Send(*relay.Package) error { return n.err }

type countingCollector struct {
	counts map[string]int
	mu     sync.Mutex
}

func (c *countingCollector) CounterInc(name string, _ metrics.Labels) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name]++
}

func (c *countingCollector) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

func (c *countingCollector) CounterAdd(string, float64, metrics.Labels)       {}
func (c *countingCollector) GaugeSet(string, float64, metrics.Labels)         {}
func (c *countingCollector) GaugeInc(string, metrics.Labels)                  {}
func (c *countingCollector) GaugeDec(string, metrics.Labels)                  {}
func (c *countingCollector) HistogramObserve(string, float64, metrics.Labels) {}
func (c *countingCollector) RegisterExporter(metrics.Exporter) error          { return nil }
func (c *countingCollector) Close() error                                     { return nil }

func startTimer(ctx context.Context, t *testing.T, d *Dispatcher, cmd clockapi.TimerStartCmd) {
	t.Helper()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	started := make(chan error, 1)
	require.NoError(t, handlers[clockapi.TimerStart].Handle(ctx, cmd, 1, &testReceiver{fn: func(_ any, err error) { started <- err }}))
	require.NoError(t, <-started)
}

// A timer fire the node cannot deliver is logged with its target and
// counted, not dropped silently.
func TestTimerFireDeliveryFailureIsReported(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	coll := &countingCollector{counts: map[string]int{}}
	d := NewDispatcher(zap.New(core), coll)
	defer func() { _ = d.Stop(context.Background()) }()

	ctx := relay.WithNode(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()),
		&failingNode{err: errors.New("mailbox unavailable")})
	target := samplePID("fire-target")
	startTimer(ctx, t, d, clockapi.TimerStartCmd{Duration: time.Millisecond, PID: target, Topic: "after@1"})

	require.Eventually(t, func() bool { return logs.FilterMessage("clock fire not delivered").Len() == 1 }, time.Second, time.Millisecond)
	entry := logs.FilterMessage("clock fire not delivered").All()[0]
	require.Equal(t, target.String(), entry.ContextMap()["target"])
	require.Contains(t, entry.ContextMap()["error"], "mailbox unavailable")
	require.Equal(t, 1, coll.count(fireFailedMetric))
}

// A fire to a process that is already gone is expected and stays quiet.
func TestTimerFireToFinishedProcessIsNotReported(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	coll := &countingCollector{counts: map[string]int{}}
	d := NewDispatcher(zap.New(core), coll)
	defer func() { _ = d.Stop(context.Background()) }()

	ctx := relay.WithNode(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()),
		&failingNode{err: process.ErrProcessNotFound})
	startTimer(ctx, t, d, clockapi.TimerStartCmd{Duration: time.Millisecond, PID: samplePID("gone"), Topic: "after@2"})

	require.Eventually(t, func() bool { return d.TimerCount() == 0 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, logs.Len())
	require.Zero(t, coll.count(fireFailedMetric))
}

// A panic while building a fire is logged with its stack and counted, and
// the dispatcher keeps firing later timers.
func TestTimerFirePanicIsReportedWithoutCrashing(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	coll := &countingCollector{counts: map[string]int{}}
	d := NewDispatcher(zap.New(core), coll)
	defer func() { _ = d.Stop(context.Background()) }()

	node := newCapturingNode()
	ctx := relay.WithNode(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), node)
	startTimer(ctx, t, d, clockapi.TimerStartCmd{Duration: time.Millisecond, PID: samplePID("panics"), Topic: "after@3",
		Build: func(time.Time, uint64) payload.Payload { panic("build exploded") }})

	require.Eventually(t, func() bool { return logs.FilterMessage("clock fire panicked").Len() == 1 }, time.Second, time.Millisecond)
	entry := logs.FilterMessage("clock fire panicked").All()[0]
	require.Contains(t, entry.ContextMap()["panic"], "build exploded")
	require.NotEmpty(t, entry.ContextMap()["stack"])
	require.Equal(t, 1, coll.count(fireFailedMetric))

	startTimer(ctx, t, d, clockapi.TimerStartCmd{Duration: time.Millisecond, PID: samplePID("after-panic"), Topic: "after@4"})
	require.Eventually(t, func() bool { return len(node.snapshot()) == 1 }, time.Second, time.Millisecond)
}

// A panic while building a tick is reported and the ticker keeps ticking;
// it does not take down the runtime.
func TestTickerFirePanicIsReportedAndTickingContinues(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	d := NewDispatcher(zap.New(core), nil)
	defer func() { _ = d.Stop(context.Background()) }()

	ctx := relay.WithNode(ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext()), newCapturingNode())
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	started := make(chan error, 1)
	require.NoError(t, handlers[clockapi.TickerStart].Handle(ctx, clockapi.TickerStartCmd{
		Duration: time.Millisecond, PID: samplePID("ticks"), Topic: "ticker@1",
		Build: func(time.Time, uint64) payload.Payload { panic("tick exploded") },
	}, 1, &testReceiver{fn: func(_ any, err error) { started <- err }}))
	require.NoError(t, <-started)

	require.Eventually(t, func() bool { return logs.FilterMessage("clock fire panicked").Len() >= 3 }, time.Second, time.Millisecond)
}
