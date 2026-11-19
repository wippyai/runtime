// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

// withoutHardLinks makes publication run as on a file system without hard links,
// such as Azure Files over SMB. The tests that use it are not parallel because
// they replace a package variable.
func withoutHardLinks(t *testing.T) {
	t.Helper()
	previous := linkArtifact
	linkArtifact = func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: syscall.EOPNOTSUPP}
	}
	t.Cleanup(func() { linkArtifact = previous })
}

func TestPublishVerifiedArtifactWithoutHardLinks(t *testing.T) {
	withoutHardLinks(t)

	destination := filepath.Join(t.TempDir(), "acme", "widget-1.2.3.sha256-content.wapp")
	content := []byte("module bytes on a file system without hard links")
	sum := sha256.Sum256(content)
	digest := fmt.Sprintf("sha256:%x", sum)

	const publishers = 8
	errs := make(chan error, publishers)
	var wg sync.WaitGroup
	for i := 0; i < publishers; i++ {
		candidate := filepath.Join(t.TempDir(), fmt.Sprintf("candidate-%d.wapp", i))
		if err := os.WriteFile(candidate, content, 0o600); err != nil {
			t.Fatalf("write candidate: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- publishVerifiedArtifact(candidate, destination, digest, uint64(len(content)))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("publish: %v", err)
		}
	}
	if err := verifyDownloadedArtifact(destination, digest, uint64(len(content))); err != nil {
		t.Fatalf("verify published artifact: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".artifact-publish-*"))
	if err != nil {
		t.Fatalf("glob private publish files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("private publish files leaked: %v", matches)
	}
}

func TestPublishVerifiedArtifactWithoutHardLinksPreservesInvalidDestination(t *testing.T) {
	withoutHardLinks(t)

	dir := t.TempDir()
	candidate := filepath.Join(dir, "candidate.wapp")
	destination := filepath.Join(dir, "cache", "immutable.wapp")
	content := []byte("verified module")
	sum := sha256.Sum256(content)
	digest := fmt.Sprintf("sha256:%x", sum)
	if err := os.WriteFile(candidate, content, 0o600); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create destination directory: %v", err)
	}
	const existing = "do not overwrite me"
	if err := os.WriteFile(destination, []byte(existing), 0o600); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}

	if err := publishVerifiedArtifact(candidate, destination, digest, uint64(len(content))); err == nil {
		t.Fatal("expected invalid immutable destination to fail publication")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read existing destination: %v", err)
	}
	if string(got) != existing {
		t.Fatalf("existing destination changed: got %q", got)
	}
}

func TestLinkOrRenameAbsentKeepsOtherLinkErrors(t *testing.T) {
	dir := t.TempDir()
	if err := linkOrRenameAbsent(filepath.Join(dir, "missing"), filepath.Join(dir, "destination")); err == nil {
		t.Fatal("expected a missing source to fail")
	}
	if _, err := os.Lstat(filepath.Join(dir, "destination")); !os.IsNotExist(err) {
		t.Fatalf("destination must not exist: %v", err)
	}
}
