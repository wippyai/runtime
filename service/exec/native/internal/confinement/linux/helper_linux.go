// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
	"golang.org/x/sys/unix"
)

const (
	helperFD = 3
	policyFD = 4
	statusFD = 5
	exitFD   = 6

	// TreeKillSignal is reserved for the parent runtime to ask PID 1 to kill
	// every other task in the namespace while remaining alive to reap and
	// report the root target's exact exit.
	TreeKillSignal = syscall.SIGUSR1
)

// HelperGrant identifies a pinned source descriptor inherited from the
// parent. FD is an inherited descriptor number, not a host path to reopen.
type HelperGrant struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	FD       int    `json:"fd"`
	Read     bool   `json:"read"`
	Write    bool   `json:"write"`
	Exec     bool   `json:"exec"`
	ReadOnly bool   `json:"read_only"`
	NoExec   bool   `json:"no_exec"`
}

// PrivateGrant refers only to a freshly created directory in the private
// mount root. It never names or opens a host object.
type PrivateGrant struct {
	Target string `json:"target"`
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
	Exec   bool   `json:"exec"`
}

// HelperPolicy is sent only on the inherited private policy descriptor. The
// parent decides authority; this structure carries its already-bound plan.
type HelperPolicy struct {
	Root          string         `json:"root"`
	PrivateHome   string         `json:"private_home,omitempty"`
	Grants        []HelperGrant  `json:"grants"`
	Private       []PrivateGrant `json:"private"`
	WorkDir       string         `json:"work_dir"`
	WorkDirSource string         `json:"work_dir_source,omitempty"`
	Path          string         `json:"path"`
	Argv          []string       `json:"argv"`
	Env           []string       `json:"env"`
	WorkDirFD     int            `json:"work_dir_fd"`
	CredentialFD  int            `json:"credential_fd"`
	NetworkNone   bool           `json:"network_none"`
	ProcessGroup  bool           `json:"process_group"`
	PTY           bool           `json:"pty"`
}

// TargetExitError carries the root application's observable exit code through
// the namespace supervisor without exposing the supervisor as the process.
type TargetExitError struct{ Code int }

func (e *TargetExitError) Error() string { return fmt.Sprintf("target exited with code %d", e.Code) }

// TargetExitStatus is written by the namespace supervisor after it has reaped
// the application and destroyed the remaining namespace tree. It preserves a
// signal death that the supervisor's own process status cannot represent.
type TargetExitStatus struct {
	Code   int `json:"code"`
	Signal int `json:"signal,omitempty"`
}

func DecodeTargetExit(reader io.Reader) (TargetExitStatus, error) {
	var wire struct {
		Code   *int `json:"code"`
		Signal *int `json:"signal,omitempty"`
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<12))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return TargetExitStatus{}, err
	}
	if wire.Code == nil {
		return TargetExitStatus{}, errors.New("missing target exit code")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return TargetExitStatus{}, errors.New("trailing target exit value")
		}
		return TargetExitStatus{}, fmt.Errorf("trailing target exit data: %w", err)
	}
	status := TargetExitStatus{Code: *wire.Code}
	if wire.Signal != nil {
		status.Signal = *wire.Signal
	}
	if status.Code < 0 || status.Code > 255 || status.Signal < 0 || status.Signal > 127 {
		return TargetExitStatus{}, errors.New("invalid target exit status")
	}
	if status.Signal != 0 && status.Code != 128+status.Signal {
		return TargetExitStatus{}, errors.New("inconsistent target signal status")
	}
	return status, nil
}

// RunHelper dispatches the two trusted modes of the separately built image.
// The default mode remains PID 1 and supervises/reaps the namespace. The
// target mode installs the policy as PID 2 and execs the application, so the
// application retains ordinary Unix signal semantics.
func RunHelper() error {
	if len(os.Args) == 2 && os.Args[1] == "--target" {
		return runTargetHelper()
	}
	if len(os.Args) != 1 {
		return errors.New("invalid helper mode")
	}
	return runSupervisorHelper()
}

