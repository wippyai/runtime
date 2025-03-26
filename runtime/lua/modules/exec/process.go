package exec

import (
	"context"
	"errors" // Import errors package
	"fmt"
	"io"
	"os"             // Import os
	osexec "os/exec" // Import os/exec for ExitError type checking
	"strings"        // Import strings
	"sync"
	"syscall" // Import syscall for SIGKILL

	apiexec "github.com/ponyruntime/pony/api/service/exec"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream" // Keep stream dependency
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Process wraps an apiexec.Process handle for Lua
type Process struct {
	log *zap.Logger
	mu  sync.Mutex
	// The actual process handle from the factory
	handle apiexec.Process
	// UoW cleanup function *for this wrapper*, calls handle.Stop()
	onWrapperRelease context.CancelFunc
	streamOnce       sync.Once
	// Store the LuaStream userdata directly, including its own cleanup func
	stdoutStream *stream.LuaStream
	stderrStream *stream.LuaStream
	// Track if explicitly closed
	closed bool
}

// NewProcess creates a new Process handle wrapper with UoW integration
func NewProcess(uw engine.UnitOfWork, handle apiexec.Process, log *zap.Logger) *Process {
	wrapper := &Process{
		handle: handle,
		log:    log,
	}

	// Register cleanup *for this wrapper* with UoW.
	// This attempts to Stop the process if the script finishes unexpectedly.
	wrapper.onWrapperRelease = uw.AddCleanup(func() error {
		wrapper.log.Debug("UoW cleanup: Stopping process via wrapper cleanup")
		// Call the internal close logic, force=false
		wrapper.internalClose(false) // Use internalClose for consistency
		return nil                   // Cleanup itself doesn't error
	})
	wrapper.log.Debug("Created Process handle wrapper and registered UoW cleanup")
	return wrapper
}

// CheckProcess checks if the Lua argument is a valid, non-closed Process handle userdata
func CheckProcess(l *lua.LState, n int) *Process {
	ud := l.CheckUserData(n)
	procWrapper, ok := ud.Value.(*Process)
	if !ok {
		l.ArgError(n, "expected process object")
		return nil
	}

	return procWrapper
}

// WrapProcess wraps an apiexec.Process handle as Lua userdata
func WrapProcess(l *lua.LState, handle apiexec.Process, log *zap.Logger) *lua.LUserData {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("Cannot wrap process handle: unit of work missing from context")
		return nil
	}
	procLogger := log.Named("lua.exec.Process")
	wrapper := NewProcess(uw, handle, procLogger)
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, value.GetTypeMetatable(l, processMetatable))
	return ud
}

// internalClose is the core logic for closing/stopping the process and streams.
// It handles locking and idempotency. forceStop determines if SIGKILL is used.
// Returns true if cleanup was performed, false if already closed.
func (p *Process) internalClose(forceStop bool) bool {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.log.Debug("InternalClose: Already closed")
		return false // Already closed
	}

	p.log.Debug("InternalClose: Closing process wrapper", zap.Bool("forceStop", forceStop))
	p.closed = true
	handle := p.handle
	onWrapperRelease := p.onWrapperRelease
	p.handle = nil
	p.onWrapperRelease = nil
	stdout := p.stdoutStream
	stderr := p.stderrStream
	p.stdoutStream = nil
	p.stderrStream = nil
	p.mu.Unlock()

	// Cancel UoW Cleanup for this wrapper
	if onWrapperRelease != nil {
		p.log.Debug("InternalClose: Cancelling UoW wrapper cleanup")
		onWrapperRelease() // Call context.CancelFunc without arguments
	}

	// Close Streams (outside lock)
	if stdout != nil {
		p.log.Debug("InternalClose: Closing stdout stream")
		_ = stdout.Close()
	}
	if stderr != nil {
		p.log.Debug("InternalClose: Closing stderr stream")
		_ = stderr.Close()
	}

	// Signal Process (outside lock)
	if handle != nil {
		sig := syscall.SIGTERM // Default: graceful termination
		action := "Stopping process gracefully"
		if forceStop {
			sig = syscall.SIGKILL
			action = "Force stopping process"
		}
		p.log.Info("InternalClose: "+action, zap.Bool("force", forceStop), zap.Int("signal", int(sig)))

		// Use Signal method from apiexec.Process interface
		err := handle.Signal(int(sig))
		// Ignore errors if the process is already done
		if err != nil && !errors.Is(err, os.ErrProcessDone) && !strings.Contains(err.Error(), "process already finished") {
			p.log.Warn("InternalClose: Error during signal", zap.Int("signal", int(sig)), zap.Error(err))
		}
	}

	p.log.Debug("InternalClose: Completed")
	return true // Cleanup performed
}

