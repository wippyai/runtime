package io

import (
	"bytes"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/service/terminal"
	lua "github.com/yuin/gopher-lua"
)

func bindIO(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

func TestModuleInfo(t *testing.T) {
	info := Module.Info()
	if info.Name != "io" {
		t.Errorf("expected module name 'io', got %s", info.Name)
	}
	if info.Description == "" {
		t.Error("module description should not be empty")
	}
}

func TestRegister(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	reg := Module.Register(l)
	if reg == nil {
		t.Fatal("registration should not be nil")
	}
	if reg.Table == nil {
		t.Fatal("registration table should not be nil")
	}
}

func TestLoader(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	mod := l.GetGlobal("io")
	if mod.Type() != lua.LTTable {
		t.Fatal("io module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("write").Type() != lua.LTFunction {
		t.Error("write function not registered")
	}
	if tbl.RawGetString("print").Type() != lua.LTFunction {
		t.Error("print function not registered")
	}
	if tbl.RawGetString("eprint").Type() != lua.LTFunction {
		t.Error("eprint function not registered")
	}
	if tbl.RawGetString("read").Type() != lua.LTFunction {
		t.Error("read function not registered")
	}
	if tbl.RawGetString("readline").Type() != lua.LTFunction {
		t.Error("readline function not registered")
	}
	if tbl.RawGetString("flush").Type() != lua.LTFunction {
		t.Error("flush function not registered")
	}
}

func TestWrite_NoTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	err := l.DoString(`
		local ok, err = io.write("test")
		if ok ~= nil then error("expected nil without terminal context") end
		if err == nil then error("expected error without terminal context") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestWrite_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	stdout := &bytes.Buffer{}
	tc := terminal.NewTerminalContext(nil, stdout, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	err := l.DoString(`
		local ok, err = io.write("hello", " ", "world")
		if not ok then error("write failed: " .. tostring(err)) end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	if stdout.String() != "hello world" {
		t.Errorf("expected 'hello world', got %s", stdout.String())
	}
}

func TestPrint_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	stdout := &bytes.Buffer{}
	tc := terminal.NewTerminalContext(nil, stdout, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	err := l.DoString(`
		local ok = io.print("hello", "world")
		if not ok then error("print failed") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	if stdout.String() != "hello\tworld\n" {
		t.Errorf("expected 'hello\\tworld\\n', got %q", stdout.String())
	}
}

func TestEprint_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	stderr := &bytes.Buffer{}
	tc := terminal.NewTerminalContext(nil, nil, stderr)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	err := l.DoString(`
		local ok = io.eprint("error", "message")
		if not ok then error("eprint failed") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	if stderr.String() != "error\tmessage\n" {
		t.Errorf("expected 'error\\tmessage\\n', got %q", stderr.String())
	}
}

func TestRead_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	stdin := bytes.NewBufferString("test input")
	tc := terminal.NewTerminalContext(stdin, nil, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	err := l.DoString(`
		local data, err = io.read(4)
		if not data then error("read failed: " .. tostring(err)) end
		if data ~= "test" then error("expected 'test', got " .. data) end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestReadline_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	stdin := bytes.NewBufferString("test line\n")
	tc := terminal.NewTerminalContext(stdin, nil, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	err := l.DoString(`
		local line, err = io.readline()
		if not line then error("readline failed: " .. tostring(err)) end
		if line ~= "test line" then error("expected 'test line', got " .. line) end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestFlush_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)

	stdout := &bytes.Buffer{}
	tc := terminal.NewTerminalContext(nil, stdout, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	err := l.DoString(`
		local ok, err = io.flush()
		if not ok then error("flush failed: " .. tostring(err)) end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
