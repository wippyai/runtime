// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package native

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	confinedarwin "github.com/wippyai/runtime/service/exec/native/internal/confinement/darwin"
)

var darwinHelperPath string
var darwinHelperSHA256 string

const darwinSetupTimeout = 10 * time.Second

type darwinEntryBinding struct {
	roots map[string]*confinedarwin.BoundDirectory
	read  map[string]*confinedarwin.BoundDirectory
	write map[string]*confinedarwin.BoundDirectory
	exec  map[string]*confinedarwin.BoundDirectory
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
	group     bool
}

func validateConfinementHost(entry *execapi.Confinement) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	policy := confinement.FromEntry(entry)
	if err := confinement.ValidateEntry(policy); err != nil {
		return execapi.NewInvalidConfinementError("confine")
	}
	if policy.NetworkNone {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS cannot yet prove total socket denial including socketpair"))
	}
	if policy.Limits.MemoryMiB > 0 || policy.Limits.PIDs > 0 {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS has no unprivileged aggregate job memory/task domain"))
	}
	if _, err := verifiedDarwinHelper(); err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	return validateDarwinPaths(policy)
}

func validateDarwinPaths(policy confinement.Policy) error {
	if policy.FS == nil {
		return nil
	}
	for _, class := range []confinement.Access{policy.FS.Read, policy.FS.Write, policy.FS.Exec} {
		for _, path := range class.Paths {
			if path == "{home}" || path == "{tmp}" {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine.fs"), err)
			}
			if !info.IsDir() {
				return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS file-granular grants are unsupported"))
			}
		}
	}
	return nil
}

func verifiedDarwinHelper() (string, error) {
	if darwinHelperPath == "" || darwinHelperSHA256 == "" {
		return "", errors.New("runtime has no verified Darwin confinement helper")
	}
	payload, err := os.ReadFile(darwinHelperPath)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != darwinHelperSHA256 {
		return "", errors.New("Darwin confinement helper digest mismatch")
	}
	return darwinHelperPath, nil
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
		read:  make(map[string]*confinedarwin.BoundDirectory),
		write: make(map[string]*confinedarwin.BoundDirectory),
		exec:  make(map[string]*confinedarwin.BoundDirectory),
	}
	for _, root := range e.confine.WorkDirRoots {
		bound, err := confinedarwin.BindDeclaredDirectory(root)
		if err != nil {
			_ = binding.Close()
			return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine.work_dir_roots"), err)
		}
		binding.roots[root] = bound
	}
	if e.confine.FS != nil {
		fs := confinement.FromEntry(e.confine).EffectiveFilesystem()
		for _, class := range []struct {
			paths []string
			into  map[string]*confinedarwin.BoundDirectory
		}{{fs.Read.Paths, binding.read}, {fs.Write.Paths, binding.write}, {fs.Exec.Paths, binding.exec}} {
			for _, path := range class.paths {
				if path == "{home}" || path == "{tmp}" || class.into[path] != nil {
					continue
				}
				bound, err := confinedarwin.BindDeclaredDirectory(path)
				if err != nil {
					_ = binding.Close()
					return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine.fs"), err)
				}
				class.into[path] = bound
			}
		}
	}
	e.confineEntry = binding
	return nil
}

func (b *darwinEntryBinding) Close() error {
	var result error
	for _, class := range []map[string]*confinedarwin.BoundDirectory{b.roots, b.read, b.write, b.exec} {
		for _, root := range class {
			result = errors.Join(result, root.Close())
		}
	}
	return result
}

