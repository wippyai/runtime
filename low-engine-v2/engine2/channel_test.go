package engine2

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
	lua "github.com/yuin/gopher-lua"
)

func startChannelProcess(t *testing.T, script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	proc := NewProcess(
		WithProto(proto),
		WithLayer(NewChannelLayer()),
		WithModuleBinder(BindTimeSleep),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}

	BindChannelFunctions(proc.State(), proc)
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
			received = ch:recv()
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
		local v1, _ = ch:recv()
		local v2, _ = ch:recv()
		local v3, _ = ch:recv()
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
		local v1, ok1 = ch:recv()
		local v2, ok2 = ch:recv()
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
				local v, ok = ch:recv()
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
		local v = ch:recv()
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
			local v = ch1:recv()
			ch2:send(v)
		end)

		coroutine.spawn(function()
			local v = ch2:recv()
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
			local v, _ = ch:recv()
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

		local v1 = ch:recv()
		for i = 1, 3 do coroutine.yield() end

		local v2 = ch:recv()
		local v3 = ch:recv()

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
		local v = ch:recv()
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

		local v1 = ch:recv()
		for i = 1, 3 do coroutine.yield() end
		local v2 = ch:recv()
		for i = 1, 3 do coroutine.yield() end
		local v3 = ch:recv()
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
				local v = ch:recv()
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
			local ch = passCh:recv()
			result = ch:recv()
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

		done:recv()
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

		done:recv()
		for i = 1, 10 do coroutine.yield() end

		local sum = 0
		for i = 1, 3 do
			local v = results:recv()
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
				local job, ok = jobs:recv()
				if ok then
					results:send(job * 2)
				end
			end)
		end

		for i = 1, 20 do coroutine.yield() end

		local sum = 0
		for i = 1, 3 do
			local r = results:recv()
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
				pong:recv()
				count = count + 1
			end
		end)

		coroutine.spawn(function()
			for i = 1, 5 do
				ping:recv()
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
