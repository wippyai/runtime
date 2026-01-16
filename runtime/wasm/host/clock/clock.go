// Package clock provides the clock host module for WASM.
package clock

import (
	"time"

	"github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/host"
)

// Namespace is the WIT namespace for clock functions.
const Namespace = "wippy:clock"

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
		Description: "Time operations: sleep, timer",
		Class:       []string{wasmapi.ClassTime, wasmapi.ClassNondeterministic},
	}
}

// Register returns the host registration.
func (h *Host) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"sleep":       host.MakeAsyncHandler(makeSleepCmd),
			"timer-start": host.MakeAsyncHandler(makeTimerStartCmd),
			"timer-wait":  host.MakeAsyncHandler(makeTimerWaitCmd),
			"timer-stop":  host.MakeAsyncHandler(makeTimerStopCmd),
		},
		YieldTypes: []wasmapi.YieldType{
			{CmdID: clock.Sleep},
			{CmdID: clock.TimerStart},
			{CmdID: clock.TimerWait},
			{CmdID: clock.TimerStop},
		},
	}
}

// makeSleepCmd creates a SleepCmd from stack args.
func makeSleepCmd(stack []uint64) dispatcher.Command {
	var duration time.Duration
	if len(stack) > 0 {
		duration = time.Duration(int64(stack[0]))
	}
	return clock.SleepCmd{Duration: duration}
}

// makeTimerStartCmd creates a TimerStartCmd from stack args.
func makeTimerStartCmd(stack []uint64) dispatcher.Command {
	var duration time.Duration
	if len(stack) > 0 {
		duration = time.Duration(int64(stack[0]))
	}
	// Note: PID and Topic are set by the scheduler based on current context
	return clock.TimerStartCmd{Duration: duration}
}

// makeTimerWaitCmd creates a TimerWaitCmd from stack args.
func makeTimerWaitCmd(stack []uint64) dispatcher.Command {
	var timerID uint64
	if len(stack) > 0 {
		timerID = stack[0]
	}
	return clock.TimerWaitCmd{TimerID: timerID}
}

// makeTimerStopCmd creates a TimerStopCmd from stack args.
func makeTimerStopCmd(stack []uint64) dispatcher.Command {
	var timerID uint64
	if len(stack) > 0 {
		timerID = stack[0]
	}
	return clock.TimerStopCmd{TimerID: timerID}
}

// WIT definition for the clock interface.
const WIT = `package wippy:clock@0.1.0;

interface clock {
    // Sleep pauses execution for the given duration in nanoseconds.
    sleep: func(duration-ns: s64);

    // Timer functions (decomposed pattern)
    timer-start: func(duration-ns: s64) -> u64;
    timer-wait: func(timer-id: u64) -> s64;
    timer-stop: func(timer-id: u64) -> u32;
}

world with-clock {
    import clock;
}
`

// compile-time check
var _ wasmapi.Host = (*Host)(nil)
