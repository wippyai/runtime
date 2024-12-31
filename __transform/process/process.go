package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/ponyruntime/go-lua"
)

// ProcessState represents the state of a process
type ProcessState string

const (
	StateNotStarted ProcessState = "not_started"
	StateRunning    ProcessState = "running"
	StateTerminated ProcessState = "terminated"
)

// Command represents a process to be started
type Command struct {
	name    string
	args    []string
	options struct {
		cwd string
		env map[string]string
	}
	ioConfig struct {
		collectStdout bool
		collectStderr bool
		maxBuffer     int
	}
	timeouts struct {
		process time.Duration
		idle    time.Duration
	}
}

// Process represents a running process
type Process struct {
	cmd          *exec.Cmd
	ctx          context.Context
	cancel       context.CancelFunc
	state        ProcessState
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	closeOnce    sync.Once
	wg           sync.WaitGroup
	lastActivity time.Time
	mu           sync.RWMutex
}

// Module represents the process Lua module
type Module struct{}

func NewProcessModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "process"
}

// Loader implements the Lua module loader
func (m *Module) Loader(l *lua.LState) int {
	// Create metatables
	cmdMt := l.NewTypeMetatable("process.command")
	l.SetField(cmdMt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"set_option":   m.cmdSetOption,
		"set_io":       m.cmdSetIO,
		"set_timeouts": m.cmdSetTimeouts,
		"start":        m.cmdStart,
		"run":          m.cmdRun,
	}))

	procMt := l.NewTypeMetatable("process.handle")
	l.SetField(procMt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"pid":          m.procPid,
		"status":       m.procStatus,
		"signal":       m.procSignal,
		"kill":         m.procKill,
		"write":        m.procWrite,
		"stdout_lines": m.procStdoutLines,
		"stderr_lines": m.procStderrLines,
		"stream":       m.procStream,
		"wait":         m.procWait,
		"close":        m.procClose,
	}))
	// Add finalizer to process handle
	l.SetField(procMt, "__gc", l.NewFunction(m.procGC))

	// Create module table
	mod := l.NewTable()
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"command": m.newCommand,
	})

	l.Push(mod)
	return 1
}

// newCommand creates a new Command object
func (m *Module) newCommand(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("command name required"))
		return 2
	}

	name := l.CheckString(1)
	args := make([]string, 0)
	for i := 2; i <= l.GetTop(); i++ {
		if arg := l.Get(i); arg.Type() == lua.LTString {
			args = append(args, arg.String())
		}
	}

	cmd := &Command{
		name: name,
		args: args,
	}

	// Set defaults
	cmd.ioConfig.collectStdout = true
	cmd.ioConfig.collectStderr = true
	cmd.ioConfig.maxBuffer = 16 * 1024 * 1024 // 16MB

	ud := l.NewUserData()
	ud.Value = cmd
	l.SetMetatable(ud, l.GetTypeMetatable("process.command"))
	l.Push(ud)
	return 1
}

// cmdSetOption sets command options
func (m *Module) cmdSetOption(l *lua.LState) int {
	cmd := checkCommand(l)
	opts := l.CheckTable(2)

	if cwd := opts.RawGetString("cwd"); cwd.Type() == lua.LTString {
		cmd.options.cwd = cwd.String()
	}

	if env := opts.RawGetString("env"); env.Type() == lua.LTTable {
		cmd.options.env = make(map[string]string)
		//env.ForEach(func(k, v lua.LValue) {
		//	if k.Type() == lua.LTString && v.Type() == lua.LTString {
		//		cmd.options.env[k.String()] = v.String()
		//	}
		//})
	}

	l.Push(lua.LTrue)
	return 1
}

// cmdSetIO sets IO configuration
func (m *Module) cmdSetIO(l *lua.LState) int {
	cmd := checkCommand(l)
	opts := l.CheckTable(2)

	if v := opts.RawGetString("collect_stdout"); v.Type() == lua.LTBool {
		cmd.ioConfig.collectStdout = bool(v.(lua.LBool))
	}
	if v := opts.RawGetString("collect_stderr"); v.Type() == lua.LTBool {
		cmd.ioConfig.collectStderr = bool(v.(lua.LBool))
	}
	if v := opts.RawGetString("max_buffer"); v.Type() == lua.LTNumber {
		cmd.ioConfig.maxBuffer = int(v.(lua.LNumber))
	}

	l.Push(lua.LTrue)
	return 1
}

