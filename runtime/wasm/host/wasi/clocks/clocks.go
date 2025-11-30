// Package clocks implements wasi:clocks@0.2.8 for wippy.
// Provides wall-clock and monotonic clock interfaces.
package clocks

import (
	"context"
	"time"

	"github.com/tetratelabs/wazero/api"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/workflow"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

const (
	// WallClockNamespace is the WASI namespace for wall-clock.
	WallClockNamespace = "wasi:clocks/wall-clock@0.2.8"
	// MonotonicClockNamespace is the WASI namespace for monotonic-clock.
	MonotonicClockNamespace = "wasi:clocks/monotonic-clock@0.2.8"
)

var monotonicStart = time.Now()

// MonotonicStart returns the monotonic clock start time.
func MonotonicStart() time.Time {
	return monotonicStart
}

// WallClockHost implements wasi:clocks/wall-clock@0.2.8.
type WallClockHost struct{}

// NewWallClockHost creates a new wall-clock host.
func NewWallClockHost() *WallClockHost {
	return &WallClockHost{}
}

// Info returns host metadata.
func (h *WallClockHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   WallClockNamespace,
		Description: "WASI wall clock for current date/time",
		Class:       []string{wasmapi.ClassTime},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *WallClockHost) Namespace() string {
	return WallClockNamespace
}

// Register returns the host registration.
func (h *WallClockHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"now":        h.now,
			"resolution": h.resolution,
		},
	}
}

// now returns current wall-clock time as datetime.
// Stack: [] -> [seconds: u64, nanoseconds: u32]
func (h *WallClockHost) now(ctx context.Context, mod api.Module, stack []uint64) {
	t := wallNow(ctx)
	if len(stack) > 0 {
		stack[0] = uint64(t.Unix())
	}
	if len(stack) > 1 {
		stack[1] = uint64(t.Nanosecond())
	}
}

// resolution returns clock resolution as datetime.
// Stack: [] -> [seconds: u64, nanoseconds: u32]
func (h *WallClockHost) resolution(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) > 0 {
		stack[0] = 0
	}
	if len(stack) > 1 {
		stack[1] = 1
	}
}

// MonotonicClockHost implements wasi:clocks/monotonic-clock@0.2.8.
// Uses shared InstanceResources for pollable management.
type MonotonicClockHost struct {
	resources *resource.InstanceResources
}

// NewMonotonicClockHost creates a new monotonic-clock host with shared resources.
func NewMonotonicClockHost(resources *resource.InstanceResources) *MonotonicClockHost {
	return &MonotonicClockHost{resources: resources}
}

// Info returns host metadata.
func (h *MonotonicClockHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   MonotonicClockNamespace,
		Description: "WASI monotonic clock for measuring elapsed time",
		Class:       []string{wasmapi.ClassTime},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *MonotonicClockHost) Namespace() string {
	return MonotonicClockNamespace
}

// Resources returns the shared resource table.
func (h *MonotonicClockHost) Resources() *resource.InstanceResources {
	return h.resources
}

// Register returns the host registration.
func (h *MonotonicClockHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"now":                h.now,
			"resolution":         h.resolution,
			"subscribe-instant":  h.subscribeInstant,
			"subscribe-duration": h.subscribeDuration,
		},
	}
}

// now returns current monotonic instant in nanoseconds.
// Uses context TimeReference if available for deterministic time.
// Stack: [] -> [instant: u64]
func (h *MonotonicClockHost) now(ctx context.Context, _ api.Module, stack []uint64) {
	if len(stack) > 0 {
		if ref := workflow.GetTimeReference(ctx); ref != nil {
			stack[0] = uint64(ref.Now().UnixNano())
		} else {
			stack[0] = uint64(time.Since(monotonicStart).Nanoseconds())
		}
	}
}

// resolution returns clock resolution in nanoseconds.
// Stack: [] -> [duration: u64]
func (h *MonotonicClockHost) resolution(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) > 0 {
		stack[0] = 1
	}
}

// subscribeInstant creates a pollable that resolves at the specified instant.
// Stack: [when: u64] -> [pollable: u32]
func (h *MonotonicClockHost) subscribeInstant(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}

	when := stack[0]
	now := uint64(time.Since(monotonicStart).Nanoseconds())

	var duration time.Duration
	if when > now {
		duration = time.Duration(when - now) //nolint:gosec // controlled subtraction
	}

	// Register with timer service if resources available
	if h.resources != nil {
		pollable := resource.AcquirePollable()
		pollable.Ready = duration == 0
		handle := h.resources.Pollables().Insert(pollable)
		h.resources.TimerDurations().Store(handle, duration)
		stack[0] = uint64(handle)
	} else {
		stack[0] = 0
	}
}

// subscribeDuration creates a pollable that resolves after the specified duration.
// Stack: [duration: u64] -> [pollable: u32]
func (h *MonotonicClockHost) subscribeDuration(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}

	duration := time.Duration(stack[0]) //nolint:gosec // wasm duration

	if h.resources != nil {
		pollable := resource.AcquirePollable()
		pollable.Ready = duration == 0
		handle := h.resources.Pollables().Insert(pollable)
		h.resources.TimerDurations().Store(handle, duration)
		stack[0] = uint64(handle)
	} else {
		stack[0] = 0
	}
}

// wallNow returns current time from TimeReference if available, else system time.
func wallNow(ctx context.Context) time.Time {
	if ref := workflow.GetTimeReference(ctx); ref != nil {
		return ref.Now()
	}
	return time.Now()
}

// Compile-time checks
var (
	_ wasmapi.Host = (*WallClockHost)(nil)
	_ wasmapi.Host = (*MonotonicClockHost)(nil)
)