// --- Lua Functions for Process Handle ---

// processStart (Lua: process:start()) - NO coroutine.Wrap needed
func processStart(l *lua.LState) int {
	procWrapper := CheckProcess(l, 1)
	if procWrapper == nil {
		return 0
	}
	procWrapper.log.Debug("Lua calling process:start()")

	procWrapper.mu.Lock()
	if procWrapper.closed { // Check closed flag now
		procWrapper.mu.Unlock()
		l.RaiseError("process has been closed")
		return 0
	}
	handle := procWrapper.handle
	procWrapper.mu.Unlock()

	// *** FIX: Use Start method from apiexec.Process interface ***
	err := handle.Start()
	if err != nil {
		procWrapper.log.Error("Failed to start process", zap.Error(err))
		l.RaiseError("failed to start process: %v", err)
		return 0
	}

	// Cannot reliably get PID here anymore, removed processPID
	procWrapper.log.Info("Process started successfully")
	return 0
}

// initializeStreams is called by sync.Once. Uses stream.NewLuaStream.
func (p *Process) initializeStreams(l *lua.LState) (errStdout, errStderr error) {
	p.log.Debug("Initializing stdout/stderr streams via sync.Once")

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		p.log.Error("Stream init failed: UnitOfWork missing from context")
		err := errors.New("missing UnitOfWork")
		return err, err
	}

	p.mu.Lock()
	if p.closed { // Check if closed before accessing handle
		p.mu.Unlock()
		p.log.Error("Stream init attempted on closed process handle")
		err := errors.New("process closed")
		return err, err
	}
	handle := p.handle
	p.mu.Unlock()

	// Stdout
	stdoutReader := handle.Stdout()
	if stdoutReader == nil {
		p.log.Error("Process handle returned nil stdout reader")
		errStdout = errors.New("nil stdout reader from process")
	} else {
		// Create underlying stream.Stream
		sOut, err := stream.NewStream(l.Context(), stdoutReader)
		if err != nil {
			p.log.Error("Failed to create underlying stream for stdout", zap.Error(err))
			errStdout = fmt.Errorf("stdout stream creation: %w", err)
		} else {
			// Wrap in LuaStream, which registers its *own* UoW cleanup via AddCleanup
			luaStreamOut := stream.NewLuaStream(uw, sOut, nil) // Pass nil closer, stream manages its own

			p.mu.Lock()
			if !p.closed { // Check again under lock
				p.stdoutStream = luaStreamOut
				p.log.Debug("Stdout stream initialized")
			} else {
				p.log.Warn("Process closed during stdout stream initialization, closing stream")
				_ = luaStreamOut.Close() // Close the LuaStream wrapper
				errStdout = errors.New("process closed during stream init")
			}
			p.mu.Unlock()
		}
	}

	// Stderr
	stderrReader := handle.Stderr()
	if stderrReader == nil {
		p.log.Error("Process handle returned nil stderr reader")
		errStderr = errors.New("nil stderr reader from process")
	} else {
		sErrR, err := stream.NewStream(l.Context(), stderrReader)
		if err != nil {
			p.log.Error("Failed to create underlying stream for stderr", zap.Error(err))
			errStderr = fmt.Errorf("stderr stream creation: %w", err)
		} else {
			luaStreamErr := stream.NewLuaStream(uw, sErrR, nil) // Manages its own cleanup

			p.mu.Lock()
			if !p.closed { // Check again under lock
				p.stderrStream = luaStreamErr
				p.log.Debug("Stderr stream initialized")
			} else {
				p.log.Warn("Process closed during stderr stream initialization, closing stream")
				_ = luaStreamErr.Close()
				errStderr = errors.New("process closed during stream init")
			}
			p.mu.Unlock()
		}
	}

	return errStdout, errStderr
}

