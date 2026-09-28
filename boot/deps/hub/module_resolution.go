// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	regapi "github.com/wippyai/runtime/api/registry"
	hubsemver "github.com/wippyai/runtime/api/semver"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func (h *DependencyHandler) resolveModules(
	ctx context.Context,
	deps []DependencyDefinition,
	lockedVersions map[string]string,
	resolution *regapi.DependencyResolution,
) ([]ResolvedModule, error) {
	roots := make([]DependencySpec, 0, len(deps))
	for _, dep := range deps {
		name, err := graph.ParseName(dep.Component)
		if err != nil {
			return nil, NewDependencyEntryInvalidError("", "invalid component", dep.Component)
		}
		roots = append(roots, DependencySpec{
			Org:        name.Organization,
			Name:       name.Module,
			Constraint: dep.Version,
		})
	}

	resolveCtx, cancel := withOptionalTimeout(ctx, h.resolveTimeout)
	defer cancel()

	provider := ManifestProvider(h.hubFor(ctx))
	if h.manifestCache != nil {
		provider = h.manifestCache
	}
	baselineDigests := h.baselineModuleDigests()
	if offlineStartup(ctx) {
		provider = newLockedManifestProvider(h, h.offlineModules(resolution))
	}
	provider = &replacementManifestProvider{
		base:           provider,
		handler:        h,
		lockedVersions: lockedVersions,
		lockedDigests:  baselineDigests,
	}
	result, err := Resolve(resolveCtx, provider, roots, &ResolveOptions{
		LockedVersions: lockedVersions,
		LockedDigests:  baselineDigests,
	})
	if err != nil {
		if h.logger != nil {
			h.logger.Error("dependency resolution failed", zap.Error(err))
		}
		return nil, NewDependencyResolutionError(err)
	}
	if len(result.Errors) > 0 {
		if h.logger != nil {
			h.logger.Error("dependency resolution failed", zap.String("errors", formatResolutionErrors(result.Errors)))
		}
		if offlineStartup(ctx) {
			// A replaced module resolves from its local tree, so its failure
			// is never a missing-evidence failure; the full error set carries
			// the actual cause.
			if module, ok := h.offlineEvidenceFailure(result.Errors); ok {
				return nil, NewDependencyOfflineError("resolve", module)
			}
		}
		return nil, NewDependencyResolutionErrors(result.Errors)
	}
	for _, mod := range result.Modules {
		name := graph.Name{Organization: mod.Org, Module: mod.Name}
		if err := validateModuleArtifactIdentity(name, mod.Version, mod.Digest); err != nil {
			return nil, NewDependencyIntegrityError(modKey(mod), err, mod.Digest, mod.SizeBytes)
		}
		if mod.VersionID == "" && mod.Digest == "" {
			if _, replaced := h.replacementPath(name.String()); !replaced && h.logger != nil {
				h.logger.Warn("resolved module has legacy identity without version id or digest",
					zap.String("module", name.String()), zap.String("version", mod.Version))
			}
		}
	}

	return result.Modules, nil
}

// ResolveWorkspaceDependencies resolves source declarations through the same
// manifest stack used by live dependency operations. In particular, local
// replacements supply their declarations while the Hub supplies a release
// version when the source tree does not declare one.
func (h *DependencyHandler) ResolveWorkspaceDependencies(
	ctx context.Context,
	deps []DependencyDefinition,
) ([]ResolvedModule, error) {
	lockedVersions := h.workspaceLockedVersions(nil, false)
	return h.resolveModules(ctx, deps, lockedVersions, nil)
}

// UpdateWorkspaceDependencies resolves a workspace graph while releasing the
// requested remote modules from their existing lock selections. An empty
// updateModules slice releases every remote module. Replacement selections are
// retained because a replacement changes source, not release identity; a new
// unversioned replacement receives the local-only zero release until stronger
// source or Hub evidence selects another version.
func (h *DependencyHandler) UpdateWorkspaceDependencies(
	ctx context.Context,
	deps []DependencyDefinition,
	updateModules []string,
) ([]ResolvedModule, error) {
	updates := make(map[string]struct{}, len(updateModules))
	for _, name := range updateModules {
		if name = strings.TrimSpace(name); name != "" {
			updates[name] = struct{}{}
		}
	}
	return h.resolveModules(ctx, deps, h.workspaceLockedVersions(updates, len(updateModules) == 0), nil)
}
func (h *DependencyHandler) workspaceLockedVersions(updates map[string]struct{}, updateAll bool) map[string]string {
	lockedVersions := make(map[string]string)
	if h.lock != nil {
		for _, module := range h.lock.GetModules() {
			if module.Name == "" || module.Version == "" {
				continue
			}
			_, replacement := h.replacementPath(module.Name)
			_, update := updates[module.Name]
			if replacement || (!updateAll && !update) {
				lockedVersions[module.Name] = module.Version
			}
		}
	}
	for name := range h.replacements {
		if lockedVersions[name] == "" && h.replacementModuleVersion(name) == "" {
			lockedVersions[name] = replacementZeroVersion
		}
	}
	return lockedVersions
}

