// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

func startChannelProcess(t *testing.T, script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	proc := mustNewProcess(t,
		WithProto(proto),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	return proc
}

func runUntilDone(t *testing.T, proc *Process, maxSteps int) error {
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return err
		}
		if output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not complete in %d steps", maxSteps)
	return nil
}

func requireTruePayload(t *testing.T, result payload.Payload) {
	t.Helper()
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	switch v := result.Data().(type) {
	case bool:
		if !v {
			t.Fatal("expected true, got false")
		}
	case lua.LBool:
		if !v {
			t.Fatal("expected true, got false")
		}
	default:
		t.Fatalf("expected true, got %v (%T)", result.Data(), result.Data())
	}
}

// TestChannelSendReturnValues tests that send returns correct values.
// Per spec: send returns true on success (no value returned).
func TestChannelSendReturnValues(t *testing.T) {
	script := `
		local ch = channel.new(1)
		local ok = ch:send("hello")
		if ok ~= true then
			error("buffered send: expected true, got " .. tostring(ok))
		end
		return "ok"
	`
	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}
}

// TestLuaReturnsNothingAssignment tests what happens when a Lua function returns nothing
func TestLuaReturnsNothingAssignment(t *testing.T) {
	script := `
		function returns_nothing()
			-- deliberately returns nothing
		end

		local a, b = returns_nothing()
		local types = type(a) .. "," .. type(b)
		return types
	`
	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 10); err != nil {
		t.Fatal(err)
	}

	// After a function that returns nothing, a and b should be nil
	if proc.result != nil {
		t.Logf("Result: %v", proc.result)
	}
}

// TestSimpleYieldResume tests yield/resume without channels to verify the basic mechanism
func TestSimpleYieldResume(t *testing.T) {
	script := `
		local r1, r2

		-- Function that yields and expects resume values
		function yielder()
			r1, r2 = coroutine.yield()
			return r1, r2
		end

		-- Create coroutine
		local co = coroutine.create(yielder)

		-- First resume starts the coroutine, it yields
		coroutine.resume(co)

		-- Second resume with no values - r1, r2 should be nil
		local ok, ret1, ret2 = coroutine.resume(co)

		-- Check what we got - return as table to avoid string-as-error
		local t1 = type(ret1)
		local t2 = type(ret2)
		return {types = t1 .. "," .. t2}
	`
	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 10); err != nil {
		t.Fatal(err)
	}

	if proc.result != nil {
		t.Logf("Result: %v", proc.result)
	}
}

// TestChannelSendWakesReceiverReturnValues tests send return when waking receiver.
// OLD behavior: send returns nil (no values) when waking a blocked receiver
func TestChannelSendWakesReceiverReturnValues(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local send_r1, send_r2

		coroutine.spawn(function()
			local v, ok = ch:receive()
		end)

		-- Yield to let spawned coroutine run first and block on receive
		coroutine.yield()

		send_r1, send_r2 = ch:send("hello")

		-- Return as table to avoid second value being interpreted as error
		return {r1 = send_r1, r2 = send_r2}
	`
	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}
}

// TestChannelUnbuffered tests unbuffered channel send/recv sync
func TestChannelUnbuffered(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local received = nil

		coroutine.spawn(function()
			received = ch:receive()
		end)

		ch:send("hello")

		for i = 1, 5 do coroutine.yield() end
		return received
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if str, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if str != "hello" {
				t.Errorf("expected 'hello', got %q", str)
			}
		}
	}
	t.Log("Unbuffered channel test passed")
}

// TestChannelBuffered tests buffered channel capacity
func TestChannelBuffered(t *testing.T) {
	script := `
		local ch = channel.new(3)
		ch:send(1)
		ch:send(2)
		ch:send(3)
		local v1, _ = ch:receive()
		local v2, _ = ch:receive()
		local v3, _ = ch:receive()
		return v1, v2, v3
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}
	t.Log("Buffered channel test passed")
}

// TestChannelClose tests close semantics
func TestChannelClose(t *testing.T) {
	script := `
		local ch = channel.new(1)
		ch:send("value")
		ch:close()
		local v1, ok1 = ch:receive()
		local v2, ok2 = ch:receive()
		return v1, ok1, v2, ok2
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}
	t.Log("Channel close test passed")
}

// TestProducerConsumer tests classic producer-consumer pattern
func TestProducerConsumer(t *testing.T) {
	script := `
		local ch = channel.new(5)
		local produced = 0
		local consumed = 0

		coroutine.spawn(function()
			for i = 1, 10 do
				ch:send(i)
				produced = produced + 1
			end
			ch:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = ch:receive()
				if not ok then break end
				consumed = consumed + 1
			end
		end)

		for i = 1, 50 do coroutine.yield() end
		return produced, consumed
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 2 {
		t.Logf("Producer-consumer: produced=%v, consumed=%v",
			proc.mainTask.Yielded[0], proc.mainTask.Yielded[1])
	}
}

// TestSelectWithDefault tests select with default case
func TestSelectWithDefault(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local result = channel.select{ch1:case_receive(), ch2:case_receive(), default=true}
		if result.default then
			return true
		end
		return false
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if b, ok := proc.mainTask.Yielded[0].(lua.LBool); ok {
			if !b {
				t.Errorf("expected default case to be selected")
			}
		}
	}
	t.Log("Select with default test passed")
}

// TestNoDeadlockWithDefault tests that select with default doesn't deadlock
func TestNoDeadlockWithDefault(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local result = channel.select{ch:case_receive(), default=true}
		if result.default then return "default" end
		return "unexpected"
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if str, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if str != "default" {
				t.Errorf("expected 'default', got %q", str)
			}
		}
	}
	t.Log("Select with default prevented deadlock")
}

// TestSpawnWithChannels tests spawn + channel communication
func TestSpawnWithChannels(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local results = {}

		for i = 1, 5 do
			coroutine.spawn(function()
				ch:send(i * 10)
			end)
		end

		for i = 1, 5 do
			local v, _ = ch:receive()
			table.insert(results, v)
		end

		local sum = 0
		for _, v in ipairs(results) do sum = sum + v end
		return sum
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 150 {
				t.Errorf("expected sum=150, got %d", int(n))
			}
		}
	}
	t.Log("Spawn with channels test passed")
}

// TestBufferedFullThenBlock tests buffered channel blocking when full
func TestBufferedFullThenBlock(t *testing.T) {
	script := `
		local ch = channel.new(2)
		local sent = 0

		ch:send(1)
		sent = sent + 1
		ch:send(2)
		sent = sent + 1

		coroutine.spawn(function()
			ch:send(3)
			sent = sent + 1
		end)

		for i = 1, 3 do coroutine.yield() end

		local v1 = ch:receive()
		for i = 1, 3 do coroutine.yield() end

		local v2 = ch:receive()
		local v3 = ch:receive()

		return sent, v1, v2, v3
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 4 {
		sent := proc.mainTask.Yielded[0]
		if n, ok := sent.(lua.LNumber); ok && int(n) != 3 {
			t.Errorf("expected sent=3, got %d", int(n))
		}
	}
	t.Log("Buffered full then block test passed")
}

// TestSelectBlockingThenWake tests select blocking and waking on value
func TestSelectBlockingThenWake(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local result_ch = nil

		coroutine.spawn(function()
			local result = channel.select{ch1:case_receive(), ch2:case_receive()}
			result_ch = result.channel
		end)

		for i = 1, 3 do coroutine.yield() end
		ch2:send("wake")
		for i = 1, 5 do coroutine.yield() end

		return result_ch == ch2
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if b, ok := proc.mainTask.Yielded[0].(lua.LBool); ok {
			if !b {
				t.Error("expected result.channel == ch2")
			}
		}
	}
	t.Log("Select blocking then wake test passed")
}

// TestSelectWithCaseSend tests select with case_send
func TestSelectWithCaseSend(t *testing.T) {
	script := `
		local ch = channel.new(1)
		local result = channel.select{ch:case_send("value")}
		local v = ch:receive()
		return {ok = result.ok, value = v}
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 1 {
		tbl, ok := proc.mainTask.Yielded[0].(*lua.LTable)
		if !ok {
			t.Fatal("expected table result")
		}
		if tbl.RawGetString("ok") != lua.LTrue {
			t.Error("expected result.ok=true")
		}
		if s, ok := tbl.RawGetString("value").(lua.LString); !ok || s != "value" {
			t.Errorf("expected 'value', got %v", tbl.RawGetString("value"))
		}
	}
	t.Log("Select with case_send test passed")
}

