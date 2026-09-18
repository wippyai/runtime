// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/boot/deps/wappextract"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func (h *DependencyHandler) materializeModuleForLoad(ctx context.Context, mod ResolvedModule) (string, *stagedModuleDirectory, error) {
	moduleName := mod.Org + "/" + mod.Name
	if _, replaced := h.replacementPath(moduleName); replaced || !h.shouldUnpackModules() {
		path, err := h.ensureModuleAvailable(ctx, mod)
		return path, nil, err
	}

	name, err := graph.ParseName(moduleName)
	if err != nil {
		return "", nil, NewDependencyEntryInvalidError("", "invalid component", moduleName)
	}
	targetDir, err := containedPath(h.vendorDir, lock.ModulePath(name))
	if err != nil {
		return "", nil, NewDependencyDownloadError(modKey(mod), err)
	}
	if info, statErr := os.Stat(targetDir); statErr == nil && info.IsDir() {
		if verifyErr := verifyExtractedModule(targetDir, mod.Digest, mod.SizeBytes); verifyErr == nil {
			return targetDir, nil, nil
		}
	}

	wappPath, err := h.ensureModuleAvailable(ctx, mod)
	if err != nil {
		return "", nil, err
	}
	digest, size := mod.Digest, mod.SizeBytes
	if digest == "" || size == 0 {
		actualDigest, actualSize, identityErr := artifactIdentityFromPath(wappPath)
		if identityErr != nil {
			return "", nil, NewDependencyIntegrityError(modKey(mod), identityErr, mod.Digest, mod.SizeBytes)
		}
		if digest == "" {
			digest = actualDigest
		}
		if size == 0 {
			size = actualSize
		}
	}
	if err := verifyDownloadedArtifact(wappPath, digest, size); err != nil {
		return "", nil, NewDependencyIntegrityError(modKey(mod), err, mod.Digest, mod.SizeBytes)
	}
	stagingDir, err := h.stageWappModule(wappPath, targetDir, digest, size)
	if err != nil {
		return "", nil, err
	}
	return stagingDir, &stagedModuleDirectory{
		module:     moduleName,
		stagingDir: stagingDir,
		targetDir:  targetDir,
	}, nil
}
func (h *DependencyHandler) stageWappModule(wappPath, targetDir, digest string, size uint64) (string, error) {
	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		return "", NewDependencyLoadError(targetDir, err)
	}
	stagingDir, err := os.MkdirTemp(filepath.Dir(targetDir), "."+filepath.Base(targetDir)+".stage-*")
	if err != nil {
		return "", NewDependencyLoadError(targetDir, err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	if err := wappextract.ExtractWappToDirKeepSource(wappPath, stagingDir); err != nil {
		return "", NewDependencyLoadError(wappPath, err)
	}
	if err := writeExtractedModuleMeta(stagingDir, digest, size); err != nil {
		return "", NewDependencyLoadError(stagingDir, err)
	}
	cleanup = false
	return stagingDir, nil
}
func (h *DependencyHandler) ensureModuleAvailable(ctx context.Context, mod ResolvedModule) (string, error) {
	if err := os.MkdirAll(h.vendorDir, 0755); err != nil {
		return "", NewDependencyDownloadError(modKey(mod), err)
	}

	name, err := graph.ParseName(mod.Org + "/" + mod.Name)
	if err != nil {
		return "", NewDependencyEntryInvalidError("", "invalid component", mod.Org+"/"+mod.Name)
	}
	moduleName := name.String()

	if replacementPath, ok := h.replacementPath(moduleName); ok && (mod.Source == "" || mod.Source == moduleSourceReplacementTreeV1) {
		stat, err := os.Stat(replacementPath)
		if err != nil {
			return "", NewDependencyLoadError(replacementPath, err)
		}
		if !stat.IsDir() {
			return "", NewDependencyLoadError(replacementPath, errReplacementNotDirectory)
		}
		digest, size, err := digestReplacementTree(replacementPath)
		if err != nil {
			return "", NewDependencyIntegrityError(modKey(mod), err, mod.Digest, mod.SizeBytes)
		}
		if mod.Digest != "" && !strings.EqualFold(mod.Digest, digest) {
			return "", NewDependencyIntegrityError(modKey(mod), errReplacementDigestMismatch, mod.Digest, mod.SizeBytes)
		}
		if mod.SizeBytes > 0 && mod.SizeBytes != size {
			return "", NewDependencyIntegrityError(modKey(mod), errReplacementSizeMismatch, mod.Digest, mod.SizeBytes)
		}
		return replacementPath, nil
	}
	if mod.Source == moduleSourceReplacementTreeV1 {
		return "", NewDependencyLoadError(moduleName, errStoredReplacementUnconfigured)
	}

	expectedDigest, expectedSize := mod.Digest, mod.SizeBytes
	if expectedDigest != "" {
		immutablePath, pathErr := h.immutableArtifactPath(name, mod.Version, expectedDigest)
		if pathErr != nil {
			return "", NewDependencyIntegrityError(modKey(mod), pathErr, expectedDigest, expectedSize)
		}
		if verifyErr := verifyExistingImmutableArtifact(immutablePath, expectedDigest, expectedSize); verifyErr == nil {
			return immutablePath, nil
		} else if !errors.Is(verifyErr, os.ErrNotExist) {
			return "", NewDependencyIntegrityError(modKey(mod), verifyErr, expectedDigest, expectedSize)
		}
	} else if immutablePath, _, _, ok, cacheErr := h.soleImmutableArtifact(name, mod.Version); cacheErr != nil {
		return "", NewDependencyDownloadError(modKey(mod), cacheErr)
	} else if ok {
		return immutablePath, nil
	}

	// A version-only artifact may have been written by an older runtime. Treat
	// it strictly as migration input: establish its identity, publish a verified
	// immutable copy, and never mutate or remove the conventional path.
	legacyPath, err := containedPath(h.vendorDir, lock.WappPath(name, mod.Version))
	if err != nil {
		return "", NewDependencyDownloadError(modKey(mod), err)
	}
	if legacyInfo, statErr := os.Stat(legacyPath); statErr == nil && legacyInfo.Mode().IsRegular() {
		legacyDigest, legacySize := expectedDigest, expectedSize
		if legacyDigest == "" {
			legacyDigest, legacySize, err = artifactIdentityFromPath(legacyPath)
		} else {
			err = verifyDownloadedArtifact(legacyPath, legacyDigest, legacySize)
		}
		if err == nil {
			privatePath, copyErr := copyArtifactToPrivateFile(legacyPath, h.vendorDir, ".artifact-migrate-*")
			if copyErr == nil {
				defer os.Remove(privatePath)
				immutablePath, pathErr := h.immutableArtifactPath(name, mod.Version, legacyDigest)
				if pathErr != nil {
					return "", NewDependencyIntegrityError(modKey(mod), pathErr, legacyDigest, legacySize)
				}
				publishErr := publishVerifiedArtifact(privatePath, immutablePath, legacyDigest, legacySize)
				if publishErr == nil {
					return immutablePath, nil
				}
				err = publishErr
			} else {
				err = copyErr
			}
		}
		if h.logger != nil {
			h.logger.Warn("legacy dependency artifact failed integrity check; ignoring migration input",
				zap.String("module", modKey(mod)),
				zap.String("path", legacyPath),
				zap.Error(err))
		}
	} else if statErr == nil {
		if h.logger != nil {
			h.logger.Warn("legacy dependency artifact is not a regular file; ignoring migration input",
				zap.String("module", modKey(mod)),
				zap.String("path", legacyPath))
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", NewDependencyDownloadError(modKey(mod), statErr)
	}
	if offlineStartup(ctx) {
		return "", NewDependencyOfflineError("load artifact", modKey(mod))
	}

	privateDir, err := os.MkdirTemp(h.vendorDir, ".artifact-download-*")
	if err != nil {
		return "", NewDependencyDownloadError(modKey(mod), err)
	}
	defer os.RemoveAll(privateDir)
	privatePath := filepath.Join(privateDir, "artifact.wapp")
	digest, size, err := h.downloadModuleArtifact(ctx, mod, privatePath)
	if err != nil {
		return "", err
	}
	immutablePath, err := h.immutableArtifactPath(name, mod.Version, digest)
	if err != nil {
		return "", NewDependencyIntegrityError(modKey(mod), err, digest, size)
	}
	if err := publishVerifiedArtifact(privatePath, immutablePath, digest, size); err != nil {
		return "", NewDependencyIntegrityError(modKey(mod), err, digest, size)
	}
	return immutablePath, nil
}
func (h *DependencyHandler) immutableArtifactPath(name graph.Name, version, digest string) (string, error) {
	relative, err := immutableWappRelativePath(name, version, digest)
	if err != nil {
		return "", err
	}
	return containedPath(h.vendorDir, relative)
}

// soleImmutableArtifact supports legacy Hub responses that identify an exact
// version but omit its digest. Reuse is unambiguous only while one verified
// digest exists for that version. If multiple republished builds coexist, the
// caller must ask the Hub which content is current instead of guessing.
func (h *DependencyHandler) soleImmutableArtifact(name graph.Name, version string) (string, string, uint64, bool, error) {
	dir, err := containedPath(h.vendorDir, name.Organization)
	if err != nil {
		return "", "", 0, false, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", 0, false, nil
	}
	if err != nil {
		return "", "", 0, false, err
	}
	prefix := name.Module + "-" + version + ".sha256-"
	const suffix = ".wapp"
	var foundPath, foundDigest string
	var foundSize uint64
	for _, entry := range entries {
		filename := entry.Name()
		if !strings.HasPrefix(filename, prefix) || !strings.HasSuffix(filename, suffix) {
			continue
		}
		hexDigest := strings.TrimSuffix(strings.TrimPrefix(filename, prefix), suffix)
		if len(hexDigest) != sha256.Size*2 {
			continue
		}
		if _, decodeErr := hex.DecodeString(hexDigest); decodeErr != nil {
			continue
		}
		path, pathErr := containedPath(h.vendorDir, filepath.Join(name.Organization, filename))
		if pathErr != nil {
			return "", "", 0, false, pathErr
		}
		digest := "sha256:" + strings.ToLower(hexDigest)
		if verifyErr := verifyExistingImmutableArtifact(path, digest, 0); verifyErr != nil {
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if foundPath != "" {
			return "", "", 0, false, nil
		}
		foundPath, foundDigest, foundSize = path, digest, uint64(info.Size())
	}
	return foundPath, foundDigest, foundSize, foundPath != "", nil
}
func (h *DependencyHandler) downloadModuleArtifact(ctx context.Context, mod ResolvedModule, destination string) (string, uint64, error) {
	expectedDigest := mod.Digest
	expectedSize := mod.SizeBytes
	url := mod.URL
	urlIsFresh := false
	if url == "" {
		info, infoErr := h.freshDownloadInfo(ctx, mod)
		if infoErr != nil {
			return "", 0, NewDependencyDownloadError(modKey(mod), infoErr)
		}
		if infoErr = validateDownloadInfo(mod, info); infoErr != nil {
			return "", 0, NewDependencyIntegrityError(modKey(mod), infoErr, expectedDigest, expectedSize)
		}
		url = info.URL
		urlIsFresh = true
		if expectedDigest == "" {
			expectedDigest = info.Digest
		}
		if expectedSize == 0 {
			expectedSize = info.Size
		}
	}
	if url == "" {
		return "", 0, NewDependencyDownloadError(modKey(mod), ErrDependencyNoContent)
	}

	downloadCtx, cancel := withOptionalTimeout(ctx, h.downloadTimeout)
	defer cancel()

	downloadErr := h.hubFor(ctx).DownloadToFile(downloadCtx, url, destination)
	if downloadErr != nil && !urlIsFresh {
		// mod.URL is a presigned URL captured at resolve time; on a long-lived
		// process it can expire (15-min TTL) before download. Fetch a fresh URL
		// and retry once before giving up.
		if info, infoErr := h.freshDownloadInfo(ctx, mod); infoErr == nil && info != nil && info.URL != "" {
			if infoErr = validateDownloadInfo(mod, info); infoErr != nil {
				return "", 0, NewDependencyIntegrityError(modKey(mod), infoErr, expectedDigest, expectedSize)
			}
			if expectedDigest == "" {
				expectedDigest = info.Digest
			}
			if expectedSize == 0 {
				expectedSize = info.Size
			}
			retryCtx, retryCancel := withOptionalTimeout(ctx, h.downloadTimeout)
			defer retryCancel()
			downloadErr = h.hubFor(ctx).DownloadToFile(retryCtx, info.URL, destination)
		}
	}
	if downloadErr != nil {
		return "", 0, NewDependencyDownloadError(modKey(mod), downloadErr)
	}
	if expectedDigest == "" {
		digest, size, identityErr := artifactIdentityFromPath(destination)
		if identityErr != nil {
			_ = os.Remove(destination)
			return "", 0, NewDependencyIntegrityError(modKey(mod), identityErr, expectedDigest, expectedSize)
		}
		expectedDigest = digest
		if expectedSize == 0 {
			expectedSize = size
		}
	}
	if err := verifyDownloadedArtifact(destination, expectedDigest, expectedSize); err != nil {
		_ = os.Remove(destination)
		return "", 0, NewDependencyIntegrityError(modKey(mod), err, expectedDigest, expectedSize)
	}
	return expectedDigest, expectedSize, nil
}
func containedPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", NewArtifactPathError(fmt.Sprintf("artifact path %q is absolute", relative), relative, nil)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", NewArtifactIOError("resolve vendor directory", root, err)
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, relative))
	if err != nil {
		return "", NewArtifactIOError("resolve artifact path", relative, err)
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", NewArtifactPathError(fmt.Sprintf("artifact path %q escapes vendor directory", relative), relative, nil)
	}
	cursor := rootAbs
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		cursor = filepath.Join(cursor, component)
		info, statErr := os.Lstat(cursor)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return "", NewArtifactIOError("inspect artifact path", relative, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", NewArtifactPathError(fmt.Sprintf("artifact path %q traverses symlink %q", relative, cursor), relative, nil)
		}
	}
	return targetAbs, nil
}
func validateDownloadInfo(mod ResolvedModule, info *DownloadInfo) error {
	if info == nil {
		return ErrDependencyNoContent
	}
	if info.Version != "" {
		got := strings.TrimPrefix(strings.TrimSpace(info.Version), "v")
		want := strings.TrimPrefix(strings.TrimSpace(mod.Version), "v")
		if got != want {
			return NewArtifactContentError(fmt.Sprintf("download version mismatch: expected %s, got %s", mod.Version, info.Version), map[string]any{"expected": mod.Version, "got": info.Version})
		}
	}
	if mod.Digest != "" && info.Digest != "" {
		wantAlgorithm, want, wantErr := parseExpectedDigest(mod.Digest)
		gotAlgorithm, got, gotErr := parseExpectedDigest(info.Digest)
		if wantErr != nil || gotErr != nil || wantAlgorithm != gotAlgorithm || !strings.EqualFold(want, got) {
			return NewArtifactContentError(fmt.Sprintf("download digest mismatch: expected %s, got %s", mod.Digest, info.Digest), map[string]any{"expected": mod.Digest, "got": info.Digest})
		}
	}
	if mod.SizeBytes > 0 && info.Size > 0 && mod.SizeBytes != info.Size {
		return NewArtifactContentError(fmt.Sprintf("download size mismatch: expected %d bytes, got %d bytes", mod.SizeBytes, info.Size), map[string]any{"expected_bytes": mod.SizeBytes, "got_bytes": info.Size})
	}
	return nil
}

