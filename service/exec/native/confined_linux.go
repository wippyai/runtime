// SPDX-License-Identifier: MPL-2.0

package native

import (
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
	confinelinux "github.com/wippyai/runtime/service/exec/native/internal/confinement/linux"
)

// Set by the release build from the separately built helper artifact. An
// unstamped build cannot silently turn an entry's confine block into a no-op.
var linuxHelperSHA256 string
var linuxHelperPath string

type linuxConfinementLaunch struct {
	entry       *Executor
	group       *confinelinux.Cgroup
	wall        *time.Timer
	wallDone    chan struct{}
	supervisor  *os.Process
	exitReport  *os.File
	workDir     string
	root        string
	hostPrivate string
	policy      confinement.Policy
	cleanup     sync.Mutex
	identity    sync.Mutex
	wallOnce    sync.Once
	targetPIDFD int
}

type linuxEntryBinding struct {
	roots map[string]*confinelinux.BoundDirectory
	read  map[string]*confinelinux.BoundDirectory
	write map[string]*confinelinux.BoundDirectory
	exec  map[string]*confinelinux.BoundDirectory
}

type linuxLaunchFiles struct {
	workdir       *os.File
	workDirSource string
	sources       []*os.File
	grants        []confinelinux.HelperGrant
	private       []confinelinux.PrivateGrant
}

func (f *linuxLaunchFiles) Close() {
	if f == nil {
		return
	}
	if f.workdir != nil {
		_ = f.workdir.Close()
	}
	for _, source := range f.sources {
		_ = source.Close()
	}
}

func bindLinuxFilesystem(fs confinement.Filesystem) (*linuxEntryBinding, error) {
	binding := &linuxEntryBinding{
		roots: make(map[string]*confinelinux.BoundDirectory),
		read:  make(map[string]*confinelinux.BoundDirectory),
		write: make(map[string]*confinelinux.BoundDirectory),
		exec:  make(map[string]*confinelinux.BoundDirectory),
	}
	for _, class := range []struct {
		pins  map[string]*confinelinux.BoundDirectory
		paths []string
	}{
		{binding.read, fs.Read.Paths},
		{binding.write, fs.Write.Paths},
		{binding.exec, fs.Exec.Paths},
	} {
		for _, path := range class.paths {
			if path == confinement.PrivateHomePath || path == confinement.PrivateTempPath || class.pins[path] != nil {
				continue
			}
			grant, err := confinelinux.BindDeclaredDirectory(path)
			if err != nil {
				_ = binding.Close()
				return nil, err
			}
			class.pins[path] = grant
		}
	}
	return binding, nil
}

func (b *linuxEntryBinding) Close() error {
	var result error
	for _, root := range b.roots {
		result = errors.Join(result, root.Close())
	}
	for _, class := range []map[string]*confinelinux.BoundDirectory{b.read, b.write, b.exec} {
		for _, grant := range class {
			result = errors.Join(result, grant.Close())
		}
	}
	return result
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
	binding := &linuxEntryBinding{
		roots: make(map[string]*confinelinux.BoundDirectory),
		read:  make(map[string]*confinelinux.BoundDirectory),
		write: make(map[string]*confinelinux.BoundDirectory),
		exec:  make(map[string]*confinelinux.BoundDirectory),
	}
	for _, path := range e.confine.WorkDirRoots {
		if binding.roots[path] != nil {
			continue
		}
		root, err := confinelinux.BindDeclaredDirectory(path)
		if err != nil {
			_ = binding.Close()
			return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine.work_dir_roots"), err)
		}
		binding.roots[path] = root
	}
	if e.confine.FS == nil {
		e.confineEntry = binding
		return nil
	}
	grants, err := bindLinuxFilesystem(confinement.ExpandPrivatePolicy(confinement.FromEntry(e.confine)).EffectiveFilesystem())
	if err != nil {
		_ = binding.Close()
		return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine.fs"), err)
	}
	binding.read, binding.write, binding.exec = grants.read, grants.write, grants.exec
	grants.read, grants.write, grants.exec = nil, nil, nil
	e.confineEntry = binding
	return nil
}

