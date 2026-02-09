package engine

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

type processTestTransportRegistry struct {
	items map[string]any
}

func (r *processTestTransportRegistry) Get(name string) (any, bool) {
	v, ok := r.items[name]
	return v, ok
}

type processTestTransport struct {
	prepareErr error
	encodeErr  error
	encodeOut  payload.Payload
	args       []any
}

func (t *processTestTransport) Prepare(context.Context, payload.Payloads) ([]any, error) {
	if t.prepareErr != nil {
		return nil, t.prepareErr
	}
	return t.args, nil
}

func (t *processTestTransport) EncodeResult(context.Context, any) (payload.Payload, error) {
	if t.encodeErr != nil {
		return nil, t.encodeErr
	}
	return t.encodeOut, nil
}

type processTestTranscoder struct {
	err error
	out payload.Payload
}

func (t *processTestTranscoder) Transcode(payload.Payload, payload.Format) (payload.Payload, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.out, nil
}

func (t *processTestTranscoder) Unmarshal(payload.Payload, interface{}) error { return nil }

func compileEchoModule(t *testing.T, ctx context.Context) (*wasmrt.Runtime, *wasmrt.Module) {
	t.Helper()

	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("wasm runtime New() error = %v", err)
	}

	mod, err := rt.LoadWAT(ctx, `(module
		(func (export "run") (result i32)
			i32.const 42
		)
		(func (export "echo") (param i32) (result i32)
			local.get 0
		)
	)`, "run: func() -> s32;\necho: func(v: s32) -> s32;")
	if err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("LoadWAT() error = %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("Compile() error = %v", err)
	}

	return rt, mod
}

func TestProcessInvokeCustomTransport(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileEchoModule(t, ctx)
	defer func() { _ = rt.Close(ctx) }()

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate() error = %v", err)
	}
	defer func() { _ = inst.Close(context.Background()) }()

	t.Run("missing transport registry", func(t *testing.T) {
		p := &Process{transport: "custom", method: "run"}
		_, err := p.invokeCustomTransport(ctxapi.NewRootContext(), inst)
		if err == nil {
			t.Fatal("invokeCustomTransport() expected transport registry missing error")
		}
	})

	t.Run("transport not found", func(t *testing.T) {
		p := &Process{transport: "custom", method: "run"}
		callCtx := wasmapi.SetTransportRegistry(ctxapi.NewRootContext(), &processTestTransportRegistry{items: map[string]any{}})
		_, err := p.invokeCustomTransport(callCtx, inst)
		if err == nil {
			t.Fatal("invokeCustomTransport() expected transport not found error")
		}
	})

	t.Run("invalid transport type", func(t *testing.T) {
		p := &Process{transport: "custom", method: "run"}
		callCtx := wasmapi.SetTransportRegistry(ctxapi.NewRootContext(), &processTestTransportRegistry{
			items: map[string]any{"custom": "bad"},
		})
		_, err := p.invokeCustomTransport(callCtx, inst)
		if err == nil {
			t.Fatal("invokeCustomTransport() expected transport type error")
		}
	})

	t.Run("prepare error", func(t *testing.T) {
		p := &Process{transport: "custom", method: "run"}
		callCtx := wasmapi.SetTransportRegistry(ctxapi.NewRootContext(), &processTestTransportRegistry{
			items: map[string]any{"custom": &processTestTransport{prepareErr: errors.New("prepare")}},
		})
		_, err := p.invokeCustomTransport(callCtx, inst)
		if err == nil {
			t.Fatal("invokeCustomTransport() expected prepare error")
		}
	})

	t.Run("call method error", func(t *testing.T) {
		p := &Process{transport: "custom", method: "missing"}
		callCtx := wasmapi.SetTransportRegistry(ctxapi.NewRootContext(), &processTestTransportRegistry{
			items: map[string]any{"custom": &processTestTransport{args: []any{int32(1)}}},
		})
		_, err := p.invokeCustomTransport(callCtx, inst)
		if err == nil {
			t.Fatal("invokeCustomTransport() expected call method error")
		}
	})

	t.Run("encode error", func(t *testing.T) {
		p := &Process{transport: "custom", method: "run"}
		callCtx := wasmapi.SetTransportRegistry(ctxapi.NewRootContext(), &processTestTransportRegistry{
			items: map[string]any{"custom": &processTestTransport{encodeErr: errors.New("encode")}},
		})
		_, err := p.invokeCustomTransport(callCtx, inst)
		if err == nil {
			t.Fatal("invokeCustomTransport() expected encode error")
		}
	})

	t.Run("nil encode result falls back to payload", func(t *testing.T) {
		p := &Process{transport: "custom", method: "run"}
		callCtx := wasmapi.SetTransportRegistry(ctxapi.NewRootContext(), &processTestTransportRegistry{
			items: map[string]any{"custom": &processTestTransport{}},
		})
		out, err := p.invokeCustomTransport(callCtx, inst)
		if err != nil {
			t.Fatalf("invokeCustomTransport() error = %v", err)
		}
		if out == nil || out.Data() != int32(42) {
			t.Fatalf("invokeCustomTransport() output = %#v, want 42", out)
		}
	})

	t.Run("cached resolved transport", func(t *testing.T) {
		tr := &processTestTransport{args: []any{int32(7)}}
		p := &Process{
			method:            "echo",
			transport:         "custom",
			resolvedTransport: tr,
		}
		out, err := p.invokeCustomTransport(ctxapi.NewRootContext(), inst)
		if err != nil {
			t.Fatalf("invokeCustomTransport() error = %v", err)
		}
		if out == nil || out.Data() != int32(7) {
			t.Fatalf("invokeCustomTransport() output = %#v, want 7", out)
		}
	})
}

