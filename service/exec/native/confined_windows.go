// SPDX-License-Identifier: MPL-2.0

//go:build windows

package native

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	confinewindows "github.com/wippyai/runtime/service/exec/native/internal/confinement/windows"
	"golang.org/x/sys/windows"
)

const windowsConfinedKillCode = 137

type windowsEntryBinding struct {
	roots map[string]*confinewindows.BoundDirectory
}

type windowsConfinementLaunch struct {
	policy    confinement.Policy
	workDir   *confinewindows.BoundPath
	job       *confinewindows.Job
	wall      *time.Timer
	private   string
	sandbox   *confinewindows.Sandbox
	spawned   *confinewindows.SpawnedProcess
	revokers  []func() error
	cleanup   sync.Once
	wallDone  chan struct{}
	wallOnce  sync.Once
	waitOnce  sync.Once
	waitDone  chan struct{}
	waitErr   error
	stateLock sync.Mutex
}

func validateConfinementHost(entry *execapi.Confinement) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	policy := confinement.FromEntry(entry)
	if err := confinement.ValidateEntry(policy); err != nil {
		return execapi.NewInvalidConfinementError("confine")
	}
	if policy.FS != nil {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows filesystem grants require the LPAC backend"))
	}
	if policy.NetworkNone {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows cannot yet prove total socket denial"))
	}
	if policy.Limits.PIDs > 0 {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows Job Objects limit processes, not policy tasks"))
	}
	return nil
}

func (e *Executor) bindConfinementEntry() error {
	e.confineMu.Lock()
	defer e.confineMu.Unlock()
	if e.confineEntry != nil {
		return nil
	}
	if e.confineClosed {
		return execapi.ErrConfineUnsupported
	}
	if err := validateConfinementHost(e.confine); err != nil {
		return err
	}
	binding := &windowsEntryBinding{roots: make(map[string]*confinewindows.BoundDirectory)}
	for _, path := range e.confine.WorkDirRoots {
		if binding.roots[path] != nil {
			continue
		}
		root, err := confinewindows.BindDeclaredDirectory(path)
		if err != nil {
			_ = binding.Close()
			return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine.work_dir_roots"), err)
		}
		binding.roots[path] = root
	}
	e.confineEntry = binding
	return nil
}

func (b *windowsEntryBinding) Close() error {
	var result error
	for _, root := range b.roots {
		result = errors.Join(result, root.Close())
	}
	return result
}

func (b *windowsEntryBinding) openWorkDir(path string) (*confinewindows.BoundPath, error) {
	var selected *confinewindows.BoundDirectory
	selectedLength := -1
	for base, root := range b.roots {
		rel, err := filepath.Rel(base, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && len(base) > selectedLength {
			selected, selectedLength = root, len(base)
		}
	}
	if selected == nil {
		return nil, confinewindows.ErrOutsideRoot
	}
	return selected.OpenDescendant(path)
}

func (e *Executor) prepareConfinement(process *ProcessExecutor, options execapi.ProcessOptions) error {
	if process.processGroup {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("process_group with Windows confinement is not yet supported"))
	}
	if process.pty != nil {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("PTY with Windows confinement is not yet supported"))
	}
	if err := e.bindConfinementEntry(); err != nil {
		return err
	}
	policy, err := narrowConfinement(process, e.confine, options)
	if err != nil {
		return err
	}
	if policy.FS != nil {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows filesystem grants require the LPAC backend"))
	}
	if policy.NetworkNone {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows cannot yet prove total socket denial"))
	}
	if policy.Limits.PIDs > 0 {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows Job Objects limit processes, not policy tasks"))
	}
	e.confineMu.RLock()
	binding, ok := e.confineEntry.(*windowsEntryBinding)
	if !ok {
		e.confineMu.RUnlock()
		return execapi.ErrConfineUnsupported
	}
	workDir, err := binding.openWorkDir(process.wd)
	e.confineMu.RUnlock()
	if err != nil {
		return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("work_dir"), err)
	}
	process.cmd.Dir = workDir.Path
	process.confinement = &windowsConfinementLaunch{policy: policy, workDir: workDir}
	return nil
}

