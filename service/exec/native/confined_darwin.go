// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package native

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/service/exec/native/helperimage"
	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	confinedarwin "github.com/wippyai/runtime/service/exec/native/internal/confinement/darwin"
)

var darwinHelperPath string
var darwinHelperSHA256 string
var darwinHelperCDHash string
var darwinEmbeddedHelper struct {
	sync.Once
	path string
	err  error
}

const darwinSetupTimeout = 10 * time.Second

type darwinEntryBinding struct {
	roots map[string]*confinedarwin.BoundDirectory
}

type darwinConfinementLaunch struct {
	entry     *Executor
	policy    confinement.Policy
	workDir   string
	boundWork *confinedarwin.BoundPath
	process   *os.Process
	private   string
	wall      *time.Timer
	wallDone  chan struct{}
	wallOnce  sync.Once
	cleanup   sync.Once
	processMu sync.Mutex
	waitOnce  sync.Once
	waitDone  chan struct{}
	waitErr   error
}

func validateConfinementHost(entry *execapi.Confinement) error {
	policy, err := validateConfinementPolicy(entry)
	if err != nil {
		return err
	}
	if err := validateDarwinConfinementPolicy(policy); err != nil {
		return err
	}
	if _, err := verifiedDarwinHelper(); err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	return nil
}

func validateDarwinConfinementPolicy(policy confinement.Policy) error {
	if policy.NetworkNone {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS cannot yet prove total socket denial including socketpair"))
	}
	if policy.FS != nil {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS Seatbelt cannot pin filesystem grants across host path replacement"))
	}
	if policy.Limits.MemoryMiB > 0 || policy.Limits.PIDs > 0 {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS has no unprivileged aggregate job memory/task domain"))
	}
	return nil
}

func verifiedDarwinHelper() (string, error) {
	path := darwinHelperPath
	if path == "" && darwinHelperSHA256 != "" {
		darwinEmbeddedHelper.Do(func() {
			darwinEmbeddedHelper.path, darwinEmbeddedHelper.err = materializeDarwinHelper()
		})
		path = darwinEmbeddedHelper.path
		if darwinEmbeddedHelper.err != nil {
			return "", darwinEmbeddedHelper.err
		}
	}
	if path == "" || darwinHelperSHA256 == "" || darwinHelperCDHash == "" {
		return "", errors.New("runtime has no verified Darwin confinement helper")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != darwinHelperSHA256 {
		return "", errors.New("Darwin confinement helper digest mismatch")
	}
	codeHash, err := confinedarwin.StaticCDHash(path)
	if err != nil {
		return "", err
	}
	if codeHash != darwinHelperCDHash {
		return "", errors.New("Darwin confinement helper code identity mismatch")
	}
	return path, nil
}

func materializeDarwinHelper() (string, error) {
	runtimePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	runtimeFile, err := os.Open(runtimePath)
	if err != nil {
		return "", err
	}
	defer runtimeFile.Close()
	info, err := runtimeFile.Stat()
	if err != nil {
		return "", err
	}
	image, err := helperimage.Locate(runtimeFile, info.Size())
	if err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp("", "wippy-confine-helper-")
	if err != nil {
		return "", err
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(directory)
		}
	}()
	path := filepath.Join(directory, "confine-darwin")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o500)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, digest), image)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if hex.EncodeToString(digest.Sum(nil)) != darwinHelperSHA256 {
		return "", errors.New("embedded Darwin confinement helper digest mismatch")
	}
	failed = false
	return path, nil
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
	binding := &darwinEntryBinding{
		roots: make(map[string]*confinedarwin.BoundDirectory),
	}
	for _, root := range e.confine.WorkDirRoots {
		if _, exists := binding.roots[root]; exists {
			continue
		}
		bound, err := confinedarwin.BindDeclaredDirectory(root)
		if err != nil {
			_ = binding.Close()
			return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine.work_dir_roots"), err)
		}
		binding.roots[root] = bound
	}
	e.confineEntry = binding
	return nil
}

func (b *darwinEntryBinding) Close() error {
	var result error
	for _, root := range b.roots {
		result = errors.Join(result, root.Close())
	}
	return result
}

