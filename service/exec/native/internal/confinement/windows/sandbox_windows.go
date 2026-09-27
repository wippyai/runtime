// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	userenv                   = windows.NewLazySystemDLL("userenv.dll")
	createAppContainerProfile = userenv.NewProc("CreateAppContainerProfile")
	deleteAppContainerProfile = userenv.NewProc("DeleteAppContainerProfile")
	pathACLMutex              sync.Mutex
)

// Sandbox is a per-launch LPAC identity. It is deliberately unique so two
// confined processes cannot address each other's private objects by package
// identity.
type Sandbox struct {
	name       string
	sid        *windows.SID
	capability *windows.SID
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
	capability, err := windows.CreateWellKnownSid(windows.WinCapabilityInternetClientSid)
	if err != nil {
		_ = deleteProfile(name)
		return nil, fmt.Errorf("create internet-client capability SID: %w", err)
	}
	return &Sandbox{name: name, sid: sid, capability: capability}, nil
}

func (s *Sandbox) Close() error {
	if s == nil || s.name == "" {
		return nil
	}
	name := s.name
	s.name = ""
	s.sid = nil
	s.capability = nil
	return deleteProfile(name)
}

// PrivateHome returns the package-private, low-integrity directory Windows
// creates for this AppContainer profile.
func (s *Sandbox) PrivateHome() (string, error) {
	if s == nil || s.name == "" {
		return "", errors.New("sandbox identity is closed")
	}
	localAppData, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", err
	}
	path := filepath.Join(localAppData, "Packages", s.name, "AC")
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("AppContainer private home is not a directory")
	}
	return path, nil
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
