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
	"sort"
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

func (b *linuxEntryBinding) openWorkDir(path string) (*os.File, error) {
	file, _, err := openBoundDescendant(b.roots, path)
	return file, err
}

func openBoundDescendant(roots map[string]*confinelinux.BoundDirectory, path string) (*os.File, string, error) {
	var selected *confinelinux.BoundDirectory
	selectedLength := -1
	for base, root := range roots {
		rel, err := filepath.Rel(base, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && len(base) > selectedLength {
			selected, selectedLength = root, len(base)
		}
	}
	if selected != nil {
		return selected.OpenDescendant(path)
	}
	return nil, "", confinelinux.ErrOutsideRoot
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
	if err := entry.Validate(); err != nil {
		return err
	}
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
	policy := confinement.ExpandPrivatePolicy(confinement.FromEntry(entry))
	if err := confinement.ValidateEntry(policy); err != nil {
		return execapi.NewInvalidConfinementError("confine")
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

func (e *Executor) prepareConfinement(process *ProcessExecutor, options execapi.ProcessOptions) error {
	if err := e.bindConfinementEntry(); err != nil {
		return err
	}
	base := confinement.ExpandPrivatePolicy(confinement.FromEntry(e.confine))
	patch := confinement.ExpandPrivatePatch(confinement.FromPatch(options.Confine))
	policy, err := confinement.Narrow(base, patch)
	if err != nil {
		if errors.Is(err, confinement.ErrWiden) {
			return execapi.ErrConfineWiden.WithCause(err)
		}
		return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("confine"), err)
	}
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
	privateTemp := slices.Contains(fs.Read.Paths, confinement.PrivateTempPath) ||
		slices.Contains(fs.Write.Paths, confinement.PrivateTempPath) ||
		slices.Contains(fs.Exec.Paths, confinement.PrivateTempPath)
	if err := applyConfinementEnvironment(process, policy.Env, policy.HomePrivate, privateTemp); err != nil {
		return err
	}
	if process.wd == "" || !policy.AllowsBoundWorkDir(process.wd) {
		return execapi.ErrConfineDenied
	}
	if policy.FS != nil {
		hostFS, _ := splitPrivateFilesystem(fs)
		if _, err := confinelinux.PlanBindMounts(hostFS); err != nil {
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
	}
	process.confinement = &linuxConfinementLaunch{policy: policy, workDir: process.wd, entry: e}
	return nil
}

// applyConfinementEnvironment validates the already-merged entry defaults and
// caller values, then installs entry-owned values. In particular, an entry
// default cannot quietly override a forced value: that would make a policy
// appear enforced while running with a different environment.
func applyConfinementEnvironment(process *ProcessExecutor, policy *confinement.Environment, homePrivate, tempPrivate bool) error {
	for name, value := range process.envs {
		if name == "" || strings.ContainsAny(name, "=\x00") || strings.ContainsRune(value, 0) {
			return execapi.NewInvalidConfinementError("confine.env")
		}
		if (homePrivate && (strings.EqualFold(name, "HOME") || strings.EqualFold(name, "USERPROFILE"))) ||
			(tempPrivate && (strings.EqualFold(name, "TMPDIR") || strings.EqualFold(name, "TMP") ||
				strings.EqualFold(name, "TEMP"))) {
			return execapi.NewInvalidConfinementError("confine.env")
		}
		if policy != nil {
			_, pinned := policy.Set[name]
			if pinned || !slices.Contains(policy.Allow, name) {
				return execapi.NewInvalidConfinementError("confine.env")
			}
		}
	}
	if process.envs == nil {
		process.envs = make(map[string]string)
	}
	if policy != nil {
		for name, value := range policy.Set {
			process.envs[name] = value
		}
	}
	if tempPrivate {
		process.envs["TMPDIR"] = confinement.PrivateTempPath
	}
	if homePrivate {
		process.envs["HOME"] = confinement.PrivateHomePath
	}
	rebuildProcessEnvironment(process)
	return nil
}

func rebuildProcessEnvironment(process *ProcessExecutor) {
	names := make([]string, 0, len(process.envs))
	for name := range process.envs {
		names = append(names, name)
	}
	sort.Strings(names)
	process.cmd.Env = make([]string, 0, len(names))
	for _, name := range names {
		process.cmd.Env = append(process.cmd.Env, name+"="+process.envs[name])
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

func (c *linuxConfinementLaunch) Start(process *ProcessExecutor) error {
	started := false
	defer func() {
		if !started {
			c.removeRoot()
			c.removeHostPrivate()
		}
	}()
	helper, err := openVerifiedConfinementHelper()
	if err != nil {
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	defer helper.Close()
	var hostFS confinement.Filesystem
	var private []confinelinux.PrivateGrant
	var mounts []confinelinux.BindMount
	if c.policy.FS != nil {
		hostFS, private = splitPrivateFilesystem(c.policy.EffectiveFilesystem())
		mounts, err = confinelinux.PlanBindMounts(hostFS)
		if err != nil {
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
	}
	sources := make([]*os.File, 0, len(mounts))
	defer func() {
		for _, source := range sources {
			_ = source.Close()
		}
	}()
	grants := make([]confinelinux.HelperGrant, 0, len(mounts))
	c.entry.confineMu.RLock()
	binding, ok := c.entry.confineEntry.(*linuxEntryBinding)
	if !ok {
		c.entry.confineMu.RUnlock()
		return execapi.ErrConfineUnsupported
	}
	grantBinding := binding
	var transient *linuxEntryBinding
	if c.entry.confine.FS == nil && c.policy.FS != nil {
		transient, err = bindLinuxFilesystem(hostFS)
		if err != nil {
			c.entry.confineMu.RUnlock()
			return execapi.ErrConfineUnsupported.WithCause(err)
		}
		defer transient.Close()
		grantBinding = transient
	}
	for _, mount := range mounts {
		read := coveredByPolicy(mount.Source, hostFS.Read.Paths)
		write := coveredByPolicy(mount.Source, hostFS.Write.Paths)
		execute := coveredByPolicy(mount.Source, hostFS.Exec.Paths)
		source, canonical, bindErr := grantBinding.openGrant(mount.Source, read, write, execute)
		if bindErr != nil {
			c.entry.confineMu.RUnlock()
			return execapi.ErrConfineUnsupported.WithCause(bindErr)
		}
		sources = append(sources, source)
		grants = append(grants, confinelinux.HelperGrant{
			Source: canonical, Target: mount.Source,
			Read:     read,
			Write:    write,
			Exec:     execute,
			ReadOnly: mount.ReadOnly, NoExec: mount.NoExec,
		})
	}
	workdir, err := binding.openWorkDir(c.workDir)
	c.entry.confineMu.RUnlock()
	if err != nil {
		return fmt.Errorf("%w: %w", execapi.NewInvalidConfinementError("work_dir"), err)
	}
	defer workdir.Close()
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
		c.removeRoot()
		return execapi.ErrConfineUnsupported.WithCause(err)
	}
	target := process.cmd.Path
	if !filepath.IsAbs(target) {
		target = filepath.Join(c.workDir, target)
	}
	policy := confinelinux.HelperPolicy{
		Root: c.root, PrivateHome: c.hostPrivate, Grants: grants, Private: private, WorkDir: c.workDir,
		Path: target, Argv: process.cmd.Args, Env: process.cmd.Env,
		ProcessGroup: process.processGroup, PTY: process.pty != nil,
	}
	launch, err := confinelinux.PrepareLaunch(helper, policy, sources, workdir, c.policy.NetworkNone)
	if err != nil {
		if c.group != nil {
			_ = c.group.Remove()
		}
		c.removeRoot()
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
	err = launch.Start(func(_ *exec.Cmd) error {
		if process.pty == nil {
			return launch.Command.Start()
		}
		width, height, _ := process.pty.Dimensions()
		master, startErr := pty.StartWithSize(launch.Command,
			&pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
		if startErr == nil {
			process.ptyMaster = master
			process.stdinPipe, process.stdoutp = master, master
			process.stderrp = io.NopCloser(strings.NewReader(""))
		}
		return startErr
	}, beforePolicy)
	if err != nil {
		if c.group != nil {
			_ = c.group.Kill()
			_ = c.group.Remove()
		}
		c.removeRoot()
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOSYS) {
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
		if c.group != nil {
			_ = c.group.Kill()
			_ = c.group.Remove()
		}
		c.removeRoot()
		return execapi.ErrConfineSetup.WithCause(errors.New("missing confined target identity"))
	}
	c.supervisor = launch.Command.Process
	c.exitReport = launch.TakeExitReader()
	c.identity.Lock()
	c.targetPIDFD = launch.TargetPIDFD
	c.identity.Unlock()
	launch.TargetPIDFD = -1
	process.pid = launch.TargetPID
	if c.policy.Limits.WallSec > 0 {
		c.wallDone = make(chan struct{})
		c.wall = time.AfterFunc(time.Duration(c.policy.Limits.WallSec)*time.Second, func() {
			defer c.wallOnce.Do(func() { close(c.wallDone) })
			if c.group != nil {
				_ = c.group.Kill()
			}
			_ = process.cmd.Process.Kill()
		})
	}
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

func coveredByPolicy(path string, grants []string) bool {
	for _, grant := range grants {
		rel, err := filepath.Rel(grant, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

func (c *linuxConfinementLaunch) Stop() {
	c.stopWall()
	if c.group != nil {
		_ = c.group.Kill()
		_ = c.group.Remove()
	}
	if c.supervisor != nil {
		_ = c.supervisor.Kill()
	}
	c.removeRoot()
	c.removeHostPrivate()
}

func (c *linuxConfinementLaunch) Wait(waitErr error) error {
	c.stopWall()
	if c.group != nil {
		_ = c.group.Kill()
		_ = c.group.Remove()
	}
	c.identity.Lock()
	if c.targetPIDFD >= 0 {
		_ = confinelinux.ClosePIDFD(c.targetPIDFD)
		c.targetPIDFD = -1
	}
	c.identity.Unlock()
	c.removeRoot()
	c.removeHostPrivate()
	if c.exitReport == nil {
		return waitErr
	}
	status, err := confinelinux.DecodeTargetExit(c.exitReport)
	_ = c.exitReport.Close()
	c.exitReport = nil
	if err != nil {
		return errors.Join(waitErr, fmt.Errorf("read confined target exit: %w", err))
	}
	if status.Code == 0 {
		if waitErr != nil {
			return errors.Join(waitErr, errors.New("confined supervisor failed after clean target exit"))
		}
		return nil
	}
	return &ExitError{Code: status.Code, Signal: status.Signal, cause: waitErr}
}

func (c *linuxConfinementLaunch) stopWall() {
	if c.wall != nil {
		if c.wall.Stop() {
			c.wallOnce.Do(func() { close(c.wallDone) })
		}
		<-c.wallDone
	}
}

func (c *linuxConfinementLaunch) removeRoot() {
	c.cleanup.Lock()
	defer c.cleanup.Unlock()
	if c.root == "" {
		return
	}
	// A Stop can race the namespace's final mount teardown. The directory is
	// empty in the runtime's mount namespace, but remains EBUSY for a short
	// interval after PID 1 is killed. Retry rather than leaking one root per
	// stopped process; never recurse through a path that was visible to the
	// confined target.
	for attempt := 0; attempt < 100; attempt++ {
		err := os.Remove(c.root)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			c.root = ""
			return
		}
		if !errors.Is(err, syscall.EBUSY) && !errors.Is(err, syscall.ENOTEMPTY) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *linuxConfinementLaunch) removeHostPrivate() {
	c.cleanup.Lock()
	defer c.cleanup.Unlock()
	if c.hostPrivate == "" {
		return
	}
	for attempt := 0; attempt < 100; attempt++ {
		err := os.Remove(c.hostPrivate)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			c.hostPrivate = ""
			return
		}
		if !errors.Is(err, syscall.EBUSY) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