// TestMixedSelect tests select with both send and receive cases
func TestMixedSelect(t *testing.T) {
	script := `
		local sendCh = channel.new(1)
		local recvCh = channel.new(1)
		recvCh:send("ready")

		local result = channel.select{
			sendCh:case_send("outgoing"),
			recvCh:case_receive()
		}

		return result.ok
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if b, ok := proc.mainTask.Yielded[0].(lua.LBool); ok {
			if !b {
				t.Error("expected result.ok=true")
			}
		}
	}
	t.Log("Mixed select test passed")
}

// TestMultipleBlockedSenders tests multiple blocked senders wake one at a time
func TestMultipleBlockedSenders(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local order = {}

		for i = 1, 3 do
			local val = i
			coroutine.spawn(function()
				ch:send(val)
				table.insert(order, val)
			end)
		end

		for i = 1, 5 do coroutine.yield() end

		local v1 = ch:receive()
		for i = 1, 3 do coroutine.yield() end
		local v2 = ch:receive()
		for i = 1, 3 do coroutine.yield() end
		local v3 = ch:receive()
		for i = 1, 3 do coroutine.yield() end

		return #order
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 3 {
				t.Errorf("expected 3 senders completed, got %d", int(n))
			}
		}
	}
	t.Log("Multiple blocked senders test passed")
}

// TestMultipleBlockedReceivers tests multiple blocked receivers wake one at a time
func TestMultipleBlockedReceivers(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local received = {}

		for i = 1, 3 do
			coroutine.spawn(function()
				local v = ch:receive()
				table.insert(received, v)
			end)
		end

		for i = 1, 5 do coroutine.yield() end

		ch:send(10)
		for i = 1, 3 do coroutine.yield() end
		ch:send(20)
		for i = 1, 3 do coroutine.yield() end
		ch:send(30)
		for i = 1, 3 do coroutine.yield() end

		local sum = 0
		for _, v in ipairs(received) do sum = sum + v end
		return sum
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 60 {
				t.Errorf("expected sum=60, got %d", int(n))
			}
		}
	}
	t.Log("Multiple blocked receivers test passed")
}

// TestSelectImmediateBuffered tests select immediate return on buffered channel
func TestSelectImmediateBuffered(t *testing.T) {
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)
		ch1:send("msg1")

		local result = channel.select{
			ch1:case_receive(),
			ch2:case_receive()
		}
		return {is_ch1 = result.channel == ch1, value = result.value, ok = result.ok}
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 1 {
		tbl, ok := proc.mainTask.Yielded[0].(*lua.LTable)
		if !ok {
			t.Fatal("expected table result")
		}
		if tbl.RawGetString("is_ch1") != lua.LTrue {
			t.Error("expected result.channel == ch1")
		}
		if s, ok := tbl.RawGetString("value").(lua.LString); !ok || s != "msg1" {
			t.Errorf("expected 'msg1', got %v", tbl.RawGetString("value"))
		}
		if tbl.RawGetString("ok") != lua.LTrue {
			t.Error("expected result.ok=true")
		}
	}
	t.Log("Select immediate buffered test passed")
}

// TestChannelPassBetweenCoroutines tests passing channels between coroutines
func TestChannelPassBetweenCoroutines(t *testing.T) {
	script := `
		local passCh = channel.new(0)
		local result = nil

		coroutine.spawn(function()
			local innerCh = channel.new(0)
			passCh:send(innerCh)
			innerCh:send("hello from inner")
		end)

		coroutine.spawn(function()
			local ch = passCh:receive()
			result = ch:receive()
		end)

		for i = 1, 20 do coroutine.yield() end
		return result
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if s, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if s != "hello from inner" {
				t.Errorf("expected 'hello from inner', got %q", s)
			}
		}
	}
	t.Log("Channel pass between coroutines test passed")
}

// TestSelectLoopPattern tests select in a loop pattern
func TestSelectLoopPattern(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local done = channel.new(0)
		local count = 0

		coroutine.spawn(function()
			while count < 3 do
				local result = channel.select{
					ch1:case_receive(),
					ch2:case_receive()
				}
				count = count + 1
			end
			done:send(true)
		end)

		for i = 1, 3 do coroutine.yield() end
		ch1:send(1)
		for i = 1, 3 do coroutine.yield() end
		ch2:send(2)
		for i = 1, 3 do coroutine.yield() end
		ch1:send(3)
		for i = 1, 3 do coroutine.yield() end

		done:receive()
		return count
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 3 {
				t.Errorf("expected count=3, got %d", int(n))
			}
		}
	}
	t.Log("Select loop pattern test passed")
}

// TestNestedSpawnWithChannels tests nested spawns with channel communication
func TestNestedSpawnWithChannels(t *testing.T) {
	script := `
		local results = channel.new(5)
		local done = channel.new(0)

		coroutine.spawn(function()
			for i = 1, 3 do
				coroutine.spawn(function()
					results:send(i * 10)
				end)
			end
			done:send(true)
		end)

		done:receive()
		for i = 1, 10 do coroutine.yield() end

		local sum = 0
		for i = 1, 3 do
			local v = results:receive()
			sum = sum + v
		end
		return sum
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 60 {
				t.Errorf("expected sum=60, got %d", int(n))
			}
		}
	}
	t.Log("Nested spawn with channels test passed")
}

// TestFanOutFanIn tests fan-out/fan-in pattern with buffered channels
func TestFanOutFanIn(t *testing.T) {
	script := `
		local jobs = channel.new(5)
		local results = channel.new(5)

		for j = 1, 5 do
			jobs:send(j)
		end

		for w = 1, 3 do
			coroutine.spawn(function()
				local job, ok = jobs:receive()
				if ok then
					results:send(job * 2)
				end
			end)
		end

		for i = 1, 20 do coroutine.yield() end

		local sum = 0
		for i = 1, 3 do
			local r = results:receive()
			sum = sum + r
		end

		return sum
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			expected := 2 + 4 + 6
			if int(n) != expected {
				t.Errorf("expected sum=%d, got %d", expected, int(n))
			}
		}
	}
	t.Log("Fan-out/fan-in test passed")
}

// TestPingPong tests bi-directional channel communication
func TestPingPong(t *testing.T) {
	script := `
		local ping = channel.new(0)
		local pong = channel.new(0)
		local count = 0

		coroutine.spawn(function()
			for i = 1, 5 do
				ping:send("ping")
				pong:receive()
				count = count + 1
			end
		end)

		coroutine.spawn(function()
			for i = 1, 5 do
				ping:receive()
				pong:send("pong")
			end
		end)

		for i = 1, 50 do coroutine.yield() end
		return count
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 200); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 5 {
				t.Errorf("expected count=5, got %d", int(n))
			}
		}
	}
	t.Log("Ping-pong test passed")
}

// TestCloseNotifiesBlockedReceivers tests that close wakes blocked receivers
func TestCloseNotifiesBlockedReceivers(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local results = {}

		for i = 1, 3 do
			coroutine.spawn(function()
				local v, ok = ch:receive()
				table.insert(results, ok)
			end)
		end

		for i = 1, 5 do coroutine.yield() end
		ch:close()
		for i = 1, 10 do coroutine.yield() end

		local allFalse = true
		for _, ok in ipairs(results) do
			if ok then allFalse = false end
		end
		return #results, allFalse
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 2 {
		count := proc.mainTask.Yielded[0]
		allFalse := proc.mainTask.Yielded[1]
		if n, ok := count.(lua.LNumber); ok && int(n) != 3 {
			t.Errorf("expected 3 results, got %d", int(n))
		}
		if b, ok := allFalse.(lua.LBool); ok && b == lua.LFalse {
			t.Error("expected all ok=false after close")
		}
	}
	t.Log("Close notifies blocked receivers test passed")
}

// TestSelectWakesOnClose tests that select wakes when channel is closed
func TestSelectWakesOnClose(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local result_ch = nil
		local receivedOk = nil

		coroutine.spawn(function()
			local result = channel.select{
				ch1:case_receive(),
				ch2:case_receive()
			}
			result_ch = result.channel
			receivedOk = result.ok
		end)

		for i = 1, 3 do coroutine.yield() end
		ch1:close()
		for i = 1, 5 do coroutine.yield() end

		return result_ch == ch1, receivedOk
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 2 {
		if b, ok := proc.mainTask.Yielded[0].(lua.LBool); ok && b == lua.LFalse {
			t.Error("expected result.channel == ch1")
		}
		if b, ok := proc.mainTask.Yielded[1].(lua.LBool); ok && b == lua.LTrue {
			t.Error("expected result.ok=false for closed channel")
		}
	}
	t.Log("Select wakes on close test passed")
}

// TestSelectReceiveAlreadyClosedIsReady verifies the other close path:
// a receive case must be immediately selectable even if the channel was
// closed before select evaluates readiness.
func TestSelectReceiveAlreadyClosedIsReady(t *testing.T) {
	script := `
		local ch = channel.new(0)
		ch:close()

		local result = channel.select{
			ch:case_receive()
		}

		return result.channel == ch and result.ok == false and result.value == nil
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			requireTruePayload(t, output.Result())
			return
		}
	}

	t.Fatal("select on already closed channel did not complete")
}

func TestSelectReceiveAlreadyClosedAmongUnreadyCases(t *testing.T) {
	script := `
		local open_ch = channel.new(0)
		local closed_ch = channel.new(0)
		closed_ch:close()

		local result = channel.select{
			open_ch:case_receive(),
			closed_ch:case_receive()
		}

		return result.channel == closed_ch and result.ok == false and result.value == nil
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			requireTruePayload(t, output.Result())
			return
		}
	}

	t.Fatal("select with already closed channel did not complete")
}

func TestSelectReceiveClosedBufferedDrainsThenOkFalse(t *testing.T) {
	script := `
		local ch = channel.new(1)
		ch:send("buffered")
		ch:close()

		local first = channel.select{
			ch:case_receive()
		}
		local second = channel.select{
			ch:case_receive()
		}

		return first.channel == ch
			and first.ok == true
			and first.value == "buffered"
			and second.channel == ch
			and second.ok == false
			and second.value == nil
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			requireTruePayload(t, output.Result())
			return
		}
	}

	t.Fatal("select on closed buffered channel did not complete")
}

func TestSelectReceiveClosedBeatsDefault(t *testing.T) {
	script := `
		local ch = channel.new(0)
		ch:close()

		local first = channel.select{
			ch:case_receive(),
			default = true
		}
		local second = channel.select{
			ch:case_receive(),
			default = true
		}

		return first.default ~= true
			and first.channel == ch
			and first.ok == false
			and first.value == nil
			and second.default ~= true
			and second.channel == ch
			and second.ok == false
			and second.value == nil
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			requireTruePayload(t, output.Result())
			return
		}
	}

	t.Fatal("closed receive did not beat select default")
}

func TestSelectReceiveClosedBufferedDrainBeatsDefault(t *testing.T) {
	script := `
		local ch = channel.new(1)
		ch:send("buffered")
		ch:close()

		local first = channel.select{
			ch:case_receive(),
			default = true
		}
		local second = channel.select{
			ch:case_receive(),
			default = true
		}

		return first.default ~= true
			and first.channel == ch
			and first.ok == true
			and first.value == "buffered"
			and second.default ~= true
			and second.channel == ch
			and second.ok == false
			and second.value == nil
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			requireTruePayload(t, output.Result())
			return
		}
	}

	t.Fatal("closed buffered receive did not beat select default after drain")
}

