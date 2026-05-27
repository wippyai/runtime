// SPDX-License-Identifier: MPL-2.0

package websocket

import (
	"sync"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/security"
	wsapi "github.com/wippyai/runtime/api/service/websocket"
)

func bind(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

func TestLoader(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	mod := l.GetGlobal("websocket")
	if mod.Type() != lua.LTTable {
		t.Fatal("websocket module not registered")
	}

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("connect").Type() != lua.LTFunction {
		t.Error("connect function not registered")
	}
}

func TestConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		-- Message types
		if websocket.TYPE_TEXT ~= "text" then
			error("TYPE_TEXT should be 'text'")
		end
		if websocket.TYPE_BINARY ~= "binary" then
			error("TYPE_BINARY should be 'binary'")
		end
		if websocket.TYPE_PING ~= "ping" then
			error("TYPE_PING should be 'ping'")
		end
		if websocket.TYPE_PONG ~= "pong" then
			error("TYPE_PONG should be 'pong'")
		end
		if websocket.TYPE_CLOSE ~= "close" then
			error("TYPE_CLOSE should be 'close'")
		end

		-- Numeric constants
		if websocket.TEXT ~= 1 then
			error("TEXT should be 1")
		end
		if websocket.BINARY ~= 2 then
			error("BINARY should be 2")
		end

		-- Close codes
		if websocket.CLOSE_CODES.NORMAL ~= 1000 then
			error("CLOSE_CODES.NORMAL should be 1000")
		end
		if websocket.CLOSE_CODES.GOING_AWAY ~= 1001 then
			error("CLOSE_CODES.GOING_AWAY should be 1001")
		end
		if websocket.CLOSE_CODES.INTERNAL_ERROR ~= 1011 then
			error("CLOSE_CODES.INTERNAL_ERROR should be 1011")
		end

		-- Compression modes
		if websocket.COMPRESSION.DISABLED ~= 0 then
			error("COMPRESSION.DISABLED should be 0")
		end
		if websocket.COMPRESSION.CONTEXT_TAKEOVER ~= 1 then
			error("COMPRESSION.CONTEXT_TAKEOVER should be 1")
		end
		if websocket.COMPRESSION.NO_CONTEXT ~= 2 then
			error("COMPRESSION.NO_CONTEXT should be 2")
		end
	`)
	if err != nil {
		t.Errorf("constants test failed: %v", err)
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local success = pcall(function()
			websocket.foo = "bar"
		end)
	`)
	if err != nil {
		t.Errorf("immutability test failed: %v", err)
	}
}

func TestConnectNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local ok, err = pcall(function()
			websocket.connect("ws://example.com")
		end)
	`)
	if err != nil {
		t.Errorf("connect no context test failed: %v", err)
	}
}

func TestConnectWithContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	bind(l)

	fn := l.GetGlobal("websocket").(*lua.LTable).RawGetString("connect")
	l.Push(fn)
	l.Push(lua.LString("ws://example.com"))
	err := l.PCall(1, lua.MultRet, nil)

	if err == nil {
		t.Error("expected yield error from main thread")
	}
}

func TestConnectWithOptions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	bind(l)

	fn := l.GetGlobal("websocket").(*lua.LTable).RawGetString("connect")
	l.Push(fn)
	l.Push(lua.LString("ws://example.com"))

	opts := l.CreateTable(0, 4)
	headers := l.CreateTable(0, 1)
	headers.RawSetString("Authorization", lua.LString("Bearer token"))
	opts.RawSetString("headers", headers)
	opts.RawSetString("dial_timeout", lua.LNumber(5))
	opts.RawSetString("compression", lua.LString("context_takeover"))
	opts.RawSetString("read_limit", lua.LNumber(1024))
	l.Push(opts)

	err := l.PCall(2, lua.MultRet, nil)
	if err == nil {
		t.Error("expected yield error from main thread")
	}
}

func TestLoaderIdempotent(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()

	l2 := lua.NewState()
	defer l2.Close()

	bind(l1)
	bind(l2)

	mod1 := l1.GetGlobal("websocket")
	mod2 := l2.GetGlobal("websocket")

	if mod1.Type() != lua.LTTable || mod2.Type() != lua.LTTable {
		t.Fatal("module should be registered in both states")
	}
}

func TestYieldTypes(t *testing.T) {
	mod, yields := buildModule()
	if mod == nil {
		t.Fatal("expected module table")
	}

	expectedCmds := map[int]bool{
		int(wsapi.Connect):   false,
		int(wsapi.Send):      false,
		int(wsapi.Close):     false,
		int(wsapi.Ping):      false,
		int(wsapi.Subscribe): false,
	}

	for _, y := range yields {
		if _, ok := expectedCmds[int(y.CmdID)]; ok {
			expectedCmds[int(y.CmdID)] = true
		}
	}

	for cmdID, found := range expectedCmds {
		if !found {
			t.Errorf("yield type for command %d not registered", cmdID)
		}
	}
}

func TestWsConnectYield(t *testing.T) {
	yield := AcquireWsConnectYield()
	yield.URL = "ws://example.com"
	yield.Headers = map[string]string{"X-Test": "value"}
	yield.DialTimeout = 5 * time.Second
	yield.CompressionMode = wsapi.CompressionContextTakeover
	yield.CompressionThreshold = 512
	yield.ReadLimit = 1024

	if yield.CmdID() != wsapi.Connect {
		t.Errorf("expected wsapi.Connect, got %d", yield.CmdID())
	}

	cmd := yield.ToCommand().(wsapi.ConnectCmd)
	if cmd.URL != "ws://example.com" {
		t.Errorf("URL mismatch: %s", cmd.URL)
	}
	if cmd.Headers["X-Test"] != "value" {
		t.Error("headers not copied")
	}
	if cmd.DialTimeout != 5*time.Second {
		t.Error("dial timeout not copied")
	}
	if cmd.CompressionMode != wsapi.CompressionContextTakeover {
		t.Error("compression mode not copied")
	}

	ReleaseWsConnectYield(yield)
}

func TestWsSendYield(t *testing.T) {
	yield := AcquireWsSendYield(123, []byte("hello"), wsapi.MessageText)

	if yield.CmdID() != wsapi.Send {
		t.Errorf("expected wsapi.Send, got %d", yield.CmdID())
	}

	cmd := yield.ToCommand().(wsapi.SendCmd)
	if cmd.ConnID != 123 {
		t.Errorf("ConnID mismatch: %d", cmd.ConnID)
	}
	if string(cmd.Data) != "hello" {
		t.Errorf("data mismatch: %s", cmd.Data)
	}
	if cmd.MessageType != wsapi.MessageText {
		t.Errorf("message type mismatch: %d", cmd.MessageType)
	}

	ReleaseWsSendYield(yield)
}

func TestWsSubscribeYield(t *testing.T) {
	p := pid.PID{UniqID: "test-pid"}
	yield := AcquireWsSubscribeYield(456, nil, p, "ws@456", nil)

	if yield.CmdID() != wsapi.Subscribe {
		t.Errorf("expected wsapi.Subscribe, got %d", yield.CmdID())
	}

	cmd := yield.ToCommand().(wsapi.SubscribeCmd)
	if cmd.ConnID != 456 {
		t.Errorf("ConnID mismatch: %d", cmd.ConnID)
	}
	if cmd.Topic != "ws@456" {
		t.Errorf("Topic mismatch: %s", cmd.Topic)
	}
	if cmd.PID.UniqID != "test-pid" {
		t.Errorf("PID mismatch: %s", cmd.PID.UniqID)
	}

	ReleaseWsSubscribeYield(yield)
}

func TestWsCloseYield(t *testing.T) {
	yield := AcquireWsCloseYield(789, 1000, "normal close", nil)

	if yield.CmdID() != wsapi.Close {
		t.Errorf("expected wsapi.Close, got %d", yield.CmdID())
	}

	cmd := yield.ToCommand().(wsapi.CloseCmd)
	if cmd.ConnID != 789 {
		t.Errorf("ConnID mismatch: %d", cmd.ConnID)
	}
	if cmd.Code != 1000 {
		t.Errorf("code mismatch: %d", cmd.Code)
	}
	if cmd.Reason != "normal close" {
		t.Errorf("reason mismatch: %s", cmd.Reason)
	}

	ReleaseWsCloseYield(yield)
}

func TestWsPingYield(t *testing.T) {
	yield := AcquireWsPingYield(111, []byte("ping"))

	if yield.CmdID() != wsapi.Ping {
		t.Errorf("expected wsapi.Ping, got %d", yield.CmdID())
	}

	cmd := yield.ToCommand().(wsapi.PingCmd)
	if cmd.ConnID != 111 {
		t.Errorf("ConnID mismatch: %d", cmd.ConnID)
	}

	ReleaseWsPingYield(yield)
}

func TestConnectYieldHandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	yield := AcquireWsConnectYield()
	defer ReleaseWsConnectYield(yield)

	// Test success
	results := yield.HandleResult(l, uint64(42), nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	ud, ok := results[0].(*lua.LUserData)
	if !ok {
		t.Fatal("expected userdata")
	}

	conn, ok := ud.Value.(*WsConn)
	if !ok {
		t.Fatal("expected WsConn")
	}
	if conn.ID != 42 {
		t.Errorf("expected conn ID 42, got %d", conn.ID)
	}
	if conn.Channel == nil {
		t.Error("expected channel")
	}

	// Test error
	results = yield.HandleResult(l, nil, &testError{msg: "connect failed"})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0] != lua.LNil {
		t.Error("expected nil first result on error")
	}
	if results[1].String() != "connect failed" {
		t.Errorf("expected error message, got %s", results[1].String())
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }

func TestConnMethods(t *testing.T) {
	if len(connMethods) != 5 {
		t.Errorf("expected 5 conn methods, got %d", len(connMethods))
	}

	expected := []string{"send", "receive", "channel", "close", "ping"}
	for _, name := range expected {
		if _, ok := connMethods[name]; !ok {
			t.Errorf("missing conn method: %s", name)
		}
	}
}

func TestWsConn(t *testing.T) {
	conn := &WsConn{ID: 456}
	if conn.ID != 456 {
		t.Errorf("expected ID 456, got %d", conn.ID)
	}
}

func TestYieldPooling(_ *testing.T) {
	// Test that yields can be acquired and released without issues
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			y1 := AcquireWsConnectYield()
			y1.URL = "ws://test"
			ReleaseWsConnectYield(y1)

			y2 := AcquireWsSendYield(1, []byte("test"), 1)
			ReleaseWsSendYield(y2)

			y3 := AcquireWsSubscribeYield(1, nil, pid.PID{}, "ws@1", nil)
			ReleaseWsSubscribeYield(y3)

			y4 := AcquireWsCloseYield(1, 1000, "", nil)
			ReleaseWsCloseYield(y4)

			y5 := AcquireWsPingYield(1, nil)
			ReleaseWsPingYield(y5)
		}()
	}
	wg.Wait()
}

func TestYieldStringMethods(t *testing.T) {
	y1 := &WsConnectYield{}
	if y1.String() != "<ws_connect_yield>" {
		t.Errorf("unexpected string: %s", y1.String())
	}

	y2 := &WsSendYield{}
	if y2.String() != "<ws_send_yield>" {
		t.Errorf("unexpected string: %s", y2.String())
	}

	y3 := &WsSubscribeYield{}
	if y3.String() != "<ws_subscribe_yield>" {
		t.Errorf("unexpected string: %s", y3.String())
	}

	y4 := &WsCloseYield{}
	if y4.String() != "<ws_close_yield>" {
		t.Errorf("unexpected string: %s", y4.String())
	}

	y5 := &WsPingYield{}
	if y5.String() != "<ws_ping_yield>" {
		t.Errorf("unexpected string: %s", y5.String())
	}
}

func TestYieldTypeMethods(t *testing.T) {
	y1 := &WsConnectYield{}
	if y1.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", y1.Type())
	}

	y2 := &WsSendYield{}
	if y2.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", y2.Type())
	}

	y3 := &WsSubscribeYield{}
	if y3.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", y3.Type())
	}

	y4 := &WsCloseYield{}
	if y4.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", y4.Type())
	}

	y5 := &WsPingYield{}
	if y5.Type() != lua.LTUserData {
		t.Errorf("expected LTUserData, got %v", y5.Type())
	}
}

func TestCloseCodesComplete(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local codes = websocket.CLOSE_CODES
		local expected = {
			"NORMAL", "GOING_AWAY", "PROTOCOL_ERROR", "UNSUPPORTED_DATA",
			"RESERVED", "NO_STATUS", "ABNORMAL_CLOSURE", "INVALID_PAYLOAD",
			"POLICY_VIOLATION", "MESSAGE_TOO_BIG", "MANDATORY_EXTENSION",
			"INTERNAL_ERROR", "SERVICE_RESTART", "TRY_AGAIN_LATER",
			"BAD_GATEWAY", "TLS_HANDSHAKE"
		}
		for _, name in ipairs(expected) do
			if codes[name] == nil then
				error("missing close code: " .. name)
			end
		end
	`)
	if err != nil {
		t.Errorf("close codes test failed: %v", err)
	}
}

