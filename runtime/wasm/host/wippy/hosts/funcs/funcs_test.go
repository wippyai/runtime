// SPDX-License-Identifier: MPL-2.0

package funcs

import (
	"context"
	"errors"
	"testing"

	contextapi "github.com/wippyai/runtime/api/context"
	functionapi "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	securityapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/wasm"
)

type mockFunctionRegistry struct {
	callFn func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error)
}

func (m *mockFunctionRegistry) Call(ctx context.Context, task runtimeapi.Task) (*runtimeapi.Result, error) {
	if m.callFn == nil {
		return nil, nil
	}
	return m.callFn(ctx, task)
}

func insecureContext() context.Context {
	return securityapi.SetStrictMode(contextapi.NewRootContext(), false)
}

func TestHost_CallString(t *testing.T) {
	host := NewHost(&mockFunctionRegistry{
		callFn: func(_ context.Context, task runtimeapi.Task) (*runtimeapi.Result, error) {
			if task.ID.String() != "app:test" {
				t.Fatalf("unexpected target: %s", task.ID.String())
			}
			if len(task.Payloads) != 1 {
				t.Fatalf("unexpected payload count: %d", len(task.Payloads))
			}
			if got := task.Payloads[0].Data(); got != "hello" {
				t.Fatalf("unexpected input: %#v", got)
			}
			return &runtimeapi.Result{Value: payload.NewString("world")}, nil
		},
	})

	out, err := host.CallString(insecureContext(), "app:test", "hello")
	if err != nil {
		t.Fatalf("CallString error: %v", err)
	}
	if out != "world" {
		t.Fatalf("CallString output = %q, want %q", out, "world")
	}
}

func TestHost_CallBytes(t *testing.T) {
	host := NewHost(&mockFunctionRegistry{
		callFn: func(_ context.Context, task runtimeapi.Task) (*runtimeapi.Result, error) {
			if got := task.Payloads[0].Format(); got != payload.Bytes {
				t.Fatalf("unexpected input format: %s", got)
			}
			in, _ := task.Payloads[0].Data().([]byte)
			if string(in) != "abc" {
				t.Fatalf("unexpected input bytes: %q", string(in))
			}
			return &runtimeapi.Result{Value: payload.NewPayload([]byte("xyz"), payload.Bytes)}, nil
		},
	})

	out, err := host.CallBytes(insecureContext(), "app:test", []byte("abc"))
	if err != nil {
		t.Fatalf("CallBytes error: %v", err)
	}
	if string(out) != "xyz" {
		t.Fatalf("CallBytes output = %q, want %q", string(out), "xyz")
	}
}

func TestHost_Errors(t *testing.T) {
	ctx := insecureContext()
	t.Run("registry missing", func(t *testing.T) {
		host := NewHost(nil)
		_, err := host.CallString(ctx, "app:test", "x")
		if !errors.Is(err, wasm.ErrFunctionRegistryNotFound) {
			t.Fatalf("expected ErrFunctionRegistryNotFound, got %v", err)
		}
	})

	t.Run("invalid target", func(t *testing.T) {
		host := NewHost(&mockFunctionRegistry{})
		_, err := host.CallString(ctx, "invalid", "x")
		if err == nil {
			t.Fatal("expected invalid target error")
		}
	})

	t.Run("call error", func(t *testing.T) {
		host := NewHost(&mockFunctionRegistry{
			callFn: func(_ context.Context, _ runtimeapi.Task) (*runtimeapi.Result, error) {
				return nil, errors.New("boom")
			},
		})
		_, err := host.CallString(ctx, "app:test", "x")
		if err == nil || err.Error() != "boom" {
			t.Fatalf("expected boom, got %v", err)
		}
	})

	t.Run("result error", func(t *testing.T) {
		host := NewHost(&mockFunctionRegistry{
			callFn: func(_ context.Context, _ runtimeapi.Task) (*runtimeapi.Result, error) {
				return &runtimeapi.Result{Error: errors.New("denied")}, nil
			},
		})
		_, err := host.CallString(ctx, "app:test", "x")
		if err == nil || err.Error() != "denied" {
			t.Fatalf("expected denied, got %v", err)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		host := NewHost(&mockFunctionRegistry{
			callFn: func(_ context.Context, _ runtimeapi.Task) (*runtimeapi.Result, error) {
				return &runtimeapi.Result{Value: nil}, nil
			},
		})
		out, err := host.CallString(ctx, "app:test", "x")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "" {
			t.Fatalf("expected empty output, got %q", out)
		}
	})
}

func TestHost_DeniesUnauthorizedCalls(t *testing.T) {
	calls := 0
	host := NewHost(&mockFunctionRegistry{
		callFn: func(_ context.Context, _ runtimeapi.Task) (*runtimeapi.Result, error) {
			calls++
			return &runtimeapi.Result{Value: payload.NewString("secret")}, nil
		},
	})

	if _, err := host.CallString(contextapi.NewRootContext(), "app:secret", ""); err == nil {
		t.Fatal("expected unauthorized string call to fail")
	}
	if _, err := host.CallBytes(contextapi.NewRootContext(), "app:secret", nil); err == nil {
		t.Fatal("expected unauthorized bytes call to fail")
	}
	if calls != 0 {
		t.Fatalf("registry called %d times", calls)
	}
}

var _ functionapi.Registry = (*mockFunctionRegistry)(nil)
var _ = registry.ID{}