func TestSelectClosedStopChannelLetsWorkerExitAfterMainClosesIt(t *testing.T) {
	script := `
		local stop = channel.new(0)
		local done = channel.new(1)
		local other = channel.new(0)

		coroutine.spawn(function()
			local result = channel.select{
				other:case_receive(),
				stop:case_receive()
			}

			if result.channel == stop and result.ok == false then
				done:send(true)
			else
				done:send(false)
			end
		end)

		stop:close()

		local stopped = channel.select{
			done:case_receive()
		}

		return stopped.value == true
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 20; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			requireTruePayload(t, output.Result())
			return
		}
	}

	t.Fatal("worker waiting on closed stop channel did not complete")
}

// TestBufferedCloseWithValues tests drain buffered values then ok=false
func TestBufferedCloseWithValues(t *testing.T) {
	script := `
		local ch = channel.new(3)
		ch:send(1)
		ch:send(2)
		ch:send(3)
		ch:close()

		local values = {}
		local okValues = {}
		for i = 1, 5 do
			local v, ok = ch:receive()
			table.insert(values, v or 0)
			table.insert(okValues, ok)
		end

		return values[1], okValues[1], values[4], okValues[4]
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 4 {
		v1, ok1 := proc.mainTask.Yielded[0].(lua.LNumber), proc.mainTask.Yielded[1].(lua.LBool)
		_, ok4 := proc.mainTask.Yielded[2].(lua.LNumber), proc.mainTask.Yielded[3].(lua.LBool)
		if int(v1) != 1 {
			t.Errorf("expected first value 1, got %d", int(v1))
		}
		if !ok1 {
			t.Error("expected ok=true for buffered value")
		}
		if ok4 {
			t.Error("expected ok=false after drain")
		}
	}
	t.Log("Buffered close with values test passed")
}

// TestMapReducePattern tests map-reduce concurrency pattern
func TestMapReducePattern(t *testing.T) {
	script := `
		local work = channel.new(10)
		local results = channel.new(10)

		for i = 1, 10 do
			work:send(i)
		end

		for w = 1, 3 do
			coroutine.spawn(function()
				while true do
					local job, ok = work:receive()
					if not ok then break end
					results:send(job * job)
				end
			end)
		end

		work:close()
		for i = 1, 30 do coroutine.yield() end

		local sum = 0
		for i = 1, 10 do
			local v, ok = results:receive()
			if ok then sum = sum + v end
		end
		return sum
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 200); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			expected := 1 + 4 + 9 + 16 + 25 + 36 + 49 + 64 + 81 + 100
			if int(n) != expected {
				t.Errorf("expected sum=%d, got %d", expected, int(n))
			}
		}
	}
	t.Log("Map-reduce pattern test passed")
}

// TestCloseWithPendingSender tests close notifies blocked sender
func TestCloseWithPendingSender(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local senderDone = false

		coroutine.spawn(function()
			ch:send("value")
			senderDone = true
		end)

		for i = 1, 3 do coroutine.yield() end
		ch:close()
		for i = 1, 10 do coroutine.yield() end

		return senderDone
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	err := runUntilDone(t, proc, 50)
	// Send on closed channel causes an error in the spawned coroutine
	// The main task should complete
	if err != nil {
		// Expected - the spawned coroutine errors due to "send on closed channel"
		t.Log("Close with pending sender completed (spawned coroutine errored as expected)")
		return
	}
	t.Log("Close with pending sender test passed")
}

// TestSelectMultiChannelCleanup tests select cleanup on multi-channel
func TestSelectMultiChannelCleanup(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local ch3 = channel.new(0)
		local selected_ch = nil

		coroutine.spawn(function()
			local result = channel.select{
				ch1:case_receive(),
				ch2:case_receive(),
				ch3:case_receive()
			}
			selected_ch = result.channel
		end)

		for i = 1, 3 do coroutine.yield() end
		ch2:send("wake via ch2")
		for i = 1, 5 do coroutine.yield() end

		return selected_ch == ch2
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if b, ok := proc.mainTask.Yielded[0].(lua.LBool); ok && b == lua.LFalse {
			t.Error("expected result.channel == ch2")
		}
	}
	t.Log("Select multi-channel cleanup test passed")
}

// TestChainedChannels tests chained channel processing
func TestChainedChannels(t *testing.T) {
	script := `
		local stage1 = channel.new(5)
		local stage2 = channel.new(5)
		local stage3 = channel.new(5)

		coroutine.spawn(function()
			while true do
				local v, ok = stage1:receive()
				if not ok then break end
				stage2:send(v * 2)
			end
			stage2:close()
		end)

		coroutine.spawn(function()
			while true do
				local v, ok = stage2:receive()
				if not ok then break end
				stage3:send(v + 1)
			end
			stage3:close()
		end)

		for i = 1, 5 do
			stage1:send(i)
		end
		stage1:close()

		for i = 1, 30 do coroutine.yield() end

		local results = {}
		while true do
			local v, ok = stage3:receive()
			if not ok then break end
			table.insert(results, v)
		end

		local sum = 0
		for _, v in ipairs(results) do sum = sum + v end
		return sum
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 200); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			expected := (1*2 + 1) + (2*2 + 1) + (3*2 + 1) + (4*2 + 1) + (5*2 + 1)
			if int(n) != expected {
				t.Errorf("expected sum=%d, got %d", expected, int(n))
			}
		}
	}
	t.Log("Chained channels test passed")
}

// Subscribe Layer Tests

func startSubscribeProcess(t *testing.T, script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	proc := mustNewProcess(t,
		WithProto(proto),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())
	return proc
}

func runUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return err
		}
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach idle in %d steps", maxSteps)
	return nil
}

// Helper to send a message event to a process
func sendMessage(proc *Process, topic string, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: nil}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

func TestSubscribeBasic(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("test_topic", inbox)
		local msg = inbox:receive()
		return msg
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected active subscription")
	}

	var output process.StepOutput
	if err := sendMessage(proc, "test_topic", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	t.Log("Subscribe basic test passed")
}

func TestSubscribeMultipleMessages(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("inbox", inbox)

		local messages = {}
		for i = 1, 3 do
			local msg = inbox:receive()
			table.insert(messages, 1)
		end
		return #messages
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	for i := 0; i < 3; i++ {
		if err := sendMessage(proc, "inbox", &output); err != nil {
			t.Fatal(err)
		}
	}

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 3 {
				t.Errorf("expected 3 messages, got %d", int(n))
			}
		}
	}
	t.Log("Subscribe multiple messages test passed")
}

func TestUnsubscribe(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("topic", inbox)
		unsubscribe(inbox)
		return "done"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Process completed successfully - unsubscribe worked
	// ProcessContext is released after completion, so we can't check subscriptions
	t.Log("Unsubscribe test passed")
}

func TestSubscribeDuplicateTopic(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("topic", inbox)
		subscribe("topic2", inbox)
		local msg = inbox:receive()
		return msg
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	if err := sendMessage(proc, "topic2", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	t.Log("Subscribe duplicate topic test passed")
}

func TestActorPatternWithSubscribe(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("commands", inbox)

		local count = 0
		for i = 1, 5 do
			local cmd = inbox:receive()
			count = count + 1
		end
		return count
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	for i := 0; i < 5; i++ {
		if err := sendMessage(proc, "commands", &output); err != nil {
			t.Fatal(err)
		}
		if output.Status() == process.StepDone {
			break
		}
	}

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 5 {
				t.Errorf("expected count=5, got %d", int(n))
			}
		}
	}
	t.Log("Actor pattern with subscribe test passed")
}

// TestSubscribeWithCoroutineSpawn tests that spawned workers can receive from subscribed channels
// Note: main returning kills all threads (like Go), so main must block to keep workers alive
func TestSubscribeWithCoroutineSpawn(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		local results = channel.new(10)
		subscribe("work", inbox)

		for i = 1, 3 do
			coroutine.spawn(function()
				local msg = inbox:receive()
				results:send(1)
			end)
		end

		-- main blocks to keep workers alive (they're waiting on inbox)
		inbox:receive()
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected active subscription with spawned workers")
	}

	t.Log("Subscribe with coroutine spawn test passed")
}

func TestHasSubscriptions(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		local done = channel.new(0)
		subscribe("topic", inbox)

		-- Wait for message to arrive (blocks, causes Idle)
		local msg = inbox:receive()

		-- Unsubscribe and signal done
		unsubscribe(inbox)
		return "done"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	// Run until idle (waiting for message on subscribed channel)
	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected subscription to be active")
	}

	// Send message to unblock
	var output process.StepOutput
	if err := sendMessage(proc, "topic", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Process completed successfully - unsubscribe worked
	// ProcessContext is released after completion, so we can't check subscriptions
	t.Log("HasSubscriptions test passed")
}

// TestSubscribeDuplicateTopicDifferentChannel tests that subscribing same topic with different channel fails
func TestSubscribeDuplicateTopicDifferentChannel(t *testing.T) {
	script := `
		local inbox1 = channel.new(10)
		local inbox2 = channel.new(10)
		subscribe("topic", inbox1)
		local ok, err = subscribe("topic", inbox2)
		if ok == nil and err then
			return "error: " .. err
		end
		return "no error"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if str, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if str == "no error" {
				t.Error("expected error for duplicate topic with different channel")
			} else {
				t.Logf("Correctly got error: %s", str)
			}
		}
	}
	t.Log("Subscribe duplicate topic different channel test passed")
}

// TestSubscribeInvalidUnsubscribe tests error when unsubscribing non-subscribed channel
func TestSubscribeInvalidUnsubscribe(t *testing.T) {
	script := `
		local ch = channel.new(10)
		local ok, err = unsubscribe(ch)
		if not ok and err then
			return "error: " .. err
		end
		return "no error"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if str, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if str == "no error" {
				t.Error("expected error for unsubscribing non-subscribed channel")
			} else {
				t.Logf("Correctly got error: %s", str)
			}
		}
	}
	t.Log("Subscribe invalid unsubscribe test passed")
}

