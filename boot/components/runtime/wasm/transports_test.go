// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"testing"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
)

func TestNewTransportRegistry_Defaults(t *testing.T) {
	reg, err := newTransportRegistry()
	if err != nil {
		t.Fatalf("newTransportRegistry() error = %v", err)
	}

	tests := []string{
		wasmapi.TransportTypePayload,
		wasmapi.TransportTypeWASIHTTP,
	}
	for _, name := range tests {
		raw, ok := reg.Get(name)
		if !ok {
			t.Fatalf("transport %q not registered", name)
		}
		if _, ok := raw.(wasmengine.Transport); !ok {
			t.Fatalf("transport %q has invalid type %T", name, raw)
		}
	}
}
