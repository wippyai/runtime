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
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
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
	pathACLMutex              sync.Mutex
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

type PathGrant struct {
	Path        string
	handle      windows.Handle
	sid         *windows.SID
	inheritance uint32
	mutexName   string
	closed      bool
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
	_ = windows.FreeSid(allocated)
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
func (s *Sandbox) GrantPath(path string, permissions windows.ACCESS_MASK) (*PathGrant, error) {
	if s == nil || s.sid == nil {
		return nil, errors.New("sandbox identity is closed")
	}
	handle, directory, actual, mutexName, err := openACLPath(path)
	if err != nil {
		return nil, err
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	err = withACLMutation(mutexName, func() error {
		return changeHandleAccess(handle, windows.GRANT_ACCESS, permissions, inheritance, s.sid)
	})
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return &PathGrant{
		Path: actual, handle: handle, sid: s.sid, inheritance: inheritance, mutexName: mutexName,
	}, nil
}

func openACLPath(path string) (windows.Handle, bool, string, string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false, "", "", err
	}
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, false, "", "", err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, false, "", "", err
	}
	actual, err := finalPath(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return 0, false, "", "", err
	}
	mutexName := fmt.Sprintf(`Global\WippyExecACL-%08x-%08x%08x`, info.VolumeSerialNumber,
		info.FileIndexHigh, info.FileIndexLow)
	return handle, info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0, actual, mutexName, nil
}

func withACLMutation(mutexName string, mutate func() error) error {
	pathACLMutex.Lock()
	defer pathACLMutex.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return err
	}
	mutex, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return fmt.Errorf("create cross-runtime ACL mutex: %w", err)
	}
	status, err := windows.WaitForSingleObject(mutex, windows.INFINITE)
	if err != nil {
		return errors.Join(fmt.Errorf("wait for cross-runtime ACL mutex: %w", err), windows.CloseHandle(mutex))
	}
	if status != windows.WAIT_OBJECT_0 && status != windows.WAIT_ABANDONED {
		return errors.Join(fmt.Errorf("unexpected ACL mutex wait status %d", status), windows.CloseHandle(mutex))
	}
	mutationErr := mutate()
	releaseErr := windows.ReleaseMutex(mutex)
	closeErr := windows.CloseHandle(mutex)
	return errors.Join(mutationErr, releaseErr, closeErr)
}

func changeHandleAccess(handle windows.Handle, mode windows.ACCESS_MODE, permissions windows.ACCESS_MASK, inheritance uint32, sid *windows.SID) error {
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return errors.New("NULL DACL paths cannot be safely modified for LPAC")
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
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION,
		nil, nil, updated, nil)
}

func (g *PathGrant) Close() error {
	if g == nil || g.closed {
		return nil
	}
	g.closed = true
	err := withACLMutation(g.mutexName, func() error {
		return changeHandleAccess(g.handle, windows.REVOKE_ACCESS, 0, g.inheritance, g.sid)
	})
	closeErr := windows.CloseHandle(g.handle)
	g.handle = 0
	g.sid = nil
	return errors.Join(err, closeErr)
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

func (s *Sandbox) VerifySpawned(p *SpawnedProcess, job *Job) error {
	if s == nil || s.sid == nil {
		return errors.New("sandbox identity is closed")
	}
	return p.verify(job, s.sid)
}

func (p *SpawnedProcess) verify(job *Job, expectedSID *windows.SID) error {
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
	isContainer, err := tokenBool(token, 29)
	if err != nil {
		return fmt.Errorf("verify AppContainer token: %w", err)
	}
	if !isContainer {
		return errors.New("suspended target does not have an AppContainer token")
	}
	isLPAC, err := tokenBool(token, 46)
	if err != nil {
		return fmt.Errorf("verify LPAC token: %w", err)
	}
	if !isLPAC {
		return errors.New("suspended target does not have a less-privileged AppContainer token")
	}
	packageSID, err := tokenSID(token, 31)
	if err != nil {
		return fmt.Errorf("verify AppContainer package SID: %w", err)
	}
	if expectedSID == nil || packageSID == nil || !packageSID.Equals(expectedSID) {
		return errors.New("suspended target has the wrong AppContainer package SID")
	}
	capabilities, err := tokenGroups(token, 30)
	if err != nil {
		return fmt.Errorf("verify AppContainer capabilities: %w", err)
	}
	if capabilities.GroupCount != 0 {
		return errors.New("suspended target unexpectedly has AppContainer capabilities")
	}
	integrity, err := tokenIntegrity(token)
	if err != nil {
		return fmt.Errorf("verify AppContainer integrity: %w", err)
	}
	if integrity > 0x1000 {
		return fmt.Errorf("suspended target integrity RID %#x exceeds low integrity", integrity)
	}
	return nil
}

func CurrentProcessIsLPAC() (bool, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false, err
	}
	defer token.Close()
	isContainer, err := tokenBool(token, 29)
	if err != nil || !isContainer {
		return false, err
	}
	return tokenBool(token, 46)
}

