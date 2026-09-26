// SPDX-License-Identifier: MPL-2.0

// Command packhelper appends the separately built confinement helper to a
// Linux runtime executable. The runtime verifies its build-stamped SHA-256
// before copying the image into a sealed memfd for each confined launch.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/service/exec/native/helperimage"
)

func pack(runtimePath, helperPath, expectedSHA256 string) error {
	runtimeFile, err := os.Open(runtimePath)
	if err != nil {
		return err
	}
	defer runtimeFile.Close()
	runtimeInfo, err := runtimeFile.Stat()
	if err != nil || !runtimeInfo.Mode().IsRegular() {
		return errors.New("runtime must be a regular file")
	}
	var elf [4]byte
	if _, err := runtimeFile.ReadAt(elf[:], 0); err != nil || elf != [4]byte{0x7f, 'E', 'L', 'F'} {
		return errors.New("runtime must be a Linux ELF executable")
	}
	if _, err := helperimage.Locate(runtimeFile, runtimeInfo.Size()); err == nil {
		return errors.New("runtime already has an embedded helper")
	}
	helper, err := os.Open(helperPath)
	if err != nil {
		return err
	}
	defer helper.Close()
	helperInfo, err := helper.Stat()
	if err != nil || !helperInfo.Mode().IsRegular() || helperInfo.Size() <= 0 || helperInfo.Size() > helperimage.MaxBytes {
		return errors.New("helper must be a bounded regular file")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, helper); err != nil {
		return err
	}
	if hex.EncodeToString(digest.Sum(nil)) != expectedSHA256 {
		return errors.New("helper digest does not match runtime build stamp")
	}
	if _, err := helper.Seek(0, io.SeekStart); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(runtimePath), ".wippy-packhelper-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := temporary.Chmod(runtimeInfo.Mode().Perm()); err != nil {
		return err
	}
	if _, err := io.Copy(temporary, runtimeFile); err != nil {
		return err
	}
	if copied, err := io.Copy(temporary, helper); err != nil || copied != helperInfo.Size() {
		return fmt.Errorf("copy helper image: %d bytes: %w", copied, err)
	}
	var trailer [helperimage.TrailerSize]byte
	copy(trailer[:], helperimage.Magic)
	binary.LittleEndian.PutUint64(trailer[len(helperimage.Magic):], uint64(helperInfo.Size()))
	if _, err := temporary.Write(trailer[:]); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), runtimePath)
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: packhelper RUNTIME HELPER SHA256")
		os.Exit(2)
	}
	if err := pack(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, "packhelper:", err)
		os.Exit(1)
	}
}
