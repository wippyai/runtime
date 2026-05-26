// SPDX-License-Identifier: MPL-2.0

// Package clock provides time-related command handlers for the dispatcher system.
package clock

import (
	"context"
	"sync"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// pendingStopTTL is how long an "early stop" tombstone is held after a
// matching start fails to arrive. The tombstone is the correctness path
// for the start/stop race (consumed by the late start). The TTL only
// reclaims memory for truly lost starts (e.g. process killed after
// issuing stop, before issuing start).
const pendingStopTTL = 5 * time.Minute

// chIDKey identifies an ephemeral router entry by (target pid, router
// epoch, chID). Used in both reverseMap and pendingStops.
type chIDKey struct {
	pidNode pid.NodeID
	pidHost pid.HostID
	pidUniq string
	epoch   uint64
	chID    uint64
}

func makeChIDKey(p pid.PID, epoch, chID uint64) chIDKey {
	return chIDKey{
		pidNode: p.Node,
		pidHost: p.Host,
		pidUniq: p.UniqID,
		epoch:   epoch,
		chID:    chID,
	}
}

// Dispatcher handles clock commands.
//
// The dispatcher tracks router-driven entries by (target pid, router
// epoch, chID) so they can be cancelled via TimerStopByChID /
// TickerStopByChID without holding the dispatcher-assigned internal id.
// A stop arriving before its matching start is tombstoned in
// pendingStops; the late start consumes the tombstone and refuses to
// schedule, completing its yield with ErrStoppedBeforeStart.
type Dispatcher struct {
	timers         *timerRegistry
	tickers        *tickerRegistry
	timerReverse   map[chIDKey]uint64
	tickerReverse  map[chIDKey]uint64
	pendingTStops  map[chIDKey]time.Time
	pendingTkStops map[chIDKey]time.Time
	sweepCancel    context.CancelFunc
	sweepDone      chan struct{}
	mu             sync.Mutex
}

func shouldIgnoreDuration(d time.Duration) bool {
	return d <= 0
}

// NewDispatcher creates a clock dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		timers:         newTimerRegistry(),
		tickers:        newTickerRegistry(),
		timerReverse:   make(map[chIDKey]uint64),
		tickerReverse:  make(map[chIDKey]uint64),
		pendingTStops:  make(map[chIDKey]time.Time),
		pendingTkStops: make(map[chIDKey]time.Time),
	}
}

// Start launches the pendingStops sweeper.
func (d *Dispatcher) Start(ctx context.Context) error {
	sweepCtx, cancel := context.WithCancel(ctx)
	d.sweepCancel = cancel
	d.sweepDone = make(chan struct{})
	go d.runPendingStopsSweeper(sweepCtx)
	return nil
}

// Stop shuts down timers, tickers, and the sweeper.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.sweepCancel != nil {
		d.sweepCancel()
		<-d.sweepDone
	}
	if d.timers != nil {
		d.timers.close()
	}
	if d.tickers != nil {
		d.tickers.close()
	}
	d.mu.Lock()
	clear(d.timerReverse)
	clear(d.tickerReverse)
	clear(d.pendingTStops)
	clear(d.pendingTkStops)
	d.mu.Unlock()
	return nil
}

// RegisterAll registers all clock handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(clockapi.Sleep, dispatcher.HandlerFunc(d.handleSleep))
	register(clockapi.TickerStart, dispatcher.HandlerFunc(d.handleTickerStart))
	register(clockapi.TickerStop, dispatcher.HandlerFunc(d.handleTickerStop))
	register(clockapi.TickerStopByChID, dispatcher.HandlerFunc(d.handleTickerStopByChID))
	register(clockapi.TimerStart, dispatcher.HandlerFunc(d.handleTimerStart))
	register(clockapi.TimerWait, dispatcher.HandlerFunc(d.handleTimerWait))
	register(clockapi.TimerStop, dispatcher.HandlerFunc(d.handleTimerStop))
	register(clockapi.TimerStopByChID, dispatcher.HandlerFunc(d.handleTimerStopByChID))
	register(clockapi.TimerReset, dispatcher.HandlerFunc(d.handleTimerReset))
}