func tokenBool(token windows.Token, class uintptr) (bool, error) {
	var value uint32
	var returned uint32
	result, _, callErr := getTokenInformation.Call(uintptr(token), class,
		uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value), uintptr(unsafe.Pointer(&returned)))
	if result == 0 {
		return false, callErr
	}
	return value != 0, nil
}

func tokenInfo(token windows.Token, class uint32) ([]byte, error) {
	var size uint32
	err := windows.GetTokenInformation(token, class, nil, 0, &size)
	if err != windows.ERROR_INSUFFICIENT_BUFFER || size == 0 {
		return nil, err
	}
	buffer := make([]byte, size)
	if err := windows.GetTokenInformation(token, class, &buffer[0], size, &size); err != nil {
		return nil, err
	}
	return buffer, nil
}

func tokenSID(token windows.Token, class uint32) (*windows.SID, error) {
	buffer, err := tokenInfo(token, class)
	if err != nil {
		return nil, err
	}
	value := *(**windows.SID)(unsafe.Pointer(&buffer[0]))
	if value == nil || !value.IsValid() {
		return nil, errors.New("token returned an invalid SID")
	}
	return value, nil
}

func tokenGroups(token windows.Token, class uint32) (*windows.Tokengroups, error) {
	buffer, err := tokenInfo(token, class)
	if err != nil {
		return nil, err
	}
	return (*windows.Tokengroups)(unsafe.Pointer(&buffer[0])), nil
}

func tokenIntegrity(token windows.Token) (uint32, error) {
	buffer, err := tokenInfo(token, windows.TokenIntegrityLevel)
	if err != nil {
		return 0, err
	}
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buffer[0]))
	if label.Label.Sid == nil || !label.Label.Sid.IsValid() || label.Label.Sid.SubAuthorityCount() == 0 {
		return 0, errors.New("token returned an invalid integrity SID")
	}
	return label.Label.Sid.SubAuthority(uint32(label.Label.Sid.SubAuthorityCount() - 1)), nil
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
		return nil, fmt.Errorf("encode executable path: %w", err)
	}
	commandLine, err := windows.UTF16FromString(windows.ComposeCommandLine(request.Args))
	if err != nil {
		return nil, fmt.Errorf("encode command line: %w", err)
	}
	workDir, err := windows.UTF16PtrFromString(request.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("encode working directory: %w", err)
	}
	environment, err := environmentBlock(request.Env)
	if err != nil {
		return nil, fmt.Errorf("encode environment: %w", err)
	}
	attributes, err := windows.NewProcThreadAttributeList(4)
	if err != nil {
		return nil, fmt.Errorf("create launch attribute list: %w", err)
	}
	defer attributes.Delete()
	security := securityCapabilities{AppContainerSID: s.sid}
	if err := attributes.Update(procThreadAttributeSecurityCapabilities,
		unsafe.Pointer(&security), unsafe.Sizeof(security)); err != nil {
		return nil, fmt.Errorf("set LPAC security capabilities: %w", err)
	}
	optOut := uint32(processCreationAllApplicationPackagesOptOut)
	if err := attributes.Update(procThreadAttributeAllApplicationPackages,
		unsafe.Pointer(&optOut), unsafe.Sizeof(optOut)); err != nil {
		return nil, fmt.Errorf("set all-application-packages opt-out: %w", err)
	}
	jobs := []windows.Handle{request.Job.handle}
	if err := attributes.Update(procThreadAttributeJobList, unsafe.Pointer(&jobs[0]), unsafe.Sizeof(jobs[0])); err != nil {
		return nil, fmt.Errorf("assign launch Job: %w", err)
	}
	handles := []windows.Handle{request.Stdin, request.Stdout, request.Stderr}
	for _, handle := range handles {
		if handle == 0 {
			return nil, errors.New("LPAC launch requires all standard handles")
		}
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return nil, fmt.Errorf("make standard handle inheritable: %w", err)
		}
		defer windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, 0) //nolint:errcheck
	}
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return nil, fmt.Errorf("set inherited handle list: %w", err)
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
		return nil, fmt.Errorf("create suspended LPAC process: %w", err)
	}
	runtime.KeepAlive(security)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(handles)
	return &SpawnedProcess{Process: process.Process, Thread: process.Thread, PID: process.ProcessId}, nil
}

func environmentBlock(values []string) ([]uint16, error) {
	environment := append([]string(nil), values...)
	hasSystemRoot := false
	for _, value := range values {
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("environment value contains NUL")
		}
		name, _, found := strings.Cut(value, "=")
		if found && strings.EqualFold(name, "SYSTEMROOT") {
			hasSystemRoot = true
		}
	}
	if !hasSystemRoot {
		environment = append(environment, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
	}
	sort.Slice(environment, func(i, j int) bool {
		return strings.ToUpper(environment[i]) < strings.ToUpper(environment[j])
	})
	block := utf16.Encode([]rune(strings.Join(environment, "\x00")))
	return append(block, 0, 0), nil
}