// resolveEffectiveModules returns the complete module selection controlled by
// the current deployment plus authored registry roots. Lock-selected root
// modules are implicit deployment inputs: a history overlay may replace one,
// but removing that overlay must reveal the locked root again rather than
// uninstalling the deployment itself.
func (h *DependencyHandler) resolveEffectiveModules(
	ctx context.Context,
	deps []DependencyDefinition,
	lockedVersions map[string]string,
	resolution *regapi.DependencyResolution,
) ([]ResolvedModule, error) {
	if offlineStartup(ctx) {
		if resolved, ok := h.lockedResolution(deps, lockedVersions); ok {
			if h.logger != nil {
				h.logger.Debug("using locked dependency resolution",
					zap.Int("modules", len(resolved)),
					zap.Int("roots", len(deps)))
			}
			return resolved, nil
		}
	}

	resolved, err := h.resolveModules(ctx, deps, lockedVersions, resolution)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(resolved))
	for _, mod := range resolved {
		selected[mod.Org+"/"+mod.Name] = struct{}{}
	}
	if h.lock != nil {
		for _, locked := range h.lock.GetModules() {
			if !locked.Root || locked.Name == "" || locked.Version == "" {
				continue
			}
			if _, exists := selected[locked.Name]; exists {
				continue
			}
			name, parseErr := graph.ParseName(locked.Name)
			if parseErr != nil {
				return nil, NewDependencyResolutionError(parseErr)
			}
			resolved = append(resolved, ResolvedModule{
				Org: name.Organization, Name: name.Module,
				Version: locked.Version, VersionID: locked.Version,
				Digest: locked.Hash,
			})
			selected[locked.Name] = struct{}{}
		}
	}
	if err := h.completeResolvedModuleIdentities(ctx, resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}
func validateModuleArtifactIdentity(name graph.Name, version, digest string) error {
	if !validModuleIdentifier(name.Organization) || !validModuleIdentifier(name.Module) {
		return NewModuleIdentityError(fmt.Sprintf("invalid module name %q: organization and module must be lowercase alphanumeric with hyphens", name.String()), name.String(), nil)
	}
	if _, err := hubsemver.ParseVersion(strings.TrimSpace(version)); err != nil {
		return NewModuleIdentityError("module version is not an exact release", name.String(), map[string]any{"version": version})
	}
	if digest == "" {
		return nil // Older hubs and local replacements did not always provide one.
	}
	algorithm, value, err := parseExpectedDigest(digest)
	if err != nil || algorithm != "sha256" || len(value) != sha256.Size*2 {
		return NewModuleIdentityError(fmt.Sprintf("invalid sha256 digest for %s@%s", name.String(), version), name.String(), map[string]any{"version": version})
	}
	if _, err := hex.DecodeString(value); err != nil {
		return NewModuleIdentityError(fmt.Sprintf("invalid sha256 digest for %s@%s", name.String(), version), name.String(), map[string]any{"version": version})
	}
	return nil
}
func validateStoredModuleArtifactIdentity(name graph.Name, version, source, digest string) error {
	if err := validateModuleArtifactIdentity(name, version, ""); err != nil {
		return err
	}
	if digest == "" {
		return NewModuleIdentityError("stored module has no content digest", name.String(), map[string]any{"version": version})
	}
	algorithm, value, err := parseExpectedDigest(digest)
	wantAlgorithm := "sha256"
	if source == moduleSourceReplacementTreeV1 {
		wantAlgorithm = "sha256-tree-v1"
	} else if source != "" && source != moduleSourceHub {
		return NewModuleIdentityError("stored module has an unsupported source", name.String(), map[string]any{"version": version, "source": source})
	}
	if err != nil || algorithm != wantAlgorithm || len(value) != sha256.Size*2 {
		return NewModuleIdentityError(fmt.Sprintf("invalid %s digest for %s@%s", wantAlgorithm, name.String(), version), name.String(), map[string]any{"version": version, "algorithm": wantAlgorithm})
	}
	if _, err := hex.DecodeString(value); err != nil {
		return NewModuleIdentityError(fmt.Sprintf("invalid %s digest for %s@%s", wantAlgorithm, name.String(), version), name.String(), map[string]any{"version": version, "algorithm": wantAlgorithm})
	}
	return nil
}

