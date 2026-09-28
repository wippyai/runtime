// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package exec

import (
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

// luaBool reads a boolean the script returned under key.
func luaBool(t *testing.T, table *lua.LTable, key string) bool {
	t.Helper()
	field := table.RawGetString(key)
	require.NotEqual(t, lua.LTNil, field.Type(), "script did not return %q", key)
	return lua.LVAsBool(field)
}

// luaText reads a string the script returned under key.
func luaText(t *testing.T, table *lua.LTable, key string) string {
	t.Helper()
	field := table.RawGetString(key)
	require.NotEqual(t, lua.LTNil, field.Type(), "script did not return %q", key)
	return lua.LVAsString(field)
}

// grandchildPID reads the "child:<pid>" line the tree command writes on its
// first line of output and registers a fallback kill for it.
func grandchildPID(t *testing.T, table *lua.LTable, key string) int {
	t.Helper()
	line := luaText(t, table, key)
	_, digits, found := strings.Cut(strings.TrimSpace(line), "child:")
	require.Truef(t, found, "expected a child pid line, got %q", line)
	pid, err := strconv.Atoi(strings.TrimSpace(digits))
	require.NoErrorf(t, err, "expected a pid, got %q", digits)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

// groupAlive reports whether the pid can still be signaled; a reaped process
// answers ESRCH.
func groupAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// A child that is already gone when done() subscribes still has its exit
// delivered: the reap that done() owns observes a finished child and reports
// what it found, once.
func TestGroupChildExitedBeforeDoneStillDeliversOnce(t *testing.T) {
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"echo bye; exit 5\"", { process_group = true })
    if err then return nil, "exec: " .. tostring(err) end

    local stdout, serr = child:stdout_stream()
    if serr then return nil, "stdout_stream: " .. tostring(serr) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    -- Draining to EOF proves the child is finished: nothing else holds the
    -- write end of its stdout, so the pipe ends when the child does.
    local seen = ""
    while true do
        local chunk, rerr = stdout:read()
        if rerr then return nil, "read: " .. tostring(rerr) end
        if chunk == nil then break end
        seen = seen .. chunk
    end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    local status, ok = exit:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    local again, more = exit:receive()

    return {
        code = status.code,
        output = seen,
        first_ok = ok,
        second_value = again ~= nil,
        second_ok = more,
    }
end

return { main = main }
`)

	assert.Equal(t, 5, luaInt(t, out, "code"))
	assert.Equal(t, "bye\n", luaText(t, out, "output"))
	assert.True(t, luaBool(t, out, "first_ok"), "the exit must arrive as an open receive")
	assert.False(t, luaBool(t, out, "second_value"), "the exit must be delivered once")
	assert.False(t, luaBool(t, out, "second_ok"), "the channel must be finished after the exit")
}

// A group signal sent while the child is on its way out races the reap. The
// outcome is whichever the kernel settles on, but it is recorded once: done()
// delivers a single value and wait() afterwards reports the same exit.
func TestGroupSignalRacingExitIsRecordedOnce(t *testing.T) {
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"exit 4\"", { process_group = true })
    if err then return nil, "exec: " .. tostring(err) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    -- The child is exiting right now; the group signal may land before or
    -- after it does, and either way must not produce a second exit.
    local signaled, sigerr = child:signal(15)
    local signal_error = ""
    if not signaled then signal_error = tostring(sigerr) end

    local status = exit:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    local again, more = exit:receive()

    local code, werr = child:wait()
    if werr then return nil, "wait after done: " .. tostring(werr) end

    local closed, cerr = child:close()
    if not closed then return nil, "close: " .. tostring(cerr) end

    return {
        done_code = status.code,
        wait_code = code,
        second_value = again ~= nil,
        second_ok = more,
        signal_error = signal_error,
    }
end

return { main = main }
`)

	doneCode := luaInt(t, out, "done_code")
	assert.Contains(t, []int{4, 143}, doneCode,
		"the child either finished on its own or was killed by the group signal")
	assert.Equal(t, doneCode, luaInt(t, out, "wait_code"),
		"wait() must report the exit done() already recorded")
	assert.False(t, luaBool(t, out, "second_value"), "the exit must be delivered once")
	assert.False(t, luaBool(t, out, "second_ok"), "the channel must be finished after the exit")
	// A signal to a group whose last member has gone is refused, and that is the
	// only failure the race may produce.
	if signalErr := luaText(t, out, "signal_error"); signalErr != "" {
		assert.Contains(t, signalErr, "process")
	}
}

