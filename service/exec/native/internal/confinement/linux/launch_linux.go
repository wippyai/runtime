// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const helperSetupTimeout = 10 * time.Second

// Launch owns the inherited policy and status channels until Start returns.
// Call Close if a prepared launch is discarded without being started.
type Launch struct {
	Command          *exec.Cmd
	policyReader     *os.File
	policyWriter     *os.File
	statusReader     *os.File
	statusWriter     *os.File
	exitReader       *os.File
	exitWriter       *os.File
	credentialParent *os.File
	credentialChild  *os.File
	policy           HelperPolicy
	TargetPID        int
	TargetPIDFD      int
}

// PrepareLaunch creates an isolated child command. sources are pinned parent
// descriptors in the same order as policy.Grants; workdir is the separately
// pinned working-directory object. The caller must have verified the helper
// binary before calling this function.
func PrepareLaunch(helper *os.File, policy HelperPolicy, sources []*os.File, workdir *os.File, networkNone bool) (*Launch, error) {
	if len(policy.Grants) != len(sources) {
		return nil, errors.New("grant descriptor count mismatch")
	}
	if helper == nil {
		return nil, errors.New("missing verified helper")
	}
	policyReader, policyWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	statusReader, statusWriter, err := os.Pipe()
	if err != nil {
		_ = policyReader.Close()
		_ = policyWriter.Close()
		return nil, err
	}
	exitReader, exitWriter, err := os.Pipe()
	if err != nil {
		_ = policyReader.Close()
		_ = policyWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		return nil, err
	}
	credentials, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		_ = policyReader.Close()
		_ = policyWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		_ = exitReader.Close()
		_ = exitWriter.Close()
		return nil, err
	}
	if err := unix.SetsockoptInt(credentials[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		_ = unix.Close(credentials[0])
		_ = unix.Close(credentials[1])
		_ = policyReader.Close()
		_ = policyWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		_ = exitReader.Close()
		_ = exitWriter.Close()
		return nil, err
	}
	launch := &Launch{
		policy: policy, policyReader: policyReader, policyWriter: policyWriter,
		statusReader: statusReader, statusWriter: statusWriter,
		exitReader: exitReader, exitWriter: exitWriter,
		credentialParent: os.NewFile(uintptr(credentials[0]), "confine-credential-parent"),
		credentialChild:  os.NewFile(uintptr(credentials[1]), "confine-credential-child"),
		TargetPIDFD:      -1,
	}
	command := exec.CommandContext(context.Background(), "/proc/self/fd/3")
	command.Env = []string{}
	command.ExtraFiles = []*os.File{helper, policyReader, statusWriter, exitWriter}
	for i, source := range sources {
		if source == nil {
			launch.Close()
			return nil, errors.New("missing pinned grant")
		}
		policy.Grants[i].FD = 7 + i
		command.ExtraFiles = append(command.ExtraFiles, source)
	}
	if workdir != nil {
		policy.WorkDirFD = 7 + len(sources)
		command.ExtraFiles = append(command.ExtraFiles, workdir)
	} else {
		policy.WorkDirFD = -1
	}
	policy.CredentialFD = 7 + len(sources)
	if workdir != nil {
		policy.CredentialFD++
	}
	command.ExtraFiles = append(command.ExtraFiles, launch.credentialChild)
	launch.policy = policy
	launch.policy.NetworkNone = networkNone
	// A target cannot address host PIDs from this namespace. As PID 1 it also
	// anchors the descendant lifetime: Linux kills the namespace's remaining
	// tasks when its init process exits or is killed.
	flags := uintptr(unix.CLONE_NEWUSER | unix.CLONE_NEWNS | unix.CLONE_NEWPID | unix.CLONE_NEWIPC)
	if networkNone {
		flags |= unix.CLONE_NEWNET
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: flags,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0, HostID: os.Getuid(), Size: 1,
		}},
		GidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0, HostID: os.Getgid(), Size: 1,
		}},
		GidMappingsEnableSetgroups: false,
	}
	launch.Command = command
	return launch, nil
}