func runTargetHelper() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// The parent executed this exact verified inode through /proc/self/fd/3.
	// It is not target authority and must not survive into the target.
	_ = unix.Close(helperFD)
	status := os.NewFile(statusFD, "confine-status")
	if status == nil {
		return errors.New("missing status descriptor")
	}
	defer status.Close()
	unix.CloseOnExec(statusFD)
	policyFile := os.NewFile(policyFD, "confine-policy")
	if policyFile == nil {
		return reportHelperFailure(status, errors.New("missing policy descriptor"))
	}
	policy, err := decodeHelperPolicy(policyFile)
	_ = policyFile.Close()
	if err != nil {
		return reportHelperFailure(status, fmt.Errorf("decode policy: %w", err))
	}
	if err := authenticateTargetToParent(policy.CredentialFD); err != nil {
		return reportHelperFailure(status, err)
	}
	if err := installHelperPolicy(policy); err != nil {
		return reportHelperFailure(status, err)
	}
	// Go's minimal helper may still own runtime descriptors (epoll, timers,
	// etc.). Ensure none can become ambient target authority. The status pipe
	// stays usable until exec and is then closed by the kernel as well.
	if err := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		return reportHelperFailure(status, fmt.Errorf("seal inherited descriptors: %w", err))
	}
	if _, err := status.Write([]byte("READY\n")); err != nil {
		return err
	}
	if err := syscall.Exec(policy.Path, policy.Argv, policy.Env); err != nil { //nolint:gosec // The parent binds policy and the helper confines itself before exec.
		return reportHelperFailure(status, fmt.Errorf("exec target: %w", err))
	}
	return nil
}

func decodeHelperPolicy(file io.Reader) (HelperPolicy, error) {
	var policy HelperPolicy
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&policy)
	return policy, err
}

func authenticateTargetToParent(fd int) error {
	if fd < 3 {
		return errors.New("missing target identity descriptor")
	}
	if err := unix.Sendmsg(fd, []byte{'T'}, nil, nil, 0); err != nil {
		return fmt.Errorf("send target identity: %w", err)
	}
	response := make([]byte, 1)
	n, _, err := unix.Recvfrom(fd, response, 0)
	if err != nil || n != 1 || response[0] != 'G' {
		return fmt.Errorf("target identity rejected: %w", err)
	}
	return unix.Close(fd)
}

func runSupervisorHelper() error {
	// The target shares this user/PID namespace and UID. Make procfs ptrace
	// access to the trusted supervisor fail even before the target installs its
	// own proc view and drops capabilities.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("protect confinement supervisor: %w", err)
	}
	treeKills := make(chan os.Signal, 1)
	signal.Notify(treeKills, TreeKillSignal)
	defer signal.Stop(treeKills)
	go func() {
		for range treeKills {
			// PID 1 is excluded, so it survives to reap the target and publish
			// its status after every other namespace task receives SIGKILL.
			_ = unix.Kill(-1, unix.SIGKILL)
		}
	}()
	status := os.NewFile(statusFD, "confine-status")
	if status == nil {
		return errors.New("missing status descriptor")
	}
	defer status.Close()
	policyFile := os.NewFile(policyFD, "confine-policy")
	if policyFile == nil {
		return reportHelperFailure(status, errors.New("missing policy descriptor"))
	}
	policy, err := decodeHelperPolicy(policyFile)
	_ = policyFile.Close()
	if err != nil {
		return reportHelperFailure(status, fmt.Errorf("decode supervisor policy: %w", err))
	}
	exitReport := os.NewFile(exitFD, "confine-target-exit")
	if exitReport == nil {
		return reportHelperFailure(status, errors.New("missing target exit descriptor"))
	}
	defer exitReport.Close()
	policyReader, policyWriter, err := os.Pipe()
	if err != nil {
		return reportHelperFailure(status, err)
	}
	defer policyReader.Close()
	defer policyWriter.Close()
	helper := os.NewFile(helperFD, "verified-helper")
	if helper == nil {
		return reportHelperFailure(status, errors.New("missing verified helper descriptor"))
	}
	files := []*os.File{helper, policyReader, status}
	for i := range policy.Grants {
		grant := &policy.Grants[i]
		file := os.NewFile(uintptr(grant.FD), "confine-grant")
		if file == nil {
			return reportHelperFailure(status, errors.New("missing grant descriptor"))
		}
		files = append(files, file)
		grant.FD = 6 + i
	}
	if policy.WorkDirFD >= 0 {
		file := os.NewFile(uintptr(policy.WorkDirFD), "confine-workdir")
		if file == nil {
			return reportHelperFailure(status, errors.New("missing workdir descriptor"))
		}
		files = append(files, file)
		policy.WorkDirFD = 6 + len(policy.Grants)
	}
	credential := os.NewFile(uintptr(policy.CredentialFD), "confine-credential")
	if credential == nil {
		return reportHelperFailure(status, errors.New("missing credential descriptor"))
	}
	files = append(files, credential)
	policy.CredentialFD = 6 + len(policy.Grants)
	if policy.WorkDirFD >= 0 {
		policy.CredentialFD++
	}
	command := exec.CommandContext(context.Background(), "/proc/self/fd/3", "--target")
	command.Env = []string{}
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.ExtraFiles = files
	if policy.ProcessGroup || policy.PTY {
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := command.Start(); err != nil {
		return reportHelperFailure(status, fmt.Errorf("start target launcher: %w", err))
	}
	if policy.PTY {
		if err := unix.IoctlSetPointerInt(int(os.Stdin.Fd()), unix.TIOCSPGRP, command.Process.Pid); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return reportHelperFailure(status, fmt.Errorf("foreground target process group: %w", err))
		}
	}
	_ = policyReader.Close()
	if err := json.NewEncoder(policyWriter).Encode(policy); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return reportHelperFailure(status, fmt.Errorf("forward confinement policy: %w", err))
	}
	_ = policyWriter.Close()
	// Only the target owns setup reporting and ambient authority from here.
	_ = status.Close()
	for _, file := range files {
		_ = file.Close()
	}
	// The target owns its copies now. Retaining these endpoints in PID 1 would
	// suppress stdin EPIPE and stdout/stderr EOF after the application closes
	// them or exits.
	_ = os.Stdin.Close()
	_ = os.Stdout.Close()
	_ = os.Stderr.Close()
	targetStatus, err := reapUntilTarget(command.Process.Pid)
	_ = command.Process.Release()
	if err != nil {
		return err
	}
	_ = unix.Kill(-1, unix.SIGKILL)
	for {
		var childStatus unix.WaitStatus
		_, waitErr := unix.Wait4(-1, &childStatus, 0, nil)
		if errors.Is(waitErr, unix.ECHILD) {
			break
		}
		if waitErr != nil && !errors.Is(waitErr, unix.EINTR) {
			return waitErr
		}
	}
	report := TargetExitStatus{}
	if targetStatus.Signaled() {
		report.Signal = int(targetStatus.Signal())
		report.Code = 128 + report.Signal
	} else if targetStatus.Exited() {
		report.Code = targetStatus.ExitStatus()
	} else {
		return errors.New("target has no final exit status")
	}
	if err := json.NewEncoder(exitReport).Encode(report); err != nil {
		return fmt.Errorf("report target exit: %w", err)
	}
	return &TargetExitError{Code: report.Code}
}

