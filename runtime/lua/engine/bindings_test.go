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
	const expected = "channel"
	if ChannelTypeName != expected {
		t.Errorf("ChannelTypeName = %q, want %q", ChannelTypeName, expected)
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

	ChannelModule.Load(l)

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

func TestChannelModuleBuild(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ChannelModule.Load(l)

	channel := l.GetGlobal("channel")
	if channel == lua.LNil {
		t.Fatal("channel global not set")
	}

	tbl, ok := channel.(*lua.LTable)
	if !ok {
		t.Fatalf("channel should be table, got %T", channel)
	}

	if tbl.RawGetString("new") == lua.LNil {
		t.Error("module should have 'new' function")
	}

	if tbl.RawGetString("select") == lua.LNil {
		t.Error("module should have 'select' function")
	}

	if !tbl.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestChannelNewFunc(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ChannelModule.Load(l)

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

	ChannelModule.Load(l)

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

	ChannelModule.Load(l)

	err := l.DoString(`
		local ch = channel.new(2)

		-- Test send to buffered channel (returns true on success)
		local ok = ch:send("hello")
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

func TestChannelMetatable(t *testing.T) {
	ChannelModule.Load(lua.NewState())

	mt := value.GetTypeMetatable(nil, ChannelTypeName)
	if mt == nil {
		t.Fatal("channel metatable should be registered")
	}

	if !mt.Immutable {
		t.Error("metatable should be immutable")
	}
}

func TestChannelModuleIsSingleton(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	ChannelModule.Load(l1)
	ChannelModule.Load(l2)

	tbl1 := l1.GetGlobal("channel").(*lua.LTable)
	tbl2 := l2.GetGlobal("channel").(*lua.LTable)

	if tbl1 != tbl2 {
		t.Error("ChannelModule should return the same table instance")
	}
}

func TestPubSubModule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	LoadCoreModules(l)

	subscribe := l.GetGlobal("subscribe")
	if subscribe == lua.LNil {
		t.Error("subscribe global should be set")
	}

	unsubscribe := l.GetGlobal("unsubscribe")
	if unsubscribe == lua.LNil {
		t.Error("unsubscribe global should be set")
	}
}

func TestErrorsModule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	lua.OpenErrors(l)

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

func TestLoadCoreModulesPayload(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	LoadCoreModules(l)

	payload := l.GetGlobal("payload")
	if payload == lua.LNil {
		t.Error("payload global should be set")
	}
}

func TestLoadCoreModulesOs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	LoadCoreModules(l)

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

func TestPrintModule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	PrintModule.Load(l)

	printFn := l.GetGlobal("print")
	if printFn == lua.LNil {
		t.Error("print global should be set")
	}
}

func TestPrintFuncWithLogger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	logger := zap.NewNop()
	ctx := logs.WithLogger(context.Background(), logger)
	l.SetContext(ctx)

	PrintModule.Load(l)

	err := l.DoString(`print("test", "message", 123)`)
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
}

func TestPrintFuncWithoutLogger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.SetContext(context.Background())
	PrintModule.Load(l)

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

func TestLoadCoreModules(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	LoadCoreModules(l)

	globals := []string{"channel", "subscribe", "unsubscribe", "print", "payload", "os"}
	for _, name := range globals {
		if l.GetGlobal(name) == lua.LNil {
			t.Errorf("%s global should be set", name)
		}
	}
}

func TestCoreModulesReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	LoadCoreModules(l1)
	LoadCoreModules(l2)

	ch1 := l1.GetGlobal("channel").(*lua.LTable)
	ch2 := l2.GetGlobal("channel").(*lua.LTable)
	if ch1 != ch2 {
		t.Error("channel table should be reused across states")
	}

	// Functions can't be compared directly, just verify they exist
	pr1 := l1.GetGlobal("print")
	pr2 := l2.GetGlobal("print")
	if pr1 == lua.LNil || pr2 == lua.LNil {
		t.Error("print function should be set in both states")
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

	_ = l.DoString(`
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

	ChannelModule.Load(l)

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

	ChannelModule.Load(l)

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
