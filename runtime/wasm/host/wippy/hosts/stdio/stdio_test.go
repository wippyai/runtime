package stdio

import (
	"bytes"
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestHosts_FromTerminalContext(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()

	stdin := bytes.NewBufferString("hello")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	tc := terminalapi.NewTerminalContext(stdin, stdout, stderr)
	if err := terminalapi.WithTerminalContext(ctx, tc); err != nil {
		t.Fatalf("WithTerminalContext() error = %v", err)
	}

	res := preview2.NewResourceTable()

	inHost := NewHost(res)
	if inHost.Namespace() != StdinNamespace {
		t.Fatalf("Namespace() = %q, want %q", inHost.Namespace(), StdinNamespace)
	}
	inHandle := inHost.GetStdin(ctx)
	inRes, ok := res.Get(inHandle)
	if !ok {
		t.Fatalf("stdin handle %d not found", inHandle)
	}
	reader, ok := inRes.(interface {
		Read(uint64) ([]byte, error)
	})
	if !ok {
		t.Fatalf("stdin resource type = %T, want reader", inRes)
	}
	chunk, err := reader.Read(5)
	if err != nil {
		t.Fatalf("stdin read error = %v", err)
	}
	if string(chunk) != "hello" {
		t.Fatalf("stdin read = %q, want %q", string(chunk), "hello")
	}

	outHost := NewStdoutHost(res)
	if outHost.Namespace() != StdoutNamespace {
		t.Fatalf("Namespace() = %q, want %q", outHost.Namespace(), StdoutNamespace)
	}
	outHandle := outHost.GetStdout(ctx)
	outRes, ok := res.Get(outHandle)
	if !ok {
		t.Fatalf("stdout handle %d not found", outHandle)
	}
	writer, ok := outRes.(interface {
		Write([]byte) error
	})
	if !ok {
		t.Fatalf("stdout resource type = %T, want writer", outRes)
	}
	if err := writer.Write([]byte("out")); err != nil {
		t.Fatalf("stdout write error = %v", err)
	}
	if stdout.String() != "out" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "out")
	}

	errHost := NewStderrHost(res)
	if errHost.Namespace() != StderrNamespace {
		t.Fatalf("Namespace() = %q, want %q", errHost.Namespace(), StderrNamespace)
	}
	errHandle := errHost.GetStderr(ctx)
	errRes, ok := res.Get(errHandle)
	if !ok {
		t.Fatalf("stderr handle %d not found", errHandle)
	}
	errWriter, ok := errRes.(interface {
		Write([]byte) error
	})
	if !ok {
		t.Fatalf("stderr resource type = %T, want writer", errRes)
	}
	if err := errWriter.Write([]byte("err")); err != nil {
		t.Fatalf("stderr write error = %v", err)
	}
	if stderr.String() != "err" {
		t.Fatalf("stderr = %q, want %q", stderr.String(), "err")
	}
}

func TestTerminalHosts_ContextPresence(t *testing.T) {
	t.Run("no terminal context", func(t *testing.T) {
		ctx := context.Background()

		if got := NewTerminalStdinHost().GetTerminalStdin(ctx); got != nil {
			t.Fatalf("GetTerminalStdin() = %v, want nil", *got)
		}
		if got := NewTerminalStdoutHost().GetTerminalStdout(ctx); got != nil {
			t.Fatalf("GetTerminalStdout() = %v, want nil", *got)
		}
		if got := NewTerminalStderrHost().GetTerminalStderr(ctx); got != nil {
			t.Fatalf("GetTerminalStderr() = %v, want nil", *got)
		}
	})

	t.Run("with terminal context", func(t *testing.T) {
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		defer func() { _ = fc.Close() }()

		tc := terminalapi.NewTerminalContext(bytes.NewBufferString("x"), &bytes.Buffer{}, &bytes.Buffer{})
		if err := terminalapi.WithTerminalContext(ctx, tc); err != nil {
			t.Fatalf("WithTerminalContext() error = %v", err)
		}

		if got := NewTerminalStdinHost().GetTerminalStdin(ctx); got == nil {
			t.Fatal("GetTerminalStdin() = nil, want non-nil")
		}
		if got := NewTerminalStdoutHost().GetTerminalStdout(ctx); got == nil {
			t.Fatal("GetTerminalStdout() = nil, want non-nil")
		}
		if got := NewTerminalStderrHost().GetTerminalStderr(ctx); got == nil {
			t.Fatal("GetTerminalStderr() = nil, want non-nil")
		}
	})
}

func TestHosts_StdoutWithoutTerminalContextErrors(t *testing.T) {
	res := preview2.NewResourceTable()
	host := NewStdoutHost(res)

	handle := host.GetStdout(context.Background())
	r, ok := res.Get(handle)
	if !ok {
		t.Fatalf("stdout handle %d not found", handle)
	}
	writer, ok := r.(interface {
		Write([]byte) error
	})
	if !ok {
		t.Fatalf("stdout resource type = %T, want writer", r)
	}
	if err := writer.Write([]byte("data")); err == nil {
		t.Fatal("Write() expected error without terminal context")
	}
}
