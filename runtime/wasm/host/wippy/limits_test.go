// SPDX-License-Identifier: MPL-2.0

package wippy

import (
	"context"
	"testing"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

func TestCallLimitsContext(t *testing.T) {
	want := wasmapi.LimitsConfig{MaxOpenSockets: 7, SocketTimeoutMS: 250}
	ctx := WithCallLimits(context.Background(), want)
	if got := GetCallLimits(ctx); got != want {
		t.Fatalf("GetCallLimits() = %#v, want %#v", got, want)
	}
}

func TestCallLimitsContextDefault(t *testing.T) {
	if got := GetCallLimits(context.Background()); got != (wasmapi.LimitsConfig{}) {
		t.Fatalf("GetCallLimits() = %#v, want zero value", got)
	}
}
