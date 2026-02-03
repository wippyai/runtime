package tty

import (
	"bytes"
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/service/terminal"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type testReceiver struct {
	data any
	err  error
}

func (r *testReceiver) CompleteYield(_ uint64, data any, err error) {
	r.data = data
	r.err = err
}

type stubRawController struct {
	enableCalls  int
	disableCalls int
}

func (s *stubRawController) Enable() error {
	s.enableCalls++
	return nil
}

func (s *stubRawController) Disable() error {
	s.disableCalls++
	return nil
}

func (s *stubRawController) Reset() error { return nil }

func (s *stubRawController) Enabled() bool {
	return s.enableCalls > s.disableCalls
}

func withTerminalContext(stdin string) context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	tc := terminal.NewTerminalContext(bytes.NewBufferString(stdin), nil, nil)
	_ = terminal.WithTerminalContext(ctx, tc)
	return ctx
}

func TestDispatcherRead_NoTerminalContext(t *testing.T) {
	d := NewDispatcher()
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	receiver := &testReceiver{}

	_ = d.handle(ctx, ttyapi.ReadCmd{Size: 4}, 1, receiver)
	if receiver.err == nil {
		t.Fatal("expected error for missing terminal context")
	}
	if !errors.Is(receiver.err, errNoTerminalContext) {
		t.Errorf("expected errNoTerminalContext, got %v", receiver.err)
	}
}

func TestDispatcherRead(t *testing.T) {
	d := NewDispatcher()
	ctx := withTerminalContext("hello")
	receiver := &testReceiver{}

	_ = d.handle(ctx, ttyapi.ReadCmd{Size: 5}, 1, receiver)
	if receiver.err != nil {
		t.Fatalf("unexpected error: %v", receiver.err)
	}
	data, ok := receiver.data.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", receiver.data)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestDispatcherReadLine(t *testing.T) {
	d := NewDispatcher()
	ctx := withTerminalContext("line\n")
	receiver := &testReceiver{}

	_ = d.handle(ctx, ttyapi.ReadLineCmd{}, 1, receiver)
	if receiver.err != nil {
		t.Fatalf("unexpected error: %v", receiver.err)
	}
	line, ok := receiver.data.(string)
	if !ok {
		t.Fatalf("expected string, got %T", receiver.data)
	}
	if line != "line" {
		t.Errorf("expected 'line', got %q", line)
	}
}

func TestDispatcherReadLine_Partial(t *testing.T) {
	d := NewDispatcher()
	ctx := withTerminalContext("partial")
	receiver := &testReceiver{}

	_ = d.handle(ctx, ttyapi.ReadLineCmd{}, 1, receiver)
	if receiver.err != nil {
		t.Fatalf("unexpected error: %v", receiver.err)
	}
	line, ok := receiver.data.(string)
	if !ok {
		t.Fatalf("expected string, got %T", receiver.data)
	}
	if line != "partial" {
		t.Errorf("expected 'partial', got %q", line)
	}
}

func TestDispatcherRawEnable_NoController(t *testing.T) {
	d := NewDispatcher()
	ctx := withTerminalContext("input")
	receiver := &testReceiver{}

	_ = d.handle(ctx, ttyapi.RawEnableCmd{}, 1, receiver)
	if receiver.err == nil {
		t.Fatal("expected error for missing raw controller")
	}
	if !errors.Is(receiver.err, errNoRawController) {
		t.Errorf("expected errNoRawController, got %v", receiver.err)
	}
}

func TestDispatcherRawEnableDisable(t *testing.T) {
	d := NewDispatcher()
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	tc := terminal.NewTerminalContext(bytes.NewBufferString("input"), nil, nil)
	raw := &stubRawController{}
	tc.Raw = raw
	_ = terminal.WithTerminalContext(ctx, tc)

	enableReceiver := &testReceiver{}
	_ = d.handle(ctx, ttyapi.RawEnableCmd{}, 1, enableReceiver)
	if enableReceiver.err != nil {
		t.Fatalf("unexpected enable error: %v", enableReceiver.err)
	}
	if raw.enableCalls != 1 {
		t.Fatalf("expected enable to be called once, got %d", raw.enableCalls)
	}

	disableReceiver := &testReceiver{}
	_ = d.handle(ctx, ttyapi.RawDisableCmd{}, 2, disableReceiver)
	if disableReceiver.err != nil {
		t.Fatalf("unexpected disable error: %v", disableReceiver.err)
	}
	if raw.disableCalls != 1 {
		t.Fatalf("expected disable to be called once, got %d", raw.disableCalls)
	}
}
