// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

	r := newResolver(provider, maxDepth, maxModules, opts.LockedVersions, opts.LockedDigests)

	for i, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.addConstraint(root.Org+"/"+root.Name, rootSource(i), root.Constraint)
	}

	if err := r.run(ctx); err != nil {
		return nil, err
	}

	return &ResolveDependenciesResult{
		Modules: r.collectModules(),
		Errors:  r.collectErrors(),
	}, nil
}

// errConstraintsIncompatible marks a live constraint set that no available
// version can satisfy simultaneously. The resolver reports it as a conflict
// error naming the module and every requester.
var errConstraintsIncompatible = errors.New("constraints incompatible")

// resolver resolves a dependency graph to a fixpoint. Every constraint and every
// dependency edge is tagged with its source (a root slot or a parent@version), so
// when a module's winning version changes the superseded version's subtree - both
// the constraints it contributed and the modules only it pulled - is retracted.
// The winning version is the highest available version satisfying the union of a
// module's live (non-retracted) constraints, which is independent of walk order.
type resolver struct {
	provider       ManifestProvider
	lockedVersions map[string]string
	lockedDigests  map[string]string

	contribs  map[string]map[string]string
	edges     map[string][]string
	resolved  map[string]string
	modules   map[string]ResolvedModule
	depth     map[string]int
	signature map[string]string
	pending   map[string]ResolutionError

	queue  []string
	queued map[string]bool

	errors     []ResolutionError
	maxDepth   int
	maxModules int
}

func newResolver(provider ManifestProvider, maxDepth, maxModules int, lockedVersions, lockedDigests map[string]string) *resolver {
	return &resolver{
		provider:       provider,
		maxDepth:       maxDepth,
		maxModules:     maxModules,
		lockedVersions: lockedVersions,
		lockedDigests:  lockedDigests,
		contribs:       make(map[string]map[string]string),
		edges:          make(map[string][]string),
		resolved:       make(map[string]string),
		modules:        make(map[string]ResolvedModule),
		depth:          make(map[string]int),
		signature:      make(map[string]string),
		pending:        make(map[string]ResolutionError),
		queued:         make(map[string]bool),
	}
}

// rootSource names a root demand slot so distinct roots never collide.
func rootSource(i int) string {
	return fmt.Sprintf("@root/%d", i)
}

// moduleSource names the demands a resolved module contributes to its dependencies.
func moduleSource(key, version string) string {
	return key + "@" + version
}

// addConstraint records that source demands constraint from module key, then
// enqueues the module for (re)resolution. Depth is derived from live edges, not
// tracked here.
func (r *resolver) addConstraint(key, source, constraint string) {
	demands := r.contribs[key]
	if demands == nil {
		demands = make(map[string]string)
		r.contribs[key] = demands
	}
	demands[source] = constraint

	if !containsString(r.edges[source], key) {
		r.edges[source] = append(r.edges[source], key)
	}

	r.enqueue(key)
}

func (r *resolver) enqueue(key string) {
	if r.queued[key] {
		return
	}
	r.queued[key] = true
	r.queue = append(r.queue, key)
}

// run drains the worklist to a fixpoint. Each module's chosen version only depends
// on its current live constraints, and a superseded version's edges are retracted
// before its successor's are added, so a valid graph converges. The iteration budget
// is a safety net against a pathological cyclic-constraint graph that never settles.
func (r *resolver) run(ctx context.Context) error {
	budget := (r.maxModules + 1) * 256
	iterations := 0

	for len(r.queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		iterations++
		if iterations > budget {
			r.errors = append(r.errors, ResolutionError{Message: "dependency resolution did not converge"})
			break
		}

		key := r.queue[0]
		r.queue = r.queue[1:]
		r.queued[key] = false
		r.process(ctx, key)
	}
	return nil
}