func TestSafeIntBoundsChecking(t *testing.T) {
	tests := []struct {
		value  lua.LValue
		name   string
		min    int
		max    int
		want   int
		wantOk bool
	}{
		{lua.LNumber(5), "valid int", 0, 10, 5, true},
		{lua.LNumber(0), "valid at min", 0, 10, 0, true},
		{lua.LNumber(10), "valid at max", 0, 10, 10, true},
		{lua.LNumber(-1), "below min", 0, 10, 0, false},
		{lua.LNumber(11), "above max", 0, 10, 0, false},
		{lua.LNumber(1e18), "very large", 0, 100, 0, false},
		{lua.LNumber(-1e18), "negative large", 0, 100, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := safeInt(tt.value, tt.min, tt.max)
			if ok != tt.wantOk {
				t.Errorf("safeInt() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Errorf("safeInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeInt64BoundsChecking(t *testing.T) {
	tests := []struct {
		value  lua.LValue
		name   string
		min    int64
		max    int64
		want   int64
		wantOk bool
	}{
		{lua.LNumber(1000000), "valid int64", 0, 1e12, 1000000, true},
		{lua.LNumber(-1), "below min", 0, 1e12, 0, false},
		{lua.LNumber(2e12), "above max", 0, 1e12, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := safeInt64(tt.value, tt.min, tt.max)
			if ok != tt.wantOk {
				t.Errorf("safeInt64() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Errorf("safeInt64() = %v, want %v", got, tt.want)
			}
		})
	}
}
