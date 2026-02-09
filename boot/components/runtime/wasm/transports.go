package wasm

import (
	"fmt"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmtransport "github.com/wippyai/runtime/runtime/wasm/transport"
)

func newTransportRegistry() (*wasmtransport.Registry, error) {
	reg := wasmtransport.NewRegistry()

	if err := reg.Register(wasmapi.TransportTypePayload, wasmtransport.NewPayloadTransport()); err != nil {
		return nil, fmt.Errorf("register payload transport: %w", err)
	}
	if err := reg.Register(wasmapi.TransportTypeWASIHTTP, wasmtransport.NewWASIHTTPTransport()); err != nil {
		return nil, fmt.Errorf("register wasi-http transport: %w", err)
	}

	return reg, nil
}
