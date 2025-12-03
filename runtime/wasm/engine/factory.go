package engine

import (
	"context"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/wasm-runtime/asyncify"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// Factory creates WASM Process instances from a compiled module.
type Factory struct {
	runtime    *wasmrt.Runtime
	module     *wasmrt.Module
	transport  wasmapi.Transport
	asyncified bool
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

// NewAsyncFactory creates a factory for an asyncified WASM module.
func NewAsyncFactory(runtime *wasmrt.Runtime, module *wasmrt.Module, asyncified bool) *Factory {
	return &Factory{
		runtime:    runtime,
		module:     module,
		asyncified: asyncified,
	}
}

// Create returns a ProcessFactory function that creates new Process instances.
// Instance is created lazily on first Execute call with proper context.
func (f *Factory) Create() process.ProcessFactory {
	return func() (process.Process, error) {
		return NewProcessWithTransport(f.runtime, f.module, f.transport), nil
	}
}

// CreateWithContext returns a ProcessFactory that pre-initializes instances.
// Use when you have a startup context available.
func (f *Factory) CreateWithContext(ctx context.Context) process.ProcessFactory {
	return func() (process.Process, error) {
		p := NewProcessWithTransport(f.runtime, f.module, f.transport)
		if err := p.Init(ctx); err != nil {
			return nil, err
		}
		return p, nil
	}
}

// CompileOptions configures WAT compilation.
type CompileOptions struct {
	// AsyncImports lists imports that trigger async behavior.
	// When set, the module is automatically asyncified using pure Go transform.
	// Format: "module.function" (e.g., "wippy:clock.sleep", "wasi:io/poll@0.2.0.poll")
	AsyncImports []string
}

// CompileWAT compiles inline WAT text to a WASM module.
func CompileWAT(ctx context.Context, runtime *wasmrt.Runtime, watSource, witText string) (*wasmrt.Module, error) {
	return CompileWATWithOptions(ctx, runtime, watSource, witText, CompileOptions{})
}

// CompileWATWithOptions compiles WAT with custom options.
func CompileWATWithOptions(ctx context.Context, runtime *wasmrt.Runtime, watSource, witText string, opts CompileOptions) (*wasmrt.Module, error) {
	// Compile WAT to WASM binary
	wasmBytes, err := wasmrt.CompileWAT(watSource)
	if err != nil {
		return nil, NewCompileWATError(err)
	}

	// Apply asyncify transform if async imports specified
	if len(opts.AsyncImports) > 0 && !wasmengine.IsAsyncified(wasmBytes) {
		wasmBytes, err = asyncify.Transform(wasmBytes, asyncify.TransformOptions{
			AsyncImports: opts.AsyncImports,
		})
		if err != nil {
			return nil, NewAsyncifyTransformError(err)
		}
	}

	// Load as WASM module
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
		wasmBytes, err = asyncify.Transform(wasmBytes, asyncify.TransformOptions{
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

// IsAsyncified checks if a WASM module has asyncify exports.
func IsAsyncified(wasmBytes []byte) bool {
	return wasmengine.IsAsyncified(wasmBytes)
}

// CompileWithAsyncify applies asyncify transform to WASM bytes.
// Returns the transformed bytes or the original if already asyncified.
func CompileWithAsyncify(wasmBytes []byte, asyncImports []string) ([]byte, error) {
	if wasmengine.IsAsyncified(wasmBytes) {
		return wasmBytes, nil
	}
	return asyncify.Transform(wasmBytes, asyncify.TransformOptions{
		AsyncImports: asyncImports,
	})
}

// InitAsyncify initializes asyncify for a wazero module instance.
// Returns the asyncify state machine or error if module doesn't support asyncify.
func InitAsyncify(inst api.Module) (*wasmengine.Asyncify, error) {
	a := wasmengine.NewAsyncify()
	a.SetStackSize(wasmengine.AsyncifyDefaultStackSize)
	a.SetDataAddr(wasmengine.AsyncifyDataAddr)
	if err := a.Init(inst); err != nil {
		return nil, err
	}
	return a, nil
}