// freshDownloadInfo fetches a current presigned download URL for a module.
// Used both when the resolved manifest carries no URL and to refresh a URL
// that expired before the artifact could be downloaded.
func (h *DependencyHandler) freshDownloadInfo(ctx context.Context, mod ResolvedModule) (*DownloadInfo, error) {
	if offlineStartup(ctx) {
		return nil, NewDependencyOfflineError("fetch artifact metadata", modKey(mod))
	}
	downloadURLCtx, cancel := withOptionalTimeout(ctx, h.downloadTimeout)
	defer cancel()

	return h.hubFor(ctx).GetDownloadURL(downloadURLCtx, &DownloadParams{
		Org:     mod.Org,
		Module:  mod.Name,
		Version: mod.Version,
	})
}
func VerifyDownloadedArtifact(path, expectedDigest string, expectedSize uint64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if expectedSize > 0 && uint64(info.Size()) != expectedSize {
		return NewArtifactContentError(fmt.Sprintf("size mismatch: expected %d bytes, got %d bytes", expectedSize, info.Size()), map[string]any{"expected_bytes": expectedSize, "got_bytes": info.Size()})
	}
	if expectedDigest == "" {
		return nil
	}

	alg, wantDigest, err := parseExpectedDigest(expectedDigest)
	if err != nil {
		return err
	}
	if alg != "sha256" {
		return NewArtifactContentError(fmt.Sprintf("unsupported digest algorithm %q", alg), map[string]any{"algorithm": alg})
	}

	gotDigest, err := sha256FileHex(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(gotDigest, wantDigest) {
		return NewArtifactContentError(fmt.Sprintf("digest mismatch: expected %s, got sha256:%s", expectedDigest, gotDigest), map[string]any{"expected": expectedDigest, "got": "sha256:" + gotDigest})
	}
	return nil
}
func verifyDownloadedArtifact(path, expectedDigest string, expectedSize uint64) error {
	return VerifyDownloadedArtifact(path, expectedDigest, expectedSize)
}

type extractedModuleMetadata struct {
	Digest     string `yaml:"digest,omitempty"`
	TreeDigest string `yaml:"tree_digest,omitempty"`
	Size       uint64 `yaml:"size,omitempty"`
}

func writeExtractedModuleMeta(dirPath, digest string, size uint64) error {
	if digest == "" && size == 0 {
		return nil
	}
	treeDigest, _, err := digestDirectoryTree(dirPath)
	if err != nil {
		return NewArtifactIOError("hash extracted module", "", err)
	}
	data, err := yaml.Marshal(extractedModuleMetadata{Digest: digest, Size: size, TreeDigest: treeDigest})
	if err != nil {
		return NewArtifactIOError("marshal extracted module metadata", "", err)
	}
	return os.WriteFile(filepath.Join(dirPath, extractedModuleMeta), data, 0600)
}
func verifyExtractedModule(dirPath, expectedDigest string, expectedSize uint64) error {
	if expectedDigest == "" && expectedSize == 0 {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dirPath, extractedModuleMeta))
	if err != nil {
		return err
	}
	var meta extractedModuleMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return NewArtifactIOError("read extracted module metadata", "", err)
	}
	if expectedDigest != "" && !strings.EqualFold(meta.Digest, expectedDigest) {
		return NewArtifactContentError(fmt.Sprintf("digest mismatch: expected %s, got %s", expectedDigest, meta.Digest), map[string]any{"expected": expectedDigest, "got": meta.Digest})
	}
	if expectedSize > 0 && meta.Size != expectedSize {
		return NewArtifactContentError(fmt.Sprintf("size mismatch: expected %d bytes, got %d bytes", expectedSize, meta.Size), map[string]any{"expected_bytes": expectedSize, "got_bytes": meta.Size})
	}
	if meta.TreeDigest == "" {
		return NewArtifactContentError("extracted module has no tree digest", nil)
	}
	treeDigest, _, err := digestDirectoryTree(dirPath)
	if err != nil {
		return NewArtifactIOError("hash extracted module", "", err)
	}
	if !strings.EqualFold(meta.TreeDigest, treeDigest) {
		return NewArtifactContentError(fmt.Sprintf("extracted module tree digest mismatch: expected %s, got %s", meta.TreeDigest, treeDigest), map[string]any{"expected": meta.TreeDigest, "got": treeDigest})
	}
	return nil
}
func parseExpectedDigest(raw string) (algorithm string, value string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", NewArtifactContentError("digest is empty", nil)
	}
	if !strings.Contains(trimmed, ":") {
		return "sha256", trimmed, nil
	}

	parts := strings.SplitN(trimmed, ":", 2)
	algorithm = strings.ToLower(strings.TrimSpace(parts[0]))
	value = strings.TrimSpace(parts[1])
	if algorithm == "" || value == "" {
		return "", "", NewArtifactContentError("digest format is invalid", map[string]any{"digest": raw})
	}
	return algorithm, value, nil
}
func artifactDigestsEqual(left, right string) bool {
	leftAlgorithm, leftValue, leftErr := parseExpectedDigest(left)
	rightAlgorithm, rightValue, rightErr := parseExpectedDigest(right)
	return leftErr == nil && rightErr == nil &&
		leftAlgorithm == rightAlgorithm && strings.EqualFold(leftValue, rightValue)
}
func sha256FileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (h *DependencyHandler) baselineModuleDigests() map[string]string {
	if h == nil {
		return nil
	}
	modules := make([]regapi.ResolvedModule, 0)
	if h.deployment != nil {
		modules = append(modules, h.deployment.Modules...)
	} else if h.lock != nil {
		for _, module := range h.lock.GetModules() {
			modules = append(modules, regapi.ResolvedModule{
				Name: module.Name, Version: module.Version, Digest: module.Hash,
			})
		}
	}
	digests := make(map[string]string, len(modules))
	for _, mod := range modules {
		if mod.Digest == "" || mod.Name == "" || mod.Version == "" {
			continue
		}
		// Key by name@version so the integrity check only fires when resolving
		// the exact version the baseline pins. A version-agnostic key would compare
		// a new version's digest against the baseline's old version and wrongly
		// block updates.
		digests[mod.Name+"@"+mod.Version] = mod.Digest
	}
	if len(digests) == 0 {
		return nil
	}
	return digests
}
