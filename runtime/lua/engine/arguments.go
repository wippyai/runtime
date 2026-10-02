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
