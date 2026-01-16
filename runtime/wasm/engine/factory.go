package engine

import (
	"context"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime/resource"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/wasm-runtime/asyncify"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Factory creates WASM Process instances from a compiled module.
type Factory struct {
	runtime   *wasmrt.Runtime
	module    *wasmrt.Module
	transport wasmapi.Transport
}

// NewFactory creates a factory for a compiled WASM module.
func NewFactory(runtime *wasmrt.Runtime, module *wasmrt.Module) *Factory {
	return &Factory{
		runtime: runtime,
		module:  module,
	}
}

// NewFactoryWithTransport creates a factory with a specific transport.
func NewFactoryWithTransport(runtime *wasmrt.Runtime, module *wasmrt.Module, transport wasmapi.Transport) *Factory {
	return &Factory{
		runtime:   runtime,
		module:    module,
		transport: transport,
	}
}

// Create returns a ProcessFactory function that creates new Process instances.
func (f *Factory) Create() process.FactoryFunc {
	return func() (process.Process, error) {
		return NewProcessWithTransport(f.runtime, f.module, f.transport), nil
	}
}

// CreateWithContext returns a ProcessFactory that pre-initializes instances.
func (f *Factory) CreateWithContext(ctx context.Context) process.FactoryFunc {
	return func() (process.Process, error) {
		p := NewProcessWithTransport(f.runtime, f.module, f.transport)
		// Pre-instantiate for faster first call
		inst, err := f.module.InstantiateWithAsyncify(ctx)
		if err != nil {
			return nil, NewInstantiateError(err)
		}
		p.instance = inst
		if p.transport != nil {
			p.store = resource.NewStore()
		}
		p.asyncify = inst.Asyncify()
		return p, nil
	}
}

// CompileOptions configures WASM compilation.
type CompileOptions struct {
	// AsyncImports lists imports that trigger async behavior.
	// Format: "module.function" (e.g., "wippy:clock.sleep")
	AsyncImports []string
}

// CompileWAT compiles inline WAT text to a WASM module.
func CompileWAT(ctx context.Context, runtime *wasmrt.Runtime, watSource, witText string) (*wasmrt.Module, error) {
	return CompileWATWithOptions(ctx, runtime, watSource, witText, CompileOptions{})
}

// CompileWATWithOptions compiles WAT with custom options.
func CompileWATWithOptions(ctx context.Context, runtime *wasmrt.Runtime, watSource, witText string, opts CompileOptions) (*wasmrt.Module, error) {
	wasmBytes, err := wasmrt.CompileWAT(watSource)
	if err != nil {
		return nil, NewCompileWATError(err)
	}

	// Apply asyncify transform if async imports specified
	if len(opts.AsyncImports) > 0 && !wasmengine.IsAsyncified(wasmBytes) {
		wasmBytes, err = asyncify.Transform(wasmBytes, asyncify.Config{
			AsyncImports: opts.AsyncImports,
		})
		if err != nil {
			return nil, NewAsyncifyTransformError(err)
		}
	}

	module, err := runtime.LoadWASM(ctx, wasmBytes, witText)
	if err != nil {
		return nil, NewLoadWASMError(err)
	}
	return module, nil
}

// CompileWASM loads pre-compiled WASM bytes.
func CompileWASM(ctx context.Context, runtime *wasmrt.Runtime, wasmBytes []byte, witText string, opts CompileOptions) (*wasmrt.Module, error) {
	var err error

	// Apply asyncify transform if async imports specified
	if len(opts.AsyncImports) > 0 && !wasmengine.IsAsyncified(wasmBytes) {
		wasmBytes, err = asyncify.Transform(wasmBytes, asyncify.Config{
			AsyncImports: opts.AsyncImports,
		})
		if err != nil {
			return nil, NewAsyncifyTransformError(err)
		}
	}

	module, err := runtime.LoadWASM(ctx, wasmBytes, witText)
	if err != nil {
		return nil, NewLoadWASMError(err)
	}
	return module, nil
}

// LoadComponent loads a WebAssembly Component Model binary.
func LoadComponent(ctx context.Context, runtime *wasmrt.Runtime, wasmBytes []byte) (*wasmrt.Module, error) {
	module, err := runtime.LoadComponent(ctx, wasmBytes)
	if err != nil {
		return nil, NewLoadComponentError(err)
	}
	return module, nil
}

// IsAsyncified checks if a WASM module has asyncify exports.
func IsAsyncified(wasmBytes []byte) bool {
	return wasmengine.IsAsyncified(wasmBytes)
}
