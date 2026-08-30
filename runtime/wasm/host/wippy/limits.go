// SPDX-License-Identifier: MPL-2.0

package wippy

import (
	"context"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

type callLimitsKey struct{}

func WithCallLimits(ctx context.Context, limits wasmapi.LimitsConfig) context.Context {
	return context.WithValue(ctx, callLimitsKey{}, limits)
}

func GetCallLimits(ctx context.Context) wasmapi.LimitsConfig {
	limits, _ := ctx.Value(callLimitsKey{}).(wasmapi.LimitsConfig)
	return limits
}