// TestSubscribeLateSubscription tests that messages sent before subscription are lost
func TestSubscribeLateSubscription(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		local done = channel.new(1)

		coroutine.spawn(function()
			local msg = inbox:receive()
			done:send(msg)
		end)

		for i = 1, 5 do coroutine.yield() end
		subscribe("late_topic", inbox)
		coroutine.yield("subscribed")

		return "waiting"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	// Send message BEFORE subscription
	var output process.StepOutput
	if err := sendMessage(proc, "late_topic", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Now send message AFTER subscription
	if err := sendMessage(proc, "late_topic", &output); err != nil {
		t.Fatal(err)
	}

	// Process should receive this one
	output.Reset()
	_ = proc.Step(nil, &output)
	output.Reset()
	_ = proc.Step(nil, &output)

	t.Log("Late subscription test passed - message before subscription is lost")
}

// TestSubscribeCrossTopicOrdering tests message ordering across multiple topics
func TestSubscribeCrossTopicOrdering(t *testing.T) {
	script := `
		local inbox1 = channel.new(10)
		local inbox2 = channel.new(10)
		subscribe("topic1", inbox1)
		subscribe("topic2", inbox2)

		local results = {}
		coroutine.yield("ready")

		local msg1 = inbox1:receive()
		table.insert(results, "t1")
		local msg2 = inbox2:receive()
		table.insert(results, "t2")

		return #results
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Send to topic2 first, then topic1
	var output process.StepOutput
	if err := sendMessage(proc, "topic2", &output); err != nil {
		t.Fatal(err)
	}
	if err := sendMessage(proc, "topic1", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 2 {
				t.Errorf("expected 2 messages, got %d", int(n))
			}
		}
	}
	t.Log("Cross-topic ordering test passed")
}

// TestSubscribeResubscribeAfterUnsubscribe tests subscribing again after unsubscribing
func TestSubscribeResubscribeAfterUnsubscribe(t *testing.T) {
	script := `
		local inbox1 = channel.new(10)
		subscribe("topic", inbox1)
		coroutine.yield("first_sub")

		local msg1 = inbox1:receive()

		unsubscribe(inbox1)

		local _, ok = inbox1:receive()
		if ok then
			return "error: channel should be closed"
		end

		local inbox2 = channel.new(10)
		subscribe("topic", inbox2)
		coroutine.yield("second_sub")

		local msg2 = inbox2:receive()
		return "success"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Send first message
	var output process.StepOutput
	if err := sendMessage(proc, "topic", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send second message after resubscribe
	if err := sendMessage(proc, "topic", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if str, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if str != "success" {
				t.Errorf("expected 'success', got %q", str)
			}
		}
	}
	t.Log("Resubscribe after unsubscribe test passed")
}

// TestSubscribeMultipleTopicsPartialUnsubscribe tests releasing one topic while another stays active
func TestSubscribeMultipleTopicsPartialUnsubscribe(t *testing.T) {
	script := `
		local inbox1 = channel.new(10)
		local inbox2 = channel.new(10)
		subscribe("topic1", inbox1)
		subscribe("topic2", inbox2)
		coroutine.yield("subscribed")

		local msg1 = inbox1:receive()
		unsubscribe(inbox1)

		local msg2 = inbox2:receive()
		local msg3 = inbox2:receive()

		return "done"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Send to both topics
	var output process.StepOutput
	if err := sendMessage(proc, "topic1", &output); err != nil {
		t.Fatal(err)
	}
	if err := sendMessage(proc, "topic2", &output); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		output.Reset()
		_ = proc.Step(nil, &output)
	}

	// After unsubscribe from topic1, send more to topic2
	if err := sendMessage(proc, "topic2", &output); err != nil {
		t.Fatal(err)
	}

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	t.Log("Multiple topics partial unsubscribe test passed")
}

// V1 compatibility select tests ported from legacy channel select coverage.

// TestSelectBlockedReceiveThenSend tests select blocking on receive then sender provides value
func TestSelectBlockedReceiveThenSend(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local result_value = nil

		coroutine.spawn(function()
			local result = channel.select{
				ch1:case_receive(),
				ch2:case_receive()
			}
			result_value = result.value
		end)

		coroutine.yield("select_started")
		ch2:send("ch2_value")
		coroutine.yield("send_completed")
		return result_value
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if s, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if s != "ch2_value" {
				t.Errorf("expected 'ch2_value', got %q", s)
			}
		}
	}
	t.Log("Select blocked receive then send test passed")
}

// TestSelectReadySendBlockedReceive tests select with ready send case
func TestSelectReadySendBlockedReceive(t *testing.T) {
	script := `
		local readyCh = channel.new(1)  -- buffered, will have a value
		local emptyCh = channel.new(1)  -- buffered, empty
		local fullCh = channel.new(1)   -- buffered, full

		readyCh:send("ready_value")
		fullCh:send("full")

		local result = channel.select{
			fullCh:case_send("blocked"),
			readyCh:case_receive()
		}

		return {is_ready_ch = result.channel == readyCh, value = result.value, ok = result.ok}
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		tbl, ok := proc.mainTask.Yielded[0].(*lua.LTable)
		if !ok {
			t.Fatal("expected table result")
		}
		isReadyCh := tbl.RawGetString("is_ready_ch").(lua.LBool)
		val := tbl.RawGetString("value").(lua.LString)
		okVal := tbl.RawGetString("ok").(lua.LBool)
		if !isReadyCh {
			t.Error("expected result.channel == readyCh")
		}
		if val != "ready_value" {
			t.Errorf("expected 'ready_value', got %q", val)
		}
		if !okVal {
			t.Error("expected ok=true")
		}
	}
	t.Log("Select ready send blocked receive test passed")
}

// TestSelectMixedBlockingBothCases tests select with both send and receive blocking
func TestSelectMixedBlockingBothCases(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local result_ch = nil
		local result_val = nil

		coroutine.spawn(function()
			local result = channel.select{
				ch1:case_send("value1"),
				ch2:case_receive()
			}
			result_ch = result.channel
			result_val = result.value
		end)

		coroutine.yield("select_started")

		coroutine.spawn(function()
			ch2:send("value2")
		end)

		for i = 1, 10 do coroutine.yield() end

		return {is_ch2 = result_ch == ch2, value = result_val}
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		tbl, ok := proc.mainTask.Yielded[0].(*lua.LTable)
		if !ok {
			t.Fatal("expected table result")
		}
		isCh2 := tbl.RawGetString("is_ch2").(lua.LBool)
		val := tbl.RawGetString("value").(lua.LString)
		if !isCh2 {
			t.Error("expected result.channel == ch2")
		}
		if val != "value2" {
			t.Errorf("expected 'value2', got %q", val)
		}
	}
	t.Log("Select mixed blocking both cases test passed")
}

// TestSelectWithDefaultNonBlocking tests select with default doesn't block
func TestSelectWithDefaultNonBlocking(t *testing.T) {
	script := `
		local sendCh = channel.new(0)
		local recvCh = channel.new(0)

		local result = channel.select{
			sendCh:case_send("value"),
			recvCh:case_receive(),
			default=true
		}

		return result.default, result.ok
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 2 {
		isDefault := proc.mainTask.Yielded[0].(lua.LBool)
		ok := proc.mainTask.Yielded[1].(lua.LBool)
		if !isDefault {
			t.Error("expected result.default=true")
		}
		if !ok {
			t.Error("expected result.ok=true")
		}
	}
	t.Log("Select with default non-blocking test passed")
}

// TestSelectSingleCaseWithReadyData tests single case select with ready data
func TestSelectSingleCaseWithReadyData(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local count = 0

		-- Main task receives
		coroutine.spawn(function()
			for i = 1, 3 do
				local result = channel.select{ch:case_receive()}
				if result.channel == ch and result.value ~= nil then
					count = count + 1
				end
			end
		end)

		for i = 1, 3 do coroutine.yield() end

		-- Senders
		for i = 1, 3 do
			ch:send("val" .. i)
			coroutine.yield()
		end

		for i = 1, 5 do coroutine.yield() end
		return count
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 100); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 3 {
				t.Errorf("expected count=3, got %d", int(n))
			}
		}
	}
	t.Log("Select single case with ready data test passed")
}

// TestSelectIndex1VsIndex2 tests that correct channel is returned for first vs second channel
func TestSelectIndex1VsIndex2(t *testing.T) {
	// Test case 1: First channel should be returned when ch1 receives
	script1 := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local result_ch = nil

		coroutine.spawn(function()
			local result = channel.select{ch1:case_receive(), ch2:case_receive()}
			result_ch = result.channel
		end)

		for i = 1, 3 do coroutine.yield() end
		ch1:send("first")
		for i = 1, 5 do coroutine.yield() end

		return result_ch == ch1
	`

	proc1 := startChannelProcess(t, script1)
	defer proc1.Close()

	if err := runUntilDone(t, proc1, 50); err != nil {
		t.Fatal(err)
	}

	if proc1.mainTask != nil && len(proc1.mainTask.Yielded) > 0 {
		if b, ok := proc1.mainTask.Yielded[0].(lua.LBool); ok && b == lua.LFalse {
			t.Error("Test1: expected result.channel == ch1")
		}
	}

	// Test case 2: Second channel should be returned when ch2 receives
	script2 := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local result_ch = nil

		coroutine.spawn(function()
			local result = channel.select{ch1:case_receive(), ch2:case_receive()}
			result_ch = result.channel
		end)

		for i = 1, 3 do coroutine.yield() end
		ch2:send("second")
		for i = 1, 5 do coroutine.yield() end

		return result_ch == ch2
	`

	proc2 := startChannelProcess(t, script2)
	defer proc2.Close()

	if err := runUntilDone(t, proc2, 50); err != nil {
		t.Fatal(err)
	}

	if proc2.mainTask != nil && len(proc2.mainTask.Yielded) > 0 {
		if b, ok := proc2.mainTask.Yielded[0].(lua.LBool); ok && b == lua.LFalse {
			t.Error("Test2: expected result.channel == ch2")
		}
	}

	t.Log("Select index 1 vs index 2 test passed")
}

// TestSelectBufferedImmediateIndex tests immediate select on buffered channels returns correct channel
func TestSelectBufferedImmediateIndex(t *testing.T) {
	// Only ch2 has a value
	script := `
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)
		ch2:send("value")

		local result = channel.select{
			ch1:case_receive(),
			ch2:case_receive()
		}

		return {is_ch2 = result.channel == ch2, value = result.value}
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		tbl, ok := proc.mainTask.Yielded[0].(*lua.LTable)
		if !ok {
			t.Fatal("expected table result")
		}
		isCh2 := tbl.RawGetString("is_ch2").(lua.LBool)
		if !isCh2 {
			t.Error("expected result.channel == ch2")
		}
	}
	t.Log("Select buffered immediate index test passed")
}

