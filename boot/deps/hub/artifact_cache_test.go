// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func TestImmutableWappRelativePathSeparatesDigestsForSameVersion(t *testing.T) {
	t.Parallel()

	name := graph.MustParseName("acme/widget")
	one := "sha256:" + strings.Repeat("1", 64)
	two := "sha256:" + strings.Repeat("2", 64)

	onePath, err := immutableWappRelativePath(name, "1.2.3", one)
	if err != nil {
		t.Fatalf("first path: %v", err)
	}
	twoPath, err := immutableWappRelativePath(name, "1.2.3", two)
	if err != nil {
		t.Fatalf("second path: %v", err)
	}
	if onePath == twoPath {
		t.Fatalf("different digests mapped to the same path %q", onePath)
	}
	if !strings.Contains(onePath, "widget-1.2.3.sha256-"+strings.Repeat("1", 64)+".wapp") {
		t.Fatalf("unexpected digest-addressed path %q", onePath)
	}
}

func TestImmutableWappRelativePathRejectsNonHexDigest(t *testing.T) {
	t.Parallel()

	_, err := immutableWappRelativePath(
		graph.MustParseName("acme/widget"),
		"1.2.3",
		"sha256:"+strings.Repeat("z", 64),
	)
	if err == nil {
		t.Fatal("expected a non-hex digest to be rejected")
	}
}

func TestPublishVerifiedArtifactConcurrentPublishers(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	destination := filepath.Join(cacheDir, "acme", "widget-1.2.3.sha256-content.wapp")
	content := []byte("same immutable module bytes")
	sum := sha256.Sum256(content)
	digest := fmt.Sprintf("sha256:%x", sum)

	const publishers = 16
	start := make(chan struct{})
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
			<-start
			errs <- publishVerifiedArtifact(candidate, destination, digest, uint64(len(content)))
		}()
	}
	close(start)
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

