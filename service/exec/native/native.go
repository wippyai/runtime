// SPDX-License-Identifier: MPL-2.0

package native

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/creack/pty"
	execapi "github.com/wippyai/runtime/api/service/exec"

	"go.uber.org/zap"
)

var (
	_ execapi.ProcessExecutor = (*Executor)(nil)
	_ execapi.Process         = (*ProcessExecutor)(nil)
	_ execapi.PTYProcess      = (*ptyProcess)(nil)
)

const (
	notStarted string = "not_started"
	running    string = "running"
	terminated string = "terminated"
)

// Executor implements the exec.ProcessExecutor interface
type Executor struct {
	log              *zap.Logger
	defaultEnv       map[string]string
	defaultWD        string
	commandWhitelist []string
}

// NewNativeExecutor creates a new native process executor
func NewNativeExecutor(log *zap.Logger, config *execapi.NativeExecutorConfig) *Executor {
	return &Executor{
		log:              log,
		defaultEnv:       config.DefaultEnv,
		defaultWD:        config.DefaultWorkDir,
		commandWhitelist: config.CommandWhitelist,
	}
}

// NewProcess implements exec.ProcessExecutor interface
func (e *Executor) NewProcess(cmd string, options execapi.ProcessOptions) (execapi.Process, error) {
	ptyOptions := options.PTY
	if ptyOptions != nil {
		copy := *ptyOptions
		ptyOptions = &copy
		if _, _, err := ptyOptions.Dimensions(); err != nil {
			return nil, err
		}
	}
	if len(e.commandWhitelist) > 0 {
		allowed := false
		for _, whitelistedCmd := range e.commandWhitelist {
			if cmd == whitelistedCmd {
				allowed = true
				break
			}
		}
		if !allowed {
			e.log.Warn("command rejected by whitelist", zap.String("command", cmd))
			return nil, NewCommandNotAllowedError(cmd)
		}
	}

	// Merge default environment with provided environment
	env := make(map[string]string)
	for k, v := range e.defaultEnv {
		env[k] = v
	}
	for k, v := range options.Env {
		env[k] = v
	}
	if ptyOptions != nil && ptyOptions.Term != "" {
		env["TERM"] = ptyOptions.Term
	}

	// Use default working directory if not specified
	workDir := options.WorkDir
	if workDir == "" {
		workDir = e.defaultWD
	}

	// Clean and validate working directory path
	if workDir != "" {
		workDir = filepath.Clean(workDir)
	}

	// Create a new process executor with the given command and options
	process := NewProcessExecutor(
		e.log,
		WithCmd(cmd),
		WithWorkingDir(workDir),
		WithEnv(env),
		WithPTY(ptyOptions),
	)
	if ptyOptions != nil {
		return &ptyProcess{ProcessExecutor: process}, nil
	}
	return process, nil
}

// ptyProcess is the capability-bearing view returned only for a process
// configured with a PTY. Ordinary pipe-backed processes do not accidentally
// satisfy exec.PTYProcess.
type ptyProcess struct{ *ProcessExecutor }

// ProcessExecutor represents a native process implementation
type ProcessExecutor struct {
	stderrp     io.ReadCloser
	stdoutp     io.ReadCloser
	stdinPipe   io.WriteCloser
	cmd         *exec.Cmd
	log         *zap.Logger
	envs        map[string]string
	ptyMaster   *os.File
	pty         *execapi.PTYOptions
	wd          string
	state       string
	command     string
	pid         int
	mu          sync.RWMutex
	ptyClose    sync.Once
	stopped     atomic.Bool
	stdoutOwned bool
}

// NewProcessExecutor creates a new process executor
func NewProcessExecutor(log *zap.Logger, opts ...Option) *ProcessExecutor {
	e := &ProcessExecutor{
		state: notStarted,
		log:   log,
	}

	for _, opt := range opts {
		opt(e)
	}

	// Split command into executable and arguments
	cmdParts := parseCommand(e.command)
	if len(cmdParts) == 0 {
		cmdParts = []string{""}
	}

	// Create command with first part as executable and rest as arguments.
	// Stop()/Signal() still own lifecycle control.
	var command *exec.Cmd
	if len(cmdParts) > 1 {
		command = exec.CommandContext(context.Background(), cmdParts[0], cmdParts[1:]...) //nolint:gosec // G204: user-provided command.
	} else {
		command = exec.CommandContext(context.Background(), cmdParts[0]) //nolint:gosec // G204: user-provided command.
	}

	if e.envs != nil {
		// Use clean environment - only include explicitly configured variables
		// Do not inherit os.Environ() to prevent LD_PRELOAD, PATH hijacking
		command.Env = make([]string, 0, len(e.envs))
		for k, v := range e.envs {
			command.Env = append(command.Env, k+"="+v)
		}
	}

	if e.wd != "" {
		command.Dir = e.wd
	}

	// we can safely skip the error here
	// because we don't initialize stderrpipe twice or after the process was already started
	if e.pty == nil {
		e.stderrp, _ = command.StderrPipe()
	}

	// we can safely skip the error here
	// because we don't initialize stdoutpipe twice or after the process was already started
	if e.pty == nil {
		e.stdoutp, _ = command.StdoutPipe()
	}

	if e.pty == nil {
		ip, _ := command.StdinPipe()
		e.stdinPipe = ip
	}
	e.cmd = command

	return e
}

