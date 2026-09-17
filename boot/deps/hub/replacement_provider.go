// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	hubsemver "github.com/wippyai/runtime/api/semver"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// replacementManifestProvider loads entries and declared dependencies from a
// local replacement tree. A replacement with no explicit source version
// delegates release availability to the Hub while startup may reach it; a
// satisfying lock avoids that call, and a verified-offline startup never makes
// it - see localReplacementVersion. Non-replaced modules delegate to the base
// provider.
type replacementManifestProvider struct {
	base           ManifestProvider
	handler        *DependencyHandler
	lockedVersions map[string]string
	lockedDigests  map[string]string
}

// replacementVersion returns the version whose local source tree should be
// loaded. An explicit source version is authoritative. Otherwise the resolver's
// exact selection is authoritative; the lock is only a checkpoint for requests
// that do not already name a selected release.
func (p *replacementManifestProvider) replacementVersion(name, constraint string) string {
	if version := p.handler.replacementModuleVersion(name); version != "" {
		return version
	}
	if isExactModuleVersion(constraint) {
		return constraint
	}
	locked := p.lockedVersions[name]
	if lockedVersionSatisfies(locked, constraint) {
		return locked
	}
	return ""
}

// localReplacementVersion labels a replaced module from local evidence alone,
// for a verified-offline startup that has no Hub to resolve against: the
// version already recorded for the module, or the zero release when none was
// ever recorded. The label faces the graph's constraints like any other
// selection, so a range no local evidence satisfies fails the resolve.
// Declaring version in the replacement's wippy.yaml settles it offline.
func (p *replacementManifestProvider) localReplacementVersion(name string) string {
	if version := p.lockedVersions[name]; version != "" {
		return version
	}
	return replacementZeroVersion
}

// offlineStartup reports that this operation may not reach the Hub at all.
func offlineStartup(ctx context.Context) bool {
	return !regapi.DependencyDownloadsAllowed(ctx)
}
func isExactModuleVersion(value string) bool {
	_, err := hubsemver.ParseVersion(strings.TrimSpace(value))
	return err == nil
}
func (p *replacementManifestProvider) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	name := org + "/" + module
	if path, ok := p.handler.replacementPath(name); ok {
		version := p.replacementVersion(name, constraint)
		if version == "" && offlineStartup(ctx) {
			version = p.localReplacementVersion(name)
		}
		if version == "" {
			// Labels do not identify a concrete release. Ask the Hub only to
			// resolve the label, then keep the local replacement tree as the
			// content and dependency source of truth.
			manifest, err := p.base.GetManifest(ctx, org, module, constraint)
			if err != nil {
				return nil, err
			}
			version = manifest.Version
		}
		dependencies, err := p.localReplacementDependencies(ctx, path)
		if err != nil {
			return nil, err
		}
		return &ModuleManifest{
			Org:          org,
			Name:         module,
			Version:      version,
			Digest:       p.lockedDigests[name+"@"+version],
			Dependencies: dependencies,
		}, nil
	}
	return p.base.GetManifest(ctx, org, module, constraint)
}
func (p *replacementManifestProvider) localReplacementDependencies(ctx context.Context, path string) ([]ManifestDep, error) {
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return nil, ErrDependencyTranscoderMissing
	}

	entries, err := loadReplacementEntries(ctx, path, p.handler.logger, transcoder)
	if err != nil {
		return nil, err
	}
	entries, err = p.handler.applyModuleConfigFilters(ctx, path, entries)
	if err != nil {
		return nil, err
	}

	return manifestDependenciesFromEntries(ctx, transcoder, entries)
}
func loadReplacementEntries(
	ctx context.Context,
	path string,
	logger *zap.Logger,
	transcoder payload.Transcoder,
) ([]regapi.Entry, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, NewDependencyLoadError(path, err)
	}
	if !stat.IsDir() {
		return nil, NewDependencyLoadError(path, errReplacementNotDirectory)
	}

	cfg, _ := depconfig.Load(path)
	dirFS := depconfig.NewSourceFS(os.DirFS(path), cfg, path, path)
	ldr := loaderFromContext(ctx, logger, transcoder)
	var entries []regapi.Entry
	if err := fs.WalkDir(dirFS, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || rel == depconfig.DefaultConfigFile {
			return nil
		}
		switch filepath.Ext(rel) {
		case ".json", ".yaml", ".yml":
		default:
			return nil
		}
		loaded, err := ldr.LoadFile(ctx, dirFS, rel)
		if err != nil {
			return err
		}
		entries = append(entries, loaded...)
		return nil
	}); err != nil {
		return nil, NewDependencyLoadError(path, err)
	}
	return entries, nil
}
func (p *replacementManifestProvider) ListAllVersions(ctx context.Context, org, module string) ([]VersionInfo, error) {
	name := org + "/" + module
	if _, ok := p.handler.replacementPath(name); ok {
		// An explicit source version is the complete candidate set. Without
		// one, the local tree supplies bytes but the Hub remains authoritative
		// for which released versions satisfy live ranges. A verified-offline
		// startup cannot reach it, so local evidence is the whole candidate
		// set.
		if version := p.handler.replacementModuleVersion(name); version != "" {
			return []VersionInfo{{Version: version}}, nil
		}
		if offlineStartup(ctx) {
			return []VersionInfo{{Version: p.localReplacementVersion(name)}}, nil
		}
		return p.base.ListAllVersions(ctx, org, module)
	}
	return p.base.ListAllVersions(ctx, org, module)
}
func (h *DependencyHandler) replacementPath(moduleName string) (string, bool) {
	replacement, ok := h.replacements[moduleName]
	if !ok || strings.TrimSpace(replacement.To) == "" {
		return "", false
	}
	path := replacement.To
	if !filepath.IsAbs(path) {
		if h.lock == nil {
			return "", false
		}
		path = filepath.Join(filepath.Dir(h.lock.Path()), path)
	}
	return path, true
}

// offlineEvidenceFailure reports the module to name in a missing-evidence
// error, and whether missing evidence explains the failures at all. A failing
// replaced module has another cause, so the set is reported as is. The named
// module is the lowest-sorted failure, independent of resolver emit order.
func (h *DependencyHandler) offlineEvidenceFailure(errs []ResolutionError) (string, bool) {
	named := ""
	for _, resolutionErr := range errs {
		module := strings.Trim(resolutionErr.Org+"/"+resolutionErr.Name, "/")
		if _, replaced := h.replacementPath(module); replaced {
			return "", false
		}
		if named == "" || module < named {
			named = module
		}
	}
	return named, len(errs) > 0
}

// replacementModuleVersion reads the authoritative version of a locally-replaced
// module from its wippy.yaml, used when resolving the module from its local source
// instead of the Hub.
func (h *DependencyHandler) replacementModuleVersion(moduleName string) string {
	path, ok := h.replacementPath(moduleName)
	if !ok {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(path, "wippy.yaml"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &manifest); err == nil {
		if version := strings.TrimSpace(manifest.Version); version != "" {
			return version
		}
	}
	// version is a top-level scalar; read it directly so an unrelated YAML
	// quirk elsewhere in the manifest cannot leave a locally replaced module
	// unresolvable and wrongly send it to the Hub.
	return topLevelYAMLScalar(data, "version")
}
func topLevelYAMLScalar(data []byte, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		return strings.Trim(value, `"'`)
	}
	return ""
}
