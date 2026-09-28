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
	policy     confinement.Policy
	workDir    *confinewindows.BoundPath
	job        *confinewindows.Job
	wall       *time.Timer
	private    string
	sandbox    *confinewindows.Sandbox
	spawned    *confinewindows.SpawnedProcess
	grants     []*confinewindows.PathGrant
	cleanup    sync.Once
	cleanupErr error
	wallDone   chan struct{}
	wallOnce   sync.Once
	waitOnce   sync.Once
	waitDone   chan struct{}
	waitErr    error
	stateLock  sync.Mutex
}

func validateConfinementHost(entry *execapi.Confinement) error {
	policy, err := validateConfinementPolicy(entry)
	if err != nil {
		return err
	}
	return validateWindowsConfinementPolicy(policy, nil)
}

func validateWindowsConfinementPolicy(policy confinement.Policy, values map[string]string) error {
	if policy.FS != nil {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows LPAC cannot enforce object-bound filesystem grants"))
	}
	if policy.NetworkNone {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows cannot yet prove total socket denial"))
	}
	if policy.Limits.PIDs > 0 {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("Windows Job Objects limit processes, not policy tasks"))
	}
	if err := validateWindowsConfinementEnvironment(policy, values); err != nil {
		return err
	}
	return nil
}

func validateWindowsConfinementEnvironment(policy confinement.Policy, values map[string]string) error {
	isManaged := func(name string) bool {
		switch strings.ToUpper(name) {
		case "SYSTEMROOT", "LOCALAPPDATA", "TEMP", "TMP":
			return true
		default:
			return false
		}
	}
	if policy.Env != nil {
		for _, name := range policy.Env.Allow {
			if isManaged(name) {
				return execapi.NewInvalidConfinementError("confine.env.allow")
			}
		}
		for name := range policy.Env.Set {
			if isManaged(name) {
				return execapi.NewInvalidConfinementError("confine.env.set")
			}
		}
	}
	for name := range values {
		if isManaged(name) {
			return execapi.NewInvalidConfinementError("confine.env")
		}
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
	selected, ok := selectConfinementRoot(b.roots, path)
	if !ok {
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
	if err := validateWindowsConfinementPolicy(policy, process.envs); err != nil {
		return err
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
	if err := confinewindows.RequireLowIntegrityDirectory(workDir.Path); err != nil {
		_ = workDir.Close()
		return execapi.ErrConfineUnsupported.WithCause(fmt.Errorf("Windows writable work directory: %w", err))
	}
	process.cmd.Dir = workDir.Path
	process.confinement = &windowsConfinementLaunch{policy: policy, workDir: workDir}
	return nil
}

func (c *windowsConfinementLaunch) Start(process *ProcessExecutor) error {
	if process.cmd.Err != nil {
		return c.cleanupCause(process.cmd.Err)
	}
	sandbox, err := confinewindows.NewSandbox()
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(c.cleanupCause(err))
	}
	c.sandbox = sandbox
	grant := func(path string, permissions windows.ACCESS_MASK) (*confinewindows.PathGrant, error) {
		pathGrant, grantErr := sandbox.GrantPath(path, permissions)
		if grantErr == nil {
			c.grants = append(c.grants, pathGrant)
		}
		return pathGrant, grantErr
	}
	workspaceAccess := windows.ACCESS_MASK(windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE |
		windows.FILE_GENERIC_EXECUTE | windows.DELETE)
	if c.policy.HomePrivate {
		private, err := sandbox.PrivateHome()
		if err != nil {
			return execapi.ErrConfineUnsupported.WithCause(c.cleanupCause(err))
		}
		if err := confinewindows.SetLowIntegrityDirectory(private); err != nil {
			cause := fmt.Errorf("provision LPAC private home integrity: %w", err)
			return execapi.ErrConfineUnsupported.WithCause(c.cleanupCause(cause))
		}
		if _, err := grant(private, workspaceAccess); err != nil {
			cause := fmt.Errorf("grant LPAC private home: %w", err)
			return execapi.ErrConfineUnsupported.WithCause(c.cleanupCause(cause))
		}
		c.private = private
		for _, name := range []string{"HOME", "USERPROFILE"} {
			process.envs[name] = private
		}
		rebuildProcessEnvironment(process)
	}
	if _, err := grant(c.workDir.Path, workspaceAccess); err != nil {
		cause := fmt.Errorf("grant LPAC work directory: %w", err)
		return execapi.ErrConfineUnsupported.WithCause(c.cleanupCause(cause))
	}
	executable, err := resolveWindowsExecutable(process.cmd.Path, c.workDir.Path)
	if err != nil {
		return execapi.ErrConfineSetup.WithCause(c.cleanupCause(err))
	}
	executableGrant, err := grant(executable, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_EXECUTE)
	if err != nil {
		cause := fmt.Errorf("grant LPAC executable: %w", err)
		return execapi.ErrConfineUnsupported.WithCause(c.cleanupCause(cause))
	}
	job, err := confinewindows.NewJob(c.policy.Limits.MemoryMiB)
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(c.cleanupCause(err))
	}
	c.job = job
	stdin, stdinOK := process.cmd.Stdin.(*os.File)
	stdout, stdoutOK := process.cmd.Stdout.(*os.File)
	stderr, stderrOK := process.cmd.Stderr.(*os.File)
	if !stdinOK || !stdoutOK || !stderrOK {
		cause := errors.New("Windows LPAC launch requires file-backed standard streams")
		return execapi.ErrConfineSetup.WithCause(c.cleanupCause(cause))
	}
	spawned, err := sandbox.SpawnSuspended(confinewindows.SpawnRequest{
		Path: executableGrant.Path, Args: process.cmd.Args, Env: process.cmd.Env, WorkDir: c.workDir.Path,
		Stdin: windows.Handle(stdin.Fd()), Stdout: windows.Handle(stdout.Fd()), Stderr: windows.Handle(stderr.Fd()),
		Job: job,
	})
	if err != nil {
		return execapi.ErrConfineSetup.WithCause(c.cleanupCause(err))
	}
	c.spawned = spawned
	process.pid = int(spawned.PID)
	fail := func(cause error) error {
		_ = c.job.Kill(windowsConfinedKillCode)
		_ = c.spawned.Kill(windowsConfinedKillCode)
		_, _ = c.spawned.Wait()
		return execapi.ErrConfineSetup.WithCause(c.cleanupCause(cause))
	}
	if err := sandbox.VerifySpawned(spawned, job); err != nil {
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
	return fmt.Errorf("signal %d is unsupported by Windows confined processes", signal)
}

func (c *windowsConfinementLaunch) Stop() {
	c.stopWall()
	c.stateLock.Lock()
	if c.job != nil {
		_ = c.job.Kill(windowsConfinedKillCode)
	}
	c.stateLock.Unlock()
	_ = c.WaitProcess()
	c.release()
}

func (c *windowsConfinementLaunch) Wait(waitErr error) error {
	c.stopWall()
	return joinExitFinalization(waitErr, c.release())
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

func (c *windowsConfinementLaunch) release() error {
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
			if err := job.WaitEmpty(5 * time.Second); err != nil {
				jobEmpty = false
				c.cleanupErr = errors.Join(c.cleanupErr, fmt.Errorf("wait for confinement Job cleanup: %w", err))
			}
			c.cleanupErr = errors.Join(c.cleanupErr, job.Close())
		}
		if spawned != nil {
			c.cleanupErr = errors.Join(c.cleanupErr, spawned.Close())
		}
		for index := len(c.grants) - 1; index >= 0; index-- {
			c.cleanupErr = errors.Join(c.cleanupErr, c.grants[index].Close())
		}
		c.grants = nil
		if c.sandbox != nil {
			c.cleanupErr = errors.Join(c.cleanupErr, c.sandbox.Close())
			c.sandbox = nil
		}
		if c.private != "" && jobEmpty {
			c.cleanupErr = errors.Join(c.cleanupErr, os.RemoveAll(c.private))
			c.private = ""
		}
		if c.workDir != nil {
			c.cleanupErr = errors.Join(c.cleanupErr, c.workDir.Close())
			c.workDir = nil
		}
	})
	return c.cleanupErr
}

