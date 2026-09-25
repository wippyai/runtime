// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"errors"
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestExecuteScriptErrorPreservesRaisedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      lua.Kind
		retryable *bool
		wantKind  apierror.Kind
		wantRetry apierror.Ternary
	}{
		{name: "invalid false", kind: lua.Invalid, retryable: boolPtr(false), wantKind: apierror.Invalid, wantRetry: apierror.False},
		{name: "invalid unspecified", kind: lua.Invalid, wantKind: apierror.Invalid, wantRetry: apierror.Unspecified},
		{name: "unavailable true", kind: lua.Unavailable, retryable: boolPtr(true), wantKind: apierror.Unavailable, wantRetry: apierror.True},
		{name: "internal true", kind: lua.Internal, retryable: boolPtr(true), wantKind: apierror.Internal, wantRetry: apierror.True},
		{name: "empty kind", wantKind: apierror.Internal, wantRetry: apierror.Unspecified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := lua.NewError("bad declaration").WithKind(tc.kind).WithDetails(map[string]any{"field": "target"})
			if tc.retryable != nil {
				source.WithRetryable(*tc.retryable)
			}
			vm := &lua.ApiError{Type: lua.ApiErrorRun, Object: source, StackTrace: "test.lua:1"}
			got := NewExecuteScriptError(vm)
			if got.Kind() != tc.wantKind || got.Retryable() != tc.wantRetry {
				t.Fatalf("metadata = %s/%s, want %s/%s", got.Kind(), got.Retryable(), tc.wantKind, tc.wantRetry)
			}
			if got.Error() != "bad declaration" || got.Details().GetString("field", "") != "target" {
				t.Fatalf("message/details = %q/%v", got.Error(), got.Details())
			}
			if !errors.Is(got, source) {
				t.Fatal("source error lost")
			}
			var envelope *lua.ApiError
			if !errors.As(got, &envelope) || envelope != vm {
				t.Fatal("VM envelope is not available through errors.As")
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }

func TestExecuteScriptErrorPreservesLuaCauses(t *testing.T) {
	inner := lua.NewError("inner").WithKind(lua.Unavailable).WithRetryable(true)
	outer := lua.WrapError(inner, "context").WithKind(lua.Invalid).WithRetryable(false)
	got := NewExecuteScriptError(&lua.ApiError{Type: lua.ApiErrorRun, Object: outer})
	if got.Kind() != apierror.Invalid || got.Error() != "context: inner" {
		t.Fatalf("outer = %s %q", got.Kind(), got.Error())
	}
	if !errors.Is(got, inner) || !errors.Is(got, outer) {
		t.Fatal("Lua cause chain lost")
	}
	chain := apierror.BuildChain(got)
	if chain.Root().Kind != string(apierror.Invalid) || len(chain.Errors) < 2 || chain.Errors[1].Kind != string(apierror.Unavailable) {
		t.Fatalf("chain = %+v", chain)
	}
}

func TestExecuteScriptErrorRejectsUntypedObjects(t *testing.T) {
	for _, object := range []lua.LValue{lua.LString("Invalid: bad declaration"), lua.LNumber(4)} {
		got := NewExecuteScriptError(&lua.ApiError{Type: lua.ApiErrorRun, Object: object})
		if got.Kind() != apierror.Internal || got.Retryable() != apierror.False {
			t.Fatalf("untyped object classified as %s/%s", got.Kind(), got.Retryable())
		}
	}
}

func TestConvertExecutionErrorRejectsNonRunEnvelopes(t *testing.T) {
	source := lua.NewError("typed").WithKind(lua.Invalid)
	for _, kind := range []lua.ApiErrorType{lua.ApiErrorSyntax, lua.ApiErrorFile, lua.ApiErrorPanic} {
		got := ConvertExecutionError(nil, &lua.ApiError{Type: kind, Object: source})
		if got.Kind() != apierror.Internal || got.Retryable() != apierror.False {
			t.Fatalf("envelope %v = %s/%s", kind, got.Kind(), got.Retryable())
		}
	}
}

func TestConvertExecutionErrorKeepsNativeCause(t *testing.T) {
	native := errors.New("native cause")
	source := lua.WrapError(native, "context").WithKind(lua.Invalid)
	got := ConvertExecutionError(nil, source)
	if got.Error() != "context: native cause" || !errors.Is(got, native) {
		t.Fatalf("native cause = %v", got)
	}
}

func TestConvertExecutionErrorFromPCall(t *testing.T) {
	l := lua.NewState()
	lua.OpenErrors(l)
	vm := l.DoString(`error(errors.new({message="bad declaration", kind=errors.INVALID, details={nested={field="target"}}}))`)
	if vm == nil {
		t.Fatal("expected VM error")
	}
	got := ConvertExecutionError(l, vm)
	if got.Kind() != apierror.Invalid || got.Retryable() != apierror.Unspecified || got.Error() != "bad declaration" {
		t.Fatalf("converted = %s/%s/%q", got.Kind(), got.Retryable(), got.Error())
	}
	nested, ok := got.Details().(interface{ Get(string) (any, bool) }).Get("nested")
	if !ok {
		t.Fatal("missing nested details")
	}
	if nested.(map[string]any)["field"] != "target" {
		t.Fatalf("nested details = %v", nested)
	}
	var rawVM any = vm
	envelope := rawVM.(*lua.ApiError)
	source := envelope.Object.(*lua.Error)
	source.Details()["nested"].(map[string]any)["field"] = "changed"
	l.Close()
	if nested.(map[string]any)["field"] != "target" {
		t.Fatal("details changed after conversion")
	}
	nested.(map[string]any)["field"] = "consumer change"
	newDetails, _ := got.Details().Get("nested")
	if newDetails.(map[string]any)["field"] != "target" {
		t.Fatal("details snapshot can be changed through another reader")
	}
	if ConvertExecutionError(nil, got) != got {
		t.Fatal("conversion is not idempotent")
	}
}

func TestConvertExecutionErrorVMFaults(t *testing.T) {
	for _, script := range []string{
		`error("Invalid: bad declaration")`,
		`error({kind="Invalid", message="bad declaration"})`,
		`local x = nil; return x.field`,
	} {
		l := lua.NewState()
		vm := l.DoString(script)
		if vm == nil {
			t.Fatalf("expected VM fault for %s", script)
		}
		got := ConvertExecutionError(l, vm)
		if got.Kind() != apierror.Internal || got.Retryable() != apierror.False || got.Error() == "" {
			t.Fatalf("fault = %s/%s/%q", got.Kind(), got.Retryable(), got.Error())
		}
		l.Close()
	}
	if ConvertExecutionError(nil, nil) != nil {
		t.Fatal("nil conversion returned error")
	}
	if got := NewExecuteScriptError(nil); got.Kind() != apierror.Internal || got.Retryable() != apierror.False {
		t.Fatalf("nil constructor = %v", got)
	}
}

func TestConvertExecutionErrorKeepsCategorizedWrapper(t *testing.T) {
	source := apierror.New(apierror.Invalid, "bad input").WithRetryable(apierror.False)
	cleanup := errors.New("cleanup failed")
	joined := errors.Join(fmt.Errorf("startup: %w", source), cleanup)
	got := ConvertExecutionError(nil, joined)
	if got.Kind() != apierror.Invalid || got.Retryable() != apierror.False || got.Error() != joined.Error() {
		t.Fatalf("converted = %s/%s/%q", got.Kind(), got.Retryable(), got.Error())
	}
	if !errors.Is(got, source) || !errors.Is(got, cleanup) {
		t.Fatal("wrapped causes lost")
	}
}
