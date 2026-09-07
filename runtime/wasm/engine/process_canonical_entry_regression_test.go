// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"bytes"
	"os"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// TestProcess_CanonicalEntryMemoryBinding_TwoCoreEcho verifies that a multi-module component
// (where the first module is an adapter/dummy with 64KB memory, and the canonical export
// module has 128KB memory and allocates at offset 70000 > 64KB) correctly binds memory
// and allocator to the canonical entry export in the production Process path.
func TestProcess_CanonicalEntryMemoryBinding_TwoCoreEcho(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatalf("wasmrt.NewWithConfig: %v", err)
	}
	defer rt.Close(ctx)

	wasmBytes, err := os.ReadFile("testdata/two_core_echo.wasm")
	if err != nil {
		t.Fatalf("ReadFile testdata/two_core_echo.wasm: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadComponent: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	t.Run("recycling includes auxiliary core memory", func(t *testing.T) {
		p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
		defer p.Close()
		if err := p.Init(ctx, "echo", []payload.Payload{payload.New("memory accounting")}); err != nil {
			t.Fatal(err)
		}
		var out process.StepOutput
		if err := p.Step(nil, &out); err != nil {
			t.Fatal(err)
		}
		if p.inst == nil || !out.IsDone() {
			t.Fatal("expected completed call retaining the instance")
		}
		entryBytes := uint64(p.inst.MemorySize())
		totalBytes, present := p.inst.LinearMemoryUsage()
		if !present || totalBytes <= entryBytes {
			t.Fatalf("fixture must have auxiliary memory: entry=%d total=%d", entryBytes, totalBytes)
		}
		p.limits.MaxRetainedMemoryBytes = int64(entryBytes + 1)
		p.retainedMemoryCheckInterval = 1
		if !p.shouldRecycleRetainedInstance() {
			t.Fatal("auxiliary core memory escaped the retained-memory limit")
		}
	})

	t.Run("string echo exact result repeatedly on resident instance", func(t *testing.T) {
		p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
		defer p.Close()

		testStrings := []string{
			"canonical-echo-message-alpha",
			"canonical-echo-message-beta-longer-string-to-verify-offsets",
			"canonical-echo-message-gamma-repeated-sequential-call",
			"hello from two-core-component",
			"final-iteration-string-exactness",
		}

		for i, want := range testStrings {
			if err := p.Init(ctx, "echo", []payload.Payload{payload.New(want)}); err != nil {
				t.Fatalf("call %d: Init error = %v", i+1, err)
			}

			var out process.StepOutput
			if err := p.Step(nil, &out); err != nil {
				t.Fatalf("call %d: Step error = %v", i+1, err)
			}
			if !out.IsDone() {
				t.Fatalf("call %d: Step not done, status = %v", i+1, out.Status())
			}

			res := out.Result()
			if res == nil {
				t.Fatalf("call %d: Result is nil", i+1)
			}

			got, ok := res.Data().(string)
			if !ok {
				t.Fatalf("call %d: expected string result, got %T (%v)", i+1, res.Data(), res.Data())
			}
			if got != want {
				t.Fatalf("call %d: exactness mismatch:\nwant %q\ngot  %q", i+1, want, got)
			}

			// Verify instance memory is bound to canonical module (128KB = 131072 bytes), not module 0 (64KB = 65536 bytes)
			if p.inst == nil {
				t.Fatalf("call %d: p.inst is nil", i+1)
			}
			memSize := p.inst.MemorySize()
			if memSize < 131072 {
				t.Fatalf("call %d: expected canonical module memory >= 131072 bytes, got %d (bound to adapter memory?)", i+1, memSize)
			}
		}
	})

	t.Run("list echo exact result repeatedly on resident instance", func(t *testing.T) {
		p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
		defer p.Close()

		testLists := [][]byte{
			{1, 2, 3, 4, 5},
			{10, 20, 30, 40, 50, 60, 70, 80},
			bytes.Repeat([]byte{0x42}, 128),
			{0xFF, 0x00, 0xAA, 0x55},
		}

		for i, wantBytes := range testLists {
			if err := p.Init(ctx, "echo-list", []payload.Payload{payload.New(wantBytes)}); err != nil {
				t.Fatalf("call %d: Init error = %v", i+1, err)
			}

			var out process.StepOutput
			if err := p.Step(nil, &out); err != nil {
				t.Fatalf("call %d: Step error = %v", i+1, err)
			}
			if !out.IsDone() {
				t.Fatalf("call %d: Step not done, status = %v", i+1, out.Status())
			}

			res := out.Result()
			if res == nil {
				t.Fatalf("call %d: Result is nil", i+1)
			}

			// Under ByteListResultBinaryString, list<u8> is decoded as string containing raw bytes
			var gotBytes []byte
			switch v := res.Data().(type) {
			case string:
				gotBytes = []byte(v)
			case []byte:
				gotBytes = v
			default:
				t.Fatalf("call %d: unexpected result data type %T", i+1, v)
			}

			if !bytes.Equal(gotBytes, wantBytes) {
				t.Fatalf("call %d: list byte mismatch:\nwant %v\ngot  %v", i+1, wantBytes, gotBytes)
			}
		}
	})
}