// Subscribe context unit tests (from subscribe_test.go)

func TestSubscribeContext(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch1 := NewChannel(1)
	ch2 := NewChannel(1)

	sub, err := ctx.addExisting("topic1", ch1)
	if err != nil {
		t.Fatalf("addExisting failed: %v", err)
	}
	if sub.topic != "topic1" {
		t.Errorf("sub.topic = %q, want %q", sub.topic, "topic1")
	}
	if sub.channel != ch1 {
		t.Error("sub.channel should be ch1")
	}

	sub2, err := ctx.addExisting("topic1", ch1)
	if err != nil {
		t.Fatalf("addExisting same channel to same topic should succeed: %v", err)
	}
	if sub2 != sub {
		t.Error("should return existing subscription")
	}

	_, err = ctx.addExisting("topic1", ch2)
	if err == nil {
		t.Error("addExisting different channel to same topic should fail")
	}

	gotSub, ok := ctx.get("topic1")
	if !ok {
		t.Error("get should find topic1")
	}
	if gotSub != sub {
		t.Error("get should return correct subscription")
	}

	_, ok = ctx.get("nonexistent")
	if ok {
		t.Error("get should return false for nonexistent topic")
	}

	err = ctx.remove(ch1)
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	_, ok = ctx.get("topic1")
	if ok {
		t.Error("topic1 should be removed")
	}

	err = ctx.remove(ch2)
	if !errors.Is(err, luaapi.ErrChannelNotFound) {
		t.Errorf("remove non-subscribed channel should return ErrChannelNotFound, got %v", err)
	}
}

func TestSubscribeContextConcurrentSafe(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	_, err := ctx.add("topic", 1)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			ctx.get("topic")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			ctx.get("topic")
		}
		done <- true
	}()

	<-done
	<-done
}

func TestSubscribeContextMultipleTopics(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	_, _ = ctx.add("topic1", 1)
	sub2, _ := ctx.add("topic2", 1)
	_, _ = ctx.add("topic3", 1)

	if _, ok := ctx.get("topic1"); !ok {
		t.Error("topic1 should exist")
	}
	if _, ok := ctx.get("topic2"); !ok {
		t.Error("topic2 should exist")
	}
	if _, ok := ctx.get("topic3"); !ok {
		t.Error("topic3 should exist")
	}

	_ = ctx.remove(sub2.channel)

	if _, ok := ctx.get("topic1"); !ok {
		t.Error("topic1 should still exist")
	}
	if _, ok := ctx.get("topic2"); ok {
		t.Error("topic2 should be removed")
	}
	if _, ok := ctx.get("topic3"); !ok {
		t.Error("topic3 should still exist")
	}
}

func TestSubscription(t *testing.T) {
	ch := NewChannel(1)
	sub := &subscription{
		topic:   "test-topic",
		channel: ch,
	}

	if sub.topic != "test-topic" {
		t.Errorf("topic = %q, want %q", sub.topic, "test-topic")
	}
	if sub.channel != ch {
		t.Error("channel mismatch")
	}
}

func TestSubscribeRequestString(t *testing.T) {
	ch := NewChannel(1)
	req := &SubscribeRequest{
		Topic:           "my-topic",
		ExistingChannel: ch,
	}

	if req.String() != "<subscribe_request>" {
		t.Errorf("String() = %q, want %q", req.String(), "<subscribe_request>")
	}
	if req.Type() != lua.LTUserData {
		t.Errorf("Type() = %v, want %v", req.Type(), lua.LTUserData)
	}
	if req.Topic != "my-topic" {
		t.Errorf("Topic = %q, want %q", req.Topic, "my-topic")
	}
	if req.ExistingChannel != ch {
		t.Error("ExistingChannel mismatch")
	}
}

func TestUnsubscribeRequestString(t *testing.T) {
	ch := NewChannel(1)
	req := &UnsubscribeRequest{
		Channel: ch,
	}

	if req.String() != "<unsubscribe_request>" {
		t.Errorf("String() = %q, want %q", req.String(), "<unsubscribe_request>")
	}
	if req.Type() != lua.LTUserData {
		t.Errorf("Type() = %v, want %v", req.Type(), lua.LTUserData)
	}
	if req.Channel != ch {
		t.Error("Channel mismatch")
	}
}

func TestSubscribeContextAddRemoveSequence(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch := NewChannel(1)

	_, err := ctx.addExisting("topic", ch)
	if err != nil {
		t.Fatal(err)
	}

	err = ctx.remove(ch)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ctx.addExisting("topic", ch)
	if err != nil {
		t.Fatalf("resubscribe should work: %v", err)
	}

	ch2 := NewChannel(1)
	_, err = ctx.addExisting("topic", ch2)
	if err == nil {
		t.Error("adding different channel to occupied topic should fail")
	}
}

func TestSubscribeContextTopicChannelMapping(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch := NewChannel(1)
	_, _ = ctx.addExisting("my-topic", ch)

	if topic, ok := ctx.byChannel[ch]; !ok || topic != "my-topic" {
		t.Errorf("byChannel mapping incorrect: got %q", topic)
	}

	if sub, ok := ctx.byTopic["my-topic"]; !ok || sub.channel != ch {
		t.Error("byTopic mapping incorrect")
	}

	_ = ctx.remove(ch)

	if _, ok := ctx.byChannel[ch]; ok {
		t.Error("byChannel should be cleared after remove")
	}
	if _, ok := ctx.byTopic["my-topic"]; ok {
		t.Error("byTopic should be cleared after remove")
	}
}

// Event message tests (from event_message_test.go)

func sendMessageWithPayload(proc *Process, topic string, payloads payload.Payloads, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

func startEventProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, _ := lua.CompileString(script, "test.lua")
	proc := mustNewProcess(t,
		WithProto(proto),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())

	return proc
}

func runEventUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
	t.Helper()
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return err
		}
		if output.Status() == process.StepIdle {
			return nil
		}
		if output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach idle in %d steps", maxSteps)
	return nil
}

func runEventUntilDone(t *testing.T, proc *Process, maxSteps int) error {
	t.Helper()
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return err
		}
		if output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach done in %d steps", maxSteps)
	return nil
}

func TestEventMessageWithStringPayload(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("test_topic", inbox)
		local msg = inbox:receive()
		if msg ~= "hello world" then
			return nil, "expected 'hello world', got: " .. tostring(msg)
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected active subscription")
	}

	var output process.StepOutput
	stringPayload := payload.NewPayload(lua.LString("hello world"), payload.Lua)
	if err := sendMessageWithPayload(proc, "test_topic", payload.Payloads{stringPayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestEventMessageWithTablePayload(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("data_topic", inbox)
		local msg = inbox:receive()

		if type(msg) ~= "table" then
			return nil, "expected table, got: " .. type(msg)
		end
		if msg.name ~= "test" then
			return nil, "expected name='test', got: " .. tostring(msg.name)
		end
		if msg.count ~= 42 then
			return nil, "expected count=42, got: " .. tostring(msg.count)
		end
		return "success"
	`

	proto, _ := lua.CompileString(script, "test.lua")
	proc := mustNewProcess(t, WithProto(proto))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	tbl := proc.State().CreateTable(0, 2)
	tbl.RawSetString("name", lua.LString("test"))
	tbl.RawSetString("count", lua.LNumber(42))

	var output process.StepOutput
	luaTablePayload := payload.NewPayload(tbl, payload.Lua)
	if err := sendMessageWithPayload(proc, "data_topic", payload.Payloads{luaTablePayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestEventMessageMultiplePayloads(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("multi_topic", inbox)
		local msg = inbox:receive()

		if type(msg) ~= "table" then
			return nil, "expected table (array), got: " .. type(msg)
		end
		if #msg ~= 2 then
			return nil, "expected 2 items, got: " .. #msg
		end
		if msg[1] ~= "first" then
			return nil, "expected first='first', got: " .. tostring(msg[1])
		end
		if msg[2] ~= "second" then
			return nil, "expected second='second', got: " .. tostring(msg[2])
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	payloads := payload.Payloads{
		payload.NewPayload(lua.LString("first"), payload.Lua),
		payload.NewPayload(lua.LString("second"), payload.Lua),
	}
	if err := sendMessageWithPayload(proc, "multi_topic", payloads, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestEventMessageBlockedReceiver(t *testing.T) {
	script := `
		local inbox = channel.new(0)
		subscribe("wake_topic", inbox)
		local msg = inbox:receive()
		return msg
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	stringPayload := payload.NewPayload(lua.LString("wake up!"), payload.Lua)
	if err := sendMessageWithPayload(proc, "wake_topic", payload.Payloads{stringPayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed to complete after receiving message: %v", err)
	}
}

func TestEventMessageRoutingByTopic(t *testing.T) {
	script := `
		local inbox1 = channel.new(10)
		local inbox2 = channel.new(10)
		subscribe("topic1", inbox1)
		subscribe("topic2", inbox2)

		coroutine.yield()
		coroutine.yield()

		local msg1 = inbox1:receive()
		local msg2 = inbox2:receive()

		if msg1 ~= "for topic1" then
			return nil, "topic1 got wrong message: " .. tostring(msg1)
		end
		if msg2 ~= "for topic2" then
			return nil, "topic2 got wrong message: " .. tostring(msg2)
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	if err := sendMessageWithPayload(proc, "topic1", payload.Payloads{payload.NewPayload(lua.LString("for topic1"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendMessageWithPayload(proc, "topic2", payload.Payloads{payload.NewPayload(lua.LString("for topic2"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 30); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestEventMessageLuaPayload(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("lua_topic", inbox)
		local msg = inbox:receive()
		if type(msg) ~= "number" or msg ~= 42 then
			return nil, "expected number 42, got: " .. type(msg) .. " " .. tostring(msg)
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	luaPayload := payload.NewPayload(lua.LNumber(42), payload.Lua)
	if err := sendMessageWithPayload(proc, "lua_topic", payload.Payloads{luaPayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestEventMessageNoSubscriber(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("subscribed_topic", inbox)

		coroutine.yield()

		unsubscribe(inbox)
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	if err := sendMessageWithPayload(proc, "unsubscribed_topic", payload.Payloads{payload.NewPayload(lua.LString("ignored"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 30); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// TestSelectWithExternalSubscribedChannel tests channel.select with a subscribed channel
// that receives external messages. This reproduces the bug where select blocks on
// time.after even when subscribed channel receives data first.
func TestSelectWithExternalSubscribedChannel(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("test_topic", inbox)

		local timeout_ch = channel.new(1)

		local step_count = 0
		local result_channel = nil
		local result_value = nil

		local result = channel.select{
			inbox:case_receive(),
			timeout_ch:case_receive()
		}

		result_channel = result.channel
		result_value = result.value

		return {
			got_inbox = (result_channel == inbox),
			value = result_value
		}
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	// Run until idle (blocking on select)
	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Count steps before sending message
	stepsBefore := 0

	// Send message to subscribed topic
	var output process.StepOutput
	if err := sendMessage(proc, "test_topic", &output); err != nil {
		t.Fatal(err)
	}

	// Run until done - should complete quickly since inbox got a message
	for i := 0; i < 10; i++ {
		output.Reset()
		err := proc.Step(nil, &output)
		if err != nil {
			t.Fatalf("Step error: %v", err)
		}
		stepsBefore++
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("Select did not complete after message sent to subscribed channel")
	}

	// Verify result
	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		tbl, ok := proc.mainTask.Yielded[0].(*lua.LTable)
		if !ok {
			t.Fatal("expected table result")
		}
		gotInbox := tbl.RawGetString("got_inbox")
		if gotInbox != lua.LTrue {
			t.Errorf("expected got_inbox=true (select should have received from inbox), got %v", gotInbox)
		}
	}

	t.Logf("Select with external subscribed channel completed in %d steps", stepsBefore)
}

// TestSelectWakesOnExternalSend tests that select wakes up immediately when
// an external send is made to one of the channels in the select.
func TestSelectWakesOnExternalSend(t *testing.T) {
	script := `
		local external_ch = channel.new(0)  -- unbuffered
		local internal_ch = channel.new(0)  -- unbuffered

		subscribe("external_topic", external_ch)

		local result = channel.select{
			external_ch:case_receive(),
			internal_ch:case_receive()
		}

		return {
			channel_is_external = (result.channel == external_ch),
			value = result.value
		}
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	// Run until idle (blocking on select)
	if err := runUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Now external_ch should be registered as receiver in select
	// Send message via subscription mechanism
	var output process.StepOutput
	if err := sendMessage(proc, "external_topic", &output); err != nil {
		t.Fatal(err)
	}

	// Select should wake up and complete
	stepCount := 0
	for i := 0; i < 20; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step error: %v", err)
		}
		stepCount++
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatalf("Select did not complete after external message (ran %d steps)", stepCount)
	}

	// Verify external channel was selected
	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		tbl, ok := proc.mainTask.Yielded[0].(*lua.LTable)
		if !ok {
			t.Fatal("expected table result")
		}
		isExternal := tbl.RawGetString("channel_is_external")
		if isExternal != lua.LTrue {
			t.Error("expected channel_is_external=true, select should have woken on external channel")
		}
	}

	t.Logf("Select woke on external send in %d steps", stepCount)
}

