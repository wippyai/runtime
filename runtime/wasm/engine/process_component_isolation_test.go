// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

type isoHost struct {
	peeks [][]byte
	mu    sync.Mutex
}

func (h *isoHost) Namespace() string { return "test:iso/host@0.1.0" }

func (h *isoHost) Peek(_ context.Context, data []byte) uint32 {
	cp := append([]byte(nil), data...)
	h.mu.Lock()
	h.peeks = append(h.peeks, cp)
	h.mu.Unlock()
	var sum uint32 = 2166136261
	for _, b := range cp {
		sum ^= uint32(b)
		sum *= 16777619
	}
	return sum
}

func (h *isoHost) snapshot() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]byte, len(h.peeks))
	for i, p := range h.peeks {
		out[i] = append([]byte(nil), p...)
	}
	return out
}

func isoChecksum(data []byte) uint32 {
	var h uint32 = 2166136261
	for _, b := range data {
		h ^= uint32(b)
		h *= 16777619
	}
	return h
}

func loadCompiledTwoCoreHost(ctx context.Context, t *testing.T, host wasmrt.Host) (*wasmrt.Runtime, *wasmrt.Module) {
	t.Helper()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatalf("wasmrt.NewWithConfig: %v", err)
	}
	if err := rt.RegisterHost(host); err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("RegisterHost: %v", err)
	}
	wasmBytes, err := os.ReadFile("testdata/two_core_host.wasm")
	if err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("ReadFile testdata/two_core_host.wasm: %v", err)
	}
	mod, err := rt.LoadComponent(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("LoadComponent: %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("Compile: %v", err)
	}
	return rt, mod
}

func processCall(t *testing.T, p *Process, method string, in payload.Payload) payload.Payload {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	var args []payload.Payload
	if in != nil {
		args = []payload.Payload{in}
	}
	if err := p.Init(ctx, method, args); err != nil {
		t.Fatalf("%s Init: %v", method, err)
	}
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("%s Step: %v", method, err)
	}
	if !out.IsDone() {
		t.Fatalf("%s not done, status=%v", method, out.Status())
	}
	res := out.Result()
	if res == nil {
		t.Fatalf("%s result is nil", method)
	}
	return res
}

func listBytes(t *testing.T, res payload.Payload) []byte {
	t.Helper()
	switch v := res.Data().(type) {
	case string:
		return []byte(v)
	case []byte:
		return v
	default:
		t.Fatalf("list result type %T (%v)", v, v)
		return nil
	}
}

func TestProcess_TwoCoreHost_TwoAliveSameCompiledModule(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	host := &isoHost{}
	rt, mod := loadCompiledTwoCoreHost(ctx, t, host)
	defer rt.Close(ctx)

	pA := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer pA.Close()
	pB := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer pB.Close()

	echoA := []byte("alpha-A")
	echoB := []byte("bravo-B-longer")
	payloadA := []byte{0xAA, 0x01, 0x11, 0x21}
	payloadB := []byte{0xBB, 0x02, 0x12, 0x22}

	if got := listBytes(t, processCall(t, pA, "echo-list", payload.New(echoA))); !bytes.Equal(got, echoA) {
		t.Fatalf("A echo-list got %q want %q", got, echoA)
	}
	if got := listBytes(t, processCall(t, pB, "echo-list", payload.New(echoB))); !bytes.Equal(got, echoB) {
		t.Fatalf("B echo-list got %q want %q", got, echoB)
	}
	if pA.inst == nil || pA.inst.MemorySize() < 131072 {
		t.Fatalf("A MemorySize after echo-list, want canonical main >= 131072")
	}
	if pB.inst == nil || pB.inst.MemorySize() < 131072 {
		t.Fatalf("B MemorySize after echo-list, want canonical main >= 131072")
	}

	if got, ok := processCall(t, pA, "stamp", payload.New(uint32(0xAA))).Data().(uint32); !ok || got != 0xAA {
		t.Fatalf("A stamp=%v", got)
	}
	if got, ok := processCall(t, pB, "stamp", payload.New(uint32(0xBB))).Data().(uint32); !ok || got != 0xBB {
		t.Fatalf("B stamp=%v", got)
	}
	if got, ok := processCall(t, pA, "read-stamp", nil).Data().(uint32); !ok || got != 0xAA {
		t.Fatalf("A read-stamp=%v want 0xAA", got)
	}
	if got, ok := processCall(t, pB, "read-stamp", nil).Data().(uint32); !ok || got != 0xBB {
		t.Fatalf("B read-stamp=%v want 0xBB", got)
	}

	if got, ok := processCall(t, pA, "peek-host", payload.New(payloadA)).Data().(uint32); !ok || got != isoChecksum(payloadA) {
		t.Fatalf("A peek-host=%v", got)
	}
	if got, ok := processCall(t, pB, "peek-host", payload.New(payloadB)).Data().(uint32); !ok || got != isoChecksum(payloadB) {
		t.Fatalf("B peek-host=%v", got)
	}
	peeks := host.snapshot()
	if len(peeks) != 2 {
		t.Fatalf("host peek count=%d want 2", len(peeks))
	}
	if !bytes.Equal(peeks[0], payloadA) {
		t.Fatalf("host callback 0=%v want %v", peeks[0], payloadA)
	}
	if !bytes.Equal(peeks[1], payloadB) {
		t.Fatalf("host callback 1=%v want %v", peeks[1], payloadB)
	}

	pA.Close()
	if got, ok := processCall(t, pB, "read-stamp", nil).Data().(uint32); !ok || got != 0xBB {
		t.Fatalf("B read-stamp after A close=%v want 0xBB", got)
	}
	if got := listBytes(t, processCall(t, pB, "echo-list", payload.New(echoB))); !bytes.Equal(got, echoB) {
		t.Fatalf("B echo-list after A close got %q want %q", got, echoB)
	}
	if got, ok := processCall(t, pB, "peek-host", payload.New(payloadB)).Data().(uint32); !ok || got != isoChecksum(payloadB) {
		t.Fatalf("B peek-host after A close=%v", got)
	}

	pC := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer pC.Close()
	if got, ok := processCall(t, pC, "stamp", payload.New(uint32(0xCC))).Data().(uint32); !ok || got != 0xCC {
		t.Fatalf("C stamp=%v", got)
	}
	if got, ok := processCall(t, pB, "read-stamp", nil).Data().(uint32); !ok || got != 0xBB {
		t.Fatalf("B read-stamp after C stamp=%v want 0xBB", got)
	}
	pB.Close()
	if got, ok := processCall(t, pC, "read-stamp", nil).Data().(uint32); !ok || got != 0xCC {
		t.Fatalf("C read-stamp after B close=%v want 0xCC", got)
	}
}

