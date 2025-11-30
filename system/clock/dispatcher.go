// Package clock provides time-related command handlers for the dispatcher system.
// Supports both blocking (for testing) and async (for production) execution modes.
// Uses timing wheel for efficient timer management.
package clock

import (
	"context"
	"sync"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/workflow"
)

// job represents a unit of work for the async dispatcher.
type job struct {
	ctx  context.Context
	cmd  dispatcher.Command
	emit dispatcher.EmitFunc
}

// Dispatcher handles clock commands with configurable execution mode.
// Uses timing wheel internally for efficient timer management.
type Dispatcher struct {
	workers int
	jobs    chan job
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc

	// wheel is the timing wheel for efficient timer/sleep management
	wheel *WheelTimerRegistry
}

// Config holds dispatcher configuration.
type Config struct {
	// Workers is the number of worker goroutines for async mode.
	// If 0, dispatcher runs in blocking mode (synchronous execution).
	Workers int
}

// NewDispatcher creates a new clock dispatcher with the given configuration.
func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{
		workers: cfg.Workers,
		wheel:   NewWheelTimerRegistry(),
	}
}

// NewBlockingDispatcher creates a dispatcher that executes synchronously.
func NewBlockingDispatcher() *Dispatcher {
	return &Dispatcher{
		workers: 0,
		wheel:   NewWheelTimerRegistry(),
	}
}

// NewAsyncDispatcher creates a dispatcher with a worker pool.
func NewAsyncDispatcher(workers int) *Dispatcher {
	if workers <= 0 {
		workers = 4
	}
	return &Dispatcher{
		workers: workers,
		wheel:   NewWheelTimerRegistry(),
	}
}

// Start initializes the dispatcher. For async mode, starts worker goroutines.
func (d *Dispatcher) Start(ctx context.Context) error {
	if d.workers <= 0 {
		return nil
	}

	d.ctx, d.cancel = context.WithCancel(ctx)
	d.jobs = make(chan job, d.workers*2)

	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}

	return nil
}

// Stop shuts down the dispatcher and waits for workers to finish.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.wheel != nil {
		d.wheel.Close()
	}

	if d.workers <= 0 {
		return nil
	}

	d.cancel()
	close(d.jobs)
	d.wg.Wait()
	return nil
}

// worker processes jobs from the queue.
func (d *Dispatcher) worker() {
	defer d.wg.Done()

	for j := range d.jobs {
		d.execute(j.ctx, j.cmd, j.emit)
	}
}

// submit sends a job to the worker pool.
func (d *Dispatcher) submit(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	select {
	case d.jobs <- job{ctx: ctx, cmd: cmd, emit: emit}:
	case <-d.ctx.Done():
	}
}

// isAsync returns true if dispatcher is in async mode.
func (d *Dispatcher) isAsync() bool {
	return d.workers > 0 && d.jobs != nil
}

// execute runs the clock operation and emits the result.
func (d *Dispatcher) execute(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) {
	switch c := cmd.(type) {
	case clockapi.SleepCmd:
		d.executeSleep(ctx, c, emit)
	case clockapi.NowCmd:
		executeNow(ctx, emit)
	case clockapi.AfterCmd:
		executeAfter(ctx, c, emit)
	case clockapi.TickerStartCmd:
		executeTickerStart(ctx, c, emit)
	case clockapi.TickerNextCmd:
		executeTickerNext(ctx, c, emit)
	case clockapi.TickerStopCmd:
		executeTickerStop(ctx, c, emit)
	case clockapi.TimerStartCmd:
		d.executeTimerStart(ctx, c, emit)
	case clockapi.TimerWaitCmd:
		d.executeTimerWait(ctx, c, emit)
	case clockapi.TimerStopCmd:
		d.executeTimerStop(ctx, c, emit)
	case clockapi.TimerResetCmd:
		d.executeTimerReset(ctx, c, emit)
	}
}

func (d *Dispatcher) executeSleep(ctx context.Context, cmd clockapi.SleepCmd, emit dispatcher.EmitFunc) {
	if cmd.Duration <= 0 {
		emit(nil)
		return
	}

	// Use wheel with callback for non-blocking sleep
	d.wheel.StartWithCallback(cmd.Duration, func() {
		emit(nil)
	})
}

func executeNow(ctx context.Context, emit dispatcher.EmitFunc) {
	if ref := workflow.GetTimeReference(ctx); ref != nil {
		emit(ref.Now().UnixNano())
		return
	}
	emit(time.Now().UnixNano())
}

func executeAfter(ctx context.Context, cmd clockapi.AfterCmd, emit dispatcher.EmitFunc) {
	if cmd.Duration <= 0 {
		return
	}

	registry := GetOrCreateAfterRegistry(ctx)
	id := registry.Create(ctx, cmd.Duration)
	emit(&AfterResult{ChannelID: id})
}