func (c *windowsConfinementLaunch) cleanupCause(cause error) error {
	return errors.Join(cause, c.release())
}

func resolveWindowsExecutable(path, workDir string) (string, error) {
	if path == "" || workDir == "" {
		return "", errors.New("empty Windows executable or working directory")
	}
	upper := strings.ToUpper(path)
	volume := filepath.VolumeName(path)
	rest := strings.TrimPrefix(path, volume)
	if strings.HasPrefix(upper, `\\?\`) || strings.HasPrefix(upper, `\\.\`) ||
		strings.HasPrefix(upper, `\\`) || strings.Contains(rest, ":") {
		return "", errors.New("device, UNC, and alternate-data-stream executable paths are unsupported")
	}
	if volume != "" && !filepath.IsAbs(path) {
		return "", errors.New("drive-relative executable paths are unsupported")
	}
	if volume == "" && strings.HasPrefix(rest, `\`) {
		return "", errors.New("current-drive-relative executable paths are unsupported")
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workDir, candidate)
	}
	candidate = filepath.Clean(candidate)
	resolved, err := resolveWindowsExecutableExtension(candidate)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func resolveWindowsExecutableExtension(path string) (string, error) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	extensions := filepath.SplitList(os.Getenv("PATHEXT"))
	if len(extensions) == 0 {
		extensions = []string{".com", ".exe", ".bat", ".cmd"}
	}
	for _, extension := range extensions {
		if extension == "" {
			continue
		}
		if extension[0] != '.' {
			extension = "." + extension
		}
		candidate := path + extension
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("executable %q does not exist", path)
}

var _ io.Closer = (*windowsEntryBinding)(nil)