// refreshReplacementModuleIdentities snapshots every configured local source
// for one reconciliation attempt. ensureModuleAvailable verifies the same
// identity again before loading, so a concurrent rebuild fails closed instead
// of mixing files from two source generations.
func (h *DependencyHandler) refreshReplacementModuleIdentities(modules []ResolvedModule) error {
	for i := range modules {
		mod := &modules[i]
		replacement, ok := h.replacementPath(mod.Org + "/" + mod.Name)
		if !ok {
			continue
		}
		digest, size, err := digestReplacementTree(replacement)
		if err != nil {
			return apierror.New(apierror.Invalid, fmt.Sprintf("load local replacement %s from %q", modKey(*mod), replacement)).
				WithDetails(attrs.NewBagFrom(map[string]any{"module": modKey(*mod), "path": replacement})).
				WithCause(err).
				WithRetryable(apierror.False)
		}
		mod.Digest = digest
		mod.Source = moduleSourceReplacementTreeV1
		mod.SizeBytes = size
		mod.URL = ""
	}
	return nil
}

// completeResolvedModuleIdentities upgrades legacy Hub responses and local
// replacements to a content-pinned graph before that graph is persisted.
func (h *DependencyHandler) completeResolvedModuleIdentities(ctx context.Context, modules []ResolvedModule) error {
	if err := h.refreshReplacementModuleIdentities(modules); err != nil {
		return err
	}
	for i := range modules {
		mod := &modules[i]
		name := graph.Name{Organization: mod.Org, Module: mod.Name}
		if _, replacement := h.replacementPath(name.String()); !replacement && mod.Digest == "" {
			// Older manifest APIs omitted artifact identity. Prefer metadata from
			// the exact download endpoint, then fall back to hashing the cached or
			// freshly downloaded artifact itself.
			if info, err := h.freshDownloadInfo(ctx, *mod); err == nil && info != nil {
				if err := validateDownloadInfo(*mod, info); err != nil {
					return NewDependencyIntegrityError(modKey(*mod), err, mod.Digest, mod.SizeBytes)
				}
				mod.Digest = info.Digest
				if mod.SizeBytes == 0 {
					mod.SizeBytes = info.Size
				}
			}
			if mod.Digest == "" {
				path, ok, err := h.cachedModuleArtifact(*mod)
				if err != nil {
					return err
				}
				if !ok {
					path, err = h.ensureModuleAvailable(ctx, *mod)
					if err != nil {
						return err
					}
				}
				digest, size, err := artifactIdentityFromPath(path)
				if err != nil {
					return NewDependencyIntegrityError(modKey(*mod), err, mod.Digest, mod.SizeBytes)
				}
				mod.Digest = digest
				if mod.SizeBytes == 0 {
					mod.SizeBytes = size
				}
			}
		}
		if mod.Source == "" {
			mod.Source = moduleSourceHub
		}
		if err := validateStoredModuleArtifactIdentity(name, mod.Version, mod.Source, mod.Digest); err != nil {
			return NewDependencyIntegrityError(modKey(*mod), err, mod.Digest, mod.SizeBytes)
		}
		algorithm, value, _ := parseExpectedDigest(mod.Digest)
		mod.Digest = algorithm + ":" + strings.ToLower(value)
	}
	return nil
}

// cachedModuleArtifact returns the packed cache entry without extracting it.
// Identity completion must not rewrite an untouched sibling merely because the
// runtime is configured to use unpacked modules.
func (h *DependencyHandler) cachedModuleArtifact(mod ResolvedModule) (string, bool, error) {
	name, err := graph.ParseName(mod.Org + "/" + mod.Name)
	if err != nil {
		return "", false, err
	}
	if mod.Digest != "" {
		path, err := h.immutableArtifactPath(name, mod.Version, mod.Digest)
		if err != nil {
			return "", false, err
		}
		if err := verifyExistingImmutableArtifact(path, mod.Digest, mod.SizeBytes); err == nil {
			return path, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false, err
		}
	} else if path, _, _, ok, err := h.soleImmutableArtifact(name, mod.Version); err != nil {
		return "", false, err
	} else if ok {
		return path, true, nil
	}
	// The version-only path is legacy migration input. Callers must establish
	// its identity before publishing it into the immutable cache.
	path, err := containedPath(h.vendorDir, lock.WappPath(name, mod.Version))
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	return path, true, nil
}
func artifactIdentityFromPath(path string) (string, uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if info.IsDir() {
		data, err := os.ReadFile(filepath.Join(path, extractedModuleMeta))
		if err != nil {
			return "", 0, err
		}
		var meta extractedModuleMetadata
		if err := yaml.Unmarshal(data, &meta); err != nil {
			return "", 0, err
		}
		if meta.Digest == "" {
			return "", 0, NewArtifactContentError("extracted module has no content digest", nil)
		}
		return meta.Digest, meta.Size, nil
	}
	digest, err := sha256FileHex(path)
	if err != nil {
		return "", 0, err
	}
	return "sha256:" + digest, uint64(info.Size()), nil
}
func validModuleIdentifier(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