func (b *darwinEntryBinding) openWorkDir(path string) (*confinedarwin.BoundPath, error) {
	selected, ok := selectConfinementRoot(b.roots, path)
	if !ok {
		return nil, confinedarwin.ErrOutsideRoot
	}
	return selected.OpenDescendant(path)
}

func (e *Executor) prepareConfinement(process *ProcessExecutor, options execapi.ProcessOptions) error {
	if process.processGroup {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("process_group with macOS confinement has no safe post-reap signal identity"))
	}
	if process.pty != nil {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("PTY with macOS confinement is not yet supported by the verified launcher"))
	}
	if err := e.bindConfinementEntry(); err != nil {
		return err
	}
	policy, err := narrowConfinement(process, e.confine, options)
	if err != nil {
		return err
	}
	if err := validateDarwinConfinementPolicy(policy); err != nil {
		return err
	}
	process.confinement = &darwinConfinementLaunch{entry: e, policy: policy, workDir: process.wd}
	return nil
}

func (c *darwinConfinementLaunch) Start(process *ProcessExecutor) error {
	if process.cmd.Err != nil {
		return process.cmd.Err
	}
	helper, err := verifiedDarwinHelper()
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	c.entry.confineMu.RLock()
	binding, ok := c.entry.confineEntry.(*darwinEntryBinding)
	if !ok {
		c.entry.confineMu.RUnlock()
		return execapi.ErrConfineUnsupported
	}
	c.boundWork, err = binding.openWorkDir(c.workDir)
	if err != nil {
		c.entry.confineMu.RUnlock()
		return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("work_dir"), err)
	}
	c.entry.confineMu.RUnlock()
	if c.policy.HomePrivate {
		c.private, err = os.MkdirTemp("", "wippy-confine-private-")
		if err != nil {
			c.release()
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
		for _, name := range []string{"home", "tmp"} {
			if err := os.Mkdir(filepath.Join(c.private, name), 0o700); err != nil {
				c.release()
				return execapi.ErrConfineUnsupported.WithCause(err)
			}
		}
		if process.envs["HOME"] == confinement.PrivateHomePath {
			process.envs["HOME"] = filepath.Join(c.private, "home")
		}
		if process.envs["TMPDIR"] == confinement.PrivateTempPath {
			process.envs["TMPDIR"] = filepath.Join(c.private, "tmp")
		}
		rebuildProcessEnvironment(process)
	}
	profile := confinedarwin.CompileProfile(confinedarwin.Profile{
		AllowFork: c.policy.Limits.WallSec == 0 && !c.policy.KillOnOwnerExit,
	})
	policyReader, policyWriter, err := os.Pipe()
	if err != nil {
		c.release()
		return err
	}
	statusReader, statusWriter, err := os.Pipe()
	if err != nil {
		_ = policyReader.Close()
		_ = policyWriter.Close()
		c.release()
		return err
	}
	target := process.cmd
	stdin, stdinOK := target.Stdin.(*os.File)
	stdout, stdoutOK := target.Stdout.(*os.File)
	stderr, stderrOK := target.Stderr.(*os.File)
	if !stdinOK || !stdoutOK || !stderrOK {
		_ = policyReader.Close()
		_ = policyWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		c.release()
		return execapi.ErrConfineSetup.WithCause(errors.New("verified Darwin launcher requires file-backed standard streams"))
	}
	pid, err := confinedarwin.SpawnVerified(helper, darwinHelperCDHash,
		[6]*os.File{stdin, stdout, stderr, policyReader, statusWriter, c.boundWork.File()})
	if err != nil {
		_ = policyReader.Close()
		_ = policyWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		c.release()
		return execapi.ErrConfineSetup.WithCause(err)
	}
	c.process, err = os.FindProcess(pid)
	if err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_, _ = syscall.Wait4(pid, nil, 0, nil)
		_ = policyReader.Close()
		_ = policyWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		c.release()
		return execapi.ErrConfineSetup.WithCause(err)
	}
	process.pid = pid
	_ = policyReader.Close()
	_ = statusWriter.Close()
	encodeErr := json.NewEncoder(policyWriter).Encode(confinedarwin.HelperPolicy{
		Profile: profile, Path: target.Path, Argv: target.Args, Env: target.Env,
	})
	_ = policyWriter.Close()
	if encodeErr != nil {
		_ = c.process.Kill()
		_ = c.WaitProcess()
		_ = statusReader.Close()
		c.release()
		return execapi.ErrConfineSetup.WithCause(encodeErr)
	}
	ready := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(io.LimitReader(statusReader, 1<<16))
		line, readErr := reader.ReadString('\n')
		if readErr != nil || line != "READY\n" {
			ready <- fmt.Errorf("Seatbelt setup failed: %q: %w", line, readErr)
			return
		}
		next, readErr := reader.ReadString('\n')
		if readErr == io.EOF && next == "" {
			ready <- nil
			return
		}
		ready <- fmt.Errorf("target exec failed: %q: %w", next, readErr)
	}()
	select {
	case err := <-ready:
		_ = statusReader.Close()
		if err != nil {
			_ = c.process.Kill()
			_ = c.WaitProcess()
			c.release()
			return execapi.ErrConfineSetup.WithCause(err)
		}
	case <-time.After(darwinSetupTimeout):
		_ = c.process.Kill()
		_ = statusReader.Close()
		_ = c.WaitProcess()
		c.release()
		return execapi.ErrConfineSetup.WithCause(errors.New("Seatbelt setup timed out"))
	}
	process.releaseOutputWriters()
	if c.policy.Limits.WallSec > 0 {
		c.wallDone = make(chan struct{})
		c.wall = time.AfterFunc(time.Duration(c.policy.Limits.WallSec)*time.Second, func() {
			defer c.wallOnce.Do(func() { close(c.wallDone) })
			c.kill()
		})
	}
	return nil
}