// TestProcess_SingleCoreModule_BackwardCompatibility ensures single-core modules
// (which have no canonical lifts) are not broken by entry export resolution.
func TestProcess_SingleCoreModule_BackwardCompatibility(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatalf("wasmrt.NewWithConfig: %v", err)
	}
	defer rt.Close(ctx)

	mod, err := rt.LoadWAT(ctx, `(module
		(memory 1)
		(func (export "add") (param i32 i32) (result i32)
			local.get 0
			local.get 1
			i32.add
		)
	)`, "add: func(a: s32, b: s32) -> s32;")
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatalf("Compile: %v", err)
	}

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer p.Close()

	if err := p.Init(ctx, "add", []payload.Payload{payload.New(int32(17)), payload.New(int32(25))}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !out.IsDone() {
		t.Fatalf("Step not done, status = %v", out.Status())
	}

	res := out.Result()
	if res == nil {
		t.Fatal("Result is nil")
	}
	if res.Data() != int32(42) {
		t.Fatalf("add result = %v, want 42", res.Data())
	}
}

// TestProcess_ResolveEntryExport_Constraints verifies exact entry identity:
// core modules retain their existing path, while component names are passed
// unchanged to the backend for canonical binding and validation.
func TestProcess_ResolveEntryExport_Constraints(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatalf("wasmrt.NewWithConfig: %v", err)
	}
	defer rt.Close(ctx)

	// 1. Single-core module
	singleMod, err := rt.LoadWAT(ctx, `(module (func (export "add") (param i32 i32) (result i32) (i32.const 0)))`, "add: func(a: s32, b: s32) -> s32;")
	if err != nil {
		t.Fatalf("LoadWAT: %v", err)
	}
	pSingle := NewProcess(singleMod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	pSingle.method = "add"
	if entry := pSingle.resolveEntryExport(ctx); entry != "" {
		t.Fatalf("single-core module should yield empty entry export, got %q", entry)
	}

	// 2. Multi-module component
	wasmBytes, err := os.ReadFile("testdata/two_core_echo.wasm")
	if err != nil {
		t.Fatalf("ReadFile testdata/two_core_echo.wasm: %v", err)
	}
	compMod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("LoadComponent: %v", err)
	}

	pComp := NewProcess(compMod, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)

	// Exact match
	pComp.method = "echo"
	if entry := pComp.resolveEntryExport(ctx); entry != "echo" {
		t.Fatalf("expected exact match 'echo', got %q", entry)
	}

	pComp.method = "echo-list"
	if entry := pComp.resolveEntryExport(ctx); entry != "echo-list" {
		t.Fatalf("expected exact match 'echo-list', got %q", entry)
	}

	// Qualified match
	pComp.method = "pkg:iface/v1#echo"
	if entry := pComp.resolveEntryExport(ctx); entry != pComp.method {
		t.Fatalf("qualified method changed to %q", entry)
	}

	// Unknown names must reach backend validation, without silently binding a default.
	pComp.method = "non_existent_method"
	if entry := pComp.resolveEntryExport(ctx); entry != pComp.method {
		t.Fatalf("unknown method changed to %q", entry)
	}

	// The backend must reject an unknown entry.
	if err := pComp.Init(ctx, "non_existent_method", nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var out process.StepOutput
	stepErr := pComp.Step(nil, &out)
	// Reject before guest invocation.
	if stepErr == nil {
		t.Fatal("expected error calling nonexistent method")
	}
}

