// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	processChildProcessPolicy             = 13
	processCreationChildProcessRestricted = 0x00000001
	lpacAccess                            = windows.ACCESS_MASK(0x2)
	maximumAllowed                        = windows.ACCESS_MASK(0x02000000)
	maxPrivilegeBytes                     = 64 * 1024
)

var (
	advapi32                   = windows.NewLazySystemDLL("advapi32.dll")
	accessCheck                = advapi32.NewProc("AccessCheck")
	getTokenInformation        = advapi32.NewProc("GetTokenInformation")
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	isProcessInJob             = kernel32.NewProc("IsProcessInJob")
	getProcessMitigationPolicy = kernel32.NewProc("GetProcessMitigationPolicy")
)

type genericMapping struct {
	read    windows.ACCESS_MASK
	write   windows.ACCESS_MASK
	execute windows.ACCESS_MASK
	all     windows.ACCESS_MASK
}

func (s *Sandbox) VerifySpawned(p *SpawnedProcess, job *Job) error {
	if s == nil || s.sid == nil || s.capability == nil {
		return errors.New("sandbox identity is closed")
	}
	return p.verify(job, s.sid, s.capability)
}

func (p *SpawnedProcess) verify(job *Job, expectedSID, expectedCapability *windows.SID) error {
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
	if err := job.verifySingleton(); err != nil {
		return fmt.Errorf("verify singleton Job: %w", err)
	}
	var childPolicy uint32
	result, _, callErr = getProcessMitigationPolicy.Call(uintptr(p.Process), processChildProcessPolicy,
		uintptr(unsafe.Pointer(&childPolicy)), unsafe.Sizeof(childPolicy))
	if result == 0 {
		return fmt.Errorf("verify child-process policy: %w", callErr)
	}
	if childPolicy != processCreationChildProcessRestricted {
		return fmt.Errorf("suspended target child-process policy %#x, want %#x",
			childPolicy, uint32(processCreationChildProcessRestricted))
	}
	var token windows.Token
	if err := windows.OpenProcessToken(p.Process, windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &token); err != nil {
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
	isLPAC, err := tokenHasLPACAccess(token)
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
	if err := verifyTokenCapability(token, expectedCapability); err != nil {
		return fmt.Errorf("verify AppContainer capabilities: %w", err)
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
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &token); err != nil {
		return false, err
	}
	defer token.Close()
	isContainer, err := tokenBool(token, 29)
	if err != nil || !isContainer {
		return false, err
	}
	return tokenHasLPACAccess(token)
}

// tokenHasLPACAccess proves the access-check semantics that distinguish an
// LPAC from an ordinary AppContainer. TokenIsLessPrivilegedAppContainer is
// present in the SDK but returns ERROR_INVALID_PARAMETER on supported Windows
// builds, so it cannot be used as a fail-closed verifier.
//
// The descriptor deliberately grants one bit to ALL APPLICATION PACKAGES and
// a different bit to ALL RESTRICTED APPLICATION PACKAGES. A verified LPAC must
// receive only the restricted-package bit. This is the same public-API proof
// used by Chromium's Windows sandbox tests.
func tokenHasLPACAccess(token windows.Token) (bool, error) {
	var client windows.Token
	if err := windows.DuplicateTokenEx(token, windows.TOKEN_QUERY, nil, windows.SecurityIdentification,
		windows.TokenImpersonation, &client); err != nil {
		return false, fmt.Errorf("duplicate token for LPAC access check: %w", err)
	}
	defer client.Close()
	descriptor, err := windows.SecurityDescriptorFromString(
		"O:SYG:SYD:(A;;0x3;;;WD)(A;;0x1;;;S-1-15-2-1)(A;;0x2;;;S-1-15-2-2)")
	if err != nil {
		return false, fmt.Errorf("build LPAC access-check descriptor: %w", err)
	}
	mapping := genericMapping{}
	privilegeBytes := uint32(1024)
	privileges := make([]uintptr, (privilegeBytes+uint32(unsafe.Sizeof(uintptr(0)))-1)/uint32(unsafe.Sizeof(uintptr(0))))
	for {
		var granted windows.ACCESS_MASK
		var allowed int32
		result, _, callErr := accessCheck.Call(
			uintptr(unsafe.Pointer(descriptor)),
			uintptr(client),
			uintptr(maximumAllowed),
			uintptr(unsafe.Pointer(&mapping)),
			uintptr(unsafe.Pointer(&privileges[0])),
			uintptr(unsafe.Pointer(&privilegeBytes)),
			uintptr(unsafe.Pointer(&granted)),
			uintptr(unsafe.Pointer(&allowed)),
		)
		runtime.KeepAlive(descriptor)
		if result != 0 {
			return allowed != 0 && granted == lpacAccess, nil
		}
		if !errors.Is(callErr, windows.ERROR_INSUFFICIENT_BUFFER) || privilegeBytes == 0 || privilegeBytes > maxPrivilegeBytes {
			return false, fmt.Errorf("check LPAC package access: %w", callErr)
		}
		privileges = make([]uintptr, (privilegeBytes+uint32(unsafe.Sizeof(uintptr(0)))-1)/uint32(unsafe.Sizeof(uintptr(0))))
	}
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

func verifyTokenCapability(token windows.Token, expected *windows.SID) error {
	if expected == nil || !expected.IsValid() {
		return errors.New("expected capability SID is invalid")
	}
	buffer, err := tokenInfo(token, 30) // TokenCapabilities
	if err != nil {
		return err
	}
	groupsOffset := int(unsafe.Offsetof(windows.Tokengroups{}.Groups))
	if len(buffer) < groupsOffset+int(unsafe.Sizeof(windows.SIDAndAttributes{})) {
		return errors.New("token returned an invalid capability list")
	}
	groups := (*windows.Tokengroups)(unsafe.Pointer(&buffer[0]))
	if groups.GroupCount != 1 {
		return fmt.Errorf("suspended target has %d capabilities, want 1", groups.GroupCount)
	}
	capability := groups.Groups[0]
	if capability.Sid == nil || !capability.Sid.IsValid() || !capability.Sid.Equals(expected) {
		return errors.New("suspended target has the wrong capability SID")
	}
	if capability.Attributes != windows.SE_GROUP_ENABLED {
		return fmt.Errorf("suspended target capability attributes %#x, want %#x",
			capability.Attributes, uint32(windows.SE_GROUP_ENABLED))
	}
	runtime.KeepAlive(buffer)
	return nil
}

func tokenIntegrity(token windows.Token) (uint32, error) {
	buffer, err := tokenInfo(token, windows.TokenIntegrityLevel)
	if err != nil {
		return 0, err
	}
	return integrityRID(buffer)
}

func integrityRID(buffer []byte) (uint32, error) {
	pointerSize := int(unsafe.Sizeof(uintptr(0)))
	if len(buffer) < pointerSize+4 {
		return 0, errors.New("token returned an invalid integrity SID")
	}
	var sidAddress uintptr
	if pointerSize == 8 {
		sidAddress = uintptr(binary.LittleEndian.Uint64(buffer[:8]))
	} else {
		sidAddress = uintptr(binary.LittleEndian.Uint32(buffer[:4]))
	}
	attributes := binary.LittleEndian.Uint32(buffer[pointerSize : pointerSize+4])
	base := uintptr(unsafe.Pointer(&buffer[0]))
	if sidAddress < base || sidAddress-base > uintptr(len(buffer)) ||
		attributes&windows.SE_GROUP_INTEGRITY == 0 {
		return 0, errors.New("token returned an invalid integrity SID")
	}
	sid := buffer[int(sidAddress-base):]
	if len(sid) < 8 || sid[0] != 1 || sid[1] == 0 ||
		sid[2] != 0 || sid[3] != 0 || sid[4] != 0 || sid[5] != 0 || sid[6] != 0 || sid[7] != 16 {
		return 0, errors.New("token returned an invalid integrity SID")
	}
	count := int(sid[1])
	required := 8 + count*4
	if required > len(sid) {
		return 0, errors.New("token returned an invalid integrity SID")
	}
	return binary.LittleEndian.Uint32(sid[8+(count-1)*4 : required]), nil
}
