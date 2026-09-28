// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/wippyai/runtime/service/exec/native/helperimage"
	"golang.org/x/sys/unix"
)

const maxHelperBytes = helperimage.MaxBytes

// OpenVerifiedHelper copies an installed helper into a sealed executable
// memfd, then checks its pinned build digest. The child executes this exact
// immutable copy; replacing the on-disk helper after verification cannot
// change which code runs.
func OpenVerifiedHelper(path, expectedSHA256 string) (*os.File, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("helper is not a regular file")
	}
	return openVerifiedHelperImage(source, info.Size(), expectedSHA256)
}

// OpenVerifiedEmbeddedHelper reads the trailer of the running runtime binary.
// Installers therefore need only the normal wippy executable; the helper is
// still a separately built, minimal program and its bytes are hash checked.
func OpenVerifiedEmbeddedHelper(runtimePath, expectedSHA256 string) (*os.File, error) {
	source, err := os.Open(runtimePath)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("runtime has no embedded confinement helper")
	}
	section, err := helperimage.Locate(source, info.Size())
	if err != nil {
		return nil, err
	}
	return openVerifiedHelperImage(section, section.Size(), expectedSHA256)
}

func openVerifiedHelperImage(source io.Reader, size int64, expectedSHA256 string) (*os.File, error) {
	if len(expectedSHA256) != sha256.Size*2 {
		return nil, errors.New("missing or invalid helper build digest")
	}
	expected, err := hex.DecodeString(expectedSHA256)
	if err != nil {
		return nil, fmt.Errorf("invalid helper build digest: %w", err)
	}
	if size <= 0 || size > maxHelperBytes {
		return nil, errors.New("helper is not a bounded regular file")
	}
	flags := unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING | unix.MFD_EXEC
	fd, err := unix.MemfdCreate("wippy-confine-linux", flags)
	if errors.Is(err, syscall.EINVAL) {
		fd, err = unix.MemfdCreate("wippy-confine-linux", flags&^unix.MFD_EXEC)
	}
	if err != nil {
		return nil, fmt.Errorf("create sealed helper image: %w", err)
	}
	image := os.NewFile(uintptr(fd), "wippy-confine-linux")
	defer func() {
		if image != nil {
			_ = image.Close()
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(image, hash), io.LimitReader(source, maxHelperBytes+1))
	if err != nil || n != size || n > maxHelperBytes {
		return nil, errors.New("helper changed while verifying")
	}
	if got := hash.Sum(nil); !equalDigest(got, expected) {
		return nil, errors.New("helper build digest mismatch")
	}
	if err := unix.Fchmod(fd, 0500); err != nil {
		return nil, fmt.Errorf("make helper executable: %w", err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS,
		unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, fmt.Errorf("seal verified helper: %w", err)
	}
	image, result := nil, image
	return result, nil
}

func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var mismatch byte
	for i := range a {
		mismatch |= a[i] ^ b[i]
	}
	return mismatch == 0
}