func (d *Dispatcher) handleSleep(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.SleepCmd)
	if shouldIgnoreDuration(c.Duration) {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}
	time.AfterFunc(c.Duration, func() {
		receiver.CompleteYield(tag, nil, nil)
	})
	return nil
}

func (d *Dispatcher) handleTickerStart(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TickerStartCmd)
	if shouldIgnoreDuration(c.Duration) {
		return nil
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return nil
	}

	routerKey, useRouter := d.tryConsumeTickerTombstone(c)
	if useRouter && routerKey == nil {
		// Stop arrived first and tombstone consumed; refuse to schedule.
		receiver.CompleteYield(tag, nil, clockapi.ErrStoppedBeforeStart)
		return nil
	}

	fire := d.tickerFireFn(c, node)
	// Reserve before installing the reverse-map entry, then arm last so
	// the ticker cannot fire before its reverse-map key exists.
	id := d.tickers.reserve(ctx, c.PID, c.Topic, fire, routerKey)
	if routerKey != nil {
		d.mu.Lock()
		d.tickerReverse[*routerKey] = id
		d.mu.Unlock()
	}
	d.tickers.arm(id, c.Duration, node)

	receiver.CompleteYield(tag, clockapi.TickerStartResult{
		ID: id,
		Stop: func() {
			// Best-effort; ticker may have already self-cleaned via
			// the engine drain path.
			_ = d.stopTickerByID(id)
		},
	}, nil)
	return nil
}

func (d *Dispatcher) handleTickerStop(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TickerStopCmd)
	if err := d.stopTickerByID(c.TickerID); err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, nil, nil)
	return nil
}

func (d *Dispatcher) handleTickerStopByChID(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TickerStopByChIDCmd)
	key := makeChIDKey(c.TargetPID, c.Epoch, c.ChID)

	d.mu.Lock()
	id, ok := d.tickerReverse[key]
	if ok {
		delete(d.tickerReverse, key)
		d.mu.Unlock()
		_ = d.tickers.stop(id) // ticker may have already self-stopped; tolerate
		receiver.CompleteYield(tag, true, nil)
		return nil
	}
	// Stop arrived before the matching start; tombstone so the late
	// start can consume it.
	d.pendingTkStops[key] = time.Now()
	d.mu.Unlock()
	receiver.CompleteYield(tag, false, nil)
	return nil
}

func (d *Dispatcher) handleTimerStart(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerStartCmd)
	if shouldIgnoreDuration(c.Duration) {
		if c.Build == nil {
			return nil
		}
		node := relay.GetNode(ctx)
		if node == nil {
			return nil
		}
		fire := d.timerFireFn(c, node)
		var gen uint64
		if c.GenRef != nil {
			gen = c.GenRef.Load()
		}
		fire(gen)
		receiver.CompleteYield(tag, clockapi.TimerStartResult{
			Stop: func() {},
		}, nil)
		return nil
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return nil
	}

	routerKey, useRouter := d.tryConsumeTimerTombstone(c)
	if useRouter && routerKey == nil {
		receiver.CompleteYield(tag, nil, clockapi.ErrStoppedBeforeStart)
		return nil
	}

	fire := d.timerFireFn(c, node)

	var onFireCleanup func()
	if routerKey != nil {
		k := *routerKey
		onFireCleanup = func() {
			d.mu.Lock()
			delete(d.timerReverse, k)
			d.mu.Unlock()
		}
	}

	// Reserve before installing the reverse-map entry, then arm last:
	// the timer cannot fire (and its onFireCleanup cannot run) until the
	// reverse-map key exists, so a sub-µs fire can never orphan it.
	id := d.timers.reserve(fire, routerKey, onFireCleanup, c.GenRef)
	if routerKey != nil {
		d.mu.Lock()
		d.timerReverse[*routerKey] = id
		d.mu.Unlock()
	}
	d.timers.arm(id, c.Duration)

	receiver.CompleteYield(tag, clockapi.TimerStartResult{
		ID: id,
		Stop: func() {
			// Best-effort; timer may have already fired or been drained
			// via the engine drain path.
			_, _ = d.stopTimerByID(id)
		},
	}, nil)
	return nil
}

