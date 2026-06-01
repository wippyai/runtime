// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readPub(t *testing.T, dir string) ed25519.PublicKey {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dir, "release_key.pub"))
	if err != nil {
		t.Fatalf("read pub: %v", err)
	}

	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("decode pub: %v", err)
	}

	return ed25519.PublicKey(pub)
}

func readSig(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path + ".sig")
	if err != nil {
		t.Fatalf("read sig: %v", err)
	}

	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}

	return sig
}

func TestRunSignsAndVerifies(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	dir := t.TempDir()
	payload := filepath.Join(dir, "wippy-linux-amd64")
	contents := []byte("binary contents under test")
	if err := os.WriteFile(payload, contents, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	if err := run([]string{payload}, hex.EncodeToString(priv)); err != nil {
		t.Fatalf("run: %v", err)
	}

	sig := readSig(t, payload)
	if !ed25519.Verify(pub, contents, sig) {
		t.Fatal("signature did not verify against payload")
	}

	if !ed25519.Verify(readPub(t, dir), contents, sig) {
		t.Fatal("signature did not verify against published public key")
	}

	if ed25519.Verify(pub, []byte("tampered"), sig) {
		t.Fatal("signature verified against tampered contents")
	}
}

func TestSeedAndFullKeyMatch(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	fromFull, err := loadKey(hex.EncodeToString(priv))
	if err != nil {
		t.Fatalf("load full key: %v", err)
	}

	fromSeed, err := loadKey(hex.EncodeToString(priv.Seed()))
	if err != nil {
		t.Fatalf("load seed key: %v", err)
	}

	if !fromFull.Equal(fromSeed) {
		t.Fatal("seed-derived key differs from full key")
	}
}

func TestLoadKeyRejectsBadLength(t *testing.T) {
	if _, err := loadKey(hex.EncodeToString([]byte("too short"))); err == nil {
		t.Fatal("expected error for invalid key length")
	}
}

func TestRunRequiresArgs(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	if err := run(nil, hex.EncodeToString(priv)); err == nil {
		t.Fatal("expected usage error with no files")
	}
}
