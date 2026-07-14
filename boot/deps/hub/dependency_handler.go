// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/auth"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/boot/deps/wappextract"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	entrypkg "github.com/wippyai/runtime/system/entry"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	metaModuleKey        = "module"
	metaModuleVersionKey = "module_version"
	extractedModuleMeta  = ".wippy-module.yaml"
)

type DependencyHandlerOptions struct {
	Hub             HubClient
	Logger          *zap.Logger
	Resolver        regapi.DependencyResolver
	LockPath        string
	VendorDir       string
	ResolveTimeout  time.Duration
	DownloadTimeout time.Duration
}

type DependencyHandler struct {
	hub             HubClient
	manifestCache   *ManifestCache
	logger          *zap.Logger
	resolver        regapi.DependencyResolver
	lockPath        string
	vendorDir       string
	resolveTimeout  time.Duration
	downloadTimeout time.Duration
}

// HubClient defines the hub operations required for dependency handling.
//
//nolint:revive // keeps explicit package-disambiguated API name.
type HubClient interface {
	ManifestProvider
	GetDownloadURL(ctx context.Context, params *DownloadParams) (*DownloadInfo, error)
	DownloadToFile(ctx context.Context, url, destPath string) error
}

// DependencyDefinition represents the data structure of an ns.dependency entry.
type DependencyDefinition struct {
	Component  string      `json:"component" yaml:"component"`
	Version    string      `json:"version" yaml:"version"`
	Parameters []Parameter `json:"parameters" yaml:"parameters"`
}

// Parameter represents a single parameter in a dependency definition.
// Value carries the supplied value in its source type so typed parameters
// decode without forcing a string.
type Parameter struct {
	Value any    `json:"value" yaml:"value"`
	Name  string `json:"name" yaml:"name"`
}

type desiredDependency struct {
	entry      regapi.Entry
	definition DependencyDefinition
}

func NewDependencyHandler(opts DependencyHandlerOptions) (*DependencyHandler, error) {
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	client := opts.Hub
	if client == nil {
		hubClient, err := newHubClientFromAuth()
		if err != nil {
			return nil, err
		}
		client = hubClient
	}

	lockPath := opts.LockPath
	if lockPath == "" {
		if found, err := lock.Find(".", lock.DefaultFilename); err == nil {
			lockPath = found
		}
	}

	vendorDir := opts.VendorDir
	if vendorDir == "" && lockPath != "" {
		if lockObj, err := lock.New(lockPath); err == nil {
			lockDir := filepath.Dir(lockObj.Path())
			vendorDir = filepath.Join(lockDir, lockObj.GetVendorPath())
		}
	}
	if vendorDir == "" {
		vendorDir = filepath.Join(".wippy", "vendor")
	}

	return &DependencyHandler{
		hub:             client,
		manifestCache:   NewManifestCache(client),
		logger:          logger,
		resolver:        opts.Resolver,
		lockPath:        lockPath,
		vendorDir:       vendorDir,
		resolveTimeout:  opts.ResolveTimeout,
		downloadTimeout: opts.DownloadTimeout,
	}, nil
}

