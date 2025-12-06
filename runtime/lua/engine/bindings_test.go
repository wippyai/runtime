package engine

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestChannelTypeName(t *testing.T) {
	if ChannelTypeName != "channel" {
		t.Errorf("ChannelTypeName = %q, want %q", ChannelTypeName, "channel")
	}
}

func TestSelectCaseString(t *testing.T) {
	sc := &SelectCase{Kind: SendOp}
	if sc.String() != "<select_case>" {
		t.Errorf("SelectCase.String() = %q, want %q", sc.String(), "<select_case>")
	}
	if sc.Type() != lua.LTUserData {
		t.Errorf("SelectCase.Type() = %v, want %v", sc.Type(), lua.LTUserData)
	}
}

func TestCheckChannel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	RegisterChannelMetatable()

	ch := NewChannel(1)
	ud := l.NewUserData()
	ud.Value = ch
	ud.Metatable = value.GetTypeMetatable(nil, ChannelTypeName)
	l.Push(ud)

	extracted := checkChannel(l, 1)
	if extracted != ch {
		t.Error("checkChannel should extract the correct channel")
	}
}

func TestCheckSelectCaseValue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ch := NewChannel(0)
	sc := &SelectCase{Kind: ReceiveOp, Channel: ch}

	ud := l.NewUserData()
	ud.Value = sc

	extracted := checkSelectCaseValue(ud)
	if extracted != sc {
		t.Error("checkSelectCaseValue should extract the correct SelectCase")
	}

	if checkSelectCaseValue(lua.LString("not a select case")) != nil {
		t.Error("checkSelectCaseValue should return nil for non-SelectCase")
	}
}

func TestGetChannelModuleTable(t *testing.T) {
	tbl := getChannelModuleTable()
	if tbl == nil {
		t.Fatal("getChannelModuleTable returned nil")
	}

	newFn := tbl.RawGetString("new")
	if newFn == lua.LNil {
		t.Error("module should have 'new' function")
	}

	selectFn := tbl.RawGetString("select")
	if selectFn == lua.LNil {
		t.Error("module should have 'select' function")
	}

	if !tbl.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestChannelNewFunc(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	RegisterChannelMetatable()
	BindChannelFunctions(l)

	err := l.DoString(`
		local ch = channel.new(5)
		if ch == nil then
			error("channel.new returned nil")
		end
	`)
	if err != nil {
		t.Fatalf("channel.new failed: %v", err)
	}
}

func TestPushChannel(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	RegisterChannelMetatable()

	ch := NewChannel(2)
	ud := PushChannel(l, ch)

	if ud == nil {
		t.Fatal("PushChannel returned nil")
	}

	if ud.Value != ch {
		t.Error("userdata value should be the channel")
	}

	if ch.Value() != ud {
		t.Error("channel's value reference should be set")
	}

	if l.GetTop() != 1 {
		t.Errorf("stack should have 1 item, got %d", l.GetTop())
	}
}

func TestChannelMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	RegisterChannelMetatable()
	BindChannelFunctions(l)

	err := l.DoString(`
		local ch = channel.new(2)

		-- Test send to buffered channel
		local v, ok = ch:send("hello")
		if not ok then
			error("send to buffered channel should succeed")
		end

		-- Test case_send
		local case1 = ch:case_send("world")
		if case1 == nil then
			error("case_send should return a case")
		end

		-- Test case_receive
		local case2 = ch:case_receive()
		if case2 == nil then
			error("case_receive should return a case")
		end

		-- Test receive
		local val, ok2 = ch:receive()
		if val ~= "hello" then
			error("receive should return the sent value")
		end

		-- Test close
		ch:close()
	`)
	if err != nil {
		t.Fatalf("channel methods test failed: %v", err)
	}
}

func TestRegisterChannelMetatable(t *testing.T) {
	RegisterChannelMetatable()

	mt := value.GetTypeMetatable(nil, ChannelTypeName)
	if mt == nil {
		t.Fatal("channel metatable should be registered")
	}

	if !mt.Immutable {
		t.Error("metatable should be immutable")
	}
}

func TestGetChannelModuleTableIsSingleton(t *testing.T) {
	tbl1 := GetChannelModuleTable()
	tbl2 := GetChannelModuleTable()

	if tbl1 != tbl2 {
		t.Error("GetChannelModuleTable should return the same table instance")
	}
}

func TestBindChannelFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindChannelFunctions(l)

	channel := l.GetGlobal("channel")
	if channel == lua.LNil {
		t.Error("channel global should be set")
	}

	if _, ok := channel.(*lua.LTable); !ok {
		t.Errorf("channel should be a table, got %T", channel)
	}
}

func TestBindSubscribeFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindSubscribeFunctions(l)

	subscribe := l.GetGlobal("subscribe")
	if subscribe == lua.LNil {
		t.Error("subscribe global should be set")
	}

	unsubscribe := l.GetGlobal("unsubscribe")
	if unsubscribe == lua.LNil {
		t.Error("unsubscribe global should be set")
	}
}

func TestBindErrorsModule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindErrorsModule(l)

	err := l.DoString(`
		local e = errors.new({message = "test error"})
		if e == nil then
			error("errors.new should return an error")
		end
	`)
	if err != nil {
		t.Fatalf("errors module test failed: %v", err)
	}
}

func TestBindPayloadModule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindPayloadModule(l)

	payload := l.GetGlobal("payload")
	if payload == lua.LNil {
		t.Error("payload global should be set")
	}
}

func TestBindOsModule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindOsModule(l)

	os := l.GetGlobal("os")
	if os == lua.LNil {
		t.Error("os global should be set")
	}

	err := l.DoString(`
		local t = os.time()
		if type(t) ~= "number" then
			error("os.time should return a number")
		end
	`)
	if err != nil {
		t.Fatalf("os module test failed: %v", err)
	}
}

func TestBindPrint(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	BindPrint(l)

	print := l.GetGlobal("print")
	if print == lua.LNil {
		t.Error("print global should be set")
	}
}

func TestPrintFuncWithLogger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	logger := zap.NewNop()
	ctx := logs.WithLogger(context.Background(), logger)
	l.SetContext(ctx)

	BindPrint(l)

	err := l.DoString(`print("test", "message", 123)`)
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
}

func TestPrintFuncWithoutLogger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.SetContext(context.Background())
	BindPrint(l)

	err := l.DoString(`print("test message")`)
	if err != nil {
		t.Fatalf("print without logger failed: %v", err)
	}
}

func TestCoreBinders(t *testing.T) {
	binders := CoreBinders()
	if len(binders) == 0 {
		t.Error("CoreBinders should return non-empty slice")
	}

	l := lua.NewState()
	defer l.Close()

	for _, binder := range binders {
		binder(l)
	}

	if l.GetGlobal("channel") == lua.LNil {
		t.Error("channel should be bound after CoreBinders")
	}
	if l.GetGlobal("subscribe") == lua.LNil {
		t.Error("subscribe should be bound after CoreBinders")
	}
	if l.GetGlobal("print") == lua.LNil {
		t.Error("print should be bound after CoreBinders")
	}
}

func TestGetStackTrace(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	err := l.DoString(`
		function foo()
			bar()
		end
		function bar()
			baz()
		end
		function baz()
			error("test")
		end
		foo()
	`)
	if err == nil {
		t.Fatal("expected error")
	}

	trace := GetStackTrace(l)
	if trace == nil {
		t.Log("stack trace is nil (expected for error state)")
	}
}

func TestGetStackFrame(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.DoString(`
		function test()
			return debug.getinfo(1)
		end
	`)

	frame, ok := GetStackFrame(l, 0)
	if !ok {
		t.Log("no stack frame at level 0 (expected outside of running function)")
	} else {
		t.Logf("frame: %+v", frame)
	}
}

func TestGetCallerLine(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	line, ok := GetCallerLine(l, 0)
	if ok {
		t.Logf("caller line: %d", line)
	}
}

func TestSelectWithDefaultFlag(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	RegisterChannelMetatable()
	BindChannelFunctions(l)

	err := l.DoString(`
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)

		-- Empty channels with default should return immediately
		local result = channel.select({ch1:case_receive(), ch2:case_receive()}, true)
		if not result.default then
			error("expected default case")
		end
		if not result.ok then
			error("expected ok=true for default")
		end
	`)
	if err != nil {
		t.Fatalf("select with default test failed: %v", err)
	}
}

func TestSelectWithBufferedChannelReady(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	RegisterChannelMetatable()
	BindChannelFunctions(l)

	err := l.DoString(`
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)

		ch1:send("ready")

		-- ch1 has data, should select it immediately
		local result = channel.select({ch1:case_receive(), ch2:case_receive()})
		if result.value ~= "ready" then
			error("expected value 'ready', got " .. tostring(result.value))
		end
		if not result.ok then
			error("expected ok=true")
		end
	`)
	if err != nil {
		t.Fatalf("select with buffered ready test failed: %v", err)
	}
}