// Start implements exec.Process
func (e *ProcessExecutor) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pty != nil {
		width, height, _ := e.pty.Dimensions()
		master, err := pty.StartWithSize(e.cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
		if err != nil {
			e.stopped.Store(true)
			return err
		}
		e.ptyMaster = master
		e.stdinPipe, e.stdoutp = master, master
		e.stderrp = io.NopCloser(strings.NewReader(""))
	} else if err := e.cmd.Start(); err != nil {
		e.stopped.Store(true)
		return err
	}

	e.pid = e.cmd.Process.Pid
	e.state = running
	return nil
}

func (p *ptyProcess) Resize(width, height int) error {
	if err := execapi.ValidatePTYSize(width, height); err != nil {
		return err
	}
	p.mu.RLock()
	master := p.ptyMaster
	p.mu.RUnlock()
	if master == nil {
		return execapi.ErrPTYUnavailable
	}
	return pty.Setsize(master, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
}

// State returns the current state of the process
func (e *ProcessExecutor) State() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.state
}

// WriteStdin implements exec.Process
func (e *ProcessExecutor) WriteStdin(data []byte) error {
	e.mu.RLock()
	if e.state != running {
		state := e.state
		e.mu.RUnlock()
		e.log.Error("process is not running", zap.String("state", state))
		return ErrProcessNotRunning
	}
	stdin := e.stdinPipe
	e.mu.RUnlock()

	// Never hold the lifecycle lock across a potentially blocking OS write.
	// Stop/Signal must remain able to close or interrupt the process, which in
	// turn wakes this write with an error.
	n, err := stdin.Write(data)
	if err != nil {
		return err
	}

	e.log.Debug("written to stdin", zap.Int("bytes", n))

	return nil
}

// Signal implements exec.Process
func (e *ProcessExecutor) Signal(sig int) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.state != running {
		e.log.Error("process is not running", zap.String("state", e.state))
		return ErrProcessNotRunning
	}

	if e.pid <= 0 {
		e.log.Error("pid is not a positive int", zap.Int("pid", e.pid))
		return ErrInvalidPID
	}

	// we're using os.FindProcess to avoid touching e.cmd
	pp, err := os.FindProcess(e.pid)
	if err != nil {
		e.log.Error("error finding process", zap.Error(err))
		return err
	}

	err = pp.Signal(syscall.Signal(sig))
	if err != nil {
		e.log.Error("error sending signal", zap.Error(err))
		return err
	}

	return nil
}

// Stderr implements exec.Process
func (e *ProcessExecutor) Stderr() io.ReadCloser {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.stderrp
}

// Stdout implements exec.Process
func (e *ProcessExecutor) Stdout() io.ReadCloser {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ptyMaster != nil && e.stdoutp != nil {
		e.stdoutOwned = true
	}

	return e.stdoutp
}

// Stop stops the process
func (e *ProcessExecutor) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pid <= 0 {
		e.closePTY()
		e.log.Warn("pid is not a positive int", zap.Int("pid", e.pid))
		return
	}

	if e.stopped.Load() {
		e.log.Warn("process already stopped")
		return
	}

	pp, err := os.FindProcess(e.pid)
	if err != nil {
		e.log.Error("error finding process", zap.Error(err))
		return
	}

	// kill the process
	_ = pp.Kill()
	// to prevent multiple calls to close()
	e.pid = 0
	e.state = terminated
	if e.ptyMaster != nil {
		e.closePTY()
	} else {
		if e.stdoutp != nil {
			_ = e.stdoutp.Close()
		}
		if e.stderrp != nil {
			_ = e.stderrp.Close()
		}
	}
	e.stopped.Store(true)
}

// Wait implements exec.Process
func (e *ProcessExecutor) Wait() error {
	err := e.cmd.Wait()
	if err != nil {
		e.log.Error("command wait error", zap.Error(err))
	}

	e.mu.Lock()
	// A PTY master is not one of os/exec's managed pipes. Wait releases an
	// unclaimed master; once Stdout hands it to a caller, that reader owns the
	// final drain and close so the child's last terminal frame is not truncated.
	if e.ptyMaster != nil && !e.stdoutOwned {
		e.closePTY()
	}
	e.state = terminated
	e.mu.Unlock()

	e.stopped.Store(true)
	e.log.Debug("command finished")

	return err
}

func (e *ProcessExecutor) closePTY() {
	e.ptyClose.Do(func() {
		if e.ptyMaster != nil {
			_ = e.ptyMaster.Close()
		}
	})
}

// parseCommand splits a command string into executable and arguments,
// handling quoted arguments properly
func parseCommand(cmd string) []string {
	if cmd == "" {
		return []string{""}
	}

	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return []string{}
	}

	if cmd == "\"\"" || cmd == "''" {
		return []string{""}
	}

	// Pre-allocate with estimated capacity
	estParts := 1 + strings.Count(cmd, " ")
	parts := make([]string, 0, estParts)

	var current strings.Builder
	current.Grow(len(cmd))

	inQuote := false
	quoteChar := rune(0)

	for _, c := range cmd {
		switch {
		case c == '"' || c == '\'':
			switch {
			case inQuote && c == quoteChar:
				inQuote = false
				quoteChar = 0
				if current.Len() == 0 {
					parts = append(parts, "")
				}
			case !inQuote:
				inQuote = true
				quoteChar = c
			default:
				current.WriteRune(c)
			}
		case c == ' ' && !inQuote:
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(c)
		}
	}

	// Handle unbalanced quotes
	if inQuote {
		if current.Len() == 0 {
			parts = append(parts, string(quoteChar))
		} else {
			// Prepend the quote character
			result := string(quoteChar) + current.String()
			parts = append(parts, result)
			return parts
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}