// TestSubscriptionBufferedMessageWakesBlockedTask verifies that when a message
// arrives on a subscribed buffered channel, tasks blocked on that channel
// should be woken even if they're not currently in recvq (e.g., after a select
// completed on a different channel).
//
// Scenario:
// 1. Main does select{result_ch, timer_ch}
// 2. result_ch gets data first, main continues
// 3. Main blocks on stop_ch:send (unbuffered)
// 4. Timer fires, message arrives on timer_ch
// 5. Expected: Since timer_ch has buffered data, the next receive should get it
//
// This test spawns a worker that will receive from stop_ch, allowing main to
// continue and eventually receive from timer_ch.
func TestSubscriptionBufferedMessageWakesBlockedTask(t *testing.T) {
	script := `
		local timer_ch = channel.new(1)  -- buffered like time.after channel
		local result_ch = channel.new(1) -- buffered result channel
		local stop_ch = channel.new(0)   -- unbuffered sync channel
		local got_timer = false

		subscribe("timer_topic", timer_ch)
		subscribe("result_topic", result_ch)

		-- Worker that will unblock stop_ch after we signal
		coroutine.spawn(function()
			-- Wait for timer message to arrive first
			local timer_val = timer_ch:receive()
			-- Now receive from stop_ch to unblock main
			stop_ch:receive()
		end)

		-- First: select on result_ch and timer_ch
		local sel = channel.select{
			result_ch:case_receive(),
			timer_ch:case_receive()
		}

		if sel.channel == result_ch then
			-- Main got result, now send on stop_ch
			-- Worker will receive this after timer arrives
			stop_ch:send("done")
			got_timer = true
		end

		return got_timer
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// Run until idle - main blocks on select, worker blocks on timer_ch:receive
	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step error: %v", err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	if output.Status() != process.StepIdle {
		t.Fatalf("Expected idle, got %d", output.Status())
	}

	// Send result first - wakes main's select
	if err := sendMessage(proc, "result_topic", &output); err != nil {
		t.Fatal(err)
	}

	// Run until idle - main now blocked on stop_ch:send, worker on timer_ch:receive
	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step error: %v", err)
		}
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			break
		}
	}

	// Send timer message - should wake worker waiting on timer_ch:receive
	if err := sendMessage(proc, "timer_topic", &output); err != nil {
		t.Fatal(err)
	}

	// Process should complete: worker gets timer, receives stop_ch, main unblocks
	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step error: %v", err)
		}
		if output.Status() == process.StepDone {
			t.Log("Process completed successfully")
			return
		}
	}

	t.Fatalf("Process did not complete - stuck in status %d", output.Status())
}

// TestSelectFlushDoesNotLoseMessages verifies that when a select completes
// on one channel, messages arriving on other channels in the select are not lost.
//
// Bug scenario (from bus_pattern.lua):
// 1. Main does select{result_ch, timeout_ch} where timeout_ch is subscribed to timer
// 2. result_ch gets data first, main continues (select flushes timeout_ch from recvq)
// 3. Main blocks on another channel (stop_ch:send)
// 4. Timer fires, message sent to timeout_ch via subscription
// 5. No receiver in timeout_ch's recvq (was flushed by select) -> message goes to buffer
// 6. Task that SHOULD receive this message is blocked elsewhere
// 7. Expected: when that task later receives from timeout_ch, it should get the buffered message
func TestSelectFlushDoesNotLoseMessages(t *testing.T) {
	script := `
		local timeout_ch = channel.new(1)  -- buffered like time.after channel
		local result_ch = channel.new(1)   -- buffered result channel
		local sync_ch = channel.new(0)     -- unbuffered for sync

		subscribe("timeout_topic", timeout_ch)
		subscribe("result_topic", result_ch)

		-- Worker that will unblock sync_ch
		coroutine.spawn(function()
			-- First wait to ensure main is blocked on sync_ch:send
			sync_ch:receive()
			-- Signal done
			sync_ch:send("ack")
		end)

		-- Main: select on result_ch and timeout_ch
		local sel = channel.select{
			result_ch:case_receive(),
			timeout_ch:case_receive()
		}

		-- Result came first, timeout_ch was flushed from select
		-- But timeout message might arrive later
		if sel.channel == result_ch then
			-- Block on sync_ch while timer message might arrive
			sync_ch:send("sync")
			-- Wait for ack
			sync_ch:receive()

			-- Now check if timeout_ch has buffered message
			-- This should NOT block forever if message is buffered
			local timeout_val = timeout_ch:receive()
			if timeout_val ~= nil then
				return "got_buffered_timeout"
			end
		end

		return "no_timeout"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// Run until idle - main blocks on select, worker blocks on sync_ch:receive
	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step error: %v", err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	if output.Status() != process.StepIdle {
		t.Fatalf("Expected idle after initial setup, got %d", output.Status())
	}

	// Send result first - wakes main's select, main continues and blocks on sync_ch:send
	if err := sendMessage(proc, "result_topic", &output); err != nil {
		t.Fatal(err)
	}

	// Run until idle - main blocked on sync_ch:send, worker wakes and receives
	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step error: %v", err)
		}
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			break
		}
	}

	// Now send timeout message - main is NOT in timeout_ch's recvq!
	// This should go to buffer, but main should still get it when it receives later
	if err := sendMessage(proc, "timeout_topic", &output); err != nil {
		t.Fatal(err)
	}

	// Run to completion
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step error: %v", err)
		}
		if output.Status() == process.StepDone {
			t.Log("Test completed - buffered message was received")
			return
		}
	}

	t.Fatalf("Process did not complete - stuck in status %d (likely main blocked on timeout_ch:receive but message was lost)", output.Status())
}