func (d *Dispatcher) handleTimerWait(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerWaitCmd)
	go func() {
		t, err := d.timers.wait(ctx, c.TimerID)
		if ctx.Err() != nil {
			receiver.CompleteYield(tag, nil, ctx.Err())
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, nil, err)
			return
		}
		receiver.CompleteYield(tag, t.UnixNano(), nil)
	}()
	return nil
}

func (d *Dispatcher) handleTimerStop(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerStopCmd)
	stopped, err := d.stopTimerByID(c.TimerID)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, stopped, nil)
	return nil
}

func (d *Dispatcher) handleTimerStopByChID(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerStopByChIDCmd)
	key := makeChIDKey(c.TargetPID, c.Epoch, c.ChID)

	d.mu.Lock()
	id, ok := d.timerReverse[key]
	if ok {
		delete(d.timerReverse, key)
		d.mu.Unlock()
		stopped, _ := d.timers.stop(id) // tolerate timer already fired
		receiver.CompleteYield(tag, stopped, nil)
		return nil
	}
	d.pendingTStops[key] = time.Now()
	d.mu.Unlock()
	receiver.CompleteYield(tag, false, nil)
	return nil
}

func (d *Dispatcher) handleTimerReset(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(clockapi.TimerResetCmd)
	if shouldIgnoreDuration(c.Duration) {
		return nil
	}
	wasActive, err := d.timers.reset(c.TimerID, c.Duration)
	if err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}
	receiver.CompleteYield(tag, wasActive, nil)
	return nil
}

// stopTimerByID stops a timer by internal id and cleans the reverse map
// entry, if any. Idempotent.
func (d *Dispatcher) stopTimerByID(id uint64) (bool, error) {
	if key := d.timers.routerKey(id); key != nil {
		d.mu.Lock()
		delete(d.timerReverse, *key)
		d.mu.Unlock()
	}
	return d.timers.stop(id)
}

// stopTickerByID stops a ticker by internal id and cleans the reverse
// map entry, if any. Idempotent.
func (d *Dispatcher) stopTickerByID(id uint64) error {
	if key := d.tickers.routerKey(id); key != nil {
		d.mu.Lock()
		delete(d.tickerReverse, *key)
		d.mu.Unlock()
	}
	return d.tickers.stop(id)
}

// tryConsumeTimerTombstone returns (routerKey, useRouter). When ChID is
// zero, useRouter is false and the caller schedules the timer with the
// legacy fire callback. When ChID is non-zero, useRouter is true; if a
// stop tombstone already exists for the key the function returns
// (nil, true) and the caller must reply with ErrStoppedBeforeStart.
func (d *Dispatcher) tryConsumeTimerTombstone(c clockapi.TimerStartCmd) (*chIDKey, bool) {
	if c.ChID == 0 {
		return nil, false
	}
	key := makeChIDKey(c.PID, c.Epoch, c.ChID)
	d.mu.Lock()
	if _, stopped := d.pendingTStops[key]; stopped {
		delete(d.pendingTStops, key)
		d.mu.Unlock()
		return nil, true
	}
	d.mu.Unlock()
	return &key, true
}

func (d *Dispatcher) tryConsumeTickerTombstone(c clockapi.TickerStartCmd) (*chIDKey, bool) {
	if c.ChID == 0 {
		return nil, false
	}
	key := makeChIDKey(c.PID, c.Epoch, c.ChID)
	d.mu.Lock()
	if _, stopped := d.pendingTkStops[key]; stopped {
		delete(d.pendingTkStops, key)
		d.mu.Unlock()
		return nil, true
	}
	d.mu.Unlock()
	return &key, true
}