func (b *linuxEntryBinding) openGrant(path string, read, write, execute bool) (*os.File, string, error) {
	var selected *os.File
	var source string
	for _, class := range []struct {
		pins   map[string]*confinelinux.BoundDirectory
		needed bool
	}{
		{b.read, read}, {b.write, write}, {b.exec, execute},
	} {
		if !class.needed {
			continue
		}
		candidate, canonical, err := openBoundDescendant(class.pins, path)
		if err != nil {
			if selected != nil {
				_ = selected.Close()
			}
			return nil, "", err
		}
		if selected == nil {
			selected, source = candidate, canonical
			continue
		}
		same, err := confinelinux.SameOpenDirectoryFDs(int(selected.Fd()), int(candidate.Fd()))
		_ = candidate.Close()
		if err != nil || !same {
			_ = selected.Close()
			if err != nil {
				return nil, "", fmt.Errorf("compare cross-right grant identity: %w", err)
			}
			return nil, "", errors.New("cross-right grant identity changed")
		}
	}
	if selected == nil {
		return nil, "", confinelinux.ErrOutsideRoot
	}
	return selected, source, nil
}

func (b *linuxEntryBinding) openWorkDir(path string) (*os.File, string, error) {
	file, canonical, err := openBoundDescendant(b.roots, path)
	if err == nil {
		if specialErr := confinelinux.RejectSpecialDirectoryFD(int(file.Fd())); specialErr != nil {
			_ = file.Close()
			return nil, "", specialErr
		}
	}
	return file, canonical, err
}

func openBoundDescendant(roots map[string]*confinelinux.BoundDirectory, path string) (*os.File, string, error) {
	selected, ok := selectConfinementRoot(roots, path)
	if !ok {
		return nil, "", confinelinux.ErrOutsideRoot
	}
	return selected.OpenDescendant(path)
}

