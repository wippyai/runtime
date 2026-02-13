package io

import (
	"bytes"
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/service/terminal"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type stubRawController struct {
	enableErr    error
	disableErr   error
	enableCalls  int
	disableCalls int
}

func (s *stubRawController) Enable() error {
	s.enableCalls++
	return s.enableErr
}

func (s *stubRawController) Disable() error {
	s.disableCalls++
	return s.disableErr
}

func (s *stubRawController) Reset() error {
	s.enableCalls = 0
	s.disableCalls = 0
	return nil
}

func (s *stubRawController) Enabled() bool {
	return s.enableCalls > s.disableCalls
}

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
	tbl, yields := Module.Build()
	if tbl == nil {
		t.Fatal("module table should not be nil")
	}
	if len(yields) != 4 {
		t.Fatalf("expected 4 yield types, got %d", len(yields))
	}
}

func TestYieldTypes(t *testing.T) {
	_, yields := Module.Build()

	expectedCmds := map[int]bool{
		int(ttyapi.Read):       false,
		int(ttyapi.ReadLine):   false,
		int(ttyapi.RawEnable):  false,
		int(ttyapi.RawDisable): false,
	}

	for _, y := range yields {
		cmdID := int(y.CmdID)
		if _, ok := expectedCmds[cmdID]; ok {
			expectedCmds[cmdID] = true
		}
	}

	for cmdID, found := range expectedCmds {
		if !found {
			t.Errorf("missing yield type for command ID %d", cmdID)
		}
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

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("write").Type() != lua.LTFunction {
		t.Error("write function not registered")
	}
	if modTbl.RawGetString("print").Type() != lua.LTFunction {
		t.Error("print function not registered")
	}
	if modTbl.RawGetString("eprint").Type() != lua.LTFunction {
		t.Error("eprint function not registered")
	}
	if modTbl.RawGetString("read").Type() != lua.LTFunction {
		t.Error("read function not registered")
	}
	if modTbl.RawGetString("readline").Type() != lua.LTFunction {
		t.Error("readline function not registered")
	}
	if modTbl.RawGetString("raw").Type() != lua.LTFunction {
		t.Error("raw function not registered")
	}
	if modTbl.RawGetString("flush").Type() != lua.LTFunction {
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

func TestReadYieldPool(t *testing.T) {
	y1 := AcquireReadYield(0)
	if y1.Size != ttyapi.DefaultReadSize {
		t.Errorf("expected default size %d, got %d", ttyapi.DefaultReadSize, y1.Size)
	}
	ReleaseReadYield(y1)

	y2 := AcquireReadYield(8)
	if y2.Size != 8 {
		t.Errorf("expected size 8, got %d", y2.Size)
	}
	ReleaseReadYield(y2)
}

func TestReadYieldHandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireReadYield(4)
	values := y.HandleResult(l, []byte("test"), nil)
	if values[0].String() != "test" {
		t.Errorf("expected 'test', got %s", values[0].String())
	}
	if values[1] != lua.LNil {
		t.Error("expected nil error")
	}
	ReleaseReadYield(y)
}

func TestReadYieldHandleError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireReadYield(4)
	values := y.HandleResult(l, nil, errors.New("boom"))
	if values[0] != lua.LNil {
		t.Error("expected nil value")
	}
	if values[1].String() != "boom" {
		t.Errorf("expected error 'boom', got %s", values[1].String())
	}
	ReleaseReadYield(y)
}

func TestReadLineYieldPool(t *testing.T) {
	y1 := AcquireReadLineYield()
	if y1 == nil {
		t.Fatal("expected non-nil yield")
	}
	ReleaseReadLineYield(y1)

	y2 := AcquireReadLineYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseReadLineYield(y2)
}

func TestReadLineYieldHandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireReadLineYield()
	values := y.HandleResult(l, "line", nil)
	if values[0].String() != "line" {
		t.Errorf("expected 'line', got %s", values[0].String())
	}
	if values[1] != lua.LNil {
		t.Error("expected nil error")
	}
	ReleaseReadLineYield(y)
}

func TestRawEnableYieldHandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireRawEnableYield()
	values := y.HandleResult(l, true, nil)
	if values[0] != lua.LTrue {
		t.Error("expected true value")
	}
	if values[1] != lua.LNil {
		t.Error("expected nil error")
	}
	ReleaseRawEnableYield(y)
}

func TestRawDisableYieldHandleError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireRawDisableYield()
	values := y.HandleResult(l, nil, errors.New("boom"))
	if values[0] != lua.LNil {
		t.Error("expected nil value")
	}
	if values[1].String() != "boom" {
		t.Errorf("expected error 'boom', got %s", values[1].String())
	}
	ReleaseRawDisableYield(y)
}

func TestReadYielding_NoTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	ret := ioReadYielding(l)
	if ret != 2 {
		t.Fatalf("expected 2 return values, got %d", ret)
	}
	if l.Get(1) != lua.LNil {
		t.Error("expected nil value")
	}
	if _, ok := l.Get(2).(*lua.Error); !ok {
		t.Error("expected structured error")
	}
}

func TestReadYielding_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	stdin := bytes.NewBufferString("test")
	tc := terminal.NewTerminalContext(stdin, nil, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ioReadYielding(l)
	if ret != -1 {
		t.Fatalf("expected yield return -1, got %d", ret)
	}
	if _, ok := l.Get(1).(*ReadYield); !ok {
		t.Fatal("expected ReadYield on stack")
	}
}

func TestReadLineYielding_WithTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	stdin := bytes.NewBufferString("test\n")
	tc := terminal.NewTerminalContext(stdin, nil, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ioReadlineYielding(l)
	if ret != -1 {
		t.Fatalf("expected yield return -1, got %d", ret)
	}
	if _, ok := l.Get(1).(*ReadLineYield); !ok {
		t.Fatal("expected ReadLineYield on stack")
	}
}

func TestRaw_NoTerminalContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	ret := ioRaw(l)
	if ret != 2 {
		t.Fatalf("expected 2 return values, got %d", ret)
	}
	if l.Get(1) != lua.LNil {
		t.Error("expected nil value")
	}
	if _, ok := l.Get(2).(*lua.Error); !ok {
		t.Error("expected structured error")
	}
}

func TestRawYielding_Enable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	stdin := bytes.NewBufferString("test")
	tc := terminal.NewTerminalContext(stdin, nil, nil)
	tc.Raw = &stubRawController{}
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ioRaw(l)
	if ret != -1 {
		t.Fatalf("expected yield return -1, got %d", ret)
	}
	if _, ok := l.Get(1).(*RawEnableYield); !ok {
		t.Fatal("expected RawEnableYield on stack")
	}
}

func TestRawYielding_Disable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	stdin := bytes.NewBufferString("test")
	tc := terminal.NewTerminalContext(stdin, nil, nil)
	tc.Raw = &stubRawController{}
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)
	l.Push(lua.LBool(false))

	ret := ioRaw(l)
	if ret != -1 {
		t.Fatalf("expected yield return -1, got %d", ret)
	}
	if _, ok := l.Get(l.GetTop()).(*RawDisableYield); !ok {
		t.Fatal("expected RawDisableYield on stack")
	}
}