// A grandchild inherits stdout and holds it open, so the pipe says nothing
// about the child. done() reports the child's own exit while the pipe is still
// open, and close(true) then takes the whole group down.
func TestGroupDoneOutrunsPipeHeldByDescendant(t *testing.T) {
	// The exit must arrive on its own timing, not on the grandchild's: the
	// descendant holding stdout lives far longer than this bound.
	const exitDeadline = 5 * time.Second

	start := time.Now()
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"sleep 300 & echo child:$!; exit 3\"", { process_group = true })
    if err then return nil, "exec: " .. tostring(err) end

    local stdout, serr = child:stdout_stream()
    if serr then return nil, "stdout_stream: " .. tostring(serr) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local line, rerr = stdout:read()
    if rerr then return nil, "read: " .. tostring(rerr) end
    if line == nil then return nil, "child reported no grandchild" end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    local status = exit:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    local closed, cerr = child:close(true)
    if not closed then return nil, "close: " .. tostring(cerr) end

    return { code = status.code, line = line }
end

return { main = main }
`)
	elapsed := time.Since(start)

	assert.Equal(t, 3, luaInt(t, out, "code"))
	assert.Less(t, elapsed, exitDeadline,
		"the exit was reported only once the inherited stdout pipe closed")

	grandchild := grandchildPID(t, out, "line")
	assert.True(t, waitFor(t, 10*time.Second, func() bool { return !groupAlive(grandchild) }),
		"grandchild %d survived close(true) of a process-group child", grandchild)
}

// Output the child wrote just before it exited belongs to the caller that still
// holds the handle. done() observes the exit without taking that output away.
func TestGroupOutputWrittenBeforeExitSurvivesDone(t *testing.T) {
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"echo final-bytes; exit 0\"", { process_group = true })
    if err then return nil, "exec: " .. tostring(err) end

    local stdout, serr = child:stdout_stream()
    if serr then return nil, "stdout_stream: " .. tostring(serr) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    local status = exit:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    local chunk, rerr = stdout:read()
    if rerr then return nil, "read after done: " .. tostring(rerr) end
    if chunk == nil then return nil, "stdout ended without the child's last output" end

    return { code = status.code, tail = chunk }
end

return { main = main }
`)

	assert.Equal(t, 0, luaInt(t, out, "code"))
	assert.Equal(t, "final-bytes\n", luaText(t, out, "tail"))
}

// close() terminates the group, so the grandchild goes with the child, and the
// exit done() reports is the signal death that close() caused.
func TestGroupCloseKillsDescendantAndDoneReportsSignalDeath(t *testing.T) {
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"sleep 300 & echo child:$!; wait\"", { process_group = true })
    if err then return nil, "exec: " .. tostring(err) end

    local stdout, serr = child:stdout_stream()
    if serr then return nil, "stdout_stream: " .. tostring(serr) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local line, rerr = stdout:read()
    if rerr then return nil, "read: " .. tostring(rerr) end
    if line == nil then return nil, "child reported no grandchild" end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    local closed, cerr = child:close()
    if not closed then return nil, "close: " .. tostring(cerr) end

    local status = exit:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    return { code = status.code, signal = status.signal or 0, line = line }
end

return { main = main }
`)

	assert.Equal(t, 15, luaInt(t, out, "signal"), "close() terminates the group with SIGTERM")
	assert.Equal(t, 128+15, luaInt(t, out, "code"), "a signal death is reported as 128+signal")

	grandchild := grandchildPID(t, out, "line")
	assert.True(t, waitFor(t, 10*time.Second, func() bool { return !groupAlive(grandchild) }),
		"grandchild %d survived close() of a process-group child", grandchild)
}

// The cleanup that runs when the owning Lua process ends releases the group
// even though done() was the only observer and nothing ever received from it.
func TestGroupOwnerExitCleanupReleasesTreeAfterUnreceivedDone(t *testing.T) {
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"sleep 300 & echo child:$!; wait\"", { process_group = true })
    if err then return nil, "exec: " .. tostring(err) end

    local stdout, serr = child:stdout_stream()
    if serr then return nil, "stdout_stream: " .. tostring(serr) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local line, rerr = stdout:read()
    if rerr then return nil, "read: " .. tostring(rerr) end
    if line == nil then return nil, "child reported no grandchild" end

    -- Subscribed and never received: the exit has nowhere to go, and the tree
    -- must still be released when the owning process ends.
    local _, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    return { line = line }
end

return { main = main }
`)

	// runExecScript releases the frame it ran the script in, which closes the
	// resource store the process owns and runs the exec cleanup.
	grandchild := grandchildPID(t, out, "line")
	assert.True(t, waitFor(t, 10*time.Second, func() bool { return !groupAlive(grandchild) }),
		"grandchild %d outlived the process that owned its group", grandchild)
}
