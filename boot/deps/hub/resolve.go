// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/boot/deps/hub/semver"
)

const (
	defaultMaxDepth   = 10
	defaultMaxModules = 200
)

// ResolveOptions configures the client-side dependency resolver.
type ResolveOptions struct {
	LockedVersions map[string]string
	LockedDigests  map[string]string
	MaxDepth       int
	MaxModules     int
}

// ManifestProvider retrieves manifests and version lists from the hub.
type ManifestProvider interface {
	GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error)
	ListAllVersions(ctx context.Context, org, module string) ([]VersionInfo, error)
}

// Resolve performs client-side dependency resolution by walking the dependency
// graph using GetManifest calls. It replaces the server-side ResolveDependencies
// endpoint, removing the 20-root limit.
func Resolve(ctx context.Context, provider ManifestProvider, roots []DependencySpec, opts *ResolveOptions) (*ResolveDependenciesResult, error) {
	if opts == nil {
		opts = &ResolveOptions{}
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	maxModules := opts.MaxModules
	if maxModules <= 0 {
		maxModules = defaultMaxModules
	}

	r := &resolver{
		provider:       provider,
		maxDepth:       maxDepth,
		maxModules:     maxModules,
		lockedVersions: opts.LockedVersions,
		lockedDigests:  opts.LockedDigests,
		visited:        make(map[string]bool),
	}

	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.resolveOne(ctx, root.Org, root.Name, root.Constraint, 0)
	}

	return &ResolveDependenciesResult{
		Modules: r.modules,
		Errors:  r.errors,
	}, nil
}

type resolver struct {
	provider       ManifestProvider
	lockedVersions map[string]string
	lockedDigests  map[string]string
	visited        map[string]bool
	modules        []ResolvedModule
	errors         []ResolutionError
	maxDepth       int
	maxModules     int
}

func (r *resolver) resolveOne(ctx context.Context, org, name, constraint string, depth int) {
	if ctx.Err() != nil {
		return
	}

	key := org + "/" + name
	if r.visited[key] {
		return
	}
	r.visited[key] = true

	if depth >= r.maxDepth {
		r.errors = append(r.errors, ResolutionError{
			Org:        org,
			Name:       name,
			Constraint: constraint,
			Message:    fmt.Sprintf("maximum dependency depth %d exceeded", r.maxDepth),
		})
		return
	}

	if len(r.modules) >= r.maxModules {
		r.errors = append(r.errors, ResolutionError{
			Org:        org,
			Name:       name,
			Constraint: constraint,
			Message:    fmt.Sprintf("maximum module count %d exceeded", r.maxModules),
		})
		return
	}

	version, err := r.resolveConstraint(ctx, org, name, constraint)
	if err != nil {
		r.errors = append(r.errors, ResolutionError{
			Org:        org,
			Name:       name,
			Constraint: constraint,
			Message:    err.Error(),
		})
		return
	}

	manifest, err := r.fetchManifest(ctx, org, name, version)
	if err != nil {
		r.errors = append(r.errors, ResolutionError{
			Org:        org,
			Name:       name,
			Constraint: constraint,
			Message:    err.Error(),
		})
		return
	}

	r.modules = append(r.modules, ResolvedModule{
		Org:       manifest.Org,
		Name:      manifest.Name,
		Version:   manifest.Version,
		VersionID: manifest.VersionID,
		Digest:    manifest.Digest,
		SizeBytes: manifest.SizeBytes,
		Protected: manifest.Protected,
		URL:       manifest.URL,
	})

	for _, dep := range manifest.Dependencies {
		r.resolveOne(ctx, dep.Org, dep.Name, dep.Version, depth+1)
	}
}

func (r *resolver) fetchManifest(ctx context.Context, org, name, version string) (*ModuleManifest, error) {
	manifest, err := r.provider.GetManifest(ctx, org, name, version)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, fmt.Errorf("hub returned no manifest for %s/%s@%s", org, name, version)
	}

	expected := r.lockedDigests[org+"/"+name]
	if expected == "" || manifest.Digest == expected {
		return manifest, nil
	}

	if cache, ok := r.provider.(*ManifestCache); ok {
		fresh, ferr := cache.Refresh(ctx, org, name, version)
		if ferr != nil {
			return nil, ferr
		}
		if fresh != nil && fresh.Digest == expected {
			return fresh, nil
		}
		if fresh != nil {
			manifest = fresh
		}
	}

	return nil, fmt.Errorf(
		"manifest digest mismatch for %s/%s@%s: lockfile pins %s, hub served %s",
		org, name, version, expected, manifest.Digest,
	)
}

// resolveConstraint determines the exact version to fetch for a given constraint.
// For exact versions, labels, and empty constraints it returns the constraint as-is
// (the server handles resolution). For semver ranges it lists versions and picks
// the best match client-side.
func (r *resolver) resolveConstraint(ctx context.Context, org, name, constraint string) (string, error) {
	if locked := r.lockedVersions[org+"/"+name]; locked != "" && lockedVersionSatisfies(locked, constraint) {
		return locked, nil
	}

	if constraint == "" || strings.HasPrefix(constraint, "@") {
		return constraint, nil
	}

	// Exact-match operator (=v1.2.3): strip the prefix and pass directly.
	if strings.HasPrefix(constraint, "=") && !strings.HasPrefix(constraint, "==") {
		rest := strings.TrimPrefix(constraint, "=")
		if !semver.IsConstraint(rest) {
			return rest, nil
		}
	}

	if !semver.IsConstraint(constraint) {
		return constraint, nil
	}

	parsed, err := semver.ParseConstraint(constraint)
	if err != nil {
		return "", fmt.Errorf("invalid constraint %q: %w", constraint, err)
	}

	versions, err := r.provider.ListAllVersions(ctx, org, name)
	if err != nil {
		return "", fmt.Errorf("list versions: %w", err)
	}

	type indexedVersion struct {
		original string
		parsed   semver.Version
	}

	indexed := make([]indexedVersion, 0, len(versions))
	semverVersions := make([]semver.Version, 0, len(versions))
	for _, v := range versions {
		sv, err := semver.ParseVersion(v.Version)
		if err != nil {
			continue
		}
		indexed = append(indexed, indexedVersion{parsed: sv, original: v.Version})
		semverVersions = append(semverVersions, sv)
	}

	best, err := parsed.FindBestMatch(semverVersions)
	if err != nil {
		return "", fmt.Errorf("no version matching %q", constraint)
	}

	for _, iv := range indexed {
		if iv.parsed.Equal(best) {
			return iv.original, nil
		}
	}

	return best.String(), nil
}

func lockedVersionSatisfies(version, constraint string) bool {
	version = strings.TrimSpace(version)
	constraint = strings.TrimSpace(constraint)
	if version == "" {
		return false
	}
	if constraint == "" {
		return true
	}
	if strings.HasPrefix(constraint, "@") {
		return false
	}

	if semver.IsConstraint(constraint) {
		parsed, err := semver.ParseConstraint(constraint)
		if err != nil {
			return false
		}
		locked, err := semver.ParseVersion(version)
		if err != nil {
			return false
		}
		return parsed.Match(locked)
	}

	if version == constraint || strings.TrimPrefix(version, "v") == strings.TrimPrefix(constraint, "v") {
		return true
	}
	return false
}
