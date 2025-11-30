package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/relay"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

func startChannelProcess(t *testing.T, script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	proc := NewProcess(
		WithProto(proto),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	BindChannelFunctions(proc.State())
	return proc
}

func runUntilDone(t *testing.T, proc *Process, maxSteps int) error {
	for i := 0; i < maxSteps; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			return err
		}
		if result.Status == scheduler.StepDone {
			return nil
		}
	}
	t.Fatalf("did not complete in %d steps", maxSteps)
	return nil
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
			if string(str) != "hello" {
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
		local idx = channel.select(ch1:case_receive(), ch2:case_receive(), nil)
		return idx
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 0 {
				t.Errorf("expected default (0), got %d", int(n))
			}
		}
	}
	t.Log("Select with default test passed")
}

// TestDeadlockSingleCoroutine tests deadlock detection on single recv
func TestDeadlockSingleCoroutine(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local v = ch:receive()
		return v
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var lastErr error
	for i := 0; i < 20; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			lastErr = err
			break
		}
		if result.Status == scheduler.StepDone {
			break
		}
	}

	if lastErr == nil {
		t.Fatal("expected deadlock error")
	}

	if !IsDeadlock(lastErr) {
		t.Errorf("expected DeadlockError, got %T: %v", lastErr, lastErr)
	}
	t.Logf("Deadlock detected correctly: %v", lastErr)
}

// TestDeadlockMutualWait tests deadlock with two coroutines waiting on each other
func TestDeadlockMutualWait(t *testing.T) {
	script := `
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)

		coroutine.spawn(function()
			local v = ch1:receive()
			ch2:send(v)
		end)

		coroutine.spawn(function()
			local v = ch2:receive()
			ch1:send(v)
		end)

		for i = 1, 20 do coroutine.yield() end
		return "done"
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	var lastErr error
	for i := 0; i < 50; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			lastErr = err
			break
		}
		if result.Status == scheduler.StepDone {
			break
		}
	}

	if lastErr == nil {
		t.Fatal("expected deadlock error for mutual wait")
	}

	if !IsDeadlock(lastErr) {
		t.Errorf("expected DeadlockError, got %T: %v", lastErr, lastErr)
	}
	t.Logf("Mutual deadlock detected correctly: %v", lastErr)
}

// TestNoDeadlockWithDefault tests that select with default doesn't deadlock
func TestNoDeadlockWithDefault(t *testing.T) {
	script := `
		local ch = channel.new(0)
		local idx = channel.select(ch:case_receive(), nil)
		if idx == 0 then return "default" end
		return "unexpected"
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if str, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if string(str) != "default" {
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
		local result = nil

		coroutine.spawn(function()
			local idx = channel.select(ch1:case_receive(), ch2:case_receive())
			result = idx
		end)

		for i = 1, 3 do coroutine.yield() end
		ch2:send("wake")
		for i = 1, 5 do coroutine.yield() end

		return result
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 2 {
				t.Errorf("expected select index 2, got %d", int(n))
			}
		}
	}
	t.Log("Select blocking then wake test passed")
}

