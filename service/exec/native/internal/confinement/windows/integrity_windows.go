// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	systemMandatoryLabelACEType = 0x11
	mandatoryNoWriteUp          = windows.ACCESS_MASK(0x1)
	mandatoryLowRID             = 0x1000
)

// RequireLowIntegrityDirectory verifies the host provisioning needed for an
// LPAC writable work tree. The label must apply to the directory itself and
// inherit to both child files and directories.
func RequireLowIntegrityDirectory(path string) error {
	handle, err := openIntegrityPath(path, windows.READ_CONTROL)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read mandatory integrity label: %w", err)
	}
	defer runtime.KeepAlive(descriptor)
	sacl, _, err := descriptor.SACL()
	if err != nil || sacl == nil {
		return errors.New("writable confinement directory has no mandatory integrity label")
	}
	for index := uint16(0); index < sacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(sacl, uint32(index), &ace); err != nil {
			return fmt.Errorf("read mandatory integrity ACE: %w", err)
		}
		if ace == nil || ace.Header.AceType != systemMandatoryLabelACEType ||
			ace.Header.AceSize < uint16(unsafe.Offsetof(ace.SidStart))+12 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() || sid.IdentifierAuthority() != windows.SECURITY_MANDATORY_LABEL_AUTHORITY ||
			sid.SubAuthorityCount() == 0 {
			continue
		}
		rid := sid.SubAuthority(uint32(sid.SubAuthorityCount() - 1))
		inherit := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
		if rid <= mandatoryLowRID && ace.Mask&mandatoryNoWriteUp != 0 &&
			ace.Header.AceFlags&inherit == inherit && ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 {
			return nil
		}
	}
	return errors.New("writable confinement directory requires an inheritable low-integrity no-write-up label")
}

// SetLowIntegrityDirectory provisions a runtime-owned Windows work root. It is
// intentionally separate from launch: labels are object-wide and must never be
// lowered temporarily behind another runtime's back.
func SetLowIntegrityDirectory(path string) error {
	handle, err := openIntegrityPath(path, windows.READ_CONTROL)
	if err != nil {
		return err
	}
	_ = windows.CloseHandle(handle)
	descriptor, err := windows.SecurityDescriptorFromString("S:(ML;OICI;NW;;;LW)")
	if err != nil {
		return err
	}
	sacl, _, err := descriptor.SACL()
	if err != nil || sacl == nil {
		return errors.New("build low-integrity mandatory label")
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION,
		nil, nil, nil, sacl)
	runtime.KeepAlive(descriptor)
	return err
}

func openIntegrityPath(path string, access windows.ACCESS_MASK) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateFile(name, uint32(access|windows.FILE_READ_ATTRIBUTES),
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		_ = windows.CloseHandle(handle)
		return 0, errors.New("mandatory integrity path is not a directory")
	}
	return handle, nil
}