// timerFireFn returns the callback the timer registry invokes when the
// timer expires. When Build is set, the registry supplies the arm
// generation captured at start/reset and the returned payload is followed
// by a terminal payload. Otherwise the legacy int64-nanos payload is sent
// with the same terminal marker.
func (d *Dispatcher) timerFireFn(c clockapi.TimerStartCmd, node relay.Node) timerCallback {
	if c.Build != nil {
		build := c.Build
		target := c.PID
		topic := c.Topic
		return func(gen uint64) {
			now := time.Now()
			p := build(now, gen)
			if p == nil {
				pkg := relay.NewPackage(pid.PID{}, target, topic, payload.NewTerminal())
				_ = node.Send(pkg)
				return
			}
			pkg := relay.NewPackage(pid.PID{}, target, topic, p, payload.NewTerminal())
			_ = node.Send(pkg)
		}
	}
	target := c.PID
	topic := c.Topic
	return func(uint64) {
		sendTimerFire(node, target, topic, time.Now())
	}
}

func (d *Dispatcher) tickerFireFn(c clockapi.TickerStartCmd, node relay.Node) func(at time.Time) {
	if c.Build != nil {
		build := c.Build
		genRef := c.GenRef
		target := c.PID
		topic := c.Topic
		return func(at time.Time) {
			var gen uint64
			if genRef != nil {
				gen = genRef.Load()
			}
			p := build(at, gen)
			if p == nil {
				return
			}
			pkg := relay.NewPackage(pid.PID{}, target, topic, p)
			_ = node.Send(pkg)
		}
	}
	return nil // ticker registry falls back to legacy sendTick when fire is nil
}

// runPendingStopsSweeper periodically expires tombstones that haven't been
// claimed by a matching start. TTL is generous; this is for memory
// hygiene only — the correctness path is "late start consumes
// tombstone."
func (d *Dispatcher) runPendingStopsSweeper(ctx context.Context) {
	defer close(d.sweepDone)
	t := time.NewTicker(pendingStopTTL / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cutoff := time.Now().Add(-pendingStopTTL)
			d.mu.Lock()
			for k, ts := range d.pendingTStops {
				if ts.Before(cutoff) {
					delete(d.pendingTStops, k)
				}
			}
			for k, ts := range d.pendingTkStops {
				if ts.Before(cutoff) {
					delete(d.pendingTkStops, k)
				}
			}
			d.mu.Unlock()
		}
	}
}

func sendTick(node relay.Node, target pid.PID, topic string, at time.Time) {
	p := payload.NewPayload(at.UnixNano(), payload.Golang)
	pkg := relay.NewPackage(pid.PID{}, target, topic, p)
	_ = node.Send(pkg)
}

func sendTimerFire(node relay.Node, target pid.PID, topic string, at time.Time) {
	p := payload.NewPayload(at.UnixNano(), payload.Golang)
	pkg := relay.NewPackage(pid.PID{}, target, topic, p, payload.NewTerminal())
	_ = node.Send(pkg)
}

// TickerCount returns the number of active tickers.
func (d *Dispatcher) TickerCount() int {
	return d.tickers.count()
}

// TimerCount returns the number of active timers.
func (d *Dispatcher) TimerCount() int {
	return d.timers.count()
}

// ReverseMapSize returns the count of router-tracked entries (for tests).
func (d *Dispatcher) ReverseMapSize() (timers, tickers int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.timerReverse), len(d.tickerReverse)
}

// PendingStopsSize returns the count of outstanding stop tombstones (for tests).
func (d *Dispatcher) PendingStopsSize() (timers, tickers int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pendingTStops), len(d.pendingTkStops)
}