func (r *resolver) process(ctx context.Context, key string) {
	live := r.liveConstraints(key)
	if len(live) == 0 {
		r.orphan(key)
		return
	}

	depth, depthKnown := r.effectiveDepth(key)
	depthChanged := r.setDepth(key, depth, depthKnown)

	if depthKnown && depth >= r.maxDepth {
		if v, ok := r.resolved[key]; ok {
			r.retract(moduleSource(key, v))
			delete(r.resolved, key)
			delete(r.modules, key)
			delete(r.signature, key)
		}
		r.pending[key] = r.capError(key, live, fmt.Sprintf("maximum dependency depth %d exceeded", r.maxDepth))
		return
	}

	sig := strings.Join(live, "\x00")
	if _, ok := r.resolved[key]; ok && r.signature[key] == sig {
		if depthChanged {
			r.enqueueChildren(key)
		}
		return
	}

	if _, ok := r.resolved[key]; !ok {
		if len(r.modules) >= r.maxModules {
			r.pending[key] = r.capError(key, live, fmt.Sprintf("maximum module count %d exceeded", r.maxModules))
			return
		}
	}

	org, name := splitKey(key)
	version, err := r.resolveVersion(ctx, org, name, live)
	r.signature[key] = sig
	if err != nil {
		if v, ok := r.resolved[key]; ok {
			r.retract(moduleSource(key, v))
			delete(r.resolved, key)
			delete(r.modules, key)
		}
		r.pending[key] = r.resolutionError(key, live, err)
		return
	}
	delete(r.pending, key)

	if v, ok := r.resolved[key]; ok {
		if v == version {
			if depthChanged {
				r.enqueueChildren(key)
			}
			return
		}
		r.retract(moduleSource(key, v))
	}

	manifest, ferr := r.fetchManifest(ctx, org, name, version)
	if ferr != nil {
		delete(r.resolved, key)
		delete(r.modules, key)
		r.pending[key] = ResolutionError{
			Err:        ferr,
			Org:        org,
			Name:       name,
			Constraint: strings.Join(live, ", "),
			Message:    ferr.Error(),
		}
		return
	}

	r.resolved[key] = version
	r.modules[key] = ResolvedModule{
		Org:       manifest.Org,
		Name:      manifest.Name,
		Version:   manifest.Version,
		VersionID: manifest.VersionID,
		Digest:    manifest.Digest,
		SizeBytes: manifest.SizeBytes,
		Protected: manifest.Protected,
		URL:       manifest.URL,
	}

	source := moduleSource(key, version)
	for _, dep := range manifest.Dependencies {
		r.addConstraint(dep.Org+"/"+dep.Name, source, declaredConstraint(dep))
	}
}

// declaredConstraint returns the range the parent module declared for a
// dependency. A hub that predates the constraint field sends only the version it
// resolved to; fall back to it so an older server still yields a usable graph.
// Grafting a resolved version in place of a real range would pin the dependency,
// discarding any lock that satisfies the range and defeating intersection.
func declaredConstraint(dep ManifestDep) string {
	if dep.Constraint != "" {
		return dep.Constraint
	}
	return dep.Version
}

// orphan removes a module that no live constraint demands and retracts its subtree.
func (r *resolver) orphan(key string) {
	if v, ok := r.resolved[key]; ok {
		r.retract(moduleSource(key, v))
		delete(r.resolved, key)
		delete(r.modules, key)
	}
	delete(r.pending, key)
	delete(r.signature, key)
	delete(r.depth, key)
}

