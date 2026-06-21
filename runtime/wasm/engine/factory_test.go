// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"errors"
	"testing"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	wasmtranscoder "github.com/wippyai/wasm-runtime/transcoder"
)

func TestProcessDecodeOptionsUseBinaryStringByteLists(t *testing.T) {
	f := NewFactory(
		nil,
		wasmapi.TransportTypePayload,
		wasmapi.WASIConfig{},
		wasmapi.LimitsConfig{},
		nil,
	)

	procAny, err := f.Create()()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer procAny.Close()

	proc, ok := procAny.(*Process)
	if !ok {
		t.Fatalf("Create() returned %T, want *Process", procAny)
	}
	if got := proc.decodeOptions().ByteListResult; got != wasmtranscoder.ByteListResultBinaryString {
		t.Fatalf("ByteListResult = %q, want %q", got, wasmtranscoder.ByteListResultBinaryString)
	}
}

func TestFactoryWithModuleFactoryLoadsModulePerProcess(t *testing.T) {
	loaded := 0
	f := NewFactoryWithModuleFactory(func() (*wasmrt.Module, error) {
		loaded++
		return nil, nil
	}, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	create := f.Create()

	for i := 0; i < 2; i++ {
		proc, err := create()
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		proc.Close()
	}

	if loaded != 2 {
		t.Fatalf("module factory calls = %d, want 2", loaded)
	}
}

func TestFactoryWithModuleFactoryPropagatesError(t *testing.T) {
	wantErr := errors.New("load failed")
	f := NewFactoryWithModuleFactory(func() (*wasmrt.Module, error) {
		return nil, wantErr
	}, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)

	proc, err := f.Create()()
	if proc != nil {
		t.Fatal("Create() returned process on module factory error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Create() error = %v, want %v", err, wantErr)
	}
}
