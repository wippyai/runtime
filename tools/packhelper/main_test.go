// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/service/exec/native/helperimage"
)

func TestPackCarriesExactlyTheVerifiedHelper(t *testing.T) {
	dir := t.TempDir()
	runtimePath := filepath.Join(dir, "wippy")
	helperPath := filepath.Join(dir, "helper")
	if err := os.WriteFile(runtimePath, []byte("\x7fELFruntime"), 0755); err != nil {
		t.Fatal(err)
	}
	helper := []byte("minimal-helper-image")
	if err := os.WriteFile(helperPath, helper, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(helper)
	if err := pack(runtimePath, helperPath, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	runtimeFile, err := os.Open(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeFile.Close()
	info, err := runtimeFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("runtime mode changed to %v", info.Mode())
	}
	section, err := helperimage.Locate(runtimeFile, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(section)
	if err != nil || string(got) != string(helper) {
		t.Fatalf("packed helper = %q, %v", got, err)
	}
	if err := pack(runtimePath, helperPath, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("accepted a second helper trailer")
	}
}

func TestPackRejectsMismatchedDigestWithoutChangingRuntime(t *testing.T) {
	dir := t.TempDir()
	runtimePath := filepath.Join(dir, "wippy")
	helperPath := filepath.Join(dir, "helper")
	original := []byte("\x7fELFruntime")
	if err := os.WriteFile(runtimePath, original, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperPath, []byte("helper"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := pack(runtimePath, helperPath, hex.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("accepted a mismatched helper digest")
	}
	got, err := os.ReadFile(runtimePath)
	if err != nil || string(got) != string(original) {
		t.Fatalf("runtime changed after rejection: %q, %v", got, err)
	}
}