// Start waits for an installed-policy READY and the exec-error channel to
// close. EOF after READY is success or an indistinguishable post-READY death;
// the latter is reported by Wait. No target can run before READY.
func (l *Launch) Start(start func(*exec.Cmd) error, beforePolicy func(int) error) error {
	if err := start(l.Command); err != nil {
		l.Close()
		return err
	}
	if beforePolicy != nil {
		if err := beforePolicy(l.Command.Process.Pid); err != nil {
			l.abort()
			l.Close()
			return fmt.Errorf("prepare confined child: %w", err)
		}
	}
	_ = l.policyReader.Close()
	_ = l.statusWriter.Close()
	_ = l.exitWriter.Close()
	_ = l.credentialChild.Close()
	encodeErr := json.NewEncoder(l.policyWriter).Encode(l.policy)
	_ = l.policyWriter.Close()
	if encodeErr != nil {
		l.abort()
		return fmt.Errorf("send confinement policy: %w", encodeErr)
	}
	targetPID, targetPIDFD, credentialErr := l.receiveTargetIdentity()
	if credentialErr != nil {
		l.abort()
		return credentialErr
	}
	l.TargetPID, l.TargetPIDFD = targetPID, targetPIDFD
	result := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(io.LimitReader(l.statusReader, 1<<16))
		first, err := reader.ReadString('\n')
		if err != nil || first != "READY\n" {
			if err != nil {
				result <- fmt.Errorf("confinement setup failed: %q: %w", first, err)
			} else {
				result <- fmt.Errorf("confinement setup failed: %q", first)
			}
			return
		}
		next, err := reader.ReadString('\n')
		if err == io.EOF && next == "" {
			result <- nil
			return
		}
		if err != nil {
			result <- fmt.Errorf("target exec failed: %q: %w", next, err)
		} else {
			result <- fmt.Errorf("target exec failed: %q", next)
		}
	}()
	select {
	case err := <-result:
		_ = l.statusReader.Close()
		if err != nil {
			l.abort()
		}
		return err
	case <-time.After(helperSetupTimeout):
		l.abort()
		_ = l.statusReader.Close()
		<-result
		return errors.New("confinement setup timed out")
	}
}

func (l *Launch) receiveTargetIdentity() (int, int, error) {
	if l.credentialParent == nil {
		return 0, -1, errors.New("missing target identity channel")
	}
	fd := int(l.credentialParent.Fd())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO,
		&unix.Timeval{Sec: int64(helperSetupTimeout / time.Second)}); err != nil {
		return 0, -1, err
	}
	data := make([]byte, 1)
	control := make([]byte, unix.CmsgSpace(unix.SizeofUcred))
	n, oobn, _, _, err := unix.Recvmsg(fd, data, control, 0)
	if err != nil || n != 1 || data[0] != 'T' {
		return 0, -1, fmt.Errorf("receive confined target identity: %w", err)
	}
	messages, err := unix.ParseSocketControlMessage(control[:oobn])
	if err != nil {
		return 0, -1, err
	}
	var credential *unix.Ucred
	for i := range messages {
		parsed, parseErr := unix.ParseUnixCredentials(&messages[i])
		if parseErr == nil {
			credential = parsed
			break
		}
	}
	if credential == nil || credential.Pid <= 0 || int(credential.Pid) == l.Command.Process.Pid ||
		credential.Uid != uint32(os.Getuid()) {
		return 0, -1, errors.New("invalid confined target credentials")
	}
	pid := int(credential.Pid)
	parent, err := processParentPID(pid)
	if err != nil || parent != l.Command.Process.Pid {
		return 0, -1, errors.New("confined target is not a direct supervisor child")
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return 0, -1, fmt.Errorf("pin confined target: %w", err)
	}
	if _, err := l.credentialParent.Write([]byte{'G'}); err != nil {
		_ = unix.Close(pidfd)
		return 0, -1, fmt.Errorf("release confined target: %w", err)
	}
	_ = l.credentialParent.Close()
	l.credentialParent = nil
	return pid, pidfd, nil
}

func processParentPID(pid int) (int, error) {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(status), "\n") {
		if !strings.HasPrefix(line, "PPid:") {
			continue
		}
		var parent int
		if _, err := fmt.Sscanf(line, "PPid:%d", &parent); err != nil || parent <= 0 {
			return 0, errors.New("invalid confined target parent")
		}
		return parent, nil
	}
	return 0, errors.New("missing confined target parent")
}

// TakeExitReader transfers the supervisor-owned target result channel to the
// process lifecycle. It must be called only after Start succeeds.
func (l *Launch) TakeExitReader() *os.File {
	reader := l.exitReader
	l.exitReader = nil
	return reader
}

func SignalPIDFD(fd int, signal syscall.Signal) error {
	if fd < 0 {
		return os.ErrClosed
	}
	return unix.PidfdSendSignal(fd, signal, nil, 0)
}

func ClosePIDFD(fd int) error { return unix.Close(fd) }

func (l *Launch) abort() {
	if l.Command.Process != nil {
		_ = l.Command.Process.Kill()
		_ = l.Command.Wait()
	}
	if l.TargetPIDFD >= 0 {
		_ = unix.Close(l.TargetPIDFD)
		l.TargetPIDFD = -1
	}
}

func (l *Launch) Close() {
	for _, file := range []*os.File{l.policyReader, l.policyWriter, l.statusReader, l.statusWriter,
		l.exitReader, l.exitWriter, l.credentialParent, l.credentialChild} {
		if file != nil {
			_ = file.Close()
		}
	}
}