func (b *darwinEntryBinding) canonical(path string, class map[string]*confinedarwin.BoundDirectory) (string, error) {
	var selected *confinedarwin.BoundDirectory
	selectedLength := -1
	for base, root := range class {
		rel, err := filepath.Rel(base, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && len(base) > selectedLength {
			selected, selectedLength = root, len(base)
		}
	}
	if selected == nil {
		return "", confinedarwin.ErrOutsideRoot
	}
	return selected.CanonicalDescendant(path)
}

func (b *darwinEntryBinding) openWorkDir(path string) (*confinedarwin.BoundPath, error) {
	var selected *confinedarwin.BoundDirectory
	selectedLength := -1
	for base, root := range b.roots {
		rel, err := filepath.Rel(base, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && len(base) > selectedLength {
			selected, selectedLength = root, len(base)
		}
	}
	if selected == nil {
		return nil, confinedarwin.ErrOutsideRoot
	}
	return selected.OpenDescendant(path)
}

func (e *Executor) prepareConfinement(process *ProcessExecutor, options execapi.ProcessOptions) error {
	if err := e.bindConfinementEntry(); err != nil {
		return err
	}
	policy, err := narrowConfinement(process, e.confine, options)
	if err != nil {
		return err
	}
	if policy.NetworkNone {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS cannot yet prove total socket denial including socketpair"))
	}
	if policy.Limits.MemoryMiB > 0 || policy.Limits.PIDs > 0 {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("macOS has no unprivileged aggregate job memory/task domain"))
	}
	if err := validateDarwinPaths(policy); err != nil {
		return err
	}
	process.confinement = &darwinConfinementLaunch{
		entry: e, policy: policy, workDir: process.wd, group: process.processGroup,
	}
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
	grants, err := c.compileGrants(binding)
	c.entry.confineMu.RUnlock()
	if err != nil {
		c.release()
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	if c.policy.HomePrivate || hasDarwinPrivateGrant(c.policy, "{home}") || hasDarwinPrivateGrant(c.policy, "{tmp}") {
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
		grants = replaceDarwinPrivateGrants(grants, c.private)
		for name, value := range process.envs {
			switch value {
			case confinement.PrivateHomePath:
				process.envs[name] = filepath.Join(c.private, "home")
			case confinement.PrivateTempPath:
				process.envs[name] = filepath.Join(c.private, "tmp")
			}
		}
		rebuildProcessEnvironment(process)
	}
	profile, err := confinedarwin.CompileProfile(confinedarwin.Profile{
		Grants: grants, FilesystemUnrestricted: c.policy.FS == nil,
		NetworkUnrestricted: !c.policy.NetworkNone,
		AllowFork:           c.policy.Limits.WallSec == 0 && !c.policy.KillOnOwnerExit,
	})
	if err != nil {
		c.release()
		return execapi.ErrConfineSetup.WithCause(err)
	}
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
	command := exec.CommandContext(context.Background(), helper)
	command.Env = []string{}
	command.ExtraFiles = []*os.File{policyReader, statusWriter, c.boundWork.File()}
	command.Stdin, command.Stdout, command.Stderr = target.Stdin, target.Stdout, target.Stderr
	if process.processGroup && process.pty == nil {
		applyProcessGroup(command)
	}
	process.cmd = command
	start := func() error {
		if process.pty == nil {
			return command.Start()
		}
		width, height, _ := process.pty.Dimensions()
		master, startErr := pty.StartWithSize(command, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
		if startErr == nil {
			process.ptyMaster = master
			process.stdinPipe, process.stdoutp = master, master
			process.stderrp = io.NopCloser(strings.NewReader(""))
		}
		return startErr
	}
	if err := start(); err != nil {
		_ = policyReader.Close()
		_ = policyWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		c.release()
		return err
	}
	c.process = command.Process
	_ = policyReader.Close()
	_ = statusWriter.Close()
	encodeErr := json.NewEncoder(policyWriter).Encode(confinedarwin.HelperPolicy{
		Profile: profile, Path: target.Path, Argv: target.Args, Env: target.Env,
	})
	_ = policyWriter.Close()
	if encodeErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
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
			_ = command.Process.Kill()
			_ = command.Wait()
			c.release()
			return execapi.ErrConfineSetup.WithCause(err)
		}
	case <-time.After(darwinSetupTimeout):
		_ = command.Process.Kill()
		_ = statusReader.Close()
		_ = command.Wait()
		c.release()
		return execapi.ErrConfineSetup.WithCause(errors.New("Seatbelt setup timed out"))
	}
	if process.pty == nil {
		process.releaseOutputWriters()
	}
	if c.policy.Limits.WallSec > 0 {
		c.wallDone = make(chan struct{})
		c.wall = time.AfterFunc(time.Duration(c.policy.Limits.WallSec)*time.Second, func() {
			defer c.wallOnce.Do(func() { close(c.wallDone) })
			c.kill()
		})
	}
	return nil
}

func (c *darwinConfinementLaunch) compileGrants(binding *darwinEntryBinding) ([]confinedarwin.Grant, error) {
	if c.policy.FS == nil {
		return nil, nil
	}
	fs := c.policy.EffectiveFilesystem()
	merged := make(map[string]confinedarwin.Grant)
	for _, class := range []struct {
		paths []string
		roots map[string]*confinedarwin.BoundDirectory
		kind  byte
	}{{fs.Read.Paths, binding.read, 'r'}, {fs.Write.Paths, binding.write, 'w'}, {fs.Exec.Paths, binding.exec, 'x'}} {
		for _, path := range class.paths {
			canonical := path
			if path != "{home}" && path != "{tmp}" {
				var err error
				canonical, err = binding.canonical(path, class.roots)
				if err != nil {
					return nil, err
				}
			}
			grant := merged[canonical]
			grant.Path = canonical
			switch class.kind {
			case 'r':
				grant.Read = true
			case 'w':
				grant.Write, grant.Read = true, true
			case 'x':
				grant.Exec = true
			}
			merged[canonical] = grant
		}
	}
	result := make([]confinedarwin.Grant, 0, len(merged))
	for _, grant := range merged {
		result = append(result, grant)
	}
	return result, nil
}

func hasDarwinPrivateGrant(policy confinement.Policy, placeholder string) bool {
	if policy.FS == nil {
		return false
	}
	fs := policy.EffectiveFilesystem()
	return slices.Contains(fs.Read.Paths, placeholder) || slices.Contains(fs.Write.Paths, placeholder) || slices.Contains(fs.Exec.Paths, placeholder)
}

func replaceDarwinPrivateGrants(grants []confinedarwin.Grant, root string) []confinedarwin.Grant {
	for i := range grants {
		switch grants[i].Path {
		case "{home}":
			grants[i].Path = filepath.Join(root, "home")
		case "{tmp}":
			grants[i].Path = filepath.Join(root, "tmp")
		}
	}
	return grants
}

func (c *darwinConfinementLaunch) Signal(signal syscall.Signal) error {
	c.processMu.Lock()
	defer c.processMu.Unlock()
	if c.process == nil {
		return ErrProcessNotRunning
	}
	if c.group {
		return signalProcessGroup(c.process.Pid, signal)
	}
	return c.process.Signal(signal)
}

func (c *darwinConfinementLaunch) Stop() { c.stopWall(); c.kill(); c.release() }
func (c *darwinConfinementLaunch) Wait(waitErr error) error {
	c.stopWall()
	c.kill()
	c.release()
	return waitErr
}

func (c *darwinConfinementLaunch) kill() {
	c.processMu.Lock()
	defer c.processMu.Unlock()
	if c.process == nil {
		return
	}
	if c.group {
		_ = signalProcessGroup(c.process.Pid, syscall.SIGKILL)
	} else {
		_ = c.process.Kill()
	}
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
