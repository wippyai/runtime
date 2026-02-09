package function

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/system/scheduler/pool"
	"github.com/wippyai/runtime/system/scheduler/pool/adaptive"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	"github.com/wippyai/runtime/system/scheduler/pool/lazy"
	"github.com/wippyai/runtime/system/scheduler/pool/static"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

type poolTestDispatcher struct{}

func (poolTestDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler {
	return nil
}

type poolTestProcess struct {
	method string
	input  payload.Payloads
	done   bool
}

func (p *poolTestProcess) Init(_ context.Context, method string, input payload.Payloads) error {
	p.done = false
	p.method = method
	p.input = input
	return nil
}

func (p *poolTestProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.done {
		out.Done(payload.NewString("already-done"))
		return nil
	}
	p.done = true

	var arg any
	if len(p.input) > 0 && p.input[0] != nil {
		arg = p.input[0].Data()
	}
	out.Done(payload.NewString(fmt.Sprintf("%s:%v", p.method, arg)))
	return nil
}

func (p *poolTestProcess) Close() {}

func poolTestFactory() process.FactoryFunc {
	return func() (process.Process, error) {
		return &poolTestProcess{}, nil
	}
}

func TestAutoSelectPool_SelectsExpectedImplementation(t *testing.T) {
	m := NewManager(zap.NewNop(), nil, poolTestDispatcher{}, nil)

	tests := []struct {
		assert func(t *testing.T, p pool.Pool)
		name   string
		cfg    wasmapi.PoolConfig
	}{
		{
			name: "default lazy",
			cfg:  wasmapi.PoolConfig{},
			assert: func(t *testing.T, p pool.Pool) {
				if _, ok := p.(*lazy.Pool); !ok {
					t.Fatalf("expected *lazy.Pool, got %T", p)
				}
			},
		},
		{
			name: "static when workers set",
			cfg:  wasmapi.PoolConfig{Workers: 1, Size: 1},
			assert: func(t *testing.T, p pool.Pool) {
				if _, ok := p.(*static.Pool); !ok {
					t.Fatalf("expected *static.Pool, got %T", p)
				}
			},
		},
		{
			name: "inline when size only",
			cfg:  wasmapi.PoolConfig{Size: 2},
			assert: func(t *testing.T, p pool.Pool) {
				if _, ok := p.(*inline.Pool); !ok {
					t.Fatalf("expected *inline.Pool, got %T", p)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := m.autoSelectPool(poolTestFactory(), tt.cfg, pool.ExecutionHooks{})
			if err != nil {
				t.Fatalf("autoSelectPool() error = %v", err)
			}
			t.Cleanup(p.Stop)
			p.Start()
			tt.assert(t, p)

			result, err := p.Call(context.Background(), "run", payload.Payloads{payload.NewString("x")})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if result == nil || result.Error != nil {
				t.Fatalf("Call() invalid result = %#v", result)
			}
			if got := result.Value.Data(); got != "run:x" {
				t.Fatalf("Call() value = %#v, want %q", got, "run:x")
			}
		})
	}
}

func TestCreatePoolByType_CoversAllPoolTypes(t *testing.T) {
	m := NewManager(zap.NewNop(), nil, poolTestDispatcher{}, nil)

	tests := []struct {
		assert   func(t *testing.T, p pool.Pool)
		name     string
		poolType string
		cfg      wasmapi.PoolConfig
	}{
		{
			name:     "inline",
			poolType: wasmapi.PoolTypeInline,
			cfg:      wasmapi.PoolConfig{},
			assert: func(t *testing.T, p pool.Pool) {
				if _, ok := p.(*inline.Pool); !ok {
					t.Fatalf("expected *inline.Pool, got %T", p)
				}
			},
		},
		{
			name:     "lazy",
			poolType: wasmapi.PoolTypeLazy,
			cfg:      wasmapi.PoolConfig{MaxSize: 2},
			assert: func(t *testing.T, p pool.Pool) {
				if _, ok := p.(*lazy.Pool); !ok {
					t.Fatalf("expected *lazy.Pool, got %T", p)
				}
			},
		},
		{
			name:     "static",
			poolType: wasmapi.PoolTypeStatic,
			cfg:      wasmapi.PoolConfig{Workers: 1, Buffer: 1},
			assert: func(t *testing.T, p pool.Pool) {
				if _, ok := p.(*static.Pool); !ok {
					t.Fatalf("expected *static.Pool, got %T", p)
				}
			},
		},
		{
			name:     "adaptive",
			poolType: wasmapi.PoolTypeAdaptive,
			cfg:      wasmapi.PoolConfig{MaxSize: 2},
			assert: func(t *testing.T, p pool.Pool) {
				if _, ok := p.(*adaptive.Pool); !ok {
					t.Fatalf("expected *adaptive.Pool, got %T", p)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := m.createPoolByType(tt.poolType, poolTestFactory(), tt.cfg, pool.ExecutionHooks{})
			if err != nil {
				t.Fatalf("createPoolByType() error = %v", err)
			}
			t.Cleanup(p.Stop)
			p.Start()
			tt.assert(t, p)

			result, err := p.Call(context.Background(), "run", payload.Payloads{payload.NewString("x")})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if result == nil || result.Error != nil {
				t.Fatalf("Call() invalid result = %#v", result)
			}
			if got := result.Value.Data(); got != "run:x" {
				t.Fatalf("Call() value = %#v, want %q", got, "run:x")
			}
		})
	}
}

func TestCreatePoolByType_UnknownType(t *testing.T) {
	m := NewManager(zap.NewNop(), nil, poolTestDispatcher{}, nil)

	_, err := m.createPoolByType("burst", poolTestFactory(), wasmapi.PoolConfig{}, pool.ExecutionHooks{})
	if err == nil {
		t.Fatal("createPoolByType() expected error for unknown type")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "unknown pool type") {
		t.Fatalf("createPoolByType() unexpected error = %v", err)
	}
}

func TestManagerCreatePoolAndExecute_AllPoolTypes(t *testing.T) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("runtime.New() error = %v", err)
	}
	defer rt.Close(ctx)

	mod, err := rt.LoadWAT(ctx, `(module
		(func (export "run") (result i32)
			i32.const 42
		)
	)`, "run: func() -> s32;")
	if err != nil {
		t.Fatalf("LoadWAT() error = %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	m := NewManager(zap.NewNop(), nil, poolTestDispatcher{}, nil)
	m.started = true
	t.Cleanup(m.Stop)

	tests := []struct {
		name string
		id   registry.ID
		pool wasmapi.PoolConfig
	}{
		{
			name: "inline",
			id:   registry.NewID("app.test.wasm", "inline"),
			pool: wasmapi.PoolConfig{Type: wasmapi.PoolTypeInline},
		},
		{
			name: "lazy",
			id:   registry.NewID("app.test.wasm", "lazy"),
			pool: wasmapi.PoolConfig{Type: wasmapi.PoolTypeLazy, MaxSize: 2},
		},
		{
			name: "static",
			id:   registry.NewID("app.test.wasm", "static"),
			pool: wasmapi.PoolConfig{Type: wasmapi.PoolTypeStatic, Workers: 1, Buffer: 1},
		},
		{
			name: "adaptive",
			id:   registry.NewID("app.test.wasm", "adaptive"),
			pool: wasmapi.PoolConfig{Type: wasmapi.PoolTypeAdaptive, MaxSize: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &configEntry{
				kind:      wasmapi.FunctionWAT,
				method:    "run",
				transport: wasmapi.TransportTypePayload,
				pool:      tt.pool,
			}

			if err := m.createPool(tt.id, cfg, mod); err != nil {
				t.Fatalf("createPool() error = %v", err)
			}
			t.Cleanup(func() { m.removePool(tt.id) })

			for i := 0; i < 3; i++ {
				result, err := m.Execute(ctx, runtimeapi.Task{ID: tt.id})
				if err != nil {
					t.Fatalf("Execute() error = %v", err)
				}
				if result == nil || result.Error != nil {
					if result == nil {
						t.Fatalf("Execute() result is nil")
					}
					t.Fatalf("Execute() result.Error = %v", result.Error)
				}
				if got := fmt.Sprint(result.Value.Data()); got != "42" {
					t.Fatalf("Execute() value = %v, want 42", result.Value.Data())
				}
			}
		})
	}
}
