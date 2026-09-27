// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/service/exec/native/helperimage"
)

func TestVerifiedHelperIsSealedCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	contents := []byte("verified image")
	if err := os.WriteFile(path, contents, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	image, err := OpenVerifiedHelper(path, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if err := os.WriteFile(path, []byte("replacement"), 0700); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(contents))
	if _, err := image.ReadAt(got, 0); err != nil || string(got) != string(contents) {
		t.Fatalf("verified image changed: %q, %v", got, err)
	}
	if _, err := image.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("sealed image accepted a write")
	}
	if _, err := OpenVerifiedHelper(path, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("replacement passed original digest")
	}
}

func TestEmbeddedHelperUsesExactTrailerImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	contents := []byte("embedded verified image")
	image := append([]byte("\x7fELFruntime"), contents...)
	var trailer [helperimage.TrailerSize]byte
	copy(trailer[:], helperimage.Magic)
	binary.LittleEndian.PutUint64(trailer[len(helperimage.Magic):], uint64(len(contents)))
	image = append(image, trailer[:]...)
	if err := os.WriteFile(path, image, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	verified, err := OpenVerifiedEmbeddedHelper(path, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	defer verified.Close()
	got := make([]byte, len(contents))
	if _, err := verified.ReadAt(got, 0); err != nil || string(got) != string(contents) {
		t.Fatalf("embedded helper = %q, %v", got, err)
	}
	if _, err := OpenVerifiedEmbeddedHelper(path, hex.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("accepted mismatched embedded helper")
	}
}
