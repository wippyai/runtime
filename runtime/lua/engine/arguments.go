// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
)

// transcodeArgumentToLua preserves failures: an unconvertible payload is not a
// legitimate nil argument, even if the declared parameter allows nil.
func transcodeArgumentToLua(ctx context.Context, pl payload.Payload) (lua.LValue, error) {
	if pl == nil {
		return lua.LNil, nil
	}
	if pl.Format() == payload.Lua {
		if value, ok := pl.Data().(lua.LValue); ok {
			return value, nil
		}
	}
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return nil, fmt.Errorf("no transcoder for argument format %q", pl.Format())
	}
	converted, err := transcoder.Transcode(pl, payload.Lua)
	if err != nil {
		return nil, err
	}
	if converted != nil {
		if value, ok := converted.Data().(lua.LValue); ok {
			return value, nil
		}
	}
	return nil, fmt.Errorf("transcoder did not produce a Lua argument")
}

func argumentConversionError(index int, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "cannot convert function argument").
		WithCause(cause).WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"argument": index + 1}))
}

func (p *Process) shouldValidateArguments(fn *lua.LFunction) bool {
	return p.factory.validateArguments && !fn.IsG && len(fn.Proto.ArgumentInfo) > 0
}

// The private bootstrap callback checks the actual selected callable, not the
// unannotated bootstrap. Conversion happens once in Init; failures remain
// failures for annotated handlers and keep their historical nil for untyped ones.
func (p *Process) argumentCheckFunction() *lua.LFunction {
	if p.argumentCheckFn == nil {
		p.argumentCheckFn = p.state.NewFunction(p.checkDeferredArguments)
	}
	return p.argumentCheckFn
}

func (p *Process) checkDeferredArguments(l *lua.LState) int {
	fn := l.CheckFunction(1)
	conversionErr := p.argumentError
	p.argumentError = nil
	if !p.shouldValidateArguments(fn) {
		return 0
	}
	if conversionErr != nil {
		// This boundary must keep its declared category and argument index even
		// when the engine is embedded without boot's global metadata extractor.
		l.Error(lua.WrapErrorWithLua(l, conversionErr, "").
			WithKind(lua.Invalid).WithRetryable(false).
			WithDetails(map[string]any{"argument": conversionErr.Details().GetInt("argument", 0)}), 1)
		return 0
	}
	args := make([]lua.LValue, l.GetTop()-1)
	for i := range args {
		args[i] = l.Get(i + 2)
	}
	if err := fn.Proto.CheckArguments(l, args); err != nil {
		l.Error(lua.WrapError(err, ""), 1)
	}
	return 0
}
