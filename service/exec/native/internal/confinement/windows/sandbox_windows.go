// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	procThreadAttributeSecurityCapabilities     = 0x00020009
	procThreadAttributeJobList                  = 0x0002000d
	procThreadAttributeAllApplicationPackages   = 0x0002000f
	processCreationAllApplicationPackagesOptOut = 0x00000001
)

var (
	userenv                   = windows.NewLazySystemDLL("userenv.dll")
	createAppContainerProfile = userenv.NewProc("CreateAppContainerProfile")
	deleteAppContainerProfile = userenv.NewProc("DeleteAppContainerProfile")
	advapi32                  = windows.NewLazySystemDLL("advapi32.dll")
	getTokenInformation       = advapi32.NewProc("GetTokenInformation")
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	isProcessInJob            = kernel32.NewProc("IsProcessInJob")
)

type securityCapabilities struct {
	AppContainerSID *windows.SID
	Capabilities    *windows.SIDAndAttributes
	CapabilityCount uint32
	Reserved        uint32
}

// Sandbox is a per-launch LPAC identity. It is deliberately unique so two
// confined processes cannot address each other's private objects by package
// identity.
type Sandbox struct {
	name string
	sid  *windows.SID
}

func NewSandbox() (*Sandbox, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	name := "wippy.exec." + hex.EncodeToString(random)
	nameValue, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	display, _ := windows.UTF16PtrFromString("Wippy confined process")
	description, _ := windows.UTF16PtrFromString("Ephemeral Wippy native exec confinement identity")
	var allocated *windows.SID
	result, _, _ := createAppContainerProfile.Call(
		uintptr(unsafe.Pointer(nameValue)), uintptr(unsafe.Pointer(display)),
		uintptr(unsafe.Pointer(description)), 0, 0, uintptr(unsafe.Pointer(&allocated)))
	if int32(result) < 0 {
		return nil, fmt.Errorf("create AppContainer profile: HRESULT 0x%08x", uint32(result))
	}
	if allocated == nil {
		_ = deleteProfile(name)
		return nil, errors.New("AppContainer profile returned no package SID")
	}
	sid, err := allocated.Copy()
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(allocated)))
	if err != nil {
		_ = deleteProfile(name)
		return nil, err
	}
	return &Sandbox{name: name, sid: sid}, nil
}

func (s *Sandbox) Close() error {
	if s == nil || s.name == "" {
		return nil
	}
	name := s.name
	s.name = ""
	s.sid = nil
	return deleteProfile(name)
}

func deleteProfile(name string) error {
	value, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	result, _, _ := deleteAppContainerProfile.Call(uintptr(unsafe.Pointer(value)))
	if int32(result) < 0 {
		return fmt.Errorf("delete AppContainer profile: HRESULT 0x%08x", uint32(result))
	}
	return nil
}

// GrantPath adds the launch package SID to a path DACL. The returned cleanup
// removes only this unique SID, preserving unrelated ACL changes.
func (s *Sandbox) GrantPath(path string, permissions windows.ACCESS_MASK) (func() error, error) {
	if s == nil || s.sid == nil {
		return nil, errors.New("sandbox identity is closed")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if info.IsDir() {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	if err := changePathAccess(path, windows.GRANT_ACCESS, permissions, inheritance, s.sid); err != nil {
		return nil, err
	}
	return func() error {
		return changePathAccess(path, windows.REVOKE_ACCESS, 0, inheritance, s.sid)
	}, nil
}

func changePathAccess(path string, mode windows.ACCESS_MODE, permissions windows.ACCESS_MASK, inheritance uint32, sid *windows.SID) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	var pinner runtime.Pinner
	pinner.Pin(sid)
	defer pinner.Unpin()
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: permissions,
		AccessMode:        mode,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
	updated, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{entry}, dacl)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION,
		nil, nil, updated, nil)
}

type SpawnRequest struct {
	Path    string
	Args    []string
	Env     []string
	WorkDir string
	Stdin   windows.Handle
	Stdout  windows.Handle
	Stderr  windows.Handle
	Job     *Job
}

type SpawnedProcess struct {
	Process windows.Handle
	Thread  windows.Handle
	PID     uint32
}