func TestPublishVerifiedArtifactPreservesInvalidExistingDestination(t *testing.T) {
	t.Parallel()

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

func TestPublishVerifiedArtifactSameVersionDifferentDigestsCoexist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	name := graph.MustParseName("acme/widget")
	for i, content := range [][]byte{[]byte("first build"), []byte("second build")} {
		sum := sha256.Sum256(content)
		digest := fmt.Sprintf("sha256:%x", sum)
		relative, err := immutableWappRelativePath(name, "1.2.3", digest)
		if err != nil {
			t.Fatalf("artifact %d path: %v", i, err)
		}
		candidate := filepath.Join(t.TempDir(), "candidate.wapp")
		if err := os.WriteFile(candidate, content, 0o600); err != nil {
			t.Fatalf("artifact %d candidate: %v", i, err)
		}
		if err := publishVerifiedArtifact(candidate, filepath.Join(dir, relative), digest, uint64(len(content))); err != nil {
			t.Fatalf("artifact %d publish: %v", i, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(dir, "acme", "widget-1.2.3.sha256-*.wapp"))
	if err != nil {
		t.Fatalf("glob immutable artifacts: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d immutable artifacts, want 2: %v", len(matches), matches)
	}
}

func TestEnsureModuleAvailableMigratesLegacyArtifactThroughPrivateCopy(t *testing.T) {
	t.Parallel()

	vendorDir := t.TempDir()
	name := graph.MustParseName("acme/widget")
	legacyPath := filepath.Join(vendorDir, lock.WappPath(name, "1.2.3"))
	content := []byte("legacy verified module")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("create legacy directory: %v", err)
	}
	if err := os.WriteFile(legacyPath, content, 0o600); err != nil {
		t.Fatalf("write legacy artifact: %v", err)
	}
	sum := sha256.Sum256(content)
	digest := fmt.Sprintf("sha256:%x", sum)
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
			t.Fatal("verified legacy artifact must not be downloaded again")
			return nil, nil
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	gotPath, err := handler.ensureModuleAvailable(context.Background(), ResolvedModule{
		Org: "acme", Name: "widget", Version: "1.2.3", Digest: digest, SizeBytes: uint64(len(content)),
	})
	if err != nil {
		t.Fatalf("ensure module: %v", err)
	}
	wantRelative, err := immutableWappRelativePath(name, "1.2.3", digest)
	if err != nil {
		t.Fatalf("immutable path: %v", err)
	}
	if want := filepath.Join(vendorDir, wantRelative); gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if err := verifyDownloadedArtifact(gotPath, digest, uint64(len(content))); err != nil {
		t.Fatalf("verify immutable artifact: %v", err)
	}
	legacyInfo, err := os.Stat(legacyPath)
	if err != nil {
		t.Fatalf("legacy artifact was removed: %v", err)
	}
	immutableInfo, err := os.Stat(gotPath)
	if err != nil {
		t.Fatalf("stat immutable artifact: %v", err)
	}
	if os.SameFile(legacyInfo, immutableInfo) {
		t.Fatal("immutable artifact aliases mutable legacy inode")
	}
}

func TestEnsureModuleAvailableIgnoresNonRegularLegacyPath(t *testing.T) {
	t.Parallel()

	vendorDir := t.TempDir()
	name := graph.MustParseName("acme/widget")
	legacyPath := filepath.Join(vendorDir, lock.WappPath(name, "1.2.3"))
	if err := os.MkdirAll(legacyPath, 0o755); err != nil {
		t.Fatalf("create corrupt legacy directory: %v", err)
	}
	content := []byte("downloaded module")
	sum := sha256.Sum256(content)
	digest := fmt.Sprintf("sha256:%x", sum)
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{downloadFile: func(_ context.Context, _ string, destination string) error {
			return os.WriteFile(destination, content, 0o600)
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	gotPath, err := handler.ensureModuleAvailable(context.Background(), ResolvedModule{
		Org: "acme", Name: "widget", Version: "1.2.3", URL: "memory://widget",
		Digest: digest, SizeBytes: uint64(len(content)),
	})
	if err != nil {
		t.Fatalf("ensure module: %v", err)
	}
	if err := verifyDownloadedArtifact(gotPath, digest, uint64(len(content))); err != nil {
		t.Fatalf("verify immutable artifact: %v", err)
	}
	if info, err := os.Stat(legacyPath); err != nil || !info.IsDir() {
		t.Fatalf("nonregular legacy path was mutated: info=%v err=%v", info, err)
	}
}

func TestEnsureModuleAvailableConcurrentPublishers(t *testing.T) {
	t.Parallel()

	vendorDir := t.TempDir()
	content := []byte("concurrently downloaded module")
	sum := sha256.Sum256(content)
	digest := fmt.Sprintf("sha256:%x", sum)
	var downloads atomic.Int32
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{downloadFile: func(_ context.Context, _ string, destination string) error {
			downloads.Add(1)
			return os.WriteFile(destination, content, 0o600)
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	mod := ResolvedModule{
		Org: "acme", Name: "widget", Version: "1.2.3", URL: "memory://widget",
		Digest: digest, SizeBytes: uint64(len(content)),
	}

	const callers = 16
	start := make(chan struct{})
	paths := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			path, err := handler.ensureModuleAvailable(context.Background(), mod)
			paths <- path
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("ensure module: %v", err)
		}
	}
	var expectedPath string
	for path := range paths {
		if expectedPath == "" {
			expectedPath = path
		} else if path != expectedPath {
			t.Errorf("publisher returned %q, want %q", path, expectedPath)
		}
	}
	if err := verifyDownloadedArtifact(expectedPath, digest, uint64(len(content))); err != nil {
		t.Fatalf("verify immutable artifact: %v", err)
	}
	if downloads.Load() == 0 {
		t.Fatal("expected at least one download")
	}
}

func TestEnsureModuleAvailableRollbackSelectsExactDigestAtSameVersion(t *testing.T) {
	t.Parallel()

	vendorDir := t.TempDir()
	artifacts := map[string][]byte{
		"memory://first":  []byte("first published build"),
		"memory://second": []byte("republished build at same version"),
	}
	var downloads atomic.Int32
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{downloadFile: func(_ context.Context, url, destination string) error {
			downloads.Add(1)
			return os.WriteFile(destination, artifacts[url], 0o600)
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	module := func(url string) ResolvedModule {
		content := artifacts[url]
		sum := sha256.Sum256(content)
		return ResolvedModule{
			Org: "acme", Name: "widget", Version: "1.2.3", URL: url,
			Digest: fmt.Sprintf("sha256:%x", sum), SizeBytes: uint64(len(content)),
		}
	}
	first, second := module("memory://first"), module("memory://second")
	firstPath, err := handler.ensureModuleAvailable(context.Background(), first)
	if err != nil {
		t.Fatalf("ensure first build: %v", err)
	}
	secondPath, err := handler.ensureModuleAvailable(context.Background(), second)
	if err != nil {
		t.Fatalf("ensure second build: %v", err)
	}
	if firstPath == secondPath {
		t.Fatalf("same-version builds share path %q", firstPath)
	}

	rollbackPath, err := handler.ensureModuleAvailable(context.Background(), first)
	if err != nil {
		t.Fatalf("select first build for rollback: %v", err)
	}
	if rollbackPath != firstPath {
		t.Fatalf("rollback selected %q, want %q", rollbackPath, firstPath)
	}
	if got := downloads.Load(); got != 2 {
		t.Fatalf("downloads = %d, want 2; rollback should reuse its exact digest", got)
	}
}