func TestProcessInvokePayload(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileEchoModule(t, ctx)
	defer func() { _ = rt.Close(ctx) }()

	inst, err := mod.Instantiate(ctx)
	if err != nil {
		t.Fatalf("Instantiate() error = %v", err)
	}
	defer func() { _ = inst.Close(context.Background()) }()

	t.Run("missing transcoder", func(t *testing.T) {
		p := &Process{method: "echo", input: payload.Payloads{
			payload.NewPayload(`7`, payload.JSON),
		}}
		_, err := p.invokePayload(ctxapi.NewRootContext(), inst)
		if err == nil {
			t.Fatal("invokePayload() expected transcoder missing error")
		}
	})

	t.Run("transcode error", func(t *testing.T) {
		callCtx := payload.WithTranscoder(ctxapi.NewRootContext(), &processTestTranscoder{err: errors.New("boom")})
		p := &Process{method: "echo", input: payload.Payloads{
			payload.NewPayload(`7`, payload.JSON),
		}}
		_, err := p.invokePayload(callCtx, inst)
		if err == nil {
			t.Fatal("invokePayload() expected transcode error")
		}
	})

	t.Run("call error", func(t *testing.T) {
		p := &Process{method: "missing", input: payload.Payloads{}}
		_, err := p.invokePayload(ctxapi.NewRootContext(), inst)
		if err == nil {
			t.Fatal("invokePayload() expected call error")
		}
	})

	t.Run("success with transcoded arg", func(t *testing.T) {
		callCtx := payload.WithTranscoder(ctxapi.NewRootContext(), &processTestTranscoder{
			out: payload.New(int32(9)),
		})
		p := &Process{method: "echo", input: payload.Payloads{
			payload.NewPayload(`9`, payload.JSON),
		}}
		out, err := p.invokePayload(callCtx, inst)
		if err != nil {
			t.Fatalf("invokePayload() error = %v", err)
		}
		if out == nil || out.Data() != int32(9) {
			t.Fatalf("invokePayload() output = %#v, want 9", out)
		}
	})
}

var _ wasmapi.TransportRegistry = (*processTestTransportRegistry)(nil)
var _ Transport = (*processTestTransport)(nil)
var _ payload.Transcoder = (*processTestTranscoder)(nil)