// cmdSetTimeouts sets command timeouts
func (m *Module) cmdSetTimeouts(l *lua.LState) int {
	cmd := checkCommand(l)
	opts := l.CheckTable(2)

	if v := opts.RawGetString("process_timeout"); v.Type() == lua.LTNumber {
		cmd.timeouts.process = time.Duration(float64(v.(lua.LNumber)) * float64(time.Second))
	}
	if v := opts.RawGetString("idle_timeout"); v.Type() == lua.LTNumber {
		cmd.timeouts.idle = time.Duration(float64(v.(lua.LNumber)) * float64(time.Second))
	}

	l.Push(lua.LTrue)
	return 1
}

// cmdStart starts the command asynchronously
func (m *Module) cmdStart(l *lua.LState) int {
	cmd := checkCommand(l)

	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context available"))
		return 2
	}

	// Create process context
	pctx, cancel := context.WithCancel(ctx)
	if cmd.timeouts.process > 0 {
		pctx, cancel = context.WithTimeout(pctx, cmd.timeouts.process)
	}

	// Create exec command
	execCmd := exec.CommandContext(pctx, cmd.name, cmd.args...)

	// Set working directory
	if cmd.options.cwd != "" {
		execCmd.Dir = cmd.options.cwd
	}

	// Set environment
	if len(cmd.options.env) > 0 {
		environ := os.Environ()
		for k, v := range cmd.options.env {
			environ = append(environ, fmt.Sprintf("%s=%s", k, v))
		}
		execCmd.Env = environ
	}

	// Create process handle
	proc := &Process{
		cmd:    execCmd,
		ctx:    pctx,
		cancel: cancel,
		state:  StateNotStarted,
	}

	// Set up pipes
	var err error

	if proc.stdin, err = execCmd.StdinPipe(); err != nil {
		cancel()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if cmd.ioConfig.collectStdout {
		if proc.stdout, err = execCmd.StdoutPipe(); err != nil {
			cancel()
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	}

	if cmd.ioConfig.collectStderr {
		if proc.stderr, err = execCmd.StderrPipe(); err != nil {
			cancel()
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	}

	// Start the process
	if err := execCmd.Start(); err != nil {
		cancel()
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	proc.state = StateRunning
	proc.lastActivity = time.Now()

	// Set up idle timeout if needed
	if cmd.timeouts.idle > 0 {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					proc.mu.RLock()
					idle := time.Since(proc.lastActivity)
					proc.mu.RUnlock()
					if idle >= cmd.timeouts.idle {
						proc.cancel()
						return
					}
				case <-proc.ctx.Done():
					return
				}
			}
		}()
	}

	// Create and return process handle
	ud := l.NewUserData()
	ud.Value = proc
	l.SetMetatable(ud, l.GetTypeMetatable("process.handle"))
	l.Push(ud)
	return 1
}

// cmdRun runs the command synchronously
func (m *Module) cmdRun(l *lua.LState) int {
	// First start the process
	if m.cmdStart(l) != 1 {
		return 2 // Error already on stack
	}

	// Get process handle and wait
	proc := l.Get(-1).(*lua.LUserData).Value.(*Process)
	code, err := proc.wait()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LNumber(code))
	return 1
}

// Process methods

func (p *Process) wait() (int, error) {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.state = StateTerminated
	p.mu.Unlock()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}

func (p *Process) close() {
	p.closeOnce.Do(func() {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		if p.stderr != nil {
			_ = p.stderr.Close()
		}
		p.cancel()
		p.wg.Wait()
	})
}

// Lua methods for Process

func (m *Module) procPid(l *lua.LState) int {
	proc := checkProcess(l)
	if proc.cmd.Process == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("process not started"))
		return 2
	}
	l.Push(lua.LNumber(proc.cmd.Process.Pid))
	return 1
}

