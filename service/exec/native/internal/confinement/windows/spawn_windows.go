// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	procThreadAttributeSecurityCapabilities     = 0x00020009
	procThreadAttributeJobList                  = 0x0002000d
	procThreadAttributeChildProcessPolicy       = 0x0002000e
	procThreadAttributeAllApplicationPackages   = 0x0002000f
	processCreationAllApplicationPackagesOptOut = 0x00000001
)

type securityCapabilities struct {
	AppContainerSID *windows.SID
	Capabilities    *windows.SIDAndAttributes
	CapabilityCount uint32
	Reserved        uint32
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
	return s.spawnSuspended(request, true)
}

func (s *Sandbox) spawnSuspended(request SpawnRequest, allApplicationPackagesOptOut bool) (*SpawnedProcess, error) {
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
	attributes, err := windows.NewProcThreadAttributeList(5)
	if err != nil {
		return nil, fmt.Errorf("create launch attribute list: %w", err)
	}
	defer attributes.Delete()
	capabilities := []windows.SIDAndAttributes{{Sid: s.capability, Attributes: windows.SE_GROUP_ENABLED}}
	security := securityCapabilities{
		AppContainerSID: s.sid,
		Capabilities:    &capabilities[0],
		CapabilityCount: uint32(len(capabilities)),
	}
	if err := attributes.Update(procThreadAttributeSecurityCapabilities,
		unsafe.Pointer(&security), unsafe.Sizeof(security)); err != nil {
		return nil, fmt.Errorf("set LPAC security capabilities: %w", err)
	}
	optOut := uint32(processCreationAllApplicationPackagesOptOut)
	if allApplicationPackagesOptOut {
		if err := attributes.Update(procThreadAttributeAllApplicationPackages,
			unsafe.Pointer(&optOut), unsafe.Sizeof(optOut)); err != nil {
			return nil, fmt.Errorf("set all-application-packages opt-out: %w", err)
		}
	}
	childPolicy := uint32(processCreationChildProcessRestricted)
	if err := attributes.Update(procThreadAttributeChildProcessPolicy,
		unsafe.Pointer(&childPolicy), unsafe.Sizeof(childPolicy)); err != nil {
		return nil, fmt.Errorf("set child-process restriction: %w", err)
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
	runtime.KeepAlive(capabilities)
	runtime.KeepAlive(childPolicy)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(handles)
	return &SpawnedProcess{Process: process.Process, Thread: process.Thread, PID: process.ProcessId}, nil
}

func environmentBlock(values []string) ([]uint16, error) {
	systemRoot, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return nil, fmt.Errorf("resolve SYSTEMROOT: %w", err)
	}
	localAppData, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return nil, fmt.Errorf("resolve LOCALAPPDATA: %w", err)
	}
	return buildEnvironmentBlock(values, []requiredEnvironmentVariable{
		{name: "SYSTEMROOT", value: systemRoot},
		{name: "LOCALAPPDATA", value: localAppData},
	})
}

type requiredEnvironmentVariable struct {
	name  string
	value string
}

func buildEnvironmentBlock(values []string, required []requiredEnvironmentVariable) ([]uint16, error) {
	requiredByName := make(map[string]requiredEnvironmentVariable, len(required))
	for _, variable := range required {
		if variable.name == "" || strings.ContainsAny(variable.name, "=\x00") ||
			variable.value == "" || strings.ContainsRune(variable.value, 0) {
			return nil, fmt.Errorf("required Windows environment variable %s is unavailable", variable.name)
		}
		requiredByName[strings.ToUpper(variable.name)] = variable
	}
	environment := make([]string, 0, len(values)+len(required))
	seen := make(map[string]struct{}, len(values)+len(required))
	for _, value := range values {
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("environment value contains NUL")
		}
		name, _, found := strings.Cut(value, "=")
		if !found || name == "" {
			return nil, errors.New("environment entry has no name")
		}
		key := strings.ToUpper(name)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate Windows environment variable %s", name)
		}
		seen[key] = struct{}{}
		if reserved, ok := requiredByName[key]; ok {
			return nil, fmt.Errorf("environment overrides required Windows variable %s", reserved.name)
		}
		if key == "TEMP" || key == "TMP" {
			return nil, fmt.Errorf("environment overrides AppContainer-managed Windows variable %s", name)
		}
		environment = append(environment, value)
	}
	for _, variable := range required {
		environment = append(environment, variable.name+"="+variable.value)
	}
	sort.Slice(environment, func(i, j int) bool {
		return strings.ToUpper(environment[i]) < strings.ToUpper(environment[j])
	})
	block := utf16.Encode([]rune(strings.Join(environment, "\x00")))
	return append(block, 0, 0), nil
}