func (c *windowsConfinementLaunch) Start(process *ProcessExecutor) error {
	if process.cmd.Err != nil {
		c.release()
		return process.cmd.Err
	}
	if c.policy.HomePrivate {
		private, err := os.MkdirTemp("", "wippy-confine-private-")
		if err != nil {
			c.release()
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
		c.private = private
		for _, name := range []string{"HOME", "USERPROFILE"} {
			process.envs[name] = private
		}
		rebuildProcessEnvironment(process)
	}
	sandbox, err := confinewindows.NewSandbox()
	if err != nil {
		c.release()
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	c.sandbox = sandbox
	grant := func(path string, permissions windows.ACCESS_MASK) error {
		revoke, grantErr := sandbox.GrantPath(path, permissions)
		if grantErr == nil {
			c.revokers = append(c.revokers, revoke)
		}
		return grantErr
	}
	workspaceAccess := windows.ACCESS_MASK(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE |
		windows.FILE_GENERIC_EXECUTE | windows.DELETE)
	if err := grant(c.workDir.Path, workspaceAccess); err != nil {
		c.release()
		return execapi.ErrConfineUnsupported.WithCause(fmt.Errorf("grant LPAC work directory: %w", err))
	}
	if err := grant(process.cmd.Path, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_EXECUTE); err != nil {
		c.release()
		return execapi.ErrConfineUnsupported.WithCause(fmt.Errorf("grant LPAC executable: %w", err))
	}
	if c.private != "" {
		if err := grant(c.private, workspaceAccess); err != nil {
			c.release()
			return execapi.ErrConfineUnsupported.WithCause(fmt.Errorf("grant LPAC private home: %w", err))
		}
	}
	job, err := confinewindows.NewJob(c.policy.Limits.MemoryMiB)
	if err != nil {
		c.release()
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	c.job = job
	stdin, stdinOK := process.cmd.Stdin.(*os.File)
	stdout, stdoutOK := process.cmd.Stdout.(*os.File)
	stderr, stderrOK := process.cmd.Stderr.(*os.File)
	if !stdinOK || !stdoutOK || !stderrOK {
		c.release()
		return execapi.ErrConfineSetup.WithCause(errors.New("Windows LPAC launch requires file-backed standard streams"))
	}
	spawned, err := sandbox.SpawnSuspended(confinewindows.SpawnRequest{
		Path: process.cmd.Path, Args: process.cmd.Args, Env: process.cmd.Env, WorkDir: c.workDir.Path,
		Stdin: windows.Handle(stdin.Fd()), Stdout: windows.Handle(stdout.Fd()), Stderr: windows.Handle(stderr.Fd()),
		Job: job,
	})
	if err != nil {
		c.release()
		return execapi.ErrConfineSetup.WithCause(err)
	}
	c.spawned = spawned
	process.pid = int(spawned.PID)
	fail := func(cause error) error {
		_ = c.job.Kill(windowsConfinedKillCode)
		_ = c.spawned.Kill(windowsConfinedKillCode)
		_, _ = c.spawned.Wait()
		c.release()
		return execapi.ErrConfineSetup.WithCause(cause)
	}
	if err := spawned.Verify(job); err != nil {
		return fail(err)
	}
	if err := spawned.Resume(); err != nil {
		return fail(fmt.Errorf("resume confined target: %w", err))
	}
	process.releaseOutputWriters()
	if c.policy.Limits.WallSec > 0 {
		c.wallDone = make(chan struct{})
		c.wall = time.AfterFunc(time.Duration(c.policy.Limits.WallSec)*time.Second, func() {
			defer c.wallOnce.Do(func() { close(c.wallDone) })
			c.stateLock.Lock()
			job := c.job
			c.stateLock.Unlock()
			if job != nil {
				_ = job.Kill(windowsConfinedKillCode)
			}
		})
	}
	return nil
}

func (c *windowsConfinementLaunch) Signal(signal syscall.Signal) error {
	c.stateLock.Lock()
	defer c.stateLock.Unlock()
	if c.spawned == nil {
		return ErrProcessNotRunning
	}
	if signal == syscall.SIGKILL {
		return c.job.Kill(windowsConfinedKillCode)
	}
	return c.spawned.Kill(uint32(signal))
}

func (c *windowsConfinementLaunch) Stop() {
	c.stopWall()
	c.stateLock.Lock()
	if c.job != nil {
		_ = c.job.Kill(windowsConfinedKillCode)
	}
	c.stateLock.Unlock()
	c.release()
}

func (c *windowsConfinementLaunch) Wait(waitErr error) error {
	c.stopWall()
	c.release()
	return waitErr
}

func (c *windowsConfinementLaunch) WaitProcess() error {
	c.waitOnce.Do(func() {
		c.waitDone = make(chan struct{})
		defer close(c.waitDone)
		c.stateLock.Lock()
		spawned := c.spawned
		c.stateLock.Unlock()
		if spawned == nil {
			c.waitErr = ErrProcessNotRunning
			return
		}
		code, err := spawned.Wait()
		if err != nil {
			c.waitErr = err
			return
		}
		if code != 0 {
			c.waitErr = &ExitError{Code: int(code)}
		}
	})
	if c.waitDone != nil {
		<-c.waitDone
	}
	return c.waitErr
}

func (c *windowsConfinementLaunch) stopWall() {
	if c.wall != nil {
		if c.wall.Stop() {
			c.wallOnce.Do(func() { close(c.wallDone) })
		}
		<-c.wallDone
	}
}

func (c *windowsConfinementLaunch) release() {
	c.cleanup.Do(func() {
		c.stateLock.Lock()
		job := c.job
		c.job = nil
		spawned := c.spawned
		c.spawned = nil
		c.stateLock.Unlock()
		jobEmpty := true
		if job != nil {
			_ = job.Kill(windowsConfinedKillCode)
			jobEmpty = job.WaitEmpty(5*time.Second) == nil
			_ = job.Close()
		}
		if spawned != nil {
			_ = spawned.Close()
		}
		if c.workDir != nil {
			_ = c.workDir.Close()
			c.workDir = nil
		}
		if c.private != "" && jobEmpty {
			_ = os.RemoveAll(c.private)
			c.private = ""
		}
		for index := len(c.revokers) - 1; index >= 0; index-- {
			_ = c.revokers[index]()
		}
		c.revokers = nil
		if c.sandbox != nil {
			_ = c.sandbox.Close()
			c.sandbox = nil
		}
	})
}

var _ io.Closer = (*windowsEntryBinding)(nil)
