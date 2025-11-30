// Package clock provides the clock host module for WASM.
// Implements wippy:clock with sleep, now, and timer functions.
package clock

import (
	"context"
	"time"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/host"
)

const (
	// Namespace is the WIT namespace for clock functions.
	Namespace = "wippy:clock"
)

// Host implements the clock host module.
type Host struct{}

// New creates a new clock host.
func New() *Host {
	return &Host{}
}

// Info returns host metadata.
func (h *Host) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   Namespace,
		Description: "Time operations: sleep, now, timer",
		Class:       []string{wasmapi.ClassTime, wasmapi.ClassNondeterministic},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *Host) Namespace() string {
	return Namespace
}

// Register returns the host registration.
func (h *Host) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"sleep":       host.MakeAsyncHandler(makeSleepCmd),
			"now":         host.MakeAsyncHandler(makeNowCmd),
			"timer_start": host.MakeAsyncHandler(makeTimerStartCmd),
			"timer_wait":  host.MakeAsyncHandler(makeTimerWaitCmd),
			"timer_stop":  host.MakeAsyncHandler(makeTimerStopCmd),
		},
		YieldTypes: []wasmapi.YieldType{
			{CmdID: clockapi.CmdSleep},
			{CmdID: clockapi.CmdNow},
			{CmdID: clockapi.CmdTimerStart},
			{CmdID: clockapi.CmdTimerWait},
			{CmdID: clockapi.CmdTimerStop},
		},
	}
}

// makeSleepCmd creates a SleepCmd from stack args (returns dispatcher.Command).
func makeSleepCmd(stack []uint64) dispatcher.Command {
	var duration time.Duration
	if len(stack) > 0 {
		duration = time.Duration(int64(stack[0]))
	}
	return clockapi.SleepCmd{Duration: duration}
}

// makeNowCmd creates a NowCmd (returns dispatcher.Command).
func makeNowCmd(_ []uint64) dispatcher.Command {
	return clockapi.NowCmd{}
}

// makeTimerStartCmd creates a TimerStartCmd from stack args.
func makeTimerStartCmd(stack []uint64) dispatcher.Command {
	var duration time.Duration
	if len(stack) > 0 {
		duration = time.Duration(int64(stack[0]))
	}
	return clockapi.TimerStartCmd{Duration: duration}
}

// makeTimerWaitCmd creates a TimerWaitCmd from stack args.
func makeTimerWaitCmd(stack []uint64) dispatcher.Command {
	var timerID uint64
	if len(stack) > 0 {
		timerID = stack[0]
	}
	return clockapi.TimerWaitCmd{TimerID: timerID}
}

// makeTimerStopCmd creates a TimerStopCmd from stack args.
func makeTimerStopCmd(stack []uint64) dispatcher.Command {
	var timerID uint64
	if len(stack) > 0 {
		timerID = stack[0]
	}
	return clockapi.TimerStopCmd{TimerID: timerID}
}

// WIT definition for the clock interface.
const WIT = `package wippy:clock@0.1.0;

interface clock {
    // Sleep pauses execution for the given duration in nanoseconds.
    sleep: func(duration-ns: s64);

    // Now returns the current time in nanoseconds since Unix epoch.
    now: func() -> s64;

    // Timer functions (decomposed pattern)
    // Creates a timer, returns timer ID
    timer-start: func(duration-ns: s64) -> u64;
    // Waits for timer to fire, returns fire time in nanoseconds
    timer-wait: func(timer-id: u64) -> s64;
    // Stops timer before it fires, returns true if stopped
    timer-stop: func(timer-id: u64) -> bool;
}

world with-clock {
    import clock;
}
`

// Compile-time check
var _ wasmapi.Host = (*Host)(nil)

// SleepHandler is a raw wazero handler for sleep (for direct registration).
func SleepHandler(ctx context.Context, mod api.Module, stack []uint64) {
	handler := host.MakeAsyncHandler(makeSleepCmd)
	handler(ctx, mod, stack)
}

// NowHandler is a raw wazero handler for now.
func NowHandler(ctx context.Context, mod api.Module, stack []uint64) {
	handler := host.MakeAsyncHandler(makeNowCmd)
	handler(ctx, mod, stack)
}

// TimerStartHandler is a raw wazero handler for timer_start.
func TimerStartHandler(ctx context.Context, mod api.Module, stack []uint64) {
	handler := host.MakeAsyncHandler(makeTimerStartCmd)
	handler(ctx, mod, stack)
}

// TimerWaitHandler is a raw wazero handler for timer_wait.
func TimerWaitHandler(ctx context.Context, mod api.Module, stack []uint64) {
	handler := host.MakeAsyncHandler(makeTimerWaitCmd)
	handler(ctx, mod, stack)
}

// TimerStopHandler is a raw wazero handler for timer_stop.
func TimerStopHandler(ctx context.Context, mod api.Module, stack []uint64) {
	handler := host.MakeAsyncHandler(makeTimerStopCmd)
	handler(ctx, mod, stack)
}
