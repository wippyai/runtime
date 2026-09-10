// SPDX-License-Identifier: MPL-2.0

package poll

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero/api"

	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	// PollNamespace exposes Preview2 readiness polling.
	PollNamespace = "wasi:io/poll@0.2.8"
)

// Host exposes wasi:io/poll using resource readiness and scheduler suspension.
type Host struct {
	resources *preview2.ResourceTable
}

// NewHost builds a poll host.
func NewHost(resources *preview2.ResourceTable) *Host {
	if resources == nil {
		resources = preview2.NewResourceTable()
	}
	return &Host{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *Host) Namespace() string {
	return PollNamespace
}

// Register returns explicit WIT function mappings for resource methods.
func (h *Host) Register() map[string]any {
	if h == nil {
		return map[string]any{}
	}
	return map[string]any{
		"poll":                    wasmengine.CheckedHostFunction{Handler: h.Poll, Validate: validatePollArguments},
		"[method]pollable.ready":  h.MethodPollableReady,
		"[method]pollable.block":  h.MethodPollableBlock,
		"[resource-drop]pollable": h.ResourceDropPollable,
	}
}

// AsyncFunctions marks blocking poll imports for embedded Asyncify.
func (h *Host) AsyncFunctions() []string {
	return []string{"poll", "[method]pollable.block"}
}

// Poll returns ready indexes, suspending when no source is ready.
func (h *Host) Poll(ctx context.Context, pollables []uint32) []uint32 {
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		return resumeIndexes(ctx)
	}
	// Bound host-side fan-in independently of a component's linear memory cap.
	if len(pollables) == 0 || len(pollables) > 4096 {
		panic("poll requires 1..4096 pollables")
	}
	sources := make([]preview2.Pollable, len(pollables))
	for i, handle := range pollables {
		sources[i] = h.pollable(handle)
	}
	op := &waitSources{sources: sources}
	if ready := op.ready(); len(ready) > 0 {
		return ready
	}
	if async == nil {
		panic("poll requires asyncify scheduler context")
	}
	if err := wasmengine.Suspend(ctx, op); err != nil {
		panic(fmt.Errorf("poll suspend: %w", err))
	}
	return nil
}

func (h *Host) pollable(handle uint32) preview2.Pollable {
	if h == nil || h.resources == nil {
		panic("poll resource table missing")
	}
	r, ok := h.resources.Get(handle)
	if !ok {
		panic("invalid pollable handle")
	}
	p, ok := r.(preview2.Pollable)
	if !ok {
		panic("resource is not a pollable")
	}
	return p
}

// MethodPollableReady checks readiness for a pollable handle.
func (h *Host) MethodPollableReady(_ context.Context, self uint32) bool {
	return h.pollable(self).Ready()
}

// MethodPollableBlock blocks until a pollable becomes ready.
// Single timers retain their clock dispatcher path; other sources use Poll.
func (h *Host) MethodPollableBlock(ctx context.Context, self uint32) {
	p := h.pollable(self)
	// Preserve the existing single-timer dispatcher path.
	if _, ok := p.(interface{ Remaining() time.Duration }); ok {
		p.Block(ctx)
		return
	}
	h.Poll(ctx, []uint32{self})
}

// ResourceDropPollable drops a pollable handle.
func (h *Host) ResourceDropPollable(_ context.Context, self uint32) {
	h.pollable(self)
	h.resources.Remove(self)
}

// Validate before canonical list lifting can allocate host memory.
func validatePollArguments(_ context.Context, mod api.Module, stack []uint64) error {
	if len(stack) != 3 || uint32(stack[1]) == 0 || uint32(stack[1]) > 4096 {
		return errors.New("poll requires 1..4096 pollables")
	}
	if mod == nil || mod.Memory() == nil {
		return errors.New("poll memory unavailable")
	}
	if _, ok := mod.Memory().Read(uint32(stack[0]), uint32(stack[1])*4); !ok {
		return errors.New("poll list outside memory")
	}
	return nil
}

func resumeIndexes(ctx context.Context) []uint32 {
	token, err := wasmengine.Resume(ctx)
	if err != nil {
		panic(fmt.Errorf("poll resume: %w", err))
	}
	value, ok := wippyhost.GetAsyncValueStore(ctx).Take(token)
	ready, typed := value.([]uint32)
	if !ok || !typed || len(ready) == 0 {
		panic("poll resumed without ready indexes")
	}
	return ready
}

// AwaitReady suspends a blocking host operation on a resource without allocating
// a guest pollable handle. False means the host must return while unwinding.
func AwaitReady(ctx context.Context, source preview2.Pollable) bool {
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		indexes := resumeIndexes(ctx)
		if len(indexes) != 1 || indexes[0] != 0 {
			panic("invalid single-resource poll result")
		}
		return true
	}
	if source.Ready() {
		return true
	}
	if async == nil {
		panic("blocking stream requires asyncify scheduler context")
	}
	if err := wasmengine.Suspend(ctx, &waitSources{sources: []preview2.Pollable{source}}); err != nil {
		panic(fmt.Errorf("stream wait suspend: %w", err))
	}
	return false
}