// TestProcess_CanonicalResultOwnsBytesAcrossGuestOverwrite retains the public
// result while a resident component overwrites the same canonical allocation.
func TestProcess_CanonicalResultOwnsBytesAcrossGuestOverwrite(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	wasmBytes, err := os.ReadFile("testdata/internal-validation/reused-result.wasm")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"echo", "echo-list"} {
		t.Run(method, func(t *testing.T) {
			p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
			defer p.Close()
			var held []payload.Payload
			var resident *wasmrt.Instance
			expected := []string{"first-owned-result", "NEXT!-owned-result", "third-owned-result"}
			checkHeld := func() {
				t.Helper()
				for i, result := range held {
					var got string
					switch v := result.Data().(type) {
					case string:
						got = v
					case []byte:
						got = string(v)
					default:
						t.Fatalf("unexpected result type %T", v)
					}
					if got != expected[i] {
						t.Fatalf("retained result %d changed: got %q, want %q", i, got, expected[i])
					}
				}
			}
			for _, input := range expected {
				var value any = input
				if method == "echo-list" {
					value = []byte(input)
				}
				if err := p.Init(ctx, method, []payload.Payload{payload.New(value)}); err != nil {
					t.Fatal(err)
				}
				var out process.StepOutput
				if err := p.Step(nil, &out); err != nil {
					t.Fatal(err)
				}
				if !out.IsDone() || out.Result() == nil {
					t.Fatal("expected completed canonical result")
				}
				if resident == nil {
					resident = p.inst
				} else if resident != p.inst {
					t.Fatal("guest memory was not reused")
				}
				// Retain the original public payload; do not make a defensive test copy.
				held = append(held, out.Result())
				checkHeld()
			}
			trapMethod := "trap-string"
			var trapInput any = "overwrite-and-trap"
			if method == "echo-list" {
				trapMethod = "trap-list"
				trapInput = []byte("overwrite-and-trap")
			}
			if err := p.Init(ctx, trapMethod, []payload.Payload{payload.New(trapInput)}); err != nil {
				t.Fatal(err)
			}
			var failed process.StepOutput
			err := p.Step(nil, &failed)
			if err == nil || !strings.Contains(err.Error(), "unreachable") {
				t.Fatalf("expected guest unreachable trap, got %v", err)
			}
			if p.inst != nil {
				t.Fatal("trapped process retained guest instance")
			}
			checkHeld()
			// Reinitialize the Process wrapper, requiring a fresh instance.
			var cleanInput any = expected[0]
			if method == "echo-list" {
				cleanInput = []byte(expected[0])
			}
			if err := p.Init(ctx, method, []payload.Payload{payload.New(cleanInput)}); err != nil {
				t.Fatal(err)
			}
			var fresh process.StepOutput
			if err := p.Step(nil, &fresh); err != nil {
				t.Fatal(err)
			}
			if !fresh.IsDone() || fresh.Result() == nil || p.inst == nil || p.inst == resident {
				t.Fatal("expected successful fresh instance after trap")
			}
			var freshText string
			switch v := fresh.Result().Data().(type) {
			case string:
				freshText = v
			case []byte:
				freshText = string(v)
			default:
				t.Fatalf("unexpected fresh result type %T", v)
			}
			if freshText != expected[0] {
				t.Fatalf("replacement returned %q", freshText)
			}
			checkHeld()
			p.Close()
			checkHeld()
		})
	}
}