func (h *DependencyHandler) Expand(ctx context.Context, op regapi.Operation, snapshot regapi.State) (regapi.DirectiveResult, error) {
	if h == nil || h.hub == nil {
		return regapi.DirectiveResult{}, ErrDependencyHandlerNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return regapi.DirectiveResult{}, err
	}

	entry, ok := resolveOperationEntry(op, snapshot)
	if !ok {
		return regapi.DirectiveResult{}, nil
	}
	if entry.Kind != regapi.NamespaceDependency {
		return regapi.DirectiveResult{}, nil
	}
	if !isRootDependency(entry) {
		return regapi.DirectiveResult{}, nil
	}

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return regapi.DirectiveResult{}, ErrDependencyTranscoderMissing
	}

	satisfied, err := h.unchangedRootDependencySatisfied(ctx, transcoder, op, entry, snapshot)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	if satisfied {
		return regapi.DirectiveResult{Applied: true}, nil
	}

	lockedVersions, err := h.installedModuleVersions(ctx, transcoder, snapshot)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	controlledModules, err := h.collectControlledModules(ctx, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	desiredDeps, err := h.collectDesiredDependencies(ctx, op, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	desiredDepEntries := make([]regapi.Entry, 0, len(desiredDeps))
	for _, dep := range desiredDeps {
		desiredDepEntries = append(desiredDepEntries, dep.entry)
	}

	var resolved []ResolvedModule
	desiredRoots := dependencyDefinitions(desiredDeps)
	if len(desiredRoots) > 0 {
		var err error
		resolved, err = h.resolveModules(ctx, desiredRoots, lockedVersions)
		if err != nil {
			return regapi.DirectiveResult{}, err
		}
	}

	opComponent := ""
	for _, dep := range desiredDeps {
		if idsEqual(dep.entry.ID, op.Entry.ID) {
			opComponent = dep.definition.Component
			break
		}
	}
	strictModules := touchedModuleNames(resolved, lockedVersions, opComponent)
	mutableModules, err := h.operationModules(ctx, op, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	touchedModules := stringSet(strictModules)
	for module := range mutableModules {
		touchedModules[module] = struct{}{}
	}
	desiredModules := resolvedModuleSet(resolved)

	moduleEntries, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, touchedModules), resolved, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	linkDeps := mergeLinkDependencies(desiredDepEntries, moduleEntries)

	combined := make([]regapi.Entry, 0, len(snapshot)+len(moduleEntries))
	for _, e := range snapshot {
		if module := entryModule(e); module != "" {
			if _, desired := desiredModules[module]; !desired {
				continue
			}
			if _, touched := touchedModules[module]; touched {
				continue
			}
		}
		combined = append(combined, e)
	}
	combined = append(combined, moduleEntries...)

	pipeline := build.New(
		stages.Override(stages.WithMissingOverrideEntriesIgnored()),
		stages.Disable(),
		stages.Link(stages.WithDependencies(linkDeps), stages.WithStrictRequirementModules(strictModules)),
		stages.Override(stages.WithMissingOverrideEntriesIgnored()),
	)
	if err := pipeline.Execute(ctx, &combined); err != nil {
		return regapi.DirectiveResult{}, NewDependencyPipelineError(err)
	}

	additional, err := h.buildOperations(snapshot, combined, op.Entry.ID, controlledModules, mutableModules)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	scoped := make([]regapi.ScopedOperation, 0, len(additional))
	for _, op := range additional {
		scoped = append(scoped, regapi.ScopedOperation{
			Operation: op,
			Scope:     regapi.ScopeBaseline,
		})
	}

	var effects []regapi.Effect
	packEffect, err := h.buildEmbedPackEffect(ctx, resolved, snapshot)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	if packEffect != nil {
		effects = append(effects, packEffect)
	}

	return regapi.DirectiveResult{
		Applied:    true,
		Additional: scoped,
		Effects:    effects,
	}, nil
}

func (h *DependencyHandler) unchangedRootDependencySatisfied(
	ctx context.Context,
	transcoder payload.Transcoder,
	op regapi.Operation,
	entry regapi.Entry,
	snapshot regapi.State,
) (bool, error) {
	if op.Kind != regapi.EntryUpdate {
		return false, nil
	}

	var current regapi.Entry
	found := false
	for _, candidate := range snapshot {
		if idsEqual(candidate.ID, entry.ID) {
			current = candidate
			found = true
			break
		}
	}
	if !found || !entriesEqual(current, entry) {
		return false, nil
	}

	definition, err := decodeDependency(ctx, transcoder, entry)
	if err != nil {
		return false, err
	}
	if definition.Component == "" {
		return false, nil
	}

	installedVersion := snapshotModuleVersions(snapshot)[definition.Component]
	if installedVersion == "" {
		installedVersion = h.replacementModuleVersion(definition.Component)
	}
	return lockedVersionSatisfies(installedVersion, definition.Version), nil
}

func (h *DependencyHandler) collectSnapshotDependencies(
	ctx context.Context,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) ([]desiredDependency, error) {
	deps := make([]desiredDependency, 0)
	for _, entry := range snapshot {
		if !isRootDependency(entry) {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}
		deps = append(deps, desiredDependency{
			entry:      entry,
			definition: def,
		})
	}
	return deps, nil
}

func (h *DependencyHandler) collectControlledModules(
	ctx context.Context,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) (map[string]struct{}, error) {
	controlled := make(map[string]struct{})
	dependencyLinks := make(map[string][]string)

	for _, entry := range snapshot {
		if entry.Kind != regapi.NamespaceDependency {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}

		if owner := entryModule(entry); owner != "" {
			dependencyLinks[owner] = append(dependencyLinks[owner], def.Component)
			continue
		}
		controlled[def.Component] = struct{}{}
	}

	queue := make([]string, 0, len(controlled))
	for module := range controlled {
		queue = append(queue, module)
	}
	for len(queue) > 0 {
		module := queue[0]
		queue = queue[1:]
		for _, dep := range dependencyLinks[module] {
			if _, seen := controlled[dep]; seen {
				continue
			}
			controlled[dep] = struct{}{}
			queue = append(queue, dep)
		}
	}

	return controlled, nil
}

func dependencyDefinitions(deps []desiredDependency) []DependencyDefinition {
	roots := make([]DependencyDefinition, 0, len(deps))
	for _, dep := range deps {
		roots = append(roots, dep.definition)
	}
	return roots
}

func (h *DependencyHandler) collectDesiredDependencies(
	ctx context.Context,
	op regapi.Operation,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) ([]desiredDependency, error) {
	deps := make(map[string]desiredDependency)
	lockedVersions, err := h.installedModuleVersions(ctx, transcoder, snapshot)
	if err != nil {
		return nil, err
	}
	operationID := op.Entry.ID

	current, err := h.collectSnapshotDependencies(ctx, snapshot, transcoder)
	if err != nil {
		return nil, err
	}
	for _, dep := range current {
		if !idsEqual(dep.entry.ID, operationID) {
			dep.definition = pinExistingDependencyVersion(dep.definition, lockedVersions)
		}
		deps[idKey(dep.entry.ID)] = dep
	}

	switch op.Kind {
	case regapi.EntryDelete:
		delete(deps, idKey(op.Entry.ID))
	case regapi.EntryCreate, regapi.EntryUpdate:
		entry, ok := resolveOperationEntry(op, snapshot)
		if !ok {
			return nil, NewDependencyEntryMissingError(op.Entry.ID.String())
		}
		if !isRootDependency(entry) {
			break
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		deps[idKey(entry.ID)] = desiredDependency{
			entry:      entry,
			definition: def,
		}
	}

	result := make([]desiredDependency, 0, len(deps))
	for id, dep := range deps {
		if dep.definition.Component == "" {
			return nil, NewDependencyEntryInvalidError(id, "component is required", "")
		}
		result = append(result, dep)
	}
	if err := validateRootDependencyComponents(result, operationID); err != nil {
		return nil, err
	}
	return result, nil
}

func validateRootDependencyComponents(deps []desiredDependency, operationID regapi.ID) error {
	seen := make(map[string]regapi.ID, len(deps))
	for _, dep := range deps {
		component := dep.definition.Component
		if existingID, ok := seen[component]; ok && !idsEqual(existingID, dep.entry.ID) {
			if idsEqual(existingID, operationID) {
				return NewDependencyRootConflictError(component, dep.entry.ID.String(), existingID.String())
			}
			return NewDependencyRootConflictError(component, existingID.String(), dep.entry.ID.String())
		}
		seen[component] = dep.entry.ID
	}
	return nil
}

func (h *DependencyHandler) installedModuleVersions(ctx context.Context, transcoder payload.Transcoder, snapshot regapi.State) (map[string]string, error) {
	versions := snapshotModuleVersions(snapshot)
	if h.lockPath == "" {
		return versions, nil
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return versions, nil
	}
	installedRoots, err := rootDependencyModules(ctx, transcoder, snapshot)
	if err != nil {
		return nil, err
	}
	for _, mod := range lockObj.GetModules() {
		if mod.Name == "" || mod.Version == "" {
			continue
		}
		if _, installed := installedRoots[mod.Name]; !installed {
			continue
		}
		if _, ok := versions[mod.Name]; !ok {
			versions[mod.Name] = mod.Version
		}
	}
	return versions, nil
}

func snapshotModuleVersions(snapshot regapi.State) map[string]string {
	versions := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, entry := range snapshot {
		module := entryModule(entry)
		if module == "" || entry.Meta == nil {
			continue
		}
		raw, ok := entry.Meta[metaModuleVersionKey]
		if !ok {
			continue
		}
		version, ok := raw.(string)
		if !ok || version == "" {
			continue
		}
		if _, bad := ambiguous[module]; bad {
			continue
		}
		if existing, seen := versions[module]; seen && existing != version {
			delete(versions, module)
			ambiguous[module] = struct{}{}
			continue
		}
		versions[module] = version
	}
	return versions
}

func pinExistingDependencyVersion(def DependencyDefinition, moduleVersions map[string]string) DependencyDefinition {
	if def.Component == "" {
		return def
	}
	if version := moduleVersions[def.Component]; version != "" {
		def.Version = version
	}
	return def
}

func mergeLinkDependencies(explicitDeps, moduleEntries []regapi.Entry) []regapi.Entry {
	merged := make([]regapi.Entry, 0, len(explicitDeps)+len(moduleEntries))
	seen := make(map[string]struct{}, len(explicitDeps)+len(moduleEntries))

	appendDep := func(entry regapi.Entry) {
		if entry.Kind != regapi.NamespaceDependency {
			return
		}
		key := idKey(entry.ID)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, entry)
	}

	for _, entry := range explicitDeps {
		appendDep(entry)
	}
	for _, entry := range moduleEntries {
		appendDep(entry)
	}

	return merged
}

func (h *DependencyHandler) resolveModules(ctx context.Context, deps []DependencyDefinition, lockedVersions map[string]string) ([]ResolvedModule, error) {
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

	provider := ManifestProvider(h.hub)
	if h.manifestCache != nil {
		provider = h.manifestCache
	}
	lockedDigests := h.lockedModuleDigests()
	provider = &replacementManifestProvider{
		base:           provider,
		handler:        h,
		lockedVersions: lockedVersions,
		lockedDigests:  lockedDigests,
	}
	result, err := Resolve(resolveCtx, provider, roots, &ResolveOptions{
		LockedVersions: lockedVersions,
		LockedDigests:  lockedDigests,
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
		return nil, NewDependencyResolutionErrors(result.Errors)
	}

	return result.Modules, nil
}

// replacementManifestProvider resolves locally-replaced modules from their lock
// replacement instead of the Hub. A replaced module's source of truth is local,
// so it must never be re-fetched from the Hub during live changeset expansion —
// otherwise installing any module fails when an already-installed, locally-sourced
// module is absent from the Hub. Non-replaced modules delegate to the base provider.
type replacementManifestProvider struct {
	base           ManifestProvider
	handler        *DependencyHandler
	lockedVersions map[string]string
	lockedDigests  map[string]string
}

func (p *replacementManifestProvider) replacedVersion(name, constraint string) string {
	if version := p.lockedVersions[name]; version != "" {
		return version
	}
	if version := p.handler.replacementModuleVersion(name); version != "" {
		return version
	}
	return strings.TrimPrefix(constraint, "@")
}

func (p *replacementManifestProvider) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	name := org + "/" + module
	if path, ok := p.handler.replacementPath(name); ok {
		if version := p.replacedVersion(name, constraint); version != "" {
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
	}
	return p.base.GetManifest(ctx, org, module, constraint)
}

func (p *replacementManifestProvider) localReplacementDependencies(ctx context.Context, path string) ([]ManifestDep, error) {
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return nil, ErrDependencyTranscoderMissing
	}

	entries, err := loadReplacementDependencyEntries(ctx, path, p.handler.logger, transcoder)
	if err != nil {
		return nil, err
	}
	entries, err = p.handler.applyModuleConfigFilters(ctx, path, entries)
	if err != nil {
		return nil, err
	}

	deps := make([]ManifestDep, 0)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if entry.Kind != regapi.NamespaceDependency {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}
		name, err := graph.ParseName(def.Component)
		if err != nil {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "invalid component", def.Component)
		}

		key := name.String() + "@" + def.Version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deps = append(deps, ManifestDep{
			Org:     name.Organization,
			Name:    name.Module,
			Version: def.Version,
		})
	}
	return deps, nil
}