func executeTickerStart(ctx context.Context, cmd clockapi.TickerStartCmd, emit dispatcher.EmitFunc) {
	if cmd.Duration <= 0 {
		return
	}

	registry := GetOrCreateTickerRegistry(ctx)
	id := registry.Start(cmd.Duration)
	emit(id)
}

func executeTickerNext(ctx context.Context, cmd clockapi.TickerNextCmd, emit dispatcher.EmitFunc) {
	registry := GetTickerRegistry(ctx)
	if registry == nil {
		return
	}

	t, err := registry.Next(ctx, cmd.TickerID)
	if err != nil {
		return
	}
	emit(t.UnixNano())
}

func executeTickerStop(ctx context.Context, cmd clockapi.TickerStopCmd, emit dispatcher.EmitFunc) {
	registry := GetTickerRegistry(ctx)
	if registry == nil {
		return
	}

	if err := registry.Stop(cmd.TickerID); err != nil {
		return
	}
	emit(nil)
}

func (d *Dispatcher) executeTimerStart(_ context.Context, cmd clockapi.TimerStartCmd, emit dispatcher.EmitFunc) {
	if cmd.Duration <= 0 {
		return
	}

	id := d.wheel.Start(cmd.Duration)
	emit(id)
}

func (d *Dispatcher) executeTimerWait(ctx context.Context, cmd clockapi.TimerWaitCmd, emit dispatcher.EmitFunc) {
	t, err := d.wheel.Wait(ctx, cmd.TimerID)
	if err != nil {
		return
	}
	emit(t.UnixNano())
}

func (d *Dispatcher) executeTimerStop(_ context.Context, cmd clockapi.TimerStopCmd, emit dispatcher.EmitFunc) {
	stopped, err := d.wheel.Stop(cmd.TimerID)
	if err != nil {
		return
	}
	emit(stopped)
}

func (d *Dispatcher) executeTimerReset(_ context.Context, cmd clockapi.TimerResetCmd, emit dispatcher.EmitFunc) {
	if cmd.Duration <= 0 {
		return
	}

	wasActive, err := d.wheel.Reset(cmd.TimerID, cmd.Duration)
	if err != nil {
		return
	}
	emit(wasActive)
}

// SleepHandler handles sleep commands.
type SleepHandler struct {
	d *Dispatcher
}

func (h *SleepHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// NowHandler handles now commands.
type NowHandler struct {
	d *Dispatcher
}

func (h *NowHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// AfterHandler handles after commands.
type AfterHandler struct {
	d *Dispatcher
}

func (h *AfterHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// TickerStartHandler handles ticker start commands.
type TickerStartHandler struct {
	d *Dispatcher
}

func (h *TickerStartHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// TickerNextHandler handles ticker next commands.
type TickerNextHandler struct {
	d *Dispatcher
}

func (h *TickerNextHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// TickerStopHandler handles ticker stop commands.
type TickerStopHandler struct {
	d *Dispatcher
}

func (h *TickerStopHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// TimerStartHandler handles timer start commands.
type TimerStartHandler struct {
	d *Dispatcher
}

func (h *TimerStartHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// TimerWaitHandler handles timer wait commands.
type TimerWaitHandler struct {
	d *Dispatcher
}

func (h *TimerWaitHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// TimerStopHandler handles timer stop commands.
type TimerStopHandler struct {
	d *Dispatcher
}

func (h *TimerStopHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// TimerResetHandler handles timer reset commands.
type TimerResetHandler struct {
	d *Dispatcher
}

func (h *TimerResetHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	if h.d.isAsync() {
		h.d.submit(ctx, cmd, emit)
		return nil
	}
	h.d.execute(ctx, cmd, emit)
	return nil
}

// RegisterAll registers all clock handlers with the given registry function.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(clockapi.CmdSleep, &SleepHandler{d: d})
	register(clockapi.CmdNow, &NowHandler{d: d})
	register(clockapi.CmdAfter, &AfterHandler{d: d})
	register(clockapi.CmdTickerStart, &TickerStartHandler{d: d})
	register(clockapi.CmdTickerNext, &TickerNextHandler{d: d})
	register(clockapi.CmdTickerStop, &TickerStopHandler{d: d})
	register(clockapi.CmdTimerStart, &TimerStartHandler{d: d})
	register(clockapi.CmdTimerWait, &TimerWaitHandler{d: d})
	register(clockapi.CmdTimerStop, &TimerStopHandler{d: d})
	register(clockapi.CmdTimerReset, &TimerResetHandler{d: d})
}

// AfterResult contains the channel and cleanup info for time.after().
type AfterResult struct {
	ChannelID uint64
}

// Service is an alias for Dispatcher for backward compatibility.
type Service = Dispatcher

// NewService creates a blocking dispatcher for backward compatibility.
func NewService() *Dispatcher {
	return NewBlockingDispatcher()
}
