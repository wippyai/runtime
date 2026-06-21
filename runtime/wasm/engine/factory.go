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
	fsReg         fsapi.Registry
	module        *wasmrt.Module
	moduleFactory func() (*wasmrt.Module, error)
	transport     string
	wasi          wasmapi.WASIConfig
	limits        wasmapi.LimitsConfig
	ownedModule   bool
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

// NewFactoryWithModuleFactory creates a process factory that loads a fresh
// compiled module for each process. Retained-memory recycling uses this for
// WASI components today because re-instantiating from an already-used component
// module can leave bridge state bound to the old core instance.
func NewFactoryWithModuleFactory(
	moduleFactory func() (*wasmrt.Module, error),
	transport string,
	wasi wasmapi.WASIConfig,
	limits wasmapi.LimitsConfig,
	fsReg fsapi.Registry,
) *Factory {
	return &Factory{
		moduleFactory: moduleFactory,
		transport:     transport,
		wasi:          wasi,
		limits:        limits,
		fsReg:         fsReg,
		ownedModule:   true,
	}
}

// Create builds a process.FactoryFunc for pool construction.
func (f *Factory) Create() process.FactoryFunc {
	return func() (process.Process, error) {
		module := f.module
		if f.moduleFactory != nil {
			var err error
			module, err = f.moduleFactory()
			if err != nil {
				return nil, err
			}
		}
		proc := NewProcess(module, f.transport, f.wasi, f.limits, f.fsReg)
		proc.ownedModule = f.ownedModule
		return proc, nil
	}
}
