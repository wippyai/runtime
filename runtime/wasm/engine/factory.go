// SPDX-License-Identifier: MPL-2.0

// Package engine provides WASM process integration for the scheduler.
package engine

import (
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Factory creates scheduler processes backed by a compiled WASM module.
type Factory struct {
	fsReg     fsapi.Registry
	module    *wasmrt.Module
	transport string
	wasi      wasmapi.WASIConfig
	limits    wasmapi.LimitsConfig
}

// NewFactory creates a process factory for a module and runtime settings.
func NewFactory(
	module *wasmrt.Module,
	transport string,
	wasi wasmapi.WASIConfig,
	limits wasmapi.LimitsConfig,
	fsReg fsapi.Registry,
) *Factory {
	return &Factory{
		module:    module,
		transport: transport,
		wasi:      wasi,
		limits:    limits,
		fsReg:     fsReg,
	}
}

// Create builds a process.FactoryFunc for pool construction.
func (f *Factory) Create() process.FactoryFunc {
	return func() (process.Process, error) {
		return NewProcess(f.module, f.transport, f.wasi, f.limits, f.fsReg), nil
	}
}