func loadReplacementDependencyEntries(
	ctx context.Context,
	path string,
	logger *zap.Logger,
	transcoder payload.Transcoder,
) ([]regapi.Entry, error) {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return nil, nil
	}

	dirFS := os.DirFS(path)
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
		if version := p.replacedVersion(name, ""); version != "" {
			return []VersionInfo{{Version: version}}, nil
		}
	}
	return p.base.ListAllVersions(ctx, org, module)
}

// touchedModuleNames returns the resolved modules this operation actually
// affects: those new or version-changed relative to the snapshot, plus the
// module of the dependency entry being changed in this operation. Modules
// already installed at the same version that this operation does not target are
// trusted — they were validated when installed — and are excluded from strict
// requirement enforcement, so a partial update does not re-validate
// dependencies it did not touch.
func touchedModuleNames(modules []ResolvedModule, installed map[string]string, opComponent string) []string {
	names := make([]string, 0, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		name := mod.Org + "/" + mod.Name
		version, known := installed[name]
		if !known || version != mod.Version || name == opComponent {
			names = append(names, name)
		}
	}
	return names
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func resolvedModuleSet(modules []ResolvedModule) map[string]struct{} {
	out := make(map[string]struct{}, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		out[mod.Org+"/"+mod.Name] = struct{}{}
	}
	return out
}

func filterResolvedModules(modules []ResolvedModule, keep map[string]struct{}) []ResolvedModule {
	if len(modules) == 0 || len(keep) == 0 {
		return nil
	}
	out := make([]ResolvedModule, 0, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		if _, ok := keep[mod.Org+"/"+mod.Name]; ok {
			out = append(out, mod)
		}
	}
	return out
}

func (h *DependencyHandler) loadModuleEntries(ctx context.Context, modules []ResolvedModule, ownerModules []ResolvedModule, snapshot regapi.State, transcoder payload.Transcoder) ([]regapi.Entry, error) {
	entries := make([]regapi.Entry, 0)
	owners := moduleOwnersByNamespace(ownerModules)
	snapshotOwners := moduleOwnersByEntryID(snapshot)
	snapshotByID := entriesByID(snapshot)
	installedRoots, err := rootDependencyModules(ctx, transcoder, snapshot)
	if err != nil {
		return nil, err
	}

	for _, mod := range modules {
		moduleName := mod.Org + "/" + mod.Name
		moduleEntries, err := h.loadEntriesForModule(ctx, transcoder, mod)
		if err != nil {
			return nil, err
		}
		for i := range moduleEntries {
			if keep, ok := preserveHostSnapshotEntry(moduleEntries[i], moduleName, snapshotByID, installedRoots); ok {
				moduleEntries[i] = keep
				continue
			}
			moduleEntries[i] = markModuleMetaForGraph(moduleEntries[i], moduleName, mod.Version, owners, snapshotOwners)
		}
		entries = append(entries, moduleEntries...)
	}

	return entries, nil
}

func entriesByID(entries regapi.State) map[string]regapi.Entry {
	byID := make(map[string]regapi.Entry, len(entries))
	for _, entry := range entries {
		byID[idKey(entry.ID)] = entry
	}
	return byID
}

func rootDependencyModules(ctx context.Context, transcoder payload.Transcoder, entries regapi.State) (map[string]struct{}, error) {
	modules := make(map[string]struct{})
	for _, entry := range entries {
		if !isRootDependency(entry) {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component != "" {
			modules[def.Component] = struct{}{}
		}
	}
	return modules, nil
}

func preserveHostSnapshotEntry(entry regapi.Entry, moduleName string, snapshot map[string]regapi.Entry, installedRoots map[string]struct{}) (regapi.Entry, bool) {
	if _, installed := installedRoots[moduleName]; !installed {
		return regapi.Entry{}, false
	}
	if entryModule(entry) != "" {
		return regapi.Entry{}, false
	}
	existing, ok := snapshot[idKey(entry.ID)]
	if !ok || entryModule(existing) != "" {
		return regapi.Entry{}, false
	}
	return existing, true
}

type moduleOwner struct {
	name    string
	version string
}

func moduleOwnersByNamespace(modules []ResolvedModule) map[string]moduleOwner {
	owners := make(map[string]moduleOwner, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		namespace := mod.Org + "." + mod.Name
		owners[namespace] = moduleOwner{
			name:    mod.Org + "/" + mod.Name,
			version: mod.Version,
		}
	}
	return owners
}

func moduleOwnersByEntryID(entries regapi.State) map[string]moduleOwner {
	owners := make(map[string]moduleOwner, len(entries))
	for _, entry := range entries {
		module := entryModule(entry)
		if module == "" {
			continue
		}
		owners[idKey(entry.ID)] = moduleOwner{
			name:    module,
			version: moduleVersion(entry),
		}
	}
	return owners
}

func (h *DependencyHandler) loadEntriesForModule(ctx context.Context, transcoder payload.Transcoder, mod ResolvedModule) ([]regapi.Entry, error) {
	modulePath, err := h.ensureModuleAvailable(ctx, mod)
	if err != nil {
		return nil, err
	}
	registerResolvedModuleSourceRoot(ctx, mod.Org+"/"+mod.Name, modulePath)
	entries, err := loadRawEntriesFromPaths(ctx, []string{modulePath}, h.logger, transcoder)
	if err != nil {
		return nil, err
	}
	return h.applyModuleConfigFilters(ctx, modulePath, entries)
}

// applyModuleConfigFilters drops entries the module's wippy.yaml excludes
// (exclude / exclude_meta) when the module is loaded from a directory tree —
// e.g. a lock replacement pointed at the module's source. Without it a host app
// picks up the module's own fixtures (test/_index.yaml under namespace "app"),
// which then collide with the host's real entries during linking. .wapp packs
// are skipped: they were already filtered at publish time.
func (h *DependencyHandler) applyModuleConfigFilters(ctx context.Context, modulePath string, entries []regapi.Entry) ([]regapi.Entry, error) {
	if filepath.Ext(modulePath) == ".wapp" {
		return entries, nil
	}
	cfg, err := depconfig.Load(modulePath)
	if err != nil {
		return entries, nil
	}
	entryExcludes := cfg.EntryExcludes()
	if len(entryExcludes) == 0 && len(cfg.ExcludeMeta) == 0 {
		return entries, nil
	}
	filtered := append([]regapi.Entry(nil), entries...)
	stage := stages.DisableWithOptions(stages.DisableOptions{
		Entries:     entryExcludes,
		MetaFilters: cfg.ExcludeMeta,
	})
	if err := stage.Execute(ctx, &filtered); err != nil {
		return nil, NewDependencyLoadError(modulePath, err)
	}
	return filtered, nil
}

func registerResolvedModuleSourceRoot(ctx context.Context, moduleName, modulePath string) {
	if moduleName == "" || filepath.Ext(modulePath) == ".wapp" {
		return
	}
	stat, err := os.Stat(modulePath)
	if err != nil || !stat.IsDir() {
		return
	}
	root, err := filepath.Abs(modulePath)
	if err != nil {
		return
	}
	moduleapi.WithSourceRoots(ctx, moduleapi.SourceRoots{moduleName: root})
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

	if replacementPath, ok := h.replacementPath(moduleName); ok {
		stat, err := os.Stat(replacementPath)
		if err != nil {
			return "", NewDependencyLoadError(replacementPath, err)
		}
		if !stat.IsDir() {
			return "", NewDependencyLoadError(replacementPath, fmt.Errorf("replacement path is not a directory"))
		}
		return replacementPath, nil
	}

	shouldUnpack := h.shouldUnpackModules()
	dirPath := filepath.Join(h.vendorDir, lock.ModulePath(name))
	expectedDigest := mod.Digest
	expectedSize := mod.SizeBytes
	wappPath := filepath.Join(h.vendorDir, lock.WappPath(name, mod.Version))
	if exists(wappPath) {
		if err := verifyDownloadedArtifact(wappPath, expectedDigest, expectedSize); err == nil {
			if shouldUnpack {
				if err := h.extractWappModule(wappPath, dirPath, expectedDigest, expectedSize); err != nil {
					return "", err
				}
				return dirPath, nil
			}
			return wappPath, nil
		}
		h.logger.Warn("cached dependency artifact failed integrity check; redownloading",
			zap.String("module", modKey(mod)),
			zap.String("path", wappPath))
		_ = os.Remove(wappPath)
	}

	if exists(dirPath) {
		if installed, ok := h.installedVersion(name.String()); ok && installed == mod.Version {
			if err := verifyExtractedModule(dirPath, expectedDigest, expectedSize); err == nil {
				return dirPath, nil
			} else if expectedDigest != "" || expectedSize > 0 {
				h.logger.Warn("unpacked dependency failed integrity check; redownloading",
					zap.String("module", modKey(mod)),
					zap.String("path", dirPath),
					zap.Error(err))
			}
		}
	}

	url := mod.URL
	urlIsFresh := false
	if url == "" {
		info, infoErr := h.freshDownloadInfo(ctx, mod)
		if infoErr != nil {
			return "", NewDependencyDownloadError(modKey(mod), infoErr)
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
		return "", NewDependencyDownloadError(modKey(mod), ErrDependencyNoContent)
	}

	downloadCtx, cancel := withOptionalTimeout(ctx, h.downloadTimeout)
	defer cancel()

	downloadErr := h.hub.DownloadToFile(downloadCtx, url, wappPath)
	if downloadErr != nil && !urlIsFresh {
		// mod.URL is a presigned URL captured at resolve time; on a long-lived
		// process it can expire (15-min TTL) before download. Fetch a fresh URL
		// and retry once before giving up.
		if info, infoErr := h.freshDownloadInfo(ctx, mod); infoErr == nil && info != nil && info.URL != "" {
			if expectedDigest == "" {
				expectedDigest = info.Digest
			}
			if expectedSize == 0 {
				expectedSize = info.Size
			}
			retryCtx, retryCancel := withOptionalTimeout(ctx, h.downloadTimeout)
			defer retryCancel()
			downloadErr = h.hub.DownloadToFile(retryCtx, info.URL, wappPath)
		}
	}
	if downloadErr != nil {
		return "", NewDependencyDownloadError(modKey(mod), downloadErr)
	}
	if err := verifyDownloadedArtifact(wappPath, expectedDigest, expectedSize); err != nil {
		_ = os.Remove(wappPath)
		return "", NewDependencyIntegrityError(modKey(mod), err, expectedDigest, expectedSize)
	}

	if shouldUnpack {
		if err := h.extractWappModule(wappPath, dirPath, expectedDigest, expectedSize); err != nil {
			return "", err
		}
		return dirPath, nil
	}

	return wappPath, nil
}

// freshDownloadInfo fetches a current presigned download URL for a module.
// Used both when the resolved manifest carries no URL and to refresh a URL
// that expired before the artifact could be downloaded.
func (h *DependencyHandler) freshDownloadInfo(ctx context.Context, mod ResolvedModule) (*DownloadInfo, error) {
	downloadURLCtx, cancel := withOptionalTimeout(ctx, h.downloadTimeout)
	defer cancel()

	return h.hub.GetDownloadURL(downloadURLCtx, &DownloadParams{
		Org:       mod.Org,
		Module:    mod.Name,
		Version:   mod.Version,
		VersionID: mod.VersionID,
	})
}

func (h *DependencyHandler) extractWappModule(wappPath, dirPath string, digest string, size uint64) error {
	tmpDir, err := os.MkdirTemp(filepath.Dir(dirPath), "."+filepath.Base(dirPath)+".extract-*")
	if err != nil {
		return NewDependencyLoadError(dirPath, err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := wappextract.ExtractWappToDirKeepSource(wappPath, tmpDir); err != nil {
		return NewDependencyLoadError(wappPath, err)
	}
	if err := writeExtractedModuleMeta(tmpDir, digest, size); err != nil {
		return NewDependencyLoadError(tmpDir, err)
	}
	if err := replaceDirectory(dirPath, tmpDir); err != nil {
		return NewDependencyLoadError(dirPath, err)
	}
	if err := os.Remove(wappPath); err != nil {
		h.logger.Warn("failed to remove unpacked module artifact",
			zap.String("path", wappPath),
			zap.Error(err))
	}
	cleanupTmp = false
	return nil
}

func replaceDirectory(targetDir, replacementDir string) error {
	parent := filepath.Dir(targetDir)
	base := filepath.Base(targetDir)
	var backupDir string

	if exists(targetDir) {
		var err error
		backupDir, err = os.MkdirTemp(parent, "."+base+".backup-*")
		if err != nil {
			return fmt.Errorf("create backup directory: %w", err)
		}
		if err := os.Remove(backupDir); err != nil {
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("prepare backup directory: %w", err)
		}
		if err := os.Rename(targetDir, backupDir); err != nil {
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("move existing directory aside: %w", err)
		}
	}

	if err := os.Rename(replacementDir, targetDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, targetDir)
		}
		return fmt.Errorf("activate extracted directory: %w", err)
	}

	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

func (h *DependencyHandler) replacementPath(moduleName string) (string, bool) {
	if h.lockPath == "" {
		return "", false
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return "", false
	}
	replacement, ok := lockObj.GetReplacement(moduleName)
	if !ok || strings.TrimSpace(replacement.To) == "" {
		return "", false
	}
	path := replacement.To
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(lockObj.Path()), path)
	}
	return path, true
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

func (h *DependencyHandler) shouldUnpackModules() bool {
	if h.lockPath == "" {
		return false
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return false
	}
	return lockObj.ShouldUnpackModules()
}

func (h *DependencyHandler) moduleUsesDirectoryMode(moduleName string) bool {
	if _, ok := h.replacementPath(moduleName); ok {
		return true
	}
	return h.shouldUnpackModules()
}

func (h *DependencyHandler) operationModules(
	ctx context.Context,
	op regapi.Operation,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) (map[string]struct{}, error) {
	modules := make(map[string]struct{})
	entry, ok := resolveOperationEntry(op, snapshot)
	if !ok || !isRootDependency(entry) {
		return modules, nil
	}
	def, err := decodeDependency(ctx, transcoder, entry)
	if err != nil {
		return nil, err
	}
	if def.Component != "" {
		modules[def.Component] = struct{}{}
	}
	return modules, nil
}

func VerifyDownloadedArtifact(path, expectedDigest string, expectedSize uint64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if expectedSize > 0 && uint64(info.Size()) != expectedSize {
		return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", expectedSize, info.Size())
	}
	if expectedDigest == "" {
		return nil
	}

	alg, wantDigest, err := parseExpectedDigest(expectedDigest)
	if err != nil {
		return err
	}
	if alg != "sha256" {
		return fmt.Errorf("unsupported digest algorithm %q", alg)
	}

	gotDigest, err := sha256FileHex(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(gotDigest, wantDigest) {
		return fmt.Errorf("digest mismatch: expected %s, got sha256:%s", expectedDigest, gotDigest)
	}
	return nil
}

func verifyDownloadedArtifact(path, expectedDigest string, expectedSize uint64) error {
	return VerifyDownloadedArtifact(path, expectedDigest, expectedSize)
}

type extractedModuleMetadata struct {
	Digest string `yaml:"digest,omitempty"`
	Size   uint64 `yaml:"size,omitempty"`
}

func writeExtractedModuleMeta(dirPath, digest string, size uint64) error {
	if digest == "" && size == 0 {
		return nil
	}
	data, err := yaml.Marshal(extractedModuleMetadata{Digest: digest, Size: size})
	if err != nil {
		return fmt.Errorf("marshal extracted module metadata: %w", err)
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
		return fmt.Errorf("read extracted module metadata: %w", err)
	}
	if expectedDigest != "" && !strings.EqualFold(meta.Digest, expectedDigest) {
		return fmt.Errorf("digest mismatch: expected %s, got %s", expectedDigest, meta.Digest)
	}
	if expectedSize > 0 && meta.Size != expectedSize {
		return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", expectedSize, meta.Size)
	}
	return nil
}

func parseExpectedDigest(raw string) (algorithm string, value string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("empty digest")
	}
	if !strings.Contains(trimmed, ":") {
		return "sha256", trimmed, nil
	}

	parts := strings.SplitN(trimmed, ":", 2)
	algorithm = strings.ToLower(strings.TrimSpace(parts[0]))
	value = strings.TrimSpace(parts[1])
	if algorithm == "" || value == "" {
		return "", "", fmt.Errorf("invalid digest format %q", raw)
	}
	return algorithm, value, nil
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

func (h *DependencyHandler) installedVersion(moduleName string) (string, bool) {
	if h.lockPath == "" {
		return "", false
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return "", false
	}
	mod, ok := lockObj.GetModule(moduleName)
	if !ok {
		return "", false
	}
	return mod.Version, true
}

func (h *DependencyHandler) lockedModuleDigests() map[string]string {
	if h.lockPath == "" {
		return nil
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return nil
	}
	modules := lockObj.GetModules()
	if len(modules) == 0 {
		return nil
	}
	digests := make(map[string]string, len(modules))
	for _, mod := range modules {
		if mod.Hash == "" || mod.Name == "" || mod.Version == "" {
			continue
		}
		// Key by name@version so the integrity check only fires when resolving
		// the exact version the lock pins. A version-agnostic key would compare
		// a new version's digest against the locked old version's and wrongly
		// block updates.
		digests[mod.Name+"@"+mod.Version] = mod.Hash
	}
	if len(digests) == 0 {
		return nil
	}
	return digests
}

func loadRawEntriesFromPaths(
	ctx context.Context,
	paths []string,
	logger *zap.Logger,
	transcoder payload.Transcoder,
) ([]regapi.Entry, error) {
	if transcoder == nil {
		return nil, ErrDependencyTranscoderMissing
	}

	ldr := loaderFromContext(ctx, logger, transcoder)

	var entries []regapi.Entry
	for _, path := range paths {
		var loaded []regapi.Entry
		if filepath.Ext(path) == ".wapp" {
			var err error
			loaded, err = loadEntriesFromWapp(path)
			if err != nil {
				return nil, NewDependencyLoadError(path, err)
			}
		} else {
			stat, err := os.Stat(path)
			if os.IsNotExist(err) {
				logger.Warn("path not found, skipping", zap.String("path", path))
				continue
			}
			if err != nil {
				return nil, NewDependencyLoadError(path, err)
			}
			if stat.IsDir() {
				dirFS := os.DirFS(path)
				loaded, err = ldr.LoadFS(ctx, dirFS)
				if err != nil {
					return nil, NewDependencyLoadError(path, err)
				}
			} else {
				logger.Warn("unknown path type, skipping", zap.String("path", path))
				continue
			}
		}
		entries = append(entries, loaded...)
	}
	return entries, nil
}

func loaderFromContext(ctx context.Context, logger *zap.Logger, transcoder payload.Transcoder) boot.Loader {
	if ldr := boot.GetLoader(ctx); ldr != nil {
		return ldr
	}

	interpolator := interpolate.NewEntryInterpolator(transcoder,
		interpolate.WithInterpolator(interpolate.LoadFile),
	)
	return loader.NewLoader(transcoder, logger.Named("loader"), interpolator)
}

func loadEntriesFromWapp(path string) ([]regapi.Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return nil, err
	}

	wappEntries, err := reader.GetEntries()
	if err != nil {
		return nil, err
	}

	entries := make([]regapi.Entry, len(wappEntries))
	for i, we := range wappEntries {
		entries[i] = regapi.Entry{
			ID:   regapi.NewID(we.ID.Namespace, we.ID.Name),
			Kind: we.Kind,
			Meta: attrs.NewBagFrom(we.Meta),
			Data: payload.New(unwrapPayloadData(we.Data)),
		}
	}
	return entries, nil
}

func unwrapPayloadData(data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	innerData, hasData := m["Data"]
	_, hasFormat := m["Format"]
	if hasData && hasFormat && len(m) == 2 {
		return innerData
	}
	return data
}

func (h *DependencyHandler) buildOperations(
	current regapi.State,
	desired []regapi.Entry,
	originalID regapi.ID,
	controlledModules map[string]struct{},
	mutableModules map[string]struct{},
) ([]regapi.Operation, error) {
	var resolver regapi.DependencyResolver
	if h != nil {
		resolver = h.resolver
	}
	return buildOperationsWithResolver(current, desired, originalID, controlledModules, mutableModules, resolver)
}

func buildOperations(
	current regapi.State,
	desired []regapi.Entry,
	originalID regapi.ID,
	controlledModules map[string]struct{},
	mutableModules map[string]struct{},
) ([]regapi.Operation, error) {
	return buildOperationsWithResolver(current, desired, originalID, controlledModules, mutableModules, nil)
}

func buildOperationsWithResolver(
	current regapi.State,
	desired []regapi.Entry,
	originalID regapi.ID,
	controlledModules map[string]struct{},
	mutableModules map[string]struct{},
	resolver regapi.DependencyResolver,
) ([]regapi.Operation, error) {
	currentByID := make(map[string]regapi.Entry, len(current))
	for _, entry := range current {
		currentByID[idKey(entry.ID)] = entry
	}

	desiredByID := make(map[string]regapi.Entry, len(desired))
	for _, entry := range desired {
		desiredByID[idKey(entry.ID)] = entry
	}

	ops := make([]regapi.Operation, 0)
	originalKey := idKey(originalID)

	for key, entry := range desiredByID {
		if key == originalKey {
			continue
		}
		if existing, ok := currentByID[key]; ok {
			if entryConflict(existing, entry) {
				return nil, NewDependencyEntryConflictError(entry.ID.String(), entryModule(existing), entryModule(entry))
			}
			if !entriesEqual(existing, entry) {
				if existing.Kind != entry.Kind {
					ops = append(ops,
						regapi.Operation{Kind: regapi.EntryDelete, Entry: existing},
						regapi.Operation{Kind: regapi.EntryCreate, Entry: entry},
					)
					continue
				}
				if sameImmutableModuleVersion(existing, entry, mutableModules) {
					continue
				}
				ops = append(ops, regapi.Operation{Kind: regapi.EntryUpdate, Entry: entry})
			}
		} else {
			ops = append(ops, regapi.Operation{Kind: regapi.EntryCreate, Entry: entry})
		}
	}

	for key, entry := range currentByID {
		if key == originalKey {
			continue
		}
		if _, ok := desiredByID[key]; ok {
			continue
		}
		if module := entryModule(entry); module != "" {
			if controlledModules != nil {
				if _, ok := controlledModules[module]; !ok {
					continue
				}
			}
			if hasLiveDependent(entry.ID, currentByID, desiredByID, controlledModules, resolver) {
				continue
			}
			ops = append(ops, regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: entry.ID}})
		}
	}

	return ops, nil
}

func hasLiveDependent(
	target regapi.ID,
	currentByID map[string]regapi.Entry,
	desiredByID map[string]regapi.Entry,
	controlledModules map[string]struct{},
	resolver regapi.DependencyResolver,
) bool {
	if resolver == nil {
		return false
	}
	universe := dependencyEntryUniverse(currentByID)
	targetKey := idKey(target)
	for key, current := range currentByID {
		if key == targetKey {
			continue
		}
		check := current
		if desired, ok := desiredByID[key]; ok {
			check = desired
		} else if missingDesiredEntryWillBeDeleted(current, controlledModules) {
			continue
		}
		if entryDependsOn(check, target, universe, resolver) {
			return true
		}
	}
	return false
}

func missingDesiredEntryWillBeDeleted(entry regapi.Entry, controlledModules map[string]struct{}) bool {
	module := entryModule(entry)
	if module == "" {
		return false
	}
	if controlledModules == nil {
		return true
	}
	_, ok := controlledModules[module]
	return ok
}

type dependencyEntryUniverseView struct {
	entries map[string]regapi.ID
	groups  map[string][]regapi.ID
	ns      map[string][]regapi.ID
}

func dependencyEntryUniverse(entries map[string]regapi.Entry) dependencyEntryUniverseView {
	universe := dependencyEntryUniverseView{
		entries: make(map[string]regapi.ID, len(entries)),
		groups:  make(map[string][]regapi.ID),
		ns:      make(map[string][]regapi.ID),
	}
	for key, entry := range entries {
		universe.entries[key] = entry.ID
		for _, group := range entry.Meta.GetSlice(regapi.TagGroups) {
			universe.groups[group] = append(universe.groups[group], entry.ID)
		}
		if entry.ID.NS != "" {
			universe.ns[entry.ID.NS] = append(universe.ns[entry.ID.NS], entry.ID)
		}
	}
	return universe
}

func entryDependsOn(entry regapi.Entry, target regapi.ID, universe dependencyEntryUniverseView, resolver regapi.DependencyResolver) bool {
	dependencies := entry.Meta.GetSlice(regapi.TagDependsOn)
	dependencies = append(dependencies, resolver.Extract(entry)...)
	for _, dep := range dependencies {
		switch depType, value := parseDependencyRef(dep); depType {
		case "direct":
			if resolveDependencyRef(entry.ID.NS, value) == target {
				return true
			}
		case "group":
			for _, id := range universe.groups[value] {
				if id == target {
					return true
				}
			}
		case "namespace":
			for _, id := range universe.ns[value] {
				if id == target {
					return true
				}
			}
		}
	}
	return false
}

func parseDependencyRef(dep string) (depType string, value string) {
	if strings.HasPrefix(dep, "group:") {
		return "group", strings.TrimPrefix(dep, "group:")
	}
	if strings.HasPrefix(dep, "ns:") {
		return "namespace", strings.TrimPrefix(dep, "ns:")
	}
	return "direct", dep
}

func resolveDependencyRef(sourceNS string, dep string) regapi.ID {
	if strings.Contains(dep, ":") {
		return regapi.ParseID(dep)
	}
	return regapi.NewID(sourceNS, dep)
}

func sameImmutableModuleVersion(existing, desired regapi.Entry, mutableModules map[string]struct{}) bool {
	if mutableModules == nil {
		return false
	}
	module := entryModule(desired)
	if module == "" || module != entryModule(existing) {
		return false
	}
	if _, mutable := mutableModules[module]; mutable {
		return false
	}
	existingVersion := moduleVersion(existing)
	desiredVersion := moduleVersion(desired)
	if existingVersion != "" && desiredVersion != "" && existingVersion != desiredVersion {
		return false
	}
	return true
}

func entryConflict(existing, desired regapi.Entry) bool {
	desiredModule := entryModule(desired)
	if desiredModule == "" {
		return false
	}
	existingModule := entryModule(existing)
	return existingModule == "" || existingModule != desiredModule
}

func entryModule(entry regapi.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[metaModuleKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func moduleVersion(entry regapi.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[metaModuleVersionKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func isRootDependency(entry regapi.Entry) bool {
	return entry.Kind == regapi.NamespaceDependency && entryModule(entry) == ""
}

func markModuleMeta(entry regapi.Entry, moduleName, moduleVersion string) regapi.Entry {
	meta := entry.Meta
	if meta == nil {
		meta = attrs.NewBag()
	} else {
		meta = attrs.NewBagFrom(meta)
	}
	existingModule := strings.TrimSpace(meta.GetString(metaModuleKey, ""))
	if existingModule == "" {
		meta.Set(metaModuleKey, moduleName)
		if moduleVersion != "" {
			meta.Set(metaModuleVersionKey, moduleVersion)
		}
	} else if existingModule == moduleName && moduleVersion != "" && meta.GetString(metaModuleVersionKey, "") == "" {
		meta.Set(metaModuleVersionKey, moduleVersion)
	}
	entry.Meta = meta
	return entry
}

func markModuleMetaForGraph(
	entry regapi.Entry,
	moduleName string,
	moduleVersion string,
	namespaceOwners map[string]moduleOwner,
	entryOwners map[string]moduleOwner,
) regapi.Entry {
	if entryModule(entry) != "" {
		return markModuleMeta(entry, moduleName, moduleVersion)
	}
	if owner, ok := entryOwners[idKey(entry.ID)]; ok && owner.name != "" {
		if owner.name == moduleName {
			return markModuleMeta(entry, moduleName, moduleVersion)
		}
		return markModuleMeta(entry, owner.name, owner.version)
	}
	if owner, ok := namespaceOwners[entry.ID.NS]; ok && owner.name != "" {
		return markModuleMeta(entry, owner.name, owner.version)
	}
	return markModuleMeta(entry, moduleName, moduleVersion)
}

func idKey(id regapi.ID) string {
	if strings.TrimSpace(id.NS) == "" {
		name := strings.TrimSpace(id.Name)
		if strings.Contains(name, ":") {
			parsed := regapi.ParseID(name)
			if parsed.NS != "" || parsed.Name != "" {
				return strings.TrimSpace(parsed.NS) + ":" + strings.TrimSpace(parsed.Name)
			}
		}
	}
	if s := strings.TrimSpace(id.String()); s != "" {
		if strings.HasPrefix(s, ":") && strings.Contains(strings.TrimPrefix(s, ":"), ":") {
			s = strings.TrimPrefix(s, ":")
		}
		parsed := regapi.ParseID(s)
		if parsed.NS != "" || parsed.Name != "" {
			return strings.TrimSpace(parsed.NS) + ":" + strings.TrimSpace(parsed.Name)
		}
	}
	if id.NS != "" || id.Name != "" {
		return strings.TrimSpace(id.NS) + ":" + strings.TrimSpace(id.Name)
	}
	return strings.TrimSpace(id.String())
}

func idsEqual(a, b regapi.ID) bool {
	if idKey(a) == idKey(b) {
		return true
	}
	return strings.TrimSpace(a.String()) == strings.TrimSpace(b.String())
}

func entriesEqual(a, b regapi.Entry) bool {
	if !idsEqual(a.ID, b.ID) || a.Kind != b.Kind {
		return false
	}
	if !reflect.DeepEqual(a.Meta, b.Meta) {
		return false
	}
	switch {
	case a.Data == nil && b.Data == nil:
		return true
	case a.Data == nil || b.Data == nil:
		return false
	}
	if a.Data.Format() != b.Data.Format() {
		return false
	}
	return reflect.DeepEqual(a.Data.Data(), b.Data.Data())
}

func resolveOperationEntry(op regapi.Operation, snapshot regapi.State) (regapi.Entry, bool) {
	if op.Entry.Kind != "" && op.Entry.Data != nil {
		return op.Entry, true
	}
	for _, entry := range snapshot {
		if idsEqual(entry.ID, op.Entry.ID) {
			return entry, true
		}
	}
	return regapi.Entry{}, false
}

func decodeDependency(ctx context.Context, transcoder payload.Transcoder, entry regapi.Entry) (DependencyDefinition, error) {
	def, err := entrypkg.DecodeEntryConfigRaw[DependencyDefinition](ctx, transcoder, entry)
	if err != nil {
		return DependencyDefinition{}, NewDependencyEntryDecodeError(entry.ID.String(), err)
	}
	return *def, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func modKey(mod ResolvedModule) string {
	return mod.Org + "/" + mod.Name + "@" + mod.Version
}

func formatResolutionErrors(errs []ResolutionError) string {
	if len(errs) == 0 {
		return ""
	}
	msg := errs[0].String()
	for i := 1; i < len(errs); i++ {
		msg += "; " + errs[i].String()
	}
	return msg
}

func newHubClientFromAuth() (*Client, error) {
	projectDir, _ := os.Getwd()
	authCfg := auth.NewConfig(projectDir)
	store := auth.NewStore(authCfg)

	registryURL := store.DefaultRegistry()
	cred, _ := store.Get(registryURL)

	var token string
	if cred != nil {
		token = cred.Token
	}

	client, err := NewClient(Options{
		BaseURL: registryURL,
		Token:   token,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

var (
	ErrDependencyHandlerNotConfigured = apierror.New(apierror.Internal, "dependency handler not configured").WithRetryable(apierror.False)
	ErrDependencyTranscoderMissing    = apierror.New(apierror.Internal, "payload transcoder not available").WithRetryable(apierror.False)
	ErrDependencyNoContent            = apierror.New(apierror.NotFound, "no download URL available").WithRetryable(apierror.False)
)

const registryAuthHint = "registry authentication required: start the process with WIPPY_TOKEN set, push a token at runtime via hub.auth.authenticate, or run `wippy auth login`"

func NewDependencyEntryInvalidError(entryID, detail, component string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id":  entryID,
			"detail":    detail,
			"component": component,
		}))
}

func NewDependencyEntryDecodeError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "decode dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID})).
		WithCause(cause)
}

func NewDependencyEntryMissingError(entryID string) apierror.Error {
	return apierror.New(apierror.NotFound, "dependency entry not found").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID}))
}

func NewDependencyResolutionError(cause error) apierror.Error {
	err := apierror.New(apierror.Unavailable, "dependency resolution failed").
		WithRetryable(apierror.False).
		WithCause(cause)
	if cause != nil {
		err = err.WithDetails(attrs.NewBagFrom(map[string]any{"reason": cause.Error()}))
	}
	return err
}

func NewDependencyResolutionErrors(errs []ResolutionError) apierror.Error {
	details := make([]map[string]any, 0, len(errs))
	unauthenticated := false
	for _, e := range errs {
		details = append(details, map[string]any{
			"module":     e.Org + "/" + e.Name,
			"constraint": e.Constraint,
			"message":    e.Message,
		})
		if errors.Is(e.Err, ErrNotAuthenticated) {
			unauthenticated = true
		}
	}

	summary := formatResolutionErrors(errs)
	bag := map[string]any{
		"count":   len(errs),
		"summary": summary,
		"errors":  details,
	}
	if unauthenticated {
		bag["hint"] = registryAuthHint
	}

	message := "dependency resolution failed"
	if summary != "" {
		message += ": " + summary
	}

	return apierror.New(apierror.Conflict, message).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(bag))
}

func NewDependencyDownloadError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "module download failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"module": module})).
		WithCause(cause)
}

