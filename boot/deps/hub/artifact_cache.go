// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/boot/deps/graph"
)

// The immutable artifact layout. A cache entry is addressed by the module it
// carries, the version it was published at and the digest of its content, so a
// republished build at the same version occupies its own path.
const (
	immutableWappDigestMarker = ".sha256-"
	immutableWappSuffix       = ".wapp"
)

// ImmutableWappRelativePath returns the cache-relative path naming the exact
// content that digest identifies, under the organization that owns the module.
func ImmutableWappRelativePath(name graph.Name, version, digest string) (string, error) {
	algorithm, value, err := parseExpectedDigest(digest)
	if err != nil || algorithm != "sha256" || len(value) != sha256.Size*2 {
		return "", NewArtifactContentError("immutable artifact path requires a sha256 digest", map[string]any{"digest": digest})
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", NewArtifactContentError("immutable artifact path requires a sha256 digest", map[string]any{"digest": digest})
	}
	return filepath.Join(
		name.Organization,
		name.Module+"-"+version+immutableWappDigestMarker+strings.ToLower(value)+immutableWappSuffix,
	), nil
}

// ImmutableWappDigest reports the digest an immutable artifact filename carries,
// in the prefixed form the cache verifies against. It reads back what
// ImmutableWappRelativePath writes, and reports false for any other filename.
func ImmutableWappDigest(filename string) (string, bool) {
	if !strings.HasSuffix(filename, immutableWappSuffix) {
		return "", false
	}
	base := strings.TrimSuffix(filename, immutableWappSuffix)
	marker := strings.LastIndex(base, immutableWappDigestMarker)
	if marker <= 0 {
		return "", false
	}
	value := strings.ToLower(base[marker+len(immutableWappDigestMarker):])
	if len(value) != sha256.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return "sha256:" + value, true
}

// PublishImmutableArtifact publishes content at relative inside the artifact
// cache rooted at cacheDir. Relative names the identity content must satisfy,
// so an entry that already carries it is kept and content is never read.
// Otherwise content is staged in a private file, verified against digest and
// size, and only then linked into place.
func PublishImmutableArtifact(cacheDir, relative string, content io.Reader, digest string, size uint64) error {
	destination, err := containedPath(cacheDir, relative)
	if err != nil {
		return err
	}
	if err := verifyExistingImmutableArtifact(destination, digest, size); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return NewArtifactIOError("create artifact cache directory", directory, err)
	}
	staged, err := stageArtifact(content, directory, ".artifact-stage-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	return publishVerifiedArtifact(staged, destination, digest, size)
}

// publishVerifiedArtifact publishes an already-downloaded private candidate at
// an immutable digest-addressed path. Concurrent publishers may race, but every
// accepted winner is verified against the same identity before it is returned.
// Existing invalid immutable paths are never removed or overwritten.
func publishVerifiedArtifact(candidate, destination, digest string, size uint64) error {
	if err := verifyDownloadedArtifact(candidate, digest, size); err != nil {
		return NewArtifactIOError("verify private artifact", candidate, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return NewArtifactIOError("create artifact cache directory", filepath.Dir(destination), err)
	}

	if err := verifyExistingImmutableArtifact(destination, digest, size); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Link(candidate, destination); err == nil {
		return syncDirectory(filepath.Dir(destination))
	}
	// A concurrent publisher may have won between the existence check and Link.
	if err := verifyExistingImmutableArtifact(destination, digest, size); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// The candidate can be on another filesystem, where the first hard-link
	// attempt cannot succeed. Copy it to a private file in the destination
	// directory, sync and verify it, then hard-link that file into place. Link is
	// the portable create-if-absent primitive: unlike Rename on Unix, it never
	// replaces an existing cache entry.
	stagedPath, err := copyArtifactToPrivateFile(candidate, filepath.Dir(destination), ".artifact-publish-*")
	if err != nil {
		return err
	}
	defer os.Remove(stagedPath)
	if err := verifyDownloadedArtifact(stagedPath, digest, size); err != nil {
		return NewArtifactIOError("verify private publish file", stagedPath, err)
	}

	if err := verifyExistingImmutableArtifact(destination, digest, size); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Link(stagedPath, destination); err != nil {
		winnerErr := verifyExistingImmutableArtifact(destination, digest, size)
		if winnerErr == nil {
			return nil
		}
		if !errors.Is(winnerErr, os.ErrNotExist) {
			return winnerErr
		}
		return NewArtifactIOError("publish immutable artifact without replacement", destination, err)
	}
	return syncDirectory(filepath.Dir(destination))
}

// stageArtifact creates a fully written, synced private candidate from content.
// No partially written bytes are ever exposed at the eventual cache path.
func stageArtifact(content io.Reader, destinationDir, pattern string) (path string, err error) {
	destination, err := os.CreateTemp(destinationDir, pattern)
	if err != nil {
		return "", NewArtifactIOError("create private artifact file", destinationDir, err)
	}
	path = destination.Name()
	committed := false
	defer func() {
		if !committed {
			_ = destination.Close()
			_ = os.Remove(path)
		}
	}()

	_, copyErr := io.Copy(destination, content)
	syncErr := destination.Sync()
	destinationCloseErr := destination.Close()
	if err := errors.Join(copyErr, syncErr, destinationCloseErr); err != nil {
		return "", NewArtifactIOError("copy private artifact", path, err)
	}
	committed = true
	return path, nil
}

// copyArtifactToPrivateFile stages a private candidate from a file the cache
// does not own, so publication never links an entry to a mutable inode.
func copyArtifactToPrivateFile(sourcePath, destinationDir, pattern string) (string, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", NewArtifactIOError("open artifact source", sourcePath, err)
	}
	path, stageErr := stageArtifact(source, destinationDir, pattern)
	if closeErr := source.Close(); closeErr != nil && stageErr == nil {
		_ = os.Remove(path)
		return "", NewArtifactIOError("copy private artifact", sourcePath, closeErr)
	}
	return path, stageErr
}

func verifyExistingImmutableArtifact(path, digest string, size uint64) error {
	err := verifyDownloadedArtifact(path, digest, size)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return err
	}
	return NewArtifactIOError("verify immutable artifact cache entry", path, err)
}
