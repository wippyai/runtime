// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformArch(t *testing.T) {
	cases := map[string][2]string{
		"wippy-linux-amd64":       {"linux", "amd64"},
		"wippy-linux-arm64":       {"linux", "arm64"},
		"wippy-darwin-arm64":      {"darwin", "arm64"},
		"wippy-windows-amd64.exe": {"windows", "amd64"},
	}
	for name, want := range cases {
		p, a, err := platformArch(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p != want[0] || a != want[1] {
			t.Fatalf("%s: got %s/%s want %s/%s", name, p, a, want[0], want[1])
		}
	}

	if _, _, err := platformArch("notawippybinary"); err == nil {
		t.Fatal("expected error for malformed name")
	}
}

func TestBuildPayload(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	dir := t.TempDir()
	content := []byte("fake wippy binary")
	bin := filepath.Join(dir, "wippy-linux-amd64")
	if err := os.WriteFile(bin, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A .sig sibling must be ignored.
	if err := os.WriteFile(bin+".sig", []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}

	p, err := buildPayload(priv, "wippyai/runtime", "v0.4.0", "notes", false, []string{bin, bin + ".sig"})
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}

	if p.Version != "0.4.0" {
		t.Fatalf("version = %q, want 0.4.0 (v stripped)", p.Version)
	}
	if len(p.Assets) != 1 {
		t.Fatalf("expected 1 asset (sig skipped), got %d", len(p.Assets))
	}

	a := p.Assets[0]
	wantURL := "https://github.com/wippyai/runtime/releases/download/v0.4.0/wippy-linux-amd64"
	if a.URL != wantURL {
		t.Fatalf("url = %q, want %q", a.URL, wantURL)
	}

	digest := sha256.Sum256(content)
	if a.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("sha256 mismatch")
	}

	sig, err := base64.StdEncoding.DecodeString(a.Signature)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if !ed25519.Verify(pub, digest[:], sig) {
		t.Fatal("signature does not verify over the sha256 digest")
	}

	wantFP := sha256.Sum256(pub)
	if a.SigningKeyFingerprint != hex.EncodeToString(wantFP[:]) {
		t.Fatal("fingerprint mismatch")
	}
}
