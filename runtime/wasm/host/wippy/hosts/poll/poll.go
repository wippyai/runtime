package poll

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	// PollNamespace exposes preview2 poll APIs used by sleep-oriented components.
	PollNamespace = "wasi:io/poll@0.2.8"
)

// PollHost exposes wasi:io/poll APIs backed by preview2 resource table.
// pollable.block becomes dispatcher-driven when the pollable implementation
// uses asyncify suspend (e.g. DispatcherTimerPollable.Block).
type PollHost struct {
	resources *preview2.ResourceTable
}

// NewPollHost builds a poll host.
func NewPollHost(resources *preview2.ResourceTable) *PollHost {
	if resources == nil {
		resources = preview2.NewResourceTable()
	}
	return &PollHost{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *PollHost) Namespace() string {
	return PollNamespace
}

// Register returns explicit WIT function mappings for resource methods.
func (h *PollHost) Register() map[string]any {
	if h == nil {
		return map[string]any{}
	}
	return map[string]any{
		"poll":                    h.Poll,
		"[method]pollable.ready":  h.MethodPollableReady,
		"[method]pollable.block":  h.MethodPollableBlock,
		"[resource-drop]pollable": h.ResourceDropPollable,
	}
}

// AsyncFunctions marks pollable.block as async import for asyncify.
func (h *PollHost) AsyncFunctions() []string {
	return []string{"[method]pollable.block"}
}

// Poll returns indexes of ready pollables.
func (h *PollHost) Poll(_ context.Context, pollables []uint32) []uint32 {
	if h == nil {
		return nil
	}
	ready := make([]uint32, 0, len(pollables))
	for i, handle := range pollables {
		r, ok := h.resources.Get(handle)
		if !ok {
			continue
		}
		if p, ok := r.(preview2.Pollable); ok {
			if p.Ready() {
				ready = append(ready, uint32(i))
			}
		}
	}
	return ready
}

// MethodPollableReady checks readiness for a pollable handle.
func (h *PollHost) MethodPollableReady(_ context.Context, self uint32) bool {
	if h == nil {
		return false
	}
	r, ok := h.resources.Get(self)
	if !ok {
		return false
	}
	if p, ok := r.(preview2.Pollable); ok {
		return p.Ready()
	}
	return false
}

// MethodPollableBlock blocks until a pollable becomes ready.
// The actual suspend/resume logic lives in the pollable implementation
// (e.g. DispatcherTimerPollable.Block uses asyncify suspend).
func (h *PollHost) MethodPollableBlock(ctx context.Context, self uint32) {
	if h == nil {
		return
	}
	r, ok := h.resources.Get(self)
	if !ok {
		return
	}
	if p, ok := r.(preview2.Pollable); ok {
		p.Block(ctx)
	}
}

// ResourceDropPollable drops a pollable handle.
func (h *PollHost) ResourceDropPollable(_ context.Context, self uint32) {
	if h == nil {
		return
	}
	h.resources.Remove(self)
}