func (m *Module) procStatus(l *lua.LState) int {
	proc := checkProcess(l)

	proc.mu.RLock()
	defer proc.mu.RUnlock()

	status := l.NewTable()
	status.RawSetString("state", lua.LString(proc.state))

	if proc.state == StateTerminated && proc.cmd.ProcessState != nil {
		status.RawSetString("success", lua.LBool(proc.cmd.ProcessState.Success()))
		status.RawSetString("exit_code", lua.LNumber(proc.cmd.ProcessState.ExitCode()))
	} else {
		status.RawSetString("success", lua.LNil)
		status.RawSetString("exit_code", lua.LNil)
	}

	l.Push(status)
	return 1
}

func (m *Module) procSignal(l *lua.LState) int {
	proc := checkProcess(l)
	sig := l.CheckNumber(2)

	if proc.cmd.Process == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("process not started"))
		return 2
	}

	err := proc.cmd.Process.Signal(syscall.Signal(sig))
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) procKill(l *lua.LState) int {
	proc := checkProcess(l)
	if proc.cmd.Process == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("process not started"))
		return 2
	}

	err := proc.cmd.Process.Kill()
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) procWrite(l *lua.LState) int {
	proc := checkProcess(l)
	data := l.CheckString(2)

	if proc.stdin == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("stdin not available"))
		return 2
	}

	_, err := io.WriteString(proc.stdin, data)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	proc.mu.Lock()
	proc.lastActivity = time.Now()
	proc.mu.Unlock()

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) procStdoutLines(l *lua.LState) int {
	return m.procLines(l, "stdout")
}

func (m *Module) procStderrLines(l *lua.LState) int {
	return m.procLines(l, "stderr")
}

func (m *Module) procLines(l *lua.LState, streamType string) int {
	proc := checkProcess(l)

	var reader io.ReadCloser
	switch streamType {
	case "stdout":
		reader = proc.stdout
	case "stderr":
		reader = proc.stderr
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid stream type"))
		return 2
	}

	if reader == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("%s not available", streamType)))
		return 2
	}

	scanner := bufio.NewScanner(reader)

	// Create closure for the iterator
	iter := func(L *lua.LState) int {
		if scanner.Scan() {
			L.Push(lua.LString(scanner.Text()))
			return 1
		}
		if err := scanner.Err(); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		return 0
	}

	l.Push(l.NewFunction(iter))
	return 1
}

func (m *Module) procStream(l *lua.LState) int {
	proc := checkProcess(l)
	fn := l.CheckFunction(2)

	if proc.stdout == nil && proc.stderr == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("no output streams available"))
		return 2
	}

	proc.wg.Add(1)
	go func() {
		defer proc.wg.Done()

		if proc.stdout != nil {
			scanner := bufio.NewScanner(proc.stdout)
			for scanner.Scan() {
				l.Push(fn)
				l.Push(lua.LString(scanner.Text()))
				l.Push(lua.LString("stdout"))
				if err := l.PCall(2, 0, nil); err != nil {
					l.RaiseError("stream callback error: %v", err)
					return
				}
			}
		}

		if proc.stderr != nil {
			scanner := bufio.NewScanner(proc.stderr)
			for scanner.Scan() {
				l.Push(fn)
				l.Push(lua.LString(scanner.Text()))
				l.Push(lua.LString("stderr"))
				if err := l.PCall(2, 0, nil); err != nil {
					l.RaiseError("stream callback error: %v", err)
					return
				}
			}
		}
	}()

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) procWait(l *lua.LState) int {
	proc := checkProcess(l)

	code, err := proc.wait()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LNumber(code))
	return 1
}

func (m *Module) procClose(l *lua.LState) int {
	proc := checkProcess(l)
	proc.close()
	l.Push(lua.LTrue)
	return 1
}

func (m *Module) procGC(l *lua.LState) int {
	proc := checkProcess(l)
	proc.close()
	return 0
}

// Helper functions

func checkCommand(l *lua.LState) *Command {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Command); ok {
		return v
	}
	l.ArgError(1, "process.command expected")
	return nil
}

func checkProcess(l *lua.LState) *Process {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Process); ok {
		return v
	}
	l.ArgError(1, "process.handle expected")
	return nil
}
