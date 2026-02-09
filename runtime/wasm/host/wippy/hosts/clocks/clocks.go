package clocks

import (
	"context"
	"errors"
	"fmt"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var (
	// ErrPollAsyncContextRequired indicates pollable.block was called without asyncify context.
	ErrPollAsyncContextRequired = errors.New("wasi poll host requires asyncify scheduler context")
	// ErrPollAsyncDispatchRequired indicates async poll operations must be routed by scheduler.
	ErrPollAsyncDispatchRequired = errors.New("wasi poll host requires scheduler-driven async dispatch")
)

const (
	// WallClockNamespace exposes wall clock functions for WASI preview2 components.
	WallClockNamespace = "wasi:clocks/wall-clock@0.2.3"
	// MonotonicClockNamespace exposes monotonic clock functions for WASI preview2 components.
	MonotonicClockNamespace = "wasi:clocks/monotonic-clock@0.2.8"
)

// Datetime mirrors the wasi:clocks/wall-clock datetime record.
type Datetime struct {
	Seconds     uint64
	Nanoseconds uint32
}

// WallClockHost exposes basic wall-clock reads.
type WallClockHost struct{}

// NewWallClockHost builds a wall-clock host.
func NewWallClockHost() *WallClockHost {
	return &WallClockHost{}
}

// Namespace implements wasm-runtime Host.
func (h *WallClockHost) Namespace() string {
	return WallClockNamespace
}

// Now returns current wall clock time.
func (h *WallClockHost) Now(_ context.Context) Datetime {
	now := time.Now()
	return Datetime{
		Seconds:     uint64(now.Unix()),
		Nanoseconds: uint32(now.Nanosecond()),
	}
}

// Resolution returns coarse wall clock resolution.
func (h *WallClockHost) Resolution(_ context.Context) Datetime {
	return Datetime{Seconds: 1, Nanoseconds: 0}
}

// MonotonicClockHost exposes monotonic clock reads.
type MonotonicClockHost struct {
	start     time.Time
	resources *preview2.ResourceTable
}

// NewMonotonicClockHost builds a monotonic clock host.
func NewMonotonicClockHost(resources *preview2.ResourceTable) *MonotonicClockHost {
	if resources == nil {
		resources = preview2.NewResourceTable()
	}
	return &MonotonicClockHost{
		start:     time.Now(),
		resources: resources,
	}
}

// Namespace implements wasm-runtime Host.
func (h *MonotonicClockHost) Namespace() string {
	return MonotonicClockNamespace
}

// Now returns monotonic nanoseconds since host creation.
func (h *MonotonicClockHost) Now(_ context.Context) uint64 {
	delta := time.Since(h.start)
	if delta < 0 {
		return 0
	}
	return uint64(delta.Nanoseconds())
}

// Resolution returns monotonic clock resolution in nanoseconds.
func (h *MonotonicClockHost) Resolution(_ context.Context) uint64 {
	return 1
}

// SubscribeInstant creates a timer pollable that becomes ready at the given instant.
func (h *MonotonicClockHost) SubscribeInstant(_ context.Context, when uint64) uint32 {
	if h.resources == nil {
		return 0
	}
	deadline := h.start.Add(time.Duration(when))
	return h.resources.Add(NewDispatcherTimerPollable(deadline))
}

// SubscribeDuration creates a timer pollable that becomes ready after duration nanoseconds.
func (h *MonotonicClockHost) SubscribeDuration(_ context.Context, duration uint64) uint32 {
	if h.resources == nil {
		return 0
	}
	deadline := time.Now().Add(time.Duration(duration))
	return h.resources.Add(NewDispatcherTimerPollable(deadline))
}

// DispatcherTimerPollable blocks via Wippy clock dispatcher; it never uses local timers for blocking.
type DispatcherTimerPollable struct {
	deadline time.Time
}

// NewDispatcherTimerPollable creates a timer pollable that integrates with dispatcher sleep.
func NewDispatcherTimerPollable(deadline time.Time) *DispatcherTimerPollable {
	return &DispatcherTimerPollable{
		deadline: deadline,
	}
}

// Type implements preview2.Resource.
func (p *DispatcherTimerPollable) Type() preview2.ResourceType {
	return preview2.ResourcePollable
}

// Drop implements preview2.Resource.
func (p *DispatcherTimerPollable) Drop() {}

// Ready reports whether timer deadline has elapsed.
func (p *DispatcherTimerPollable) Ready() bool {
	return !time.Now().Before(p.deadline)
}

// Remaining returns remaining time until deadline.
func (p *DispatcherTimerPollable) Remaining() time.Duration {
	remaining := time.Until(p.deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Block waits until timer is ready through dispatcher sleep command.
func (p *DispatcherTimerPollable) Block(ctx context.Context) {
	if p == nil {
		return
	}

	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		if _, err := wasmengine.Resume(ctx); err != nil {
			// Resume protocol failures are engine-level invariants; external/user
			// operation failures flow through EventYieldComplete, not through this path.
			panic(fmt.Errorf("pollable.block resume: %w", err))
		}
		return
	}

	remaining := p.Remaining()
	if remaining <= 0 {
		return
	}

	if async == nil {
		// pollable.block has no error return channel in WIT. Missing async context
		// is a host wiring invariant violation, so we must trap the current call.
		panic(ErrPollAsyncContextRequired)
	}

	op := &SleepPendingOp{duration: remaining}
	if err := wasmengine.Suspend(ctx, op); err != nil {
		// Suspend protocol failures are engine-level invariants; trap so scheduler
		// exits this process instead of continuing in an invalid state.
		panic(fmt.Errorf("pollable.block suspend: %w", err))
	}
}

// SleepPendingOp bridges asyncify suspension to Wippy clock dispatcher.
type SleepPendingOp struct {
	duration time.Duration
}

// CmdID implements wasm async pending op command ID.
func (o *SleepPendingOp) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(clockapi.Sleep)
}

// ToCommand returns dispatcher command for yield path.
func (o *SleepPendingOp) ToCommand() dispatcher.Command {
	return clockapi.SleepCmd{Duration: o.duration}
}

// Execute is used by standalone wasm-runtime scheduler loops.
// Wippy process execution uses ToCommand()+Step/Yield dispatch instead.
func (o *SleepPendingOp) Execute(ctx context.Context) (uint64, error) {
	_ = ctx
	return 0, ErrPollAsyncDispatchRequired
}
