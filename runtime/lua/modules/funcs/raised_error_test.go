// SPDX-License-Identifier: MPL-2.0

package funcs

import (
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/function"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
)

func TestCallYieldPreservesRaisedError(t *testing.T) {
	lua.SetErrorMetadataExtractor(func(err error) *lua.ErrorMetadata {
		chain := apierror.BuildChain(err)
		if chain == nil || chain.Root() == nil {
			return nil
		}
		root := chain.Root()
		return &lua.ErrorMetadata{Kind: lua.Kind(root.Kind), Retryable: root.Retryable, Details: root.Details}
	})
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	source := lua.NewError("bad declaration").WithKind(lua.Invalid).WithRetryable(false).WithDetails(map[string]any{"field": "target"})
	boundary := runtimelua.ConvertExecutionError(l, &lua.ApiError{Type: lua.ApiErrorRun, Object: source})
	yield := AcquireCallYield()
	defer ReleaseCallYield(yield)
	for _, tc := range []struct {
		data any
		err  error
		name string
	}{
		{function.CallResult{Error: boundary}, nil, "result"},
		{nil, boundary, "dispatcher"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := yield.HandleResult(l, tc.data, tc.err)
			if len(values) != 2 || values[0] != lua.LNil {
				t.Fatalf("values = %v", values)
			}
			l.SetGlobal("received", values[1])
			if err := l.DoString(`
				assert(received:kind() == errors.INVALID)
				assert(received:message() == "bad declaration")
				assert(received:retryable() == false)
				assert(received:details().field == "target")
				assert(errors.is(received, errors.INVALID))
			`); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(values[1].(*lua.Error), source) {
				t.Fatal("source error lost")
			}
		})
	}
}
