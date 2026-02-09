package funcs

import (
	"context"
	"fmt"

	functionapi "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
)

const (
	// FuncsNamespace exposes Wippy function-calling host APIs to WASM components.
	FuncsNamespace = "wippy:runtime/funcs@0.1.0"
)

// Host bridges component host calls into Wippy function registry execution.
// WIT example:
//
//	interface funcs {
//	  call-string: func(target: string, input: string) -> result<string, string>;
//	  call-bytes: func(target: string, input: list<u8>) -> result<list<u8>, string>;
//	}
type Host struct {
	registry functionapi.Registry
}

// NewHost builds a funcs host bound to runtime function registry.
func NewHost(reg functionapi.Registry) *Host {
	return &Host{registry: reg}
}

// Namespace implements wasm-runtime Host.
func (h *Host) Namespace() string {
	return FuncsNamespace
}

// CallString invokes a runtime function with text input and returns text output.
func (h *Host) CallString(ctx context.Context, target string, input string) (string, error) {
	if h.registry == nil {
		return "", runtimewasm.ErrFunctionRegistryNotFound
	}

	id, err := parseTargetID(target)
	if err != nil {
		return "", err
	}

	result, err := h.registry.Call(ctx, runtimeapi.Task{
		ID:       id,
		Payloads: payload.Payloads{payload.NewString(input)},
	})
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	if result.Error != nil {
		return "", result.Error
	}
	if result.Value == nil {
		return "", nil
	}

	return payloadToString(result.Value), nil
}

// CallBytes invokes a runtime function with byte input and returns byte output.
func (h *Host) CallBytes(ctx context.Context, target string, input []byte) ([]byte, error) {
	if h.registry == nil {
		return nil, runtimewasm.ErrFunctionRegistryNotFound
	}

	id, err := parseTargetID(target)
	if err != nil {
		return nil, err
	}

	result, err := h.registry.Call(ctx, runtimeapi.Task{
		ID:       id,
		Payloads: payload.Payloads{payload.NewPayload(input, payload.Bytes)},
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	if result.Value == nil {
		return nil, nil
	}

	return payloadToBytes(result.Value), nil
}

func parseTargetID(target string) (registry.ID, error) {
	id := registry.ParseID(target)
	if id.NS == "" || id.Name == "" {
		return registry.NewID("", ""), runtimewasm.NewInvalidFunctionTargetError(target)
	}
	return id, nil
}

func payloadToString(pl payload.Payload) string {
	switch v := pl.Data().(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func payloadToBytes(pl payload.Payload) []byte {
	switch v := pl.Data().(type) {
	case []byte:
		out := make([]byte, len(v))
		copy(out, v)
		return out
	case string:
		return []byte(v)
	case nil:
		return nil
	default:
		return []byte(fmt.Sprint(v))
	}
}