// processStdoutStream (Lua: process:stdout_stream()) - NO coroutine.Wrap
func processStdoutStream(l *lua.LState) int {
	procWrapper := CheckProcess(l, 1)
	if procWrapper == nil {
		return 0
	}
	procWrapper.log.Debug("Lua calling process:stdout_stream()")

	var initErr error
	procWrapper.streamOnce.Do(func() {
		errOut, _ := procWrapper.initializeStreams(l)
		initErr = errOut
	})

	if initErr != nil {
		l.RaiseError("failed to initialize stdout stream: %v", initErr)
		return 0
	}

	procWrapper.mu.Lock()
	streamWrapper := procWrapper.stdoutStream // This is *stream.LuaStream now
	closed := procWrapper.closed
	procWrapper.mu.Unlock()

	if closed {
		l.RaiseError("process has been closed")
		return 0
	}
	if streamWrapper == nil {
		l.RaiseError("stdout stream is unexpectedly nil after initialization")
		return 0
	}

	ud := l.NewUserData()
	ud.Value = streamWrapper // Push the *stream.LuaStream
	// *** FIX: Use correct stream metatable name ***
	l.SetMetatable(ud, l.GetTypeMetatable(streamMetatable))
	l.Push(ud)
	return 1
}

// processStderrStream (Lua: process:stderr_stream()) - NO coroutine.Wrap
func processStderrStream(l *lua.LState) int {
	procWrapper := CheckProcess(l, 1)
	if procWrapper == nil {
		return 0
	}
	procWrapper.log.Debug("Lua calling process:stderr_stream()")

	var initErr error
	procWrapper.streamOnce.Do(func() {
		_, errStderr := procWrapper.initializeStreams(l)
		initErr = errStderr
	})

	if initErr != nil {
		l.RaiseError("failed to initialize stderr stream: %v", initErr)
		return 0
	}

	procWrapper.mu.Lock()
	streamWrapper := procWrapper.stderrStream // This is *stream.LuaStream now
	closed := procWrapper.closed
	procWrapper.mu.Unlock()

	if closed {
		l.RaiseError("process has been closed")
		return 0
	}
	if streamWrapper == nil {
		l.RaiseError("stderr stream is unexpectedly nil after initialization")
		return 0
	}

	ud := l.NewUserData()
	ud.Value = streamWrapper // Push the *stream.LuaStream
	// *** FIX: Use correct stream metatable name ***
	l.SetMetatable(ud, l.GetTypeMetatable(streamMetatable))
	l.Push(ud)
	return 1
}

// processWriteStdin (Lua: process:write_stdin(data)) - NO coroutine.Wrap
func processWriteStdin(l *lua.LState) int {
	procWrapper := CheckProcess(l, 1)
	if procWrapper == nil {
		return 0
	}
	data := l.CheckString(2)
	procWrapper.log.Debug("Lua calling process:write_stdin()", zap.Int("data_len", len(data)))

	procWrapper.mu.Lock()
	if procWrapper.closed {
		procWrapper.mu.Unlock()
		l.RaiseError("process has been closed")
		return 0
	}
	handle := procWrapper.handle
	procWrapper.mu.Unlock() // Unlock before potentially blocking IO

	// *** FIX: Use WriteStdin method from apiexec.Process interface ***
	err := handle.WriteStdin([]byte(data))
	if err != nil {
		procWrapper.log.Error("Failed to write to stdin", zap.Error(err))
		errMsg := fmt.Sprintf("failed to write to stdin: %v", err)
		if errors.Is(err, io.ErrClosedPipe) || strings.Contains(err.Error(), "process is not running") {
			errMsg = "failed to write to stdin (pipe closed or process not running)"
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(errMsg))
		return 2
	}
	l.Push(lua.LTrue)
	return 1
}

