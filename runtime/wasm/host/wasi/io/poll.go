// Package io implements wasi:io@0.2.8 interfaces for wippy.
package io

import (
	"context"

	"github.com/tetratelabs/wazero/api"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	pollapi "github.com/wippyai/runtime/api/dispatcher/poll"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/host"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

const (
	// PollNamespace is the WASI namespace for poll.
	PollNamespace = "wasi:io/poll@0.2.8"
)

// PollHost implements wasi:io/poll@0.2.8.
type PollHost struct {
	resources *resource.InstanceResources
}

// NewPollHost creates a new poll host with shared resources.
func NewPollHost(resources *resource.InstanceResources) *PollHost {
	return &PollHost{
		resources: resources,
	}
}

// Info returns host metadata.
func (h *PollHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   PollNamespace,
		Description: "WASI poll for async I/O readiness",
		Class:       []string{wasmapi.ClassIO},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *PollHost) Namespace() string {
	return PollNamespace
}

// Register returns the host registration.
func (h *PollHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"poll":                    host.MakeAsyncHandler(h.makePollCmd),
			"[method]pollable.ready":  h.pollableReady,
			"[method]pollable.block":  host.MakeAsyncHandler(h.makePollableBlock),
			"[resource-drop]pollable": h.dropPollable,
		},
		YieldTypes: []wasmapi.YieldType{
			{CmdID: pollapi.CmdPoll},
			{CmdID: clockapi.CmdSleep},
		},
	}
}

// Resources returns the shared resource table.
func (h *PollHost) Resources() *resource.InstanceResources {
	return h.resources
}

// makePollCmd creates a poll command for multiple pollables.
// Stack: [list_ptr: u32, list_len: u32] -> [ready_list_ptr: u32, ready_list_len: u32]
func (h *PollHost) makePollCmd(stack []uint64) dispatcher.Command {
	cmd := pollapi.PollCmd{
		Pollables: make([]uint64, 0),
	}

	if len(stack) >= 2 {
		// In a real implementation, we'd read the list from WASM memory
		// For now, handle single pollable case
		listLen := stack[1]
		if listLen > 0 && len(stack) > 0 {
			handle := resource.Handle(stack[0])
			if p, ok := h.resources.Pollables().Get(handle); ok {
				cmd.Pollables = append(cmd.Pollables, p.SourceID)
			}
		}
	}

	return cmd
}

// pollableReady checks if a pollable is ready without blocking.
// Stack: [handle: u32] -> [ready: u32 (bool)]
func (h *PollHost) pollableReady(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}

	handle := resource.Handle(stack[0])
	p, ok := h.resources.Pollables().Get(handle)
	if !ok {
		stack[0] = 0
		return
	}

	if p.Ready {
		stack[0] = 1
	} else {
		stack[0] = 0
	}
}

// makePollableBlock creates a command to block on a single pollable.
// For timer pollables (from clock.subscribe-*), returns SleepCmd.
// For I/O pollables, returns PollCmd.
func (h *PollHost) makePollableBlock(stack []uint64) dispatcher.Command {
	if len(stack) == 0 {
		return pollapi.PollCmd{}
	}

	handle := resource.Handle(stack[0])

	// Check if this is a timer pollable
	if duration, ok := h.resources.TimerDurations().Load(handle); ok {
		return clockapi.SleepCmd{Duration: duration}
	}

	// Otherwise, treat as I/O pollable
	cmd := pollapi.PollCmd{
		Pollables: make([]uint64, 0, 1),
	}
	if p, ok := h.resources.Pollables().Get(handle); ok {
		cmd.Pollables = append(cmd.Pollables, p.SourceID)
	}
	return cmd
}

// dropPollable removes a pollable resource.
func (h *PollHost) dropPollable(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		h.resources.TimerDurations().Delete(handle)
		h.resources.Table().Remove(handle)
	}
}

// Compile-time check
var _ wasmapi.Host = (*PollHost)(nil)
