// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type terminalTestInput struct{}

func (terminalTestInput) Start() error                  { return nil }
func (terminalTestInput) Stop() error                   { return nil }
func (terminalTestInput) ScreenSize() (int, int, error) { return 80, 24, nil }
func (terminalTestInput) EnableMouse()                  {}
func (terminalTestInput) DisableMouse()                 {}

type terminalTestSurface struct{ closed atomic.Bool }

func (*terminalTestSurface) Present(frame ttyapi.Frame) (ttyapi.PresentStats, error) {
	return ttyapi.PresentStats{Rows: len(frame.Rows)}, nil
}
func (*terminalTestSurface) Invalidate() {}
func (s *terminalTestSurface) Close() error {
	s.closed.Store(true)
	return nil
}

type terminalTestPort struct{ surface *terminalTestSurface }

func (*terminalTestPort) InputController() ttyapi.InputController { return terminalTestInput{} }
func (p *terminalTestPort) OpenSurface(ttyapi.SurfaceOptions) (ttyapi.Surface, error) {
	return p.surface, nil
}
func (*terminalTestPort) Close() error { return nil }

func TestExecutorTerminalReturnsStartedHostProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native PTY startup is Unix-only")
	}
	port := &terminalTestPort{surface: &terminalTestSurface{}}
	out := runExecScriptWithPort(t, `
local function main()
    local terminal, err = executor:terminal("sh -c 'printf hello'", {pty = {term = "xterm-256color"}})
    if err then error(err) end
    local host_pid, pid_err = terminal:pid()
    if pid_err then error(pid_err) end
    local done = terminal:done()
    local finished, open = done:receive()
    if not open or not finished or not finished.exit then error("terminal completion missing") end
    if finished.exit.code ~= 0 or finished.terminal_error then error("unexpected terminal outcome") end
    terminal:close()
    return {pid = host_pid, code = finished.exit.code}
end
return {main = main}
`, port)
	require.Greater(t, int(lua.LVAsNumber(out.RawGetString("pid"))), 0)
	require.Equal(t, 0, int(lua.LVAsNumber(out.RawGetString("code"))))
	require.True(t, port.surface.closed.Load())
}

func TestExecutorTerminalReportsNonzeroChildExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native PTY startup is Unix-only")
	}
	port := &terminalTestPort{surface: &terminalTestSurface{}}
	out := runExecScriptWithPort(t, `
local function main()
    local terminal = assert(executor:terminal("sh -c 'exit 7'"))
    local finished, open = terminal:done():receive()
    if not open or not finished or not finished.exit then error("terminal completion missing") end
    if finished.terminal_error then error(finished.terminal_error) end
    return {code = finished.exit.code}
end
return {main = main}
`, port)
	require.Equal(t, 7, int(lua.LVAsNumber(out.RawGetString("code"))))
	require.True(t, port.surface.closed.Load())
}

func TestExecutorTerminalStartupFailureReleasesSurface(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native PTY startup is Unix-only")
	}
	port := &terminalTestPort{surface: &terminalTestSurface{}}
	out := runExecScriptWithPort(t, `
local function main()
    local terminal, err = executor:terminal("wippy-missing-terminal-executable-824")
    if terminal ~= nil or err == nil then error("expected terminal startup failure") end
    return {failed = true}
end
return {main = main}
`, port)
	require.Equal(t, lua.LTrue, out.RawGetString("failed"))
	require.True(t, port.surface.closed.Load())
}
