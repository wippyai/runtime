// Package engine provides WASM process integration for the scheduler.
package engine

import (
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Factory creates scheduler processes backed by a compiled WASM module.
type Factory struct {
	module    *wasmrt.Module
	transport string
	limits    wasmapi.LimitsConfig
}

// NewFactory creates a process factory for a module and runtime settings.
func NewFactory(module *wasmrt.Module, transport string, limits wasmapi.LimitsConfig) *Factory {
	return &Factory{
		module:    module,
		transport: transport,
		limits:    limits,
	}
}

// Create builds a process.FactoryFunc for pool construction.
func (f *Factory) Create() process.FactoryFunc {
	return func() (process.Process, error) {
		return NewProcess(f.module, f.transport, f.limits), nil
	}
}