func TestProcess_TwoCoreHost_RepeatedNewInstanceAfterClose(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, mod := loadCompiledTwoCoreHost(ctx, t, &isoHost{})
	defer rt.Close(ctx)

	for i := 1; i <= 3; i++ {
		p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
		marker := uint32(0xA0 + i)
		body := []byte{byte(marker), byte(i), 0x7E}
		if got := listBytes(t, processCall(t, p, "echo-list", payload.New(body))); !bytes.Equal(got, body) {
			t.Fatalf("echo-list #%d got %v want %v", i, got, body)
		}
		if got, ok := processCall(t, p, "stamp", payload.New(marker)).Data().(uint32); !ok || got != marker {
			t.Fatalf("stamp #%d=%v", i, got)
		}
		if got, ok := processCall(t, p, "read-stamp", nil).Data().(uint32); !ok || got != marker {
			t.Fatalf("read-stamp #%d=%v", i, got)
		}
		if got, ok := processCall(t, p, "peek-host", payload.New(body)).Data().(uint32); !ok || got != isoChecksum(body) {
			t.Fatalf("peek-host #%d=%v", i, got)
		}
		p.Close()
	}
}

func TestProcess_TwoCoreEcho_IndependentInstancesSameExport(t *testing.T) {
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

	pA := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer pA.Close()
	pB := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer pB.Close()

	msgA := "canonical-A"
	msgB := "canonical-B-longer"
	gotA, ok := processCall(t, pA, "echo", payload.New(msgA)).Data().(string)
	if !ok || gotA != msgA {
		t.Fatalf("A echo=%q want %q", gotA, msgA)
	}
	gotB, ok := processCall(t, pB, "echo", payload.New(msgB)).Data().(string)
	if !ok || gotB != msgB {
		t.Fatalf("B echo=%q want %q", gotB, msgB)
	}
	if pA.inst.MemorySize() < 131072 {
		t.Fatalf("A MemorySize=%d want >= 131072", pA.inst.MemorySize())
	}
	if pB.inst.MemorySize() < 131072 {
		t.Fatalf("B MemorySize=%d want >= 131072", pB.inst.MemorySize())
	}

	listA := []byte{1, 2, 3}
	listB := []byte{9, 8, 7, 6}
	if got := listBytes(t, processCall(t, pA, "echo-list", payload.New(listA))); !bytes.Equal(got, listA) {
		t.Fatalf("A echo-list got %v want %v", got, listA)
	}
	if got := listBytes(t, processCall(t, pB, "echo-list", payload.New(listB))); !bytes.Equal(got, listB) {
		t.Fatalf("B echo-list got %v want %v", got, listB)
	}

	pA.Close()
	if got := listBytes(t, processCall(t, pB, "echo-list", payload.New(listB))); !bytes.Equal(got, listB) {
		t.Fatalf("B echo-list after A close got %v want %v", got, listB)
	}
}