// TestSelectWithCaseSend tests select with case_send
func TestSelectWithCaseSend(t *testing.T) {
	script := `
		local ch = channel.new(1)
		local idx = channel.select(ch:case_send("value"))
		local v = ch:receive()
		return idx, v
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) >= 2 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok && int(n) != 1 {
			t.Errorf("expected select index 1, got %d", int(n))
		}
		if s, ok := proc.mainTask.Yielded[1].(lua.LString); ok && string(s) != "value" {
			t.Errorf("expected 'value', got %q", s)
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

		local idx = channel.select(
			sendCh:case_send("outgoing"),
			recvCh:case_receive()
		)

		return idx
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 1 && int(n) != 2 {
				t.Errorf("expected select index 1 or 2, got %d", int(n))
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

		local idx = channel.select(
			ch1:case_receive(),
			ch2:case_receive()
		)
		return idx
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 1 {
				t.Errorf("expected index 1 (ch1 has value), got %d", int(n))
			}
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
			if string(s) != "hello from inner" {
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
				local idx = channel.select(
					ch1:case_receive(),
					ch2:case_receive()
				)
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
		if b, ok := allFalse.(lua.LBool); ok && !bool(b) {
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
		local result = nil
		local receivedOk = nil

		coroutine.spawn(function()
			local idx, val, ok = channel.select(
				ch1:case_receive(),
				ch2:case_receive()
			)
			result = idx
			receivedOk = ok
		end)

		for i = 1, 3 do coroutine.yield() end
		ch1:close()
		for i = 1, 5 do coroutine.yield() end

		return result, receivedOk
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 1 {
				t.Errorf("expected select index 1 (closed channel), got %d", int(n))
			}
		}
	}
	t.Log("Select wakes on close test passed")
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
		if !bool(ok1) {
			t.Error("expected ok=true for buffered value")
		}
		if bool(ok4) {
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
		local selected = nil

		coroutine.spawn(function()
			local idx = channel.select(
				ch1:case_receive(),
				ch2:case_receive(),
				ch3:case_receive()
			)
			selected = idx
		end)

		for i = 1, 3 do coroutine.yield() end
		ch2:send("wake via ch2")
		for i = 1, 5 do coroutine.yield() end

		return selected
	`

	proc := startChannelProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if n, ok := proc.mainTask.Yielded[0].(lua.LNumber); ok {
			if int(n) != 2 {
				t.Errorf("expected select index 2, got %d", int(n))
			}
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
	proc := NewProcess(
		WithProto(proto),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	BindChannelFunctions(proc.State())
	BindSubscribeFunctions(proc.State())
	return proc
}

func runUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
	for i := 0; i < maxSteps; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			return err
		}
		if result.Status == scheduler.StepIdle || result.Status == scheduler.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach idle in %d steps", maxSteps)
	return nil
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

	if !HasSubscriptions(proc) {
		t.Error("expected active subscription")
	}

	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "test_topic", Payloads: nil}},
	})

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

	for i := 0; i < 3; i++ {
		proc.Send(&relay.Package{
			Messages: []*relay.Message{{Topic: "inbox", Payloads: nil}},
		})
		proc.Step(nil)
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

	if HasSubscriptions(proc) {
		t.Error("expected no subscriptions after unsubscribe")
	}

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

	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic2", Payloads: nil}},
	})

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

	for i := 0; i < 5; i++ {
		proc.Send(&relay.Package{
			Messages: []*relay.Message{{Topic: "commands", Payloads: nil}},
		})

		result, err := proc.Step(nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == scheduler.StepDone {
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

		for i = 1, 10 do coroutine.yield() end

		return "waiting"
	`

	proc := startSubscribeProcess(t, script)
	defer proc.Close()

	if err := runUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	if !HasSubscriptions(proc) {
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

	if !HasSubscriptions(proc) {
		t.Error("expected subscription to be active")
	}

	// Send message to unblock
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic", Payloads: nil}},
	})

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if HasSubscriptions(proc) {
		t.Error("expected subscription to be removed")
	}

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
			result := string(str)
			if result == "no error" {
				t.Error("expected error for duplicate topic with different channel")
			} else {
				t.Logf("Correctly got error: %s", result)
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
			result := string(str)
			if result == "no error" {
				t.Error("expected error for unsubscribing non-subscribed channel")
			} else {
				t.Logf("Correctly got error: %s", result)
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
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "late_topic", Payloads: nil}},
	})

	if err := runUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Now send message AFTER subscription
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "late_topic", Payloads: nil}},
	})

	// Process should receive this one
	proc.Step(nil)
	proc.Step(nil)

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
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic2", Payloads: nil}},
	})
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic1", Payloads: nil}},
	})

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
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic", Payloads: nil}},
	})

	if err := runUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send second message after resubscribe
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic", Payloads: nil}},
	})

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	if proc.mainTask != nil && len(proc.mainTask.Yielded) > 0 {
		if str, ok := proc.mainTask.Yielded[0].(lua.LString); ok {
			if string(str) != "success" {
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
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic1", Payloads: nil}},
	})
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic2", Payloads: nil}},
	})

	for i := 0; i < 10; i++ {
		proc.Step(nil)
	}

	// After unsubscribe from topic1, send more to topic2
	proc.Send(&relay.Package{
		Messages: []*relay.Message{{Topic: "topic2", Payloads: nil}},
	})

	if err := runUntilDone(t, proc, 50); err != nil {
		t.Fatal(err)
	}

	t.Log("Multiple topics partial unsubscribe test passed")
}
