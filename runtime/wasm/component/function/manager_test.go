package function

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	functionapi "github.com/wippyai/runtime/api/function"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"go.uber.org/zap"
)

type noopFunctionRegistry struct{}

func (noopFunctionRegistry) Call(context.Context, runtimeapi.Task) (*runtimeapi.Result, error) {
	return nil, nil
}

func TestLoadWATModuleWithFunctionHostRegistered(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = functionapi.WithRegistry(ctx, noopFunctionRegistry{})

	m := NewManager(zap.NewNop(), nil, nil, nil)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(m.Stop)

	cfg := &wasmapi.WATFunctionConfig{
		Source: `(module
			(func (export "run") (result i32)
				i32.const 7
			)
		)`,
		Method: "run",
	}

	mod, err := m.loadWATModule(ctx, cfg)
	if err != nil {
		t.Fatalf("loadWATModule() error = %v", err)
	}
	if mod == nil {
		t.Fatal("loadWATModule() module is nil")
	}
}

var _ functionapi.Registry = noopFunctionRegistry{}