func NewDependencyLoadError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "load module entries failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path})).
		WithCause(cause)
}

func NewDependencyIntegrityError(module string, cause error, expectedDigest string, expectedSize uint64) apierror.Error {
	details := map[string]any{"module": module}
	if expectedDigest != "" {
		details["expected_digest"] = expectedDigest
	}
	if expectedSize > 0 {
		details["expected_size"] = expectedSize
	}

	return apierror.New(apierror.Invalid, "downloaded module artifact failed integrity verification").
		WithDetails(attrs.NewBagFrom(details)).
		WithCause(cause).
		WithRetryable(apierror.False)
}

func NewDependencyPipelineError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "dependency pipeline failed").WithCause(cause)
}

func NewDependencyEntryConflictError(entryID, existingModule, desiredModule string) apierror.Error {
	msg := fmt.Sprintf("entry %q conflicts: owned by %q, wanted by %q", entryID, existingModule, desiredModule)
	return apierror.New(apierror.Conflict, msg).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id":        entryID,
			"existing_module": existingModule,
			"desired_module":  desiredModule,
		})).
		WithRetryable(apierror.False)
}

func NewDependencyRootConflictError(component, existingEntryID, requestedEntryID string) apierror.Error {
	msg := fmt.Sprintf("dependency component %q is already installed as %q; update that dependency instead of creating %q", component, existingEntryID, requestedEntryID)
	return apierror.New(apierror.Conflict, msg).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"component":          component,
			"existing_entry_id":  existingEntryID,
			"requested_entry_id": requestedEntryID,
		})).
		WithRetryable(apierror.False)
}