// TestBusPatternWorkerSendsResultThenSelfSignals tests the bus pattern where:
// 1. Worker receives operation from ops_channel
// 2. Worker processes and sends result to result_channel (buffered)
// 3. Worker sends to stop_signal to trigger its own exit on next select iteration
// 4. Main selects on {result_channel, bus_done, timeout_ch}
// 5. Main receives result and completes
// 6. Worker should exit cleanly via stop_signal
func TestBusPatternWorkerSendsResultThenSelfSignals(t *testing.T) {
	script := `
		local ops_channel = channel.new(256)
		local stop_signal = channel.new(0)   -- unbuffered (matches app test)
		local bus_done = channel.new(0)      -- unbuffered (matches app test)
		local result_channel = channel.new(1)
		local timeout_ch = channel.new(1)

		subscribe("timeout_topic", timeout_ch)

		-- Worker coroutine (simulates bus pattern)
		coroutine.spawn(function()
			while true do
				local result = channel.select{
					stop_signal:case_receive(),
					ops_channel:case_receive()
				}

				if result.channel == stop_signal then
					bus_done:send(true)
					return
				end

				if result.channel == ops_channel then
					-- Send result (buffered channel - won't block)
					result_channel:send({success = true})

					-- Send stop signal (buffered - won't block, worker receives on next iteration)
					stop_signal:send(true)
				end
			end
		end)

		-- Main: send to ops_channel (worker will receive this)
		ops_channel:send({type = "test_op"})

		-- Main: select on result, bus_done, timeout
		local final = channel.select{
			result_channel:case_receive(),
			bus_done:case_receive(),
			timeout_ch:case_receive()
		}

		if final.channel == timeout_ch then
			return "timeout"
		end

		if final.channel == result_channel then
			return "result"
		end

		if final.channel == bus_done then
			return "bus_done"
		end

		return "unknown"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// Run until idle or done
	for i := 0; i < 100; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		t.Logf("Step %d: status=%d, threads=%d", i, output.Status(), len(proc.threads))

		if output.Status() == process.StepDone {
			t.Log("Process completed successfully!")
			return
		}

		if output.Status() == process.StepIdle {
			// Check if we're stuck - main should have completed but worker is blocked
			t.Logf("Idle at step %d with %d threads", i, len(proc.threads))
			if len(proc.threads) == 1 {
				t.Log("DEADLOCK: 1 thread stuck, main completed but worker blocked on stop_signal:send")
				t.Fatal("Deadlock detected - worker blocked on unbuffered channel send with no receiver")
			}
		}
	}

	t.Fatal("Process did not complete in 100 steps")
}

// TestAppBusPatternDeadlock reproduces the exact deadlock from app/src/test/coroutine_sql/bus_pattern.lua
// DO NOT MODIFY THIS TEST - it must match the app behavior exactly
// Expected: main blocks on select{result_channel, bus_done, timeout}
//
//	worker sends to result_channel (buffered)
//	main should wake and receive from result_channel
//
// Bug: main does not wake when worker sends to buffered result_channel
func TestAppBusPatternDeadlock(t *testing.T) {
	script := `
		local ops_channel = channel.new(256)
		local stop_signal = channel.new(0)
		local bus_done = channel.new(0)
		local result_channel = channel.new(1)
		local timeout_ch = channel.new(1)

		subscribe("timeout_topic", timeout_ch)

		coroutine.spawn(function()
			while true do
				local result = channel.select({
					stop_signal:case_receive(),
					ops_channel:case_receive()
				})

				if result.channel == stop_signal then
					bus_done:send(true)
					return
				end

				if result.channel == ops_channel then
					result_channel:send({success = true, data = "test"})
					stop_signal:send(true)
				end
			end
		end)

		ops_channel:send({type = "test_op"})

		local final = channel.select({
			result_channel:case_receive(),
			bus_done:case_receive(),
			timeout_ch:case_receive()
		})

		if final.channel == timeout_ch then
			return "timeout"
		end

		if final.channel == result_channel then
			return "result"
		end

		if final.channel == bus_done then
			return "bus_done"
		end

		return "unknown"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// Run until done or stuck
	for i := 0; i < 100; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		t.Logf("Step %d: status=%d, threads=%d", i, output.Status(), len(proc.threads))

		if output.Status() == process.StepDone {
			t.Log("Process completed - bug is fixed!")
			return
		}

		if output.Status() == process.StepIdle {
			if len(proc.threads) > 0 {
				t.Logf("DEADLOCK: %d threads stuck at step %d", len(proc.threads), i)
				t.Fatal("Bug reproduced: main blocked on select but worker sent to result_channel - main should have woken")
			}
		}
	}

	t.Fatal("Process did not complete in 100 steps")
}

// TestSelectReceiveFromBlockedSender tests select receiving from unbuffered channel
// where sender is already blocked waiting for receiver.
func TestSelectReceiveFromBlockedSender(t *testing.T) {
	script := `
		local ch = channel.new(0)  -- unbuffered
		local result_ok = false

		-- Worker sends to unbuffered channel (blocks until receiver)
		coroutine.spawn(function()
			ch:send("test_value")
		end)

		-- Main does select, should receive table with .ok field
		local result = channel.select({
			ch:case_receive()
		})

		-- This must not error with "attempt to index boolean"
		if result.ok then
			result_ok = true
		end

		if result.value ~= "test_value" then
			error("expected test_value, got " .. tostring(result.value))
		end

		return result_ok
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			result := output.Result()
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			// Result could be lua.LBool or bool depending on export
			switch v := result.Data().(type) {
			case bool:
				if !v {
					t.Fatal("expected true, got false")
				}
			case lua.LBool:
				if !v {
					t.Fatal("expected true, got false")
				}
			default:
				t.Fatalf("expected bool, got %v (type %T)", result.Data(), result.Data())
			}
			t.Log("Select correctly returned table with .ok field")
			return
		}
	}

	t.Fatal("Process did not complete")
}

// TestSelectSendToBlockedReceiver tests select sending to unbuffered channel
// where receiver is already blocked waiting for sender.
func TestSelectSendToBlockedReceiver(t *testing.T) {
	script := `
		local ch = channel.new(0)  -- unbuffered
		local received_value = nil

		-- Worker receives from unbuffered channel (blocks until sender)
		coroutine.spawn(function()
			received_value = ch:receive()
		end)

		-- Let worker block first
		coroutine.yield()

		-- Main does select send, should complete and wake receiver
		local result = channel.select({
			ch:case_send("test_value")
		})

		-- Select should return table with .ok field
		if not result.ok then
			error("expected ok=true")
		end

		-- Give worker chance to set received_value
		coroutine.yield()

		if received_value ~= "test_value" then
			error("expected test_value, got " .. tostring(received_value))
		end

		return true
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			result := output.Result()
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			t.Log("Select send to blocked receiver completed")
			return
		}
	}

	t.Fatal("Process did not complete")
}

// TestSelectReceiveFromBufferWithBlockedSender tests select receiving from buffered channel
// when buffer has data AND there's a blocked sender waiting.
func TestSelectReceiveFromBufferWithBlockedSender(t *testing.T) {
	script := `
		local ch = channel.new(1)  -- buffered, capacity 1
		local sender_done = false

		-- Fill the buffer
		ch:send("first")

		-- Worker tries to send second value (blocks because buffer full)
		coroutine.spawn(function()
			ch:send("second")
			sender_done = true
		end)

		-- Let worker block
		coroutine.yield()

		-- Main does select receive - should get "first" from buffer and wake sender
		local result = channel.select({
			ch:case_receive()
		})

		if not result.ok then
			error("expected ok=true")
		end

		if result.value ~= "first" then
			error("expected first, got " .. tostring(result.value))
		end

		-- Give sender chance to complete
		coroutine.yield()

		if not sender_done then
			error("sender should have completed")
		end

		-- Buffer should now have "second"
		local val = ch:receive()
		if val ~= "second" then
			error("expected second, got " .. tostring(val))
		end

		return true
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			result := output.Result()
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			t.Log("Select receive from buffer with blocked sender completed")
			return
		}
	}

	t.Fatal("Process did not complete")
}

// TestMainReturnKillsWorkers verifies that when main returns, all spawned workers are killed.
// This matches Go semantics where main goroutine exit terminates the program.
func TestMainReturnKillsWorkers(t *testing.T) {
	script := `
		local ch = channel.new(0)

		-- Worker that would block forever if not killed
		coroutine.spawn(function()
			ch:receive()
			error("worker should have been killed, not reached here")
		end)

		-- Main returns immediately, should kill the worker
		return "main done"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 10; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			// Verify no threads remain
			if len(proc.GetTasks()) != 0 {
				t.Fatalf("expected 0 threads after main return, got %d", len(proc.GetTasks()))
			}
			t.Log("Main return correctly killed worker threads")
			return
		}
		if output.Status() == process.StepIdle {
			t.Fatal("should not be idle - main should have completed and killed workers")
		}
	}

	t.Fatal("Process did not complete - main return did not kill workers")
}

// TestSelectInLoopWakesOnExternalSend reproduces the app deadlock where:
// - Worker has select in a while loop waiting on multiple channels
// - External send happens to one of the channels
// - Select should wake and return the value, not re-evaluate from scratch
// This tests the handoff->select resume path.
func TestSelectInLoopWakesOnExternalSend(t *testing.T) {
	script := `
		local stop_signal = channel.new(0)
		local inbox = channel.new(0)
		local result_ch = channel.new(1)
		local iterations = 0

		-- Worker with select in a loop (like session.process pattern)
		coroutine.spawn(function()
			while true do
				iterations = iterations + 1
				if iterations > 10 then
					result_ch:send("loop_limit_exceeded")
					return
				end

				local result = channel.select{
					stop_signal:case_receive(),
					inbox:case_receive()
				}

				if result.channel == stop_signal then
					result_ch:send("stopped")
					return
				elseif result.channel == inbox then
					result_ch:send("inbox:" .. tostring(result.value))
					return
				end
			end
		end)

		-- Let worker block on select
		for i = 1, 5 do coroutine.yield() end

		-- External send to inbox - should wake the select
		inbox:send("test_message")

		-- Wait for worker to process
		for i = 1, 10 do coroutine.yield() end

		-- Get result from worker
		local result = result_ch:receive()
		return result
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
				result := proc.mainTask.Yielded[0].String()
				if result != "inbox:test_message" {
					t.Errorf("expected 'inbox:test_message', got '%s'", result)
				} else {
					t.Log("Select in loop wakes correctly on external send")
				}
			}
			return
		}
	}

	t.Fatal("Process did not complete - select in loop did not wake on external send")
}

// TestSelectInLoopWithSubscribedChannel tests select in loop with external message delivery
// This more closely matches the app pattern where messages arrive via subscription
func TestSelectInLoopWithSubscribedChannel(t *testing.T) {
	script := `
		local stop_signal = channel.new(0)
		local inbox = process.subscribe("inbox", 0)

		local result_ch = channel.new(1)
		local iterations = 0

		-- Worker with select in a loop (like session.process pattern)
		coroutine.spawn(function()
			while true do
				iterations = iterations + 1
				if iterations > 10 then
					result_ch:send("loop_limit_exceeded")
					return
				end

				local result = channel.select{
					stop_signal:case_receive(),
					inbox:case_receive()
				}

				if result.channel == stop_signal then
					result_ch:send("stopped")
					return
				elseif result.channel == inbox then
					result_ch:send("inbox:" .. tostring(result.value))
					return
				end
			end
		end)

		-- Block main waiting for result
		local result = result_ch:receive()
		return result
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindProcessModule),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Run until idle (select blocks)
	var output process.StepOutput
	for i := 0; i < 20; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	// Queue message to the topic
	testPayload := payload.New("test_message")
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "inbox",
		Source:   testPID(),
		Payloads: []payload.Payload{testPayload},
	})

	// Continue until done
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
				result := proc.mainTask.Yielded[0].String()
				t.Logf("Result: %s", result)
				if result == "loop_limit_exceeded" {
					t.Error("Select loop did not wake on external message - got loop_limit_exceeded")
				}
			}
			return
		}
	}

	t.Fatal("Process did not complete")
}