// effectiveDepth derives a module's depth from its live edges: the shallowest of
// its live sources (a root source is depth 0, a parent source is the parent's
// effective depth plus one). The bool is false when no source depth is yet known.
func (r *resolver) effectiveDepth(key string) (int, bool) {
	best := -1
	for source := range r.contribs[key] {
		d, ok := r.sourceDepth(source)
		if !ok {
			continue
		}
		if best < 0 || d < best {
			best = d
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// sourceDepth reports the depth a demand source implies for the module it targets.
func (r *resolver) sourceDepth(source string) (int, bool) {
	if strings.HasPrefix(source, "@root/") {
		return 0, true
	}
	parent := displaySource(source)
	if d, ok := r.depth[parent]; ok {
		return d + 1, true
	}
	return 0, false
}

// setDepth caches a module's effective depth and reports whether it changed.
func (r *resolver) setDepth(key string, depth int, known bool) bool {
	if !known {
		return false
	}
	prev, had := r.depth[key]
	if had && prev == depth {
		return false
	}
	r.depth[key] = depth
	return true
}

// enqueueChildren re-enqueues the modules a resolved module contributes to, so a
// change in its depth propagates through the live graph.
func (r *resolver) enqueueChildren(key string) {
	v, ok := r.resolved[key]
	if !ok {
		return
	}
	for _, child := range r.edges[moduleSource(key, v)] {
		r.enqueue(child)
	}
}

// retract removes every constraint a source contributed and re-enqueues the
// affected modules, cascading to modules that lose their last demand.
func (r *resolver) retract(source string) {
	children := r.edges[source]
	delete(r.edges, source)
	for _, child := range children {
		if demands := r.contribs[child]; demands != nil {
			delete(demands, source)
		}
		r.enqueue(child)
	}
}

// liveConstraints returns the distinct, sorted constraint strings currently
// demanded of a module.
func (r *resolver) liveConstraints(key string) []string {
	demands := r.contribs[key]
	if len(demands) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(demands))
	out := make([]string, 0, len(demands))
	for _, c := range demands {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// resolveVersion picks the version satisfying a module's live constraints. A single
// constraint follows the original per-constraint path; multiple constraints resolve
// against their intersection, yielding an order-independent result or a conflict.
func (r *resolver) resolveVersion(ctx context.Context, org, name string, live []string) (string, error) {
	if len(live) == 1 {
		return r.resolveConstraint(ctx, org, name, live[0])
	}
	return r.resolveIntersection(ctx, org, name, live)
}

// resolveIntersection selects the highest available version satisfying every
// constraint, or errConstraintsIncompatible when none does.
//
// The constraints are matched individually rather than concatenated into one
// string and reparsed: the parser caps a constraint at maxConstraintParts, a
// budget sized for a single authored range, and a handful of parents each
// declaring a two-sided range exceeds it. Concatenating would turn a perfectly
// satisfiable set into a parse failure reported as a version conflict.
func (r *resolver) resolveIntersection(ctx context.Context, org, name string, constraints []string) (string, error) {
	key := org + "/" + name

	if locked := r.lockedVersions[key]; locked != "" && lockedSatisfiesAll(locked, constraints) {
		return locked, nil
	}

	parsed := make([]semver.Constraint, 0, len(constraints))
	for _, c := range constraints {
		p, err := semver.ParseConstraint(c)
		if err != nil {
			return "", fmt.Errorf("invalid constraint %q: %w", c, err)
		}
		parsed = append(parsed, p)
	}

	versions, err := r.provider.ListAllVersions(ctx, org, name)
	if err != nil {
		return "", fmt.Errorf("list versions: %w", err)
	}

	var best, bestStable *VersionInfo
	var bestSem, bestStableSem semver.Version

	for i := range versions {
		v := &versions[i]
		sv, err := semver.ParseVersion(v.Version)
		if err != nil || !matchesAll(parsed, sv) {
			continue
		}
		if best == nil || sv.GreaterThan(bestSem) {
			best, bestSem = v, sv
		}
		if !sv.IsPrerelease() && (bestStable == nil || sv.GreaterThan(bestStableSem)) {
			bestStable, bestStableSem = v, sv
		}
	}

	// Mirror FindBestMatch: a stable release always wins over a prerelease.
	if bestStable != nil {
		return bestStable.Version, nil
	}
	if best != nil {
		return best.Version, nil
	}

	return "", errConstraintsIncompatible
}

// matchesAll reports whether a version satisfies every constraint.
func matchesAll(constraints []semver.Constraint, v semver.Version) bool {
	for _, c := range constraints {
		if !c.Match(v) {
			return false
		}
	}
	return true
}

// lockedSatisfiesAll reports whether a locked version satisfies every constraint.
func lockedSatisfiesAll(locked string, constraints []string) bool {
	for _, c := range constraints {
		if !lockedVersionSatisfies(locked, c) {
			return false
		}
	}
	return true
}

// capError builds a depth/count limit error for a module.
func (r *resolver) capError(key string, live []string, message string) ResolutionError {
	org, name := splitKey(key)
	return ResolutionError{
		Org:        org,
		Name:       name,
		Constraint: strings.Join(live, ", "),
		Message:    message,
	}
}

// resolutionError converts a resolution failure into a ResolutionError, rendering
// an incompatibility as a conflict that names the module and every requester.
func (r *resolver) resolutionError(key string, live []string, err error) ResolutionError {
	org, name := splitKey(key)
	if errors.Is(err, errConstraintsIncompatible) {
		return ResolutionError{
			Err:        err,
			Org:        org,
			Name:       name,
			Constraint: strings.Join(live, ", "),
			Message:    r.conflictMessage(key),
		}
	}
	return ResolutionError{
		Err:        err,
		Org:        org,
		Name:       name,
		Constraint: strings.Join(live, ", "),
		Message:    err.Error(),
	}
}

// conflictMessage names the module and each requester's constraint in deterministic
// order.
func (r *resolver) conflictMessage(key string) string {
	type demand struct {
		source     string
		constraint string
	}
	demands := make([]demand, 0, len(r.contribs[key]))
	for source, constraint := range r.contribs[key] {
		demands = append(demands, demand{source: source, constraint: constraint})
	}
	sort.Slice(demands, func(i, j int) bool {
		if demands[i].constraint != demands[j].constraint {
			return demands[i].constraint < demands[j].constraint
		}
		return demands[i].source < demands[j].source
	})

	var b strings.Builder
	fmt.Fprintf(&b, "conflicting version constraints for %s: ", key)
	for i, d := range demands {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q (required by %s)", d.constraint, displaySource(d.source))
	}
	return b.String()
}

// collectModules returns the resolved modules in a deterministic, walk-order
// independent order.
func (r *resolver) collectModules() []ResolvedModule {
	keys := make([]string, 0, len(r.modules))
	for key := range r.modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]ResolvedModule, 0, len(keys))
	for _, key := range keys {
		out = append(out, r.modules[key])
	}
	return out
}

// collectErrors returns resolution errors deterministically: any global failure
// first, then the per-module pending errors sorted by module.
func (r *resolver) collectErrors() []ResolutionError {
	keys := make([]string, 0, len(r.pending))
	for key := range r.pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := append([]ResolutionError(nil), r.errors...)
	for _, key := range keys {
		out = append(out, r.pending[key])
	}
	return out
}

// displaySource renders a demand source for an error message: a root slot as
// "root" and a parent@version as the bare parent module.
func displaySource(source string) string {
	if strings.HasPrefix(source, "@root/") {
		return "root"
	}
	if idx := strings.LastIndex(source, "@"); idx > 0 {
		return source[:idx]
	}
	return source
}

func splitKey(key string) (string, string) {
	if idx := strings.Index(key, "/"); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return key, ""
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func (r *resolver) fetchManifest(ctx context.Context, org, name, version string) (*ModuleManifest, error) {
	manifest, err := r.provider.GetManifest(ctx, org, name, version)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, fmt.Errorf("hub returned no manifest for %s/%s@%s", org, name, version)
	}
	if manifest.Org != org || manifest.Name != name {
		return nil, fmt.Errorf(
			"manifest identity mismatch: requested %s/%s@%s, hub returned %s/%s@%s",
			org, name, version, manifest.Org, manifest.Name, manifest.Version,
		)
	}
	if version != "" && !strings.HasPrefix(version, "@") &&
		strings.TrimPrefix(manifest.Version, "v") != strings.TrimPrefix(version, "v") {
		return nil, fmt.Errorf(
			"manifest version mismatch for %s/%s: requested %s, hub returned %s",
			org, name, version, manifest.Version,
		)
	}

	expected := r.lockedDigests[org+"/"+name+"@"+version]
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