func (c *darwinConfinementLaunch) WaitProcess() error {
	c.waitOnce.Do(func() {
		c.waitDone = make(chan struct{})
		defer close(c.waitDone)
		c.processMu.Lock()
		process := c.process
		c.processMu.Unlock()
		if process == nil {
			c.waitErr = ErrProcessNotRunning
			return
		}
		var status syscall.WaitStatus
		for {
			c.processMu.Lock()
			if c.process != process {
				c.processMu.Unlock()
				c.waitErr = ErrProcessNotRunning
				return
			}
			pid, err := syscall.Wait4(process.Pid, &status, syscall.WNOHANG, nil)
			if pid == process.Pid {
				// Reap and identity invalidation are atomic with respect to Signal
				// and the wall timer, so neither can hit a recycled numeric PID.
				c.process = nil
			}
			c.processMu.Unlock()
			if err == syscall.EINTR {
				continue
			}
			if err != nil {
				c.waitErr = err
				return
			}
			if pid == process.Pid {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if status.Signaled() {
			signal := int(status.Signal())
			c.waitErr = &ExitError{Code: 128 + signal, Signal: signal}
		} else if code := status.ExitStatus(); code != 0 {
			c.waitErr = &ExitError{Code: code}
		}
	})
	if c.waitDone != nil {
		<-c.waitDone
	}
	return c.waitErr
}

func (c *darwinConfinementLaunch) Signal(signal syscall.Signal) error {
	c.processMu.Lock()
	defer c.processMu.Unlock()
	if c.process == nil {
		return ErrProcessNotRunning
	}
	return c.process.Signal(signal)
}

func (c *darwinConfinementLaunch) Stop() {
	c.stopWall()
	c.kill()
	_ = c.WaitProcess()
	c.release()
}
func (c *darwinConfinementLaunch) Wait(waitErr error) error {
	c.stopWall()
	c.release()
	return waitErr
}

func (c *darwinConfinementLaunch) kill() {
	c.processMu.Lock()
	defer c.processMu.Unlock()
	if c.process == nil {
		return
	}
	_ = c.process.Kill()
}

func (c *darwinConfinementLaunch) stopWall() {
	if c.wall != nil {
		if c.wall.Stop() {
			c.wallOnce.Do(func() { close(c.wallDone) })
		}
		<-c.wallDone
	}
}

func (c *darwinConfinementLaunch) release() {
	c.cleanup.Do(func() {
		c.processMu.Lock()
		c.process = nil
		c.processMu.Unlock()
		if c.boundWork != nil {
			_ = c.boundWork.Close()
			c.boundWork = nil
		}
		if c.private != "" {
			_ = os.RemoveAll(c.private)
			c.private = ""
		}
	})
}