func openVerifiedConfinementHelper() (*os.File, error) {
	if linuxHelperPath != "" {
		return confinelinux.OpenVerifiedHelper(linuxHelperPath, linuxHelperSHA256)
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return confinelinux.OpenVerifiedEmbeddedHelper(binary, linuxHelperSHA256)
}

func validateConfinementHost(entry *execapi.Confinement) error {
	policy, err := validateConfinementPolicy(entry)
	if err != nil {
		return err
	}
	if err := validateLinuxConfinementPaths(entry); err != nil {
		return err
	}
	if entry.FS != nil {
		if abi, err := confinelinux.LandlockABI(); err != nil {
			return execapi.ErrConfineUnsupported.WithCause(err)
		} else if abi < 5 {
			return execapi.ErrConfineUnsupported.WithCause(fmt.Errorf("landlock ABI %d is below 5", abi))
		}
	}
	if entry.Limits != nil && (entry.Limits.MemoryMiB > 0 || entry.Limits.PIDs > 0) {
		if err := confinelinux.ProbeCgroupDelegation(policy.Limits); err != nil {
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
	}
	if linuxHelperSHA256 == "" {
		return execapi.ErrConfineUnsupported
	}
	helper, err := openVerifiedConfinementHelper()
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	return helper.Close()
}

func validateLinuxConfinementPaths(entry *execapi.Confinement) error {
	var classes [][]string
	if entry.FS != nil {
		classes = [][]string{entry.FS.Read, entry.FS.Write, entry.FS.Exec}
	}
	for _, class := range classes {
		for _, path := range class {
			if path == "{home}" {
				if entry.Home != "private" {
					return execapi.NewInvalidConfinementError("confine.fs")
				}
				continue
			}
			if path == "{tmp}" {
				continue
			}
			if path == "/" || path == confinement.PrivateRootPath ||
				strings.HasPrefix(path, confinement.PrivateRootPath+"/") {
				return execapi.ErrConfineUnsupported
			}
			if path == "/dev" || strings.HasPrefix(path, "/dev/") ||
				path == "/proc" || strings.HasPrefix(path, "/proc/") {
				return execapi.ErrConfineUnsupported
			}
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				// File-granular grants are valid policy, but need a distinct
				// pinned-file mount path. Reject the capability, not the entry.
				return execapi.ErrConfineUnsupported
			}
		}
	}
	return nil
}

func (e *Executor) prepareConfinement(process *ProcessExecutor, options execapi.ProcessOptions) error {
	if process.processGroup {
		return execapi.ErrConfineUnsupported.WithCause(errors.New("process_group with confinement is not yet supported"))
	}
	if err := e.bindConfinementEntry(); err != nil {
		return err
	}
	policy, err := narrowConfinement(process, e.confine, options)
	if err != nil {
		return err
	}
	policy = confinement.ExpandPrivatePolicy(policy)
	if policy.FS != nil {
		for _, class := range []confinement.Access{policy.FS.Read, policy.FS.Write, policy.FS.Exec} {
			for _, path := range class.Paths {
				if path != confinement.PrivateHomePath && path != confinement.PrivateTempPath &&
					strings.HasPrefix(path, confinement.PrivateRootPath+"/") {
					return execapi.NewInvalidConfinementError("confine.fs")
				}
			}
		}
	}
	fs := policy.EffectiveFilesystem()
	if policy.FS != nil {
		hostFS, _ := splitPrivateFilesystem(fs)
		if _, err := confinelinux.PlanBindMounts(hostFS); err != nil {
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
	}
	process.confinement = newLinuxConfinementLaunch(policy, process.wd, e)
	return nil
}

func newLinuxConfinementLaunch(policy confinement.Policy, workDir string, entry *Executor) *linuxConfinementLaunch {
	return &linuxConfinementLaunch{
		policy: policy, workDir: workDir, entry: entry,
		targetPIDFD: -1,
	}
}

func splitPrivateFilesystem(fs confinement.Filesystem) (confinement.Filesystem, []confinelinux.PrivateGrant) {
	host := fs
	private := make([]confinelinux.PrivateGrant, 0, 2)
	for _, path := range []string{confinement.PrivateHomePath, confinement.PrivateTempPath} {
		grant := confinelinux.PrivateGrant{
			Target: path,
			Read:   slices.Contains(fs.Read.Paths, path),
			Write:  slices.Contains(fs.Write.Paths, path),
			Exec:   slices.Contains(fs.Exec.Paths, path),
		}
		if grant.Read || grant.Write || grant.Exec {
			private = append(private, grant)
		}
	}
	withoutPrivate := func(paths []string) []string {
		out := make([]string, 0, len(paths))
		for _, path := range paths {
			if path != confinement.PrivateHomePath && path != confinement.PrivateTempPath {
				out = append(out, path)
			}
		}
		return out
	}
	host.Read.Paths = withoutPrivate(fs.Read.Paths)
	host.Write.Paths = withoutPrivate(fs.Write.Paths)
	host.Exec.Paths = withoutPrivate(fs.Exec.Paths)
	return host, private
}

func (c *linuxConfinementLaunch) bindLaunchFiles() (_ *linuxLaunchFiles, resultErr error) {
	var hostFS confinement.Filesystem
	var mounts []confinelinux.BindMount
	files := &linuxLaunchFiles{}
	if c.policy.FS != nil {
		hostFS, files.private = splitPrivateFilesystem(c.policy.EffectiveFilesystem())
		var err error
		mounts, err = confinelinux.PlanBindMounts(hostFS)
		if err != nil {
			return nil, execapi.ErrConfineUnsupported.WithCause(err)
		}
	}
	defer func() {
		if resultErr != nil {
			files.Close()
		}
	}()

	c.entry.confineMu.RLock()
	defer c.entry.confineMu.RUnlock()
	binding, ok := c.entry.confineEntry.(*linuxEntryBinding)
	if !ok {
		return nil, execapi.ErrConfineUnsupported
	}
	grantBinding := binding
	var transient *linuxEntryBinding
	if c.entry.confine.FS == nil && c.policy.FS != nil {
		var err error
		transient, err = bindLinuxFilesystem(hostFS)
		if err != nil {
			return nil, execapi.ErrConfineUnsupported.WithCause(err)
		}
		defer transient.Close()
		grantBinding = transient
	}
	for _, mount := range mounts {
		read := hostFS.Read.Covers(mount.Source)
		write := hostFS.Write.Covers(mount.Source)
		execute := hostFS.Exec.Covers(mount.Source)
		source, canonical, err := grantBinding.openGrant(mount.Source, read, write, execute)
		if err != nil {
			return nil, execapi.ErrConfineUnsupported.WithCause(err)
		}
		files.sources = append(files.sources, source)
		files.grants = append(files.grants, confinelinux.HelperGrant{
			Source: canonical, Target: mount.Source,
			Read:     read,
			Write:    write,
			Exec:     execute,
			ReadOnly: mount.ReadOnly, NoExec: mount.NoExec,
		})
	}
	var err error
	files.workdir, files.workDirSource, err = binding.openWorkDir(c.workDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("work_dir"), err)
	}
	return files, nil
}

func startLinuxCommand(process *ProcessExecutor, launch *confinelinux.Launch) error {
	if process.pty == nil {
		return launch.Command.Start()
	}
	width, height, _ := process.pty.Dimensions()
	master, err := pty.StartWithSize(launch.Command, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		return err
	}
	process.ptyMaster = master
	process.stdinPipe, process.stdoutp = master, master
	process.stderrp = io.NopCloser(strings.NewReader(""))
	return nil
}

func (c *linuxConfinementLaunch) armWallDeadline(process *ProcessExecutor) {
	if c.policy.Limits.WallSec <= 0 {
		return
	}
	c.wallDone = make(chan struct{})
	c.wall = time.AfterFunc(time.Duration(c.policy.Limits.WallSec)*time.Second, func() {
		defer c.wallOnce.Do(func() { close(c.wallDone) })
		// Ask namespace PID 1 to kill the task domain while it survives to
		// reap and report the target. Direct target-only kill leaks descendants.
		if c.supervisor == nil || c.supervisor.Signal(confinelinux.TreeKillSignal) != nil {
			if c.group != nil {
				_ = c.group.Kill()
			}
			_ = process.cmd.Process.Kill()
		}
	})
}

func (c *linuxConfinementLaunch) Start(process *ProcessExecutor) (resultErr error) {
	started := false
	defer func() {
		if !started {
			var cleanupErr error
			if c.group != nil {
				cleanupErr = errors.Join(c.group.Kill(), c.group.Remove())
			}
			cleanupErr = errors.Join(cleanupErr, c.removeRoot(), c.removeHostPrivate())
			resultErr = errors.Join(resultErr, cleanupErr)
		}
	}()
	// exec.Command records lookup failures (including ErrDot) on Cmd.Err.
	// Replacing the command with the verified helper must not turn a command
	// that Go refused to resolve into a different executable under workDir.
	if process.cmd.Err != nil {
		return process.cmd.Err
	}
	helper, err := openVerifiedConfinementHelper()
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	defer helper.Close()
	files, err := c.bindLaunchFiles()
	if err != nil {
		return err
	}
	defer files.Close()
	if c.policy.FS != nil {
		c.root, err = os.MkdirTemp("", "wippy-confine-root-")
		if err != nil {
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
	}
	if c.policy.FS == nil && c.policy.HomePrivate {
		c.hostPrivate, err = os.MkdirTemp("", "wippy-confine-private-")
		if err != nil {
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
		process.envs["HOME"] = filepath.Join(c.hostPrivate, "home")
		rebuildProcessEnvironment(process)
	}
	c.group, err = confinelinux.NewCgroup(c.policy.Limits)
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	target := process.cmd.Path
	policy := confinelinux.HelperPolicy{
		Root: c.root, PrivateHome: c.hostPrivate, Grants: files.grants, Private: files.private, WorkDir: c.workDir,
		WorkDirSource: files.workDirSource, Path: target, Argv: process.cmd.Args, Env: process.cmd.Env,
		ProcessGroup: process.processGroup, PTY: process.pty != nil,
	}
	launch, err := confinelinux.PrepareLaunch(helper, policy, files.sources, files.workdir, c.policy.NetworkNone)
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	defer launch.Close()
	launch.Command.Stdin = process.cmd.Stdin
	launch.Command.Stdout = process.cmd.Stdout
	launch.Command.Stderr = process.cmd.Stderr
	if process.processGroup && process.pty == nil {
		applyProcessGroup(launch.Command)
	}
	process.cmd = launch.Command
	var beforePolicy func(int) error
	if c.group != nil {
		beforePolicy = c.group.Add
	}
	err = launch.Start(func(_ *exec.Cmd) error { return startLinuxCommand(process, launch) }, beforePolicy)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ENOSYS) {
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
		return execapi.ErrConfineSetup.WithCause(err)
	}
	if process.pty == nil {
		process.releaseOutputWriters()
	}
	if launch.TargetPID <= 0 || launch.TargetPIDFD < 0 {
		_ = launch.Command.Process.Kill()
		_ = launch.Command.Wait()
		return execapi.ErrConfineSetup.WithCause(errors.New("missing confined target identity"))
	}
	c.supervisor = launch.Command.Process
	c.exitReport = launch.TakeExitReader()
	c.identity.Lock()
	c.targetPIDFD = launch.TargetPIDFD
	c.identity.Unlock()
	launch.TargetPIDFD = -1
	process.pid = launch.TargetPID
	c.armWallDeadline(process)
	started = true
	return nil
}

func (c *linuxConfinementLaunch) Signal(signal syscall.Signal) error {
	c.identity.Lock()
	defer c.identity.Unlock()
	if c.targetPIDFD < 0 {
		return ErrProcessNotRunning
	}
	return confinelinux.SignalPIDFD(c.targetPIDFD, signal)
}

func (c *linuxConfinementLaunch) Stop() {
	c.stopWall()
	if c.supervisor != nil && c.supervisor.Signal(confinelinux.TreeKillSignal) == nil {
		return
	}
	if c.group != nil {
		_ = c.group.Kill()
		_ = c.group.Remove()
	}
	if c.supervisor != nil {
		_ = c.supervisor.Kill()
	}
	_ = c.removeRoot()
	_ = c.removeHostPrivate()
}

func (c *linuxConfinementLaunch) Wait(waitErr error) (resultErr error) {
	c.stopWall()
	var finalizationErr error
	if c.group != nil {
		finalizationErr = errors.Join(c.group.Kill(), c.group.Remove())
	}
	c.identity.Lock()
	if c.targetPIDFD >= 0 {
		_ = confinelinux.ClosePIDFD(c.targetPIDFD)
		c.targetPIDFD = -1
	}
	c.identity.Unlock()
	finalizationErr = errors.Join(finalizationErr, c.removeRoot(), c.removeHostPrivate())
	defer func() {
		resultErr = joinExitFinalization(resultErr, finalizationErr)
	}()
	if c.exitReport == nil {
		if c.supervisor != nil && waitErr != nil {
			return fmt.Errorf("missing confined target exit report (supervisor wait: %s)", waitErr.Error())
		}
		return waitErr
	}
	status, err := confinelinux.DecodeTargetExit(c.exitReport)
	_ = c.exitReport.Close()
	c.exitReport = nil
	if err != nil {
		// The helper's own process status is not the application's status. Keep
		// it diagnostic-only so the generic exit classifier cannot mistake the
		// supervisor's ExitError for an observed target exit.
		if waitErr != nil {
			return fmt.Errorf("read confined target exit: %w (supervisor wait: %s)", err, waitErr.Error())
		}
		return fmt.Errorf("read confined target exit: %w", err)
	}
	if status.Code == 0 {
		if waitErr != nil {
			return fmt.Errorf("confined supervisor failed after clean target exit: %s", waitErr.Error())
		}
		return nil
	}
	var supervisorExit *exec.ExitError
	if !errors.As(waitErr, &supervisorExit) || supervisorExit.ExitCode() != status.Code {
		return fmt.Errorf("confined supervisor status contradicts target exit %d: %s", status.Code, diagnosticError(waitErr))
	}
	return &ExitError{Code: status.Code, Signal: status.Signal, cause: waitErr}
}

func diagnosticError(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func (c *linuxConfinementLaunch) stopWall() {
	if c.wall != nil {
		if c.wall.Stop() {
			c.wallOnce.Do(func() { close(c.wallDone) })
		}
		<-c.wallDone
	}
}

func (c *linuxConfinementLaunch) removeRoot() error {
	c.cleanup.Lock()
	defer c.cleanup.Unlock()
	if c.root == "" {
		return nil
	}
	// A Stop can race the namespace's final mount teardown. The directory is
	// empty in the runtime's mount namespace, but remains EBUSY for a short
	// interval after PID 1 is killed. Retry rather than leaking one root per
	// stopped process; never recurse through a path that was visible to the
	// confined target.
	var result error
	for attempt := 0; attempt < 100; attempt++ {
		result = os.Remove(c.root)
		if result == nil || errors.Is(result, os.ErrNotExist) {
			c.root = ""
			return nil
		}
		if !errors.Is(result, syscall.EBUSY) && !errors.Is(result, syscall.ENOTEMPTY) {
			return fmt.Errorf("remove confinement root: %w", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("remove confinement root: %w", result)
}

func (c *linuxConfinementLaunch) removeHostPrivate() error {
	c.cleanup.Lock()
	defer c.cleanup.Unlock()
	if c.hostPrivate == "" {
		return nil
	}
	var result error
	for attempt := 0; attempt < 100; attempt++ {
		result = os.Remove(c.hostPrivate)
		if result == nil || errors.Is(result, os.ErrNotExist) {
			c.hostPrivate = ""
			return nil
		}
		if !errors.Is(result, syscall.EBUSY) {
			return fmt.Errorf("remove confinement private home: %w", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("remove confinement private home: %w", result)
}