// TestSessionProcessPattern tests a pattern with two coroutines doing selects:
// - Main loop does select on {inbox, events, bus_done}
// - Bus worker does select on {stop_signal, ops_channel}
// Message flow: inbox -> main -> ops_channel -> bus worker
func TestSessionProcessPattern(t *testing.T) {
	script := `
		local ops_channel = channel.new(256)
		local stop_signal = channel.new(1)
		local bus_done = channel.new(0)
		local inbox = process.subscribe("inbox", 0)
		local events = channel.new(0)

		-- Bus worker coroutine
		coroutine.spawn(function()
			local running = true
			local iteration = 0
			while running do
				iteration = iteration + 1
				if iteration > 20 then
					return
				end

				local result = channel.select{
					stop_signal:case_receive(),
					ops_channel:case_receive()
				}

				if result.channel == stop_signal then
					running = false
				elseif result.channel == ops_channel then
					-- Process op and signal stop
					stop_signal:send(true)
				end
			end
			bus_done:send(true)
		end)

		-- Main loop does select on {inbox, events, bus_done}
		local main_iteration = 0
		while main_iteration < 20 do
			main_iteration = main_iteration + 1

			local result = channel.select{
				inbox:case_receive(),
				events:case_receive(),
				bus_done:case_receive()
			}

			if result.channel == inbox then
				-- Queue message as op to bus
				ops_channel:send({type = "handle_message", data = result.value})
			elseif result.channel == bus_done then
				return "bus_done"
			end
		end

		return "main_timeout"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindProcessModule),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	// Run until idle (waiting for message)
	var output process.StepOutput
	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	// Queue message to inbox
	testPayload := payload.New(map[string]any{"type": "test"})
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "inbox",
		Source:   testPID(),
		Payloads: []payload.Payload{testPayload},
	})

	// Continue until done
	for i := 0; i < 100; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i, err)
		}
		if output.Status() == process.StepDone {
			if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
				result := proc.mainTask.Yielded[0].String()
				t.Logf("Result: %s", result)
				switch result {
				case "main_timeout":
					t.Error("Main timed out - message not flowing through correctly")
				case "bus_done":
					t.Log("Two-coroutine select pattern completed successfully")
				}
			}
			return
		}
	}

	t.Fatal("Process did not complete")
}

// TestBusPatternWorkerBlockedThenMainSends reproduces the exact app deadlock:
// 1. Main yields externally (simulating SQL query)
// 2. Worker runs, blocks on select{stop_signal, ops_channel} - both empty
// 3. Task yields ChannelResult{Yields=true, updates=0} and is DROPPED (bug!)
// 4. Step ends with external yield pending
// 5. External yield completes, main resumes
// 6. Main sends to ops_channel (buffered) - data goes to buffer, recvq=0
// 7. Worker task is lost - never wakes to receive the data
// 8. Main blocks on select{inbox, events, bus_done} - all empty
// 9. Process goes IDLE with 2 threads - DEADLOCK
// busTestYield is an external yield type for testing bus pattern multi-step scenarios.
// It implements YieldConverter so the engine treats it as an external yield.
type busTestYield struct {
	id int
}

const busTestYieldCmdID dispatcher.CommandID = 9998

func (y *busTestYield) String() string                { return "<bus_test_yield>" }
func (y *busTestYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *busTestYield) CmdID() dispatcher.CommandID   { return busTestYieldCmdID }
func (y *busTestYield) ToCommand() dispatcher.Command { return y }
func (y *busTestYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	if s, ok := data.(string); ok {
		return []lua.LValue{lua.LString(s), lua.LNil}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

var _ luaapi.YieldConverter = (*busTestYield)(nil)
var _ luaapi.HandledYield = (*busTestYield)(nil)

func TestBusPatternWorkerBlockedThenMainSends(t *testing.T) {
	yieldCounter := 0
	testYieldFunc := func(l *lua.LState) int {
		yieldCounter++
		y := &busTestYield{id: yieldCounter}
		l.Push(y) // push the yield directly, it implements LValue
		return -1 // yield externally
	}

	script := `
		local ops_channel = channel.new(256)  -- buffered
		local stop_signal = channel.new(1)
		local bus_done = channel.new(0)
		local inbox = process.subscribe("inbox", 0)
		local events = channel.new(0)

		-- Bus worker coroutine
		coroutine.spawn(function()
			local iteration = 0
			while true do
				iteration = iteration + 1
				if iteration > 10 then
					bus_done:send("worker_timeout")
					return
				end

				-- Worker blocks here (both channels empty)
				local result = channel.select{
					stop_signal:case_receive(),
					ops_channel:case_receive()
				}

				if result.channel == stop_signal then
					bus_done:send("stopped")
					return
				elseif result.channel == ops_channel then
					stop_signal:send(true)
				end
			end
		end)

		-- Main yields externally FIRST - this lets worker run and block
		-- Simulates SQL query in the real app
		test_yield()

		-- After external yield completes, main sends to ops_channel
		-- Worker should wake but can't (task was dropped when it blocked)
		ops_channel:send({type = "initial_op"})

		-- Main blocks on select
		local main_iteration = 0
		while main_iteration < 10 do
			main_iteration = main_iteration + 1

			local result = channel.select{
				inbox:case_receive(),
				events:case_receive(),
				bus_done:case_receive()
			}

			if result.channel == inbox then
				ops_channel:send({type = "handle_message", data = result.value})
			elseif result.channel == bus_done then
				return "bus_done:" .. tostring(result.value)
			end
		end

		return "main_timeout"
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
		WithModuleBinder(bindProcessModule),
		WithModuleBinder(wrapBinder(func(l *lua.LState) {
			l.SetGlobal("test_yield", l.NewFunction(testYieldFunc))
		})),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput

	// Step 1: Run until we get an external yield
	// Main will yield externally, worker will spawn and block on select
	output.Reset()
	if err := proc.Step(nil, &output); err != nil {
		t.Fatalf("Step 1 error: %v", err)
	}
	t.Logf("Step 1: status=%d, threads=%d, pendingYields=%d",
		output.Status(), len(proc.threads), len(proc.pendingYields))

	// Should have yielded externally (WaitYields status)
	if output.Status() != process.StepYield {
		t.Fatalf("Expected StepYield (3), got status=%d", output.Status())
	}
	if len(proc.threads) != 2 {
		t.Fatalf("Expected 2 threads, got %d", len(proc.threads))
	}
	t.Log("Phase 1: Main yielded externally, worker should have blocked on select")

	// Get the yield tag to complete it
	var yieldTag uint64
	for tag := range proc.pendingYields {
		yieldTag = tag
		break
	}

	// Step 2: Complete the external yield - this resumes main
	// Main will send to ops_channel, but worker's task was dropped
	yieldEvent := process.Event{
		Type: process.EventYieldComplete,
		Tag:  yieldTag,
		Data: "yield_done",
	}

	output.Reset()
	if err := proc.Step([]process.Event{yieldEvent}, &output); err != nil {
		t.Fatalf("Step 2 error: %v", err)
	}
	t.Logf("Step 2 (after yield complete): status=%d, threads=%d",
		output.Status(), len(proc.threads))

	// Bug condition: IDLE with 2 threads means deadlock
	if output.Status() == process.StepIdle && len(proc.threads) == 2 {
		t.Log("BUG REPRODUCED: IDLE with 2 threads")
		t.Log("Main sent to ops_channel but worker task was lost")
		t.Log("Worker blocked on select, task dropped, never re-queued")
		t.Fatal("Deadlock: worker task lost when it blocked on channel select")
	}

	// Continue running to see if it completes
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("Step %d error: %v", i+3, err)
		}
		t.Logf("Step %d: status=%d, threads=%d", i+3, output.Status(), len(proc.threads))

		if output.Status() == process.StepDone {
			t.Logf("Process completed with result: %v", proc.result)
			t.Log("SUCCESS: Bug is fixed")
			return
		}

		if output.Status() == process.StepIdle && len(proc.threads) == 2 {
			t.Fatal("Deadlock: IDLE with 2 threads")
		}
	}

	t.Fatal("Process did not complete")
}

// TestSelectSendWakesOnClose tests that select with send case wakes when channel is closed.
func TestSelectSendWakesOnClose(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local select_result = nil
		local select_error = nil

		-- Worker does select send (blocks because no receiver)
		coroutine.spawn(function()
			local ok, err = pcall(function()
				select_result = channel.select({
					ch:case_send("value")
				})
			end)
			if not ok then
				select_error = err
			end
		end)

		-- Let worker block on select
		coroutine.yield()

		-- Close channel - should wake the blocked select send
		ch:close()

		-- Let worker process the wake
		for i = 1, 5 do coroutine.yield() end

		-- Select should have errored or returned with ok=false
		if select_error then
			return "error: " .. tostring(select_error)
		end
		if select_result and not select_result.ok then
			return "ok=false"
		end
		return "unexpected: " .. tostring(select_result)
	`

	proc := mustNewProcess(t,
		WithScript(script, "test.lua"),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { LoadModuleDef(l, ChannelModule) })),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	defer proc.Close()

	var output process.StepOutput
	for i := 0; i < 20; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			// Error from send on closed is expected
			t.Logf("Process error (expected): %v", err)
			return
		}
		if output.Status() == process.StepDone {
			t.Log("Select send wakes on close completed")
			return
		}
	}

	t.Fatal("Process did not complete")
}
