// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/boot/deps/graph"
)

func immutableWappRelativePath(name graph.Name, version, digest string) (string, error) {
	algorithm, value, err := parseExpectedDigest(digest)
	if err != nil || algorithm != "sha256" || len(value) != 64 {
		return "", fmt.Errorf("immutable artifact path requires a sha256 digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("immutable artifact path requires a sha256 digest")
	}
	return filepath.Join(
		name.Organization,
		name.Module+"-"+version+".sha256-"+strings.ToLower(value)+".wapp",
	), nil
}

// publishVerifiedArtifact publishes an already-downloaded private candidate at
// an immutable digest-addressed path. Concurrent publishers may race, but every
// accepted winner is verified against the same identity before it is returned.
// Existing invalid immutable paths are never removed or overwritten.
func publishVerifiedArtifact(candidate, destination, digest string, size uint64) error {
	if err := verifyDownloadedArtifact(candidate, digest, size); err != nil {
		return fmt.Errorf("verify private artifact: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create artifact cache directory: %w", err)
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
		return fmt.Errorf("verify private publish file: %w", err)
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
		return fmt.Errorf("publish immutable artifact without replacement: %w", err)
	}
	return syncDirectory(filepath.Dir(destination))
}

// copyArtifactToPrivateFile creates a fully written, synced private candidate.
// No partially copied bytes are ever exposed at the eventual cache path.
func copyArtifactToPrivateFile(sourcePath, destinationDir, pattern string) (path string, err error) {
	destination, err := os.CreateTemp(destinationDir, pattern)
	if err != nil {
		return "", fmt.Errorf("create private artifact file: %w", err)
	}
	path = destination.Name()
	committed := false
	defer func() {
		if !committed {
			_ = destination.Close()
			_ = os.Remove(path)
		}
	}()

	source, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("open artifact source: %w", err)
	}
	_, copyErr := io.Copy(destination, source)
	sourceCloseErr := source.Close()
	syncErr := destination.Sync()
	destinationCloseErr := destination.Close()
	if err := errors.Join(copyErr, sourceCloseErr, syncErr, destinationCloseErr); err != nil {
		return "", fmt.Errorf("copy private artifact: %w", err)
	}
	committed = true
	return path, nil
}

func verifyExistingImmutableArtifact(path, digest string, size uint64) error {
	err := verifyDownloadedArtifact(path, digest, size)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fmt.Errorf("immutable artifact cache entry %q has invalid content: %w", path, err)
}