func reapUntilTarget(targetPID int) (unix.WaitStatus, error) {
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(-1, &status, 0, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("reap confined target: %w", err)
		}
		if pid == targetPID {
			return status, nil
		}
	}
}

func installHelperPolicy(policy HelperPolicy) error {
	if policy.Path == "" || len(policy.Argv) == 0 || policy.Argv[0] == "" {
		return errors.New("missing target")
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	lastCapability, err := KernelLastCapability()
	if err != nil {
		return err
	}
	if err := installHelperFilesystem(policy); err != nil {
		return err
	}
	if err := enterHelperWorkDir(policy); err != nil {
		return err
	}
	for _, grant := range policy.Grants {
		_ = unix.Close(grant.FD)
	}
	if policy.WorkDirFD >= 0 {
		_ = unix.Close(policy.WorkDirFD)
	}
	if err := DropTargetCapabilities(lastCapability); err != nil {
		return err
	}
	if err := InstallIsolationSeccomp(policy.NetworkNone, policy.Root != ""); err != nil {
		return err
	}
	return nil
}

func installHelperFilesystem(policy HelperPolicy) error {
	if policy.Root == "" {
		if err := InstallUnrestrictedKernelView(); err != nil {
			return err
		}
	}
	if policy.PrivateHome != "" {
		if policy.Root != "" {
			return errors.New("conflicting private home roots")
		}
		if err := InstallStandalonePrivateHome(policy.PrivateHome); err != nil {
			return err
		}
	}
	if policy.Root == "" {
		return nil
	}

	mounts, err := reopenPinnedMounts(policy.Grants)
	if err != nil {
		return err
	}
	defer closePinnedMounts(mounts)
	privateExec := false
	for _, grant := range policy.Private {
		privateExec = privateExec || grant.Exec
	}
	if err := InstallMountViewWithPrivateExec(policy.Root, mounts, privateExec); err != nil {
		return err
	}

	landlock, err := openLandlockGrants(policy)
	if err != nil {
		return err
	}
	defer closeLandlockGrants(landlock)
	return InstallLandlock(landlock)
}

func reopenPinnedMounts(grants []HelperGrant) ([]PinnedMount, error) {
	mounts := make([]PinnedMount, 0, len(grants))
	failed := true
	defer func() {
		if failed {
			closePinnedMounts(mounts)
		}
	}()
	for _, grant := range grants {
		if err := RejectSpecialDirectoryFD(grant.FD); err != nil {
			return nil, fmt.Errorf("reject special grant: %w", err)
		}
		if !cleanAbsolute(grant.Source) {
			return nil, errors.New("invalid grant source")
		}
		// Reopen inside the new user namespace, then prove it is still the
		// object pinned by the parent before it becomes a mount authority.
		sourceFD, err := unix.Openat2(unix.AT_FDCWD, grant.Source, &unix.OpenHow{
			Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
			Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
		})
		if err != nil {
			return nil, fmt.Errorf("reopen grant: %w", err)
		}
		if err := RejectSpecialDirectoryFD(sourceFD); err != nil {
			_ = unix.Close(sourceFD)
			return nil, fmt.Errorf("reject reopened special grant: %w", err)
		}
		same, err := SameOpenDirectoryFDs(grant.FD, sourceFD)
		if err != nil || !same {
			_ = unix.Close(sourceFD)
			if err != nil {
				return nil, fmt.Errorf("compare grant identity: %w", err)
			}
			return nil, errors.New("grant identity changed")
		}
		mounts = append(mounts, PinnedMount{
			Target: grant.Target, FD: sourceFD,
			ReadOnly: grant.ReadOnly, NoExec: grant.NoExec,
		})
	}
	failed = false
	return mounts, nil
}

func closePinnedMounts(mounts []PinnedMount) {
	for _, mount := range mounts {
		_ = unix.Close(mount.FD)
	}
}

func openLandlockGrants(policy HelperPolicy) ([]LandlockGrant, error) {
	grants := make([]LandlockGrant, 0, len(policy.Grants)+len(policy.Private)+5)
	failed := true
	defer func() {
		if failed {
			closeLandlockGrants(grants)
		}
	}()
	for _, grant := range policy.Grants {
		fd, err := openPolicyPath(grant.Target)
		if err != nil {
			return nil, fmt.Errorf("open mounted Landlock grant: %w", err)
		}
		grants = append(grants, LandlockGrant{
			FD: fd, Read: grant.Read, Write: grant.Write, Exec: grant.Exec,
		})
	}
	for _, grant := range policy.Private {
		if grant.Target != confinement.PrivateHomePath && grant.Target != confinement.PrivateTempPath {
			return nil, errors.New("invalid private grant target")
		}
		fd, err := openPolicyPath(grant.Target)
		if err != nil {
			return nil, fmt.Errorf("open private Landlock grant: %w", err)
		}
		grants = append(grants, LandlockGrant{
			FD: fd, Read: grant.Read, Write: grant.Write, Exec: grant.Exec,
		})
	}
	for _, device := range []string{"/dev/null", "/dev/zero", "/dev/random", "/dev/urandom", "/dev/tty"} {
		fd, err := unix.Open(device, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, fmt.Errorf("open private device: %w", err)
		}
		grants = append(grants, LandlockGrant{
			FD: fd, Read: true, Write: true, FileOnly: true, IOCTLDev: true,
		})
	}
	failed = false
	return grants, nil
}

func openPolicyPath(path string) (int, error) {
	return unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
}

func closeLandlockGrants(grants []LandlockGrant) {
	for _, grant := range grants {
		_ = unix.Close(grant.FD)
	}
}

func enterHelperWorkDir(policy HelperPolicy) error {
	if policy.WorkDir == "" {
		return nil
	}
	if policy.WorkDirFD < 0 {
		return errors.New("missing pinned work directory")
	}
	viewPath := policy.WorkDir
	label := "mounted"
	if policy.Root == "" {
		if err := RejectSpecialDirectoryFD(policy.WorkDirFD); err != nil {
			return fmt.Errorf("reject special working directory: %w", err)
		}
		viewPath = policy.WorkDirSource
		if viewPath == "" {
			viewPath = policy.WorkDir
		}
		if !cleanAbsolute(viewPath) {
			return errors.New("invalid canonical working directory")
		}
		label = "unrestricted"
	}
	viewFD, err := openPolicyPath(viewPath)
	if err != nil {
		return fmt.Errorf("open %s working directory: %w", label, err)
	}
	defer unix.Close(viewFD)
	same, err := SameOpenDirectoryFDs(policy.WorkDirFD, viewFD)
	if err != nil {
		return fmt.Errorf("compare %s working directory identity: %w", label, err)
	}
	if !same {
		return fmt.Errorf("%s working directory identity changed", label)
	}
	if err := unix.Fchdir(viewFD); err != nil {
		return fmt.Errorf("enter %s working directory: %w", label, err)
	}
	return nil
}

func reportHelperFailure(status *os.File, err error) error {
	_, _ = fmt.Fprintf(status, "ERROR:%s\n", err)
	return err
}