// processSignal (Lua: process:signal(sigNum)) - NO coroutine.Wrap
func processSignal(l *lua.LState) int {
	procWrapper := CheckProcess(l, 1)
	if procWrapper == nil {
		return 0
	}
	sig := l.CheckInt(2)
	procWrapper.log.Debug("Lua calling process:signal()", zap.Int("signal", sig))

	procWrapper.mu.Lock()
	if procWrapper.closed {
		procWrapper.mu.Unlock()
		l.RaiseError("process has been closed")
		return 0
	}
	handle := procWrapper.handle
	procWrapper.mu.Unlock() // Unlock before Signal

	// *** FIX: Use Signal method from apiexec.Process interface ***
	err := handle.Signal(sig)
	if err != nil {
		errMsg := fmt.Sprintf("failed to send signal %d: %v", sig, err)
		// Use os.ErrProcessDone imported via osexec
		if errors.Is(err, os.ErrProcessDone) || strings.Contains(err.Error(), "process already finished") {
			errMsg = fmt.Sprintf("failed to send signal %d (process already finished)", sig)
			procWrapper.log.Debug("Signal failed as process already finished", zap.Int("signal", sig))
			l.Push(lua.LNil)
			l.Push(lua.LString(errMsg))
			return 2
		}
		procWrapper.log.Error("Failed to send signal", zap.Int("signal", sig), zap.Error(err))
		l.Push(lua.LNil)
		l.Push(lua.LString(errMsg))
		return 2
	}
	procWrapper.log.Info("Signal sent successfully", zap.Int("signal", sig))
	l.Push(lua.LTrue)
	return 1
}

func processWait(l *lua.LState) int {
	procWrapper := CheckProcess(l, 1)
	if procWrapper == nil {
		return 0
	}
	procWrapper.log.Debug("Lua calling process:wait()")

	procWrapper.mu.Lock()
	closed := procWrapper.closed
	handle := procWrapper.handle
	procWrapper.mu.Unlock()

	if closed {
		l.Push(lua.LNil)
		l.Push(lua.LString("process closed before wait completed"))
		return 2
	}

	if handle == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("internal error: process handle is nil but not marked closed"))
		return 2
	}

	coroutine.Wrap(l, func() *engine.Update {
		procWrapper.mu.Lock()
		closedInside := procWrapper.closed
		handleInside := procWrapper.handle
		procWrapper.mu.Unlock()

		if closedInside || handleInside == nil {
			procWrapper.log.Warn("Process closed before Wait could execute")
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("process closed before wait completed")}, nil)
		}

		procWrapper.log.Debug("Waiting for process to exit")
		err := handleInside.Wait() // Use Wait method from apiexec.Process interface
		procWrapper.log.Debug("Process wait completed", zap.Error(err))

		// --- Cleanup after Wait ---
		procWrapper.mu.Lock()
		onWrapperRelease := procWrapper.onWrapperRelease
		procWrapper.onWrapperRelease = nil // Prevent UoW calling Stop now
		procWrapper.closed = true          // Mark as closed since Wait finished
		procWrapper.handle = nil           // Process is done
		stdout := procWrapper.stdoutStream
		stderr := procWrapper.stderrStream
		procWrapper.stdoutStream = nil
		procWrapper.stderrStream = nil
		procWrapper.mu.Unlock()

		if onWrapperRelease != nil {
			// Call context.CancelFunc without arguments
			onWrapperRelease()
			procWrapper.log.Debug("Cancelled UoW Stop cleanup after Wait completed")
		}

		// Close streams outside lock
		if stdout != nil {
			_ = stdout.Close()
		}
		if stderr != nil {
			_ = stderr.Close()
		}
		// --------------------------

		if err == nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNumber(0), lua.LNil}, nil) // Exit code 0
		}

		var exitErr *osexec.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitCode()
			procWrapper.log.Info("Process exited with non-zero code", zap.Int("exit_code", exitCode))
			return engine.NewUpdate(nil, []lua.LValue{lua.LNumber(exitCode), lua.LNil}, nil)
		}

		procWrapper.log.Error("Error waiting for process", zap.Error(err))
		return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("wait error: %v", err))}, nil)
	})

	return -1 // Yield
}

// processClose (Lua: process:close(force_stop_opt)) - Calls internalClose
func processClose(l *lua.LState) int {
	ud := l.CheckUserData(1)
	procWrapper, ok := ud.Value.(*Process)
	if !ok {
		l.ArgError(1, "expected process object")
		return 0
	}

	forceStop := false
	if l.GetTop() >= 2 {
		forceStop = l.ToBool(2)
	}
	procWrapper.log.Debug("Lua calling process:close()", zap.Bool("forceStop", forceStop))

	closedNow := procWrapper.internalClose(forceStop)

	if closedNow {
		procWrapper.log.Info("Process handle closed explicitly")
	} else {
		procWrapper.log.Debug("Process:close() called, but handle was already closed")
	}

	l.Push(lua.LTrue)
	return 1
}
