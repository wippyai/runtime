package component

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
	"testing/fstest"

	fsapi "github.com/wippyai/runtime/api/fs"
)

type testFSRegistry struct {
	fsByName map[string]fsapi.FS
}

func (r testFSRegistry) GetFS(name string) (fsapi.FS, bool) {
	v, ok := r.fsByName[name]
	return v, ok
}

func TestVerifyHash(t *testing.T) {
	data := []byte("wasm-bytes")
	sum := sha256.Sum256(data)
	valid := "sha256:" + hex.EncodeToString(sum[:])

	if err := VerifyHash(data, valid); err != nil {
		t.Fatalf("VerifyHash(valid) error = %v", err)
	}

	if err := VerifyHash(data, "sha256"); err == nil {
		t.Fatal("VerifyHash(invalid format) expected error")
	}

	if err := VerifyHash(data, "md5:abcd"); err == nil {
		t.Fatal("VerifyHash(unsupported algorithm) expected error")
	}

	if err := VerifyHash(data, "sha256:deadbeef"); err == nil {
		t.Fatal("VerifyHash(mismatch) expected error")
	}
}

func TestLoadWASM(t *testing.T) {
	mapFS := fstest.MapFS{
		"mod.wasm": {Data: []byte("hello wasm"), Mode: 0o644},
	}
	reg := testFSRegistry{
		fsByName: map[string]fsapi.FS{
			"app:test": fsapi.NewReadOnlyFS(mapFS),
		},
	}

	data, err := LoadWASM(reg, "app:test", "mod.wasm")
	if err != nil {
		t.Fatalf("LoadWASM() error = %v", err)
	}
	if string(data) != "hello wasm" {
		t.Fatalf("LoadWASM() data = %q, want %q", string(data), "hello wasm")
	}

	if _, err := LoadWASM(reg, "missing:fs", "mod.wasm"); err == nil {
		t.Fatal("LoadWASM() expected filesystem not found error")
	}

	if _, err := LoadWASM(reg, "app:test", "missing.wasm"); err == nil {
		t.Fatal("LoadWASM() expected open file error")
	}
}

func TestLoadAndVerifyWASM(t *testing.T) {
	data := []byte("component payload")
	sum := sha256.Sum256(data)
	valid := "sha256:" + hex.EncodeToString(sum[:])

	mapFS := fstest.MapFS{
		"component.wasm": {Data: data, Mode: fs.FileMode(0o644)},
	}
	reg := testFSRegistry{
		fsByName: map[string]fsapi.FS{
			"app:test": fsapi.NewReadOnlyFS(mapFS),
		},
	}

	got, err := LoadAndVerifyWASM(reg, "app:test", "component.wasm", valid)
	if err != nil {
		t.Fatalf("LoadAndVerifyWASM(valid) error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("LoadAndVerifyWASM(valid) data mismatch")
	}

	if _, err := LoadAndVerifyWASM(reg, "app:test", "component.wasm", "sha256:deadbeef"); err == nil {
		t.Fatal("LoadAndVerifyWASM(mismatch) expected error")
	}
}