func (p *SpawnedProcess) Verify(job *Job) error {
	if p == nil || p.Process == 0 || job == nil || job.handle == 0 {
		return errors.New("invalid suspended LPAC process")
	}
	var inJob int32
	result, _, callErr := isProcessInJob.Call(uintptr(p.Process), uintptr(job.handle), uintptr(unsafe.Pointer(&inJob)))
	if result == 0 {
		return fmt.Errorf("verify Job membership: %w", callErr)
	}
	if inJob == 0 {
		return errors.New("suspended target is not in confinement Job")
	}
	var token windows.Token
	if err := windows.OpenProcessToken(p.Process, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	var isContainer uint32
	var returned uint32
	result, _, callErr = getTokenInformation.Call(uintptr(token), 29,
		uintptr(unsafe.Pointer(&isContainer)), unsafe.Sizeof(isContainer), uintptr(unsafe.Pointer(&returned)))
	if result == 0 {
		return fmt.Errorf("verify AppContainer token: %w", callErr)
	}
	if isContainer == 0 {
		return errors.New("suspended target does not have an AppContainer token")
	}
	return nil
}

func CurrentProcessIsAppContainer() (bool, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false, err
	}
	defer token.Close()
	var isContainer uint32
	var returned uint32
	result, _, callErr := getTokenInformation.Call(uintptr(token), 29,
		uintptr(unsafe.Pointer(&isContainer)), unsafe.Sizeof(isContainer), uintptr(unsafe.Pointer(&returned)))
	if result == 0 {
		return false, callErr
	}
	return isContainer != 0, nil
}

func (p *SpawnedProcess) Resume() error {
	if p == nil || p.Thread == 0 {
		return errors.New("invalid suspended LPAC thread")
	}
	_, err := windows.ResumeThread(p.Thread)
	if err == nil {
		_ = windows.CloseHandle(p.Thread)
		p.Thread = 0
	}
	return err
}

func (p *SpawnedProcess) Kill(exitCode uint32) error {
	if p == nil || p.Process == 0 {
		return nil
	}
	return windows.TerminateProcess(p.Process, exitCode)
}

func (p *SpawnedProcess) Wait() (uint32, error) {
	if p == nil || p.Process == 0 {
		return 0, errors.New("invalid LPAC process")
	}
	status, err := windows.WaitForSingleObject(p.Process, windows.INFINITE)
	if err != nil {
		return 0, err
	}
	if status != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("unexpected process wait status %d", status)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.Process, &code); err != nil {
		return 0, err
	}
	return code, nil
}

func (p *SpawnedProcess) Close() error {
	if p == nil {
		return nil
	}
	var result error
	if p.Thread != 0 {
		result = errors.Join(result, windows.CloseHandle(p.Thread))
		p.Thread = 0
	}
	if p.Process != 0 {
		result = errors.Join(result, windows.CloseHandle(p.Process))
		p.Process = 0
	}
	return result
}

func (s *Sandbox) SpawnSuspended(request SpawnRequest) (*SpawnedProcess, error) {
	if s == nil || s.sid == nil {
		return nil, errors.New("sandbox identity is closed")
	}
	if request.Path == "" || request.WorkDir == "" || request.Job == nil || request.Job.handle == 0 {
		return nil, errors.New("incomplete LPAC launch request")
	}
	application, err := windows.UTF16PtrFromString(request.Path)
	if err != nil {
		return nil, err
	}
	commandLine, err := windows.UTF16FromString(windows.ComposeCommandLine(request.Args))
	if err != nil {
		return nil, err
	}
	workDir, err := windows.UTF16PtrFromString(request.WorkDir)
	if err != nil {
		return nil, err
	}
	environment, err := environmentBlock(request.Env)
	if err != nil {
		return nil, err
	}
	attributes, err := windows.NewProcThreadAttributeList(4)
	if err != nil {
		return nil, err
	}
	defer attributes.Delete()
	security := securityCapabilities{AppContainerSID: s.sid}
	if err := attributes.Update(procThreadAttributeSecurityCapabilities,
		unsafe.Pointer(&security), unsafe.Sizeof(security)); err != nil {
		return nil, err
	}
	optOut := uint32(processCreationAllApplicationPackagesOptOut)
	if err := attributes.Update(procThreadAttributeAllApplicationPackages,
		unsafe.Pointer(&optOut), unsafe.Sizeof(optOut)); err != nil {
		return nil, err
	}
	jobs := []windows.Handle{request.Job.handle}
	if err := attributes.Update(procThreadAttributeJobList, unsafe.Pointer(&jobs[0]), unsafe.Sizeof(jobs[0])); err != nil {
		return nil, err
	}
	handles := []windows.Handle{request.Stdin, request.Stdout, request.Stderr}
	for _, handle := range handles {
		if handle == 0 {
			return nil, errors.New("LPAC launch requires all standard handles")
		}
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return nil, err
		}
		defer windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, 0) //nolint:errcheck
	}
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return nil, err
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES,
			StdInput: request.Stdin, StdOutput: request.Stdout, StdErr: request.Stderr,
		},
		ProcThreadAttributeList: attributes.List(),
	}
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT |
		windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_DEFAULT_ERROR_MODE)
	if err := windows.CreateProcess(application, &commandLine[0], nil, nil, true, flags,
		&environment[0], workDir, &startup.StartupInfo, &process); err != nil {
		return nil, err
	}
	runtime.KeepAlive(security)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(handles)
	return &SpawnedProcess{Process: process.Process, Thread: process.Thread, PID: process.ProcessId}, nil
}

func environmentBlock(values []string) ([]uint16, error) {
	joined := strings.Join(values, "\x00") + "\x00\x00"
	return windows.UTF16FromString(joined)
}
