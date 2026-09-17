// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/lock"
	entrypkg "github.com/wippyai/runtime/system/entry"
	"go.uber.org/zap"
)

const (
	moduleSourceHub               = "hub"
	moduleSourceReplacementTreeV1 = "replacement-tree-v1"
	extractedModuleMeta           = ".wippy-module.yaml"
)

type DependencyHandlerOptions struct {
	Hub                   HubClient
	Resolver              regapi.DependencyResolver
	Artifacts             *artifact.Registry
	Logger                *zap.Logger
	LockPath              string
	VendorDir             string
	ArtifactRoot          string
	WorkspaceReplacements []lock.Replacement
	ResolveTimeout        time.Duration
	DownloadTimeout       time.Duration
}
type DependencyHandler struct {
	hub             HubClient
	resolver        regapi.DependencyResolver
	manifestCache   *ManifestCache
	logger          *zap.Logger
	lock            *lock.Lock
	artifacts       *artifact.Registry
	deployment      *regapi.Deployment
	artifactRoot    string
	replacements    map[string]lock.Replacement
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

	var lockObj *lock.Lock
	if lockPath != "" {
		var err error
		lockObj, err = lock.New(lockPath, lock.WithWorkspaceReplacements(opts.WorkspaceReplacements))
		if err != nil {
			return nil, err
		}
	}

	vendorDir := opts.VendorDir
	if vendorDir == "" && lockObj != nil {
		lockDir := filepath.Dir(lockObj.Path())
		vendorDir = filepath.Join(lockDir, lockObj.GetVendorPath())
	}
	if vendorDir == "" {
		vendorDir = filepath.Join(".wippy", "vendor")
	}
	artifactRoot := opts.ArtifactRoot
	if artifactRoot == "" {
		artifactRoot = filepath.Dir(vendorDir)
	}

	replacements := make(map[string]lock.Replacement)
	if lockObj != nil {
		for _, replacement := range lockObj.GetReplacements() {
			replacements[replacement.From] = replacement
		}
	}

	return &DependencyHandler{
		hub:             client,
		manifestCache:   NewManifestCache(client),
		logger:          logger,
		resolver:        opts.Resolver,
		artifacts:       opts.Artifacts,
		artifactRoot:    artifactRoot,
		vendorDir:       vendorDir,
		resolveTimeout:  opts.ResolveTimeout,
		downloadTimeout: opts.DownloadTimeout,
		lock:            lockObj,
		replacements:    replacements,
	}, nil
}

// PrepareRestore loads the deployment baseline and prefetches immutable artifacts
// recorded for the current registry version. Historical local replacements are
// validated during reconciliation against the final declarations. Version
// selection remains the stored resolution. The caller's dependency access
// policy determines whether a missing artifact may be downloaded.
func (h *DependencyHandler) PrepareRestore(ctx context.Context, history regapi.History) error {
	if h == nil {
		return ErrDependencyHandlerNotConfigured
	}
	resolutions, ok := history.(regapi.ResolutionHistory)
	if !ok {
		// A history that records no resolutions has no recorded artifacts to
		// materialize, so startup proceeds on the baseline alone.
		if h.logger != nil {
			h.logger.Debug("history records no dependency resolutions, skipping restore")
		}
		return nil
	}
	configured, err := h.deploymentFromLock()
	if err != nil {
		return err
	}
	head, err := history.Head()
	if err != nil {
		var categorized apierror.Error
		if errors.As(err, &categorized) && categorized.Kind() == apierror.NotFound {
			h.deployment = configured
			return nil
		}
		return NewRestoreReadError("read registry head", err)
	}
	resolution, err := resolutions.GetDependencyResolution(head)
	if errors.Is(err, regapi.ErrDependencyResolutionNotFound) {
		h.deployment = configured
		return nil
	}
	if err != nil {
		return NewRestoreReadError("read dependency resolution", err)
	}
	deployment := configured
	if deployment == nil {
		deployment = resolution.Deployment.Canonical()
	}
	effective, err := resolvedModulesFromStored(resolution)
	if err != nil {
		return err
	}
	if deployment == nil {
		// A source checkout has an unrooted lock: its ordinary lock loader has
		// already published the deployment sources. History may add modules, but
		// it must not turn that development shape into a persisted Hub root.
		if h.lock != nil {
			h.deployment = nil
			return h.prepareRecordedArtifacts(ctx, effective)
		}
		return NewDeploymentBaselineError("persisted deployment baseline is unavailable", "start once with its lock file")
	}
	h.deployment = deployment
	baseline, err := resolvedModulesFromRecords(deployment.Modules)
	if err != nil {
		return err
	}
	if err := h.refreshReplacementModuleIdentities(baseline); err != nil {
		return err
	}
	if err := h.prepareRestoreSources(ctx, baseline); err != nil {
		return err
	}
	return h.prepareRecordedArtifacts(ctx, effective)
}
func (h *DependencyHandler) deploymentFromLock() (*regapi.Deployment, error) {
	if h == nil || h.lock == nil {
		return nil, nil
	}
	roots := h.lock.GetRootModules()
	if len(roots) != 1 {
		return nil, nil
	}
	if len(h.replacements) != 0 {
		return nil, nil
	}
	deployment := &regapi.Deployment{Root: roots[0], Modules: make([]regapi.ResolvedModule, 0, len(h.lock.GetModules()))}
	for _, module := range h.lock.GetModules() {
		algorithm, digest, err := parseExpectedDigest(module.Hash)
		if err != nil || algorithm != "sha256" || len(digest) != 64 {
			return nil, NewModuleIdentityError("deployment module has invalid artifact digest", module.Name, nil)
		}
		deployment.Modules = append(deployment.Modules, regapi.ResolvedModule{
			Name: module.Name, Version: module.Version, VersionID: module.Version,
			Source: moduleSourceHub, Digest: "sha256:" + strings.ToLower(digest),
		})
	}
	return deployment.Canonical(), nil
}
func (h *DependencyHandler) isDeploymentRoot(module string) bool {
	if h.deployment != nil {
		return module == h.deployment.Root
	}
	return h.lock != nil && h.lock.IsRootModule(module)
}
func (h *DependencyHandler) Expand(ctx context.Context, op regapi.Operation, snapshot regapi.State) (regapi.DirectiveResult, error) {
	return h.expand(ctx, op, snapshot, nil, nil, nil)
}
func (h *DependencyHandler) expand(
	ctx context.Context,
	op regapi.Operation,
	snapshot regapi.State,
	extraControlled map[string]struct{},
	extraMutable map[string]struct{},
	freshRoots map[string]struct{},
) (regapi.DirectiveResult, error) {
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

	lockedVersions, err := h.installedModuleVersions(ctx, transcoder, snapshot)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	controlledModules, err := h.collectControlledModules(ctx, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	for module := range extraControlled {
		controlledModules[module] = struct{}{}
	}

	rootDeps, refDeps, err := h.collectDesiredDependencies(ctx, op, snapshot, transcoder, freshRoots)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	desiredDeps := append(append([]desiredDependency(nil), rootDeps...), refDeps...)

	// A reference introduced by this changeset for a component that is not
	// installed yet is a planning error, not a fold: the second install attempt
	// keeps the established "update that dependency instead" answer. Every
	// fresh declaration is gated, not only the driving operation's.
	fresh := make(map[string]struct{}, len(freshRoots)+1)
	if op.Kind == regapi.EntryCreate {
		fresh[idKey(op.Entry.ID)] = struct{}{}
	}
	for key := range freshRoots {
		fresh[key] = struct{}{}
	}
	if len(fresh) > 0 && len(refDeps) > 0 {
		for _, ref := range refDeps {
			if _, isFresh := fresh[idKey(ref.entry.ID)]; !isFresh {
				continue
			}
			if lockedVersions[ref.definition.Component] != "" {
				continue
			}
			controllerID := ref.entry.ID
			for _, root := range rootDeps {
				if root.definition.Component == ref.definition.Component {
					controllerID = root.entry.ID
					break
				}
			}
			return regapi.DirectiveResult{}, NewDependencyRootConflictError(
				ref.definition.Component, controllerID.String(), ref.entry.ID.String(),
			)
		}
	}

	desiredDepEntries := make([]regapi.Entry, 0, len(desiredDeps))
	for _, dep := range desiredDeps {
		desiredDepEntries = append(desiredDepEntries, dep.entry)
	}

	desiredRoots := dependencyDefinitions(desiredDeps)
	resolved, err := h.resolveEffectiveModules(ctx, desiredRoots, lockedVersions, h.currentResolution(ctx))
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	for _, ref := range refDeps {
		selected, ok := selectedModuleVersion(resolved, ref.definition.Component)
		if !ok || !storedVersionSatisfies(selected, ref.definition.Version) {
			return regapi.DirectiveResult{}, NewStoredResolutionError("folded dependency reference is not satisfied by the selection", map[string]any{
				"entry_id":  ref.entry.ID.String(),
				"component": ref.definition.Component,
				"required":  ref.definition.Version,
				"selected":  selected,
			})
		}
	}

	opComponent := ""
	for _, dep := range desiredDeps {
		if idsEqual(dep.entry.ID, op.Entry.ID) {
			opComponent = dep.definition.Component
			break
		}
	}
	_, installedDigests := h.currentModuleIdentities(ctx)
	strictModules := touchedModuleIdentities(
		resolved,
		lockedVersions,
		installedDigests,
		opComponent,
	)
	strictSet := stringSet(strictModules)
	resolvedSet := resolvedModuleSet(resolved)
	for module := range extraMutable {
		if _, desired := resolvedSet[module]; desired {
			strictSet[module] = struct{}{}
		}
	}
	strictModules = make([]string, 0, len(strictSet))
	for module := range strictSet {
		strictModules = append(strictModules, module)
	}
	sort.Strings(strictModules)
	mutableModules, err := h.operationModules(ctx, op, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	for module := range extraMutable {
		mutableModules[module] = struct{}{}
	}
	touchedModules := stringSet(strictModules)
	for module := range mutableModules {
		touchedModules[module] = struct{}{}
	}
	desiredModules := resolvedModuleSet(resolved)
	addModuleSet(controlledModules, desiredModules)

	moduleEntries, unpackPlan, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, touchedModules), snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	defer func() { _ = unpackPlan.cleanup() }()
	linkDeps := mergeLinkDependencies(desiredDepEntries, moduleEntries)

	combined := make([]regapi.Entry, 0, len(snapshot)+len(moduleEntries))
	for _, e := range snapshot {
		if module := entryModule(e); module != "" {
			if _, controlled := controlledModules[module]; controlled {
				if _, desired := desiredModules[module]; !desired {
					continue
				}
				if _, touched := touchedModules[module]; touched {
					continue
				}
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

	additional, err := (operationPlanner{resolver: h.resolver}).plan(snapshot, combined, operationPlanOptions{
		originalKey:       idKey(op.Entry.ID),
		controlledModules: controlledModules,
		mutableModules:    mutableModules,
	})
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
	artifactEffect, err := h.buildArtifactEffect(ctx, resolved, combined)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	packEffect, err := h.buildEmbedPackEffect(ctx, resolved, controlledModules)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	filesystemEffect, err := h.buildModuleFilesystemEffect(resolved, controlledModules, unpackPlan)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	if artifactEffect != nil {
		effects = append(effects, artifactEffect)
	}
	if filesystemEffect != nil {
		effects = append(effects, filesystemEffect)
	}
	if packEffect != nil {
		effects = append(effects, packEffect)
	}

	// The graph describes the state this operation produces; its baseline
	// binding must be computed over that state, never over the one being left,
	// or a later version transition sees a digest that names the wrong side.
	selectedResolution, err := h.resolutionForSnapshot(ctx, applyOperationToState(snapshot, op), rootDeps, refDeps, resolved, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	return regapi.DirectiveResult{
		Applied:    true,
		Resolution: selectedResolution,
		Additional: scoped,
		Effects:    effects,
	}, nil
}

// ExpandChanges resolves the final declarative root set for one registry
// transaction. Earlier operations are folded into a temporary snapshot and
// the last operation drives the existing expansion path, so agents retain the
// same simple entry-update surface for both single and multi-root updates.
func (h *DependencyHandler) ExpandChanges(ctx context.Context, changes regapi.ChangeSet, snapshot regapi.State) (regapi.DirectiveResult, error) {
	if len(changes) == 0 {
		return regapi.DirectiveResult{}, nil
	}
	if h == nil || h.hub == nil {
		return regapi.DirectiveResult{}, ErrDependencyHandlerNotConfigured
	}
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return regapi.DirectiveResult{}, ErrDependencyTranscoderMissing
	}
	// Planner batches every ns.dependency operation, including dependencies
	// owned by module manifests. Only authored roots drive whole-graph solving.
	// Filter against a rolling state so a delete is recognized from its old
	// entry and a create/update from its new entry.
	rollingMap := make(regapi.StateMap, len(snapshot)+len(changes))
	for _, entry := range snapshot {
		rollingMap[entry.ID] = entry
	}
	rootChanges := make(regapi.ChangeSet, 0, len(changes))
	for _, op := range changes {
		old, hadOld := rollingMap[op.Entry.ID]
		rolling := make(regapi.State, 0, len(rollingMap))
		for _, entry := range rollingMap {
			rolling = append(rolling, entry)
		}
		resolved, hasResolved := resolveOperationEntry(op, rolling)
		if (hadOld && isRootDependency(old)) || (hasResolved && isRootDependency(resolved)) {
			rootChanges = append(rootChanges, op)
		}
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			rollingMap[op.Entry.ID] = op.Entry
		case regapi.EntryDelete:
			delete(rollingMap, op.Entry.ID)
		}
	}
	if len(rootChanges) == 0 {
		return regapi.DirectiveResult{}, nil
	}
	originalIDs := make(map[string]struct{}, len(snapshot))
	for _, entry := range snapshot {
		originalIDs[idKey(entry.ID)] = struct{}{}
	}
	freshRoots := make(map[string]struct{}, len(rootChanges))
	for _, op := range rootChanges {
		if op.Kind != regapi.EntryCreate {
			continue
		}
		key := idKey(op.Entry.ID)
		if _, existed := originalIDs[key]; !existed {
			freshRoots[key] = struct{}{}
		}
	}
	if len(rootChanges) == 1 {
		driver, ok := rootExpansionDriver(rootChanges[0], snapshot)
		if !ok {
			return regapi.DirectiveResult{}, nil
		}
		return h.expand(ctx, driver, snapshot, nil, nil, freshRoots)
	}
	changes = rootChanges
	// Preserve ownership from both sides of the batch. Looking only at the
	// folded state loses modules owned by an earlier delete or retarget, leaving
	// their entries and embedded packs live after the root has gone away.
	extraControlled, err := h.collectControlledModules(ctx, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	extraMutable := make(map[string]struct{})
	stateMap := make(regapi.StateMap, len(snapshot)+len(changes)-1)
	for _, entry := range snapshot {
		stateMap[entry.ID] = entry
	}
	for _, op := range changes[:len(changes)-1] {
		rolling := make(regapi.State, 0, len(stateMap))
		for _, entry := range stateMap {
			rolling = append(rolling, entry)
		}
		affected, opErr := h.operationModules(ctx, op, rolling, transcoder)
		if opErr != nil {
			return regapi.DirectiveResult{}, opErr
		}
		for module := range affected {
			extraMutable[module] = struct{}{}
		}
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			stateMap[op.Entry.ID] = op.Entry
		case regapi.EntryDelete:
			delete(stateMap, op.Entry.ID)
		}
	}
	working := make(regapi.State, 0, len(stateMap))
	for _, entry := range stateMap {
		working = append(working, entry)
	}
	// A root retagged as module-owned is declaratively a root deletion. Drive
	// expansion with that deletion so cleanup cannot be skipped merely because
	// the final form is still an ns.dependency entry.
	driver, ok := rootExpansionDriver(changes[len(changes)-1], working)
	if !ok {
		return regapi.DirectiveResult{}, nil
	}
	return h.expand(ctx, driver, working, extraControlled, extraMutable, freshRoots)
}

// ReconcileResolution materializes a previously selected graph. An unchanged
// deployment replays it without resolving again. A changed deployment resolves
// the final combined declarations once and binds the repaired graph to the new
// baseline. History is reduced before either path, so this remains a whole-graph
// operation rather than one expansion per historical version.
func (h *DependencyHandler) ReconcileResolution(
	ctx context.Context,
	current regapi.State,
	target regapi.State,
	resolution *regapi.DependencyResolution,
) (regapi.DirectiveResult, error) {
	if h == nil || h.hub == nil {
		return regapi.DirectiveResult{}, ErrDependencyHandlerNotConfigured
	}
	if resolution == nil || !resolution.Valid() {
		return regapi.DirectiveResult{}, NewStoredResolutionError("", nil)
	}
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return regapi.DirectiveResult{}, ErrDependencyTranscoderMissing
	}

	snapshotDeps, err := h.collectSnapshotDependencies(ctx, target, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	// Lenient fold: committed state must always reconcile — replay is anchored
	// by the stored root/reference partition, and parameter drift is handled by
	// the parameter reconciliation sweep, never by a fold conflict here.
	rootDeps, refDeps, err := foldRootDependencyComponents(snapshotDeps, nil, false)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	desiredDeps := append(append([]desiredDependency(nil), rootDeps...), refDeps...)

	// Deployment identity is evaluated on the state the stored graph
	// describes. The current state names the version being left; comparing
	// against it makes every transition across a baseline-owned declaration
	// change look like a deployment change and rebind graphs in both
	// directions.
	baselineDigest, err := h.deploymentBaselineDigest(ctx, target, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	refreshReason, err := h.resolutionRefreshReason(ctx, current, rootDeps, refDeps, resolution, baselineDigest, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	var resolved []ResolvedModule
	effectiveResolution := resolution.Canonical()
	if refreshReason != "" {
		logger := h.logger
		if logger == nil {
			logger = zap.NewNop()
		}
		if upgraded, stored, ok := h.upgradeLegacyReferencedResolution(ctx, current, snapshotDeps, resolution, baselineDigest, transcoder); ok {
			logger.Info("upgrading legacy dependency resolution in place: stored selection satisfies folded reference declarations",
				zap.String("stored_resolution_digest", resolution.Digest),
				zap.String("upgraded_resolution_digest", upgraded.Digest))
			effectiveResolution = upgraded
			resolved = stored
		} else {
			logger.Warn("stored dependency resolution does not match deployment baseline; resolving final declarations",
				zap.String("reason", refreshReason),
				zap.String("stored_baseline_digest", resolution.BaselineDigest),
				zap.String("deployment_baseline_digest", baselineDigest),
				zap.String("stored_resolution_digest", resolution.Digest))
			resolved, err = h.refreshResolvedModules(ctx, current, transcoder, resolution, desiredDeps)
			if err != nil {
				return regapi.DirectiveResult{}, err
			}
			effectiveResolution = dependencyResolution(rootDeps, refDeps, resolved)
			effectiveResolution.BaselineDigest = baselineDigest
			effectiveResolution = effectiveResolution.Canonical()
		}
	} else {
		rootDeps, refDeps, err = h.collectResolutionDependencies(ctx, target, transcoder, resolution.Roots, resolution.References)
		if err != nil {
			return regapi.DirectiveResult{}, err
		}
		desiredDeps = append(append([]desiredDependency(nil), rootDeps...), refDeps...)
		if got := dependencyInputDigest(rootDeps); got != resolution.InputDigest {
			return regapi.DirectiveResult{}, NewStoredResolutionError("stored dependency input digest does not match declarations", map[string]any{
				"stored":  resolution.InputDigest,
				"current": got,
			})
		}
		resolved, err = resolvedModulesFromStored(resolution)
		if err != nil {
			return regapi.DirectiveResult{}, err
		}
	}
	for _, root := range desiredDeps {
		selected, ok := selectedModuleVersion(resolved, root.definition.Component)
		if !ok || !storedVersionSatisfies(selected, root.definition.Version) {
			return regapi.DirectiveResult{}, NewStoredResolutionError("selected module does not satisfy its declaration", map[string]any{
				"component": root.definition.Component,
				"selected":  selected,
				"declared":  root.definition.Version,
				"entry_id":  root.entry.ID.String(),
			})
		}
	}
	if err := h.refreshReplacementModuleIdentities(resolved); err != nil {
		return regapi.DirectiveResult{}, err
	}
	effectiveResolution.Deployment = h.deployment.Canonical()
	effectiveResolution = effectiveResolution.Canonical()
	// A local replacement is a mutable development source. Reconciliation uses
	// its current identity to decide which resident entries must be reloaded,
	// while effectiveResolution remains the immutable checkpoint for this
	// history version.

	desiredModules := resolvedModuleSet(resolved)
	// A stored graph is authoritative by content identity, not merely version.
	_, installedDigests := h.currentModuleIdentities(ctx)
	baselineDigests := h.baselineModuleDigests()
	touched := make(map[string]struct{}, len(desiredModules))
	mutable := make(map[string]struct{}, len(desiredModules))
	parameterModules, err := changedDependencyParameterModules(ctx, current, target, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	for module := range parameterModules {
		if _, desired := desiredModules[module]; !desired {
			continue
		}
		// Parameter changes must be linked from the raw artifact. Re-linking the
		// resident entry would apply append targets ("+=") a second time.
		mutable[module] = struct{}{}
		touched[module] = struct{}{}
	}
	for _, mod := range resolved {
		module := mod.Org + "/" + mod.Name
		installedDigest := installedDigests[module]
		if installedDigest == "" {
			installedDigest = baselineDigests[module+"@"+mod.Version]
		}
		if !artifactDigestsEqual(installedDigest, mod.Digest) {
			mutable[module] = struct{}{}
			touched[module] = struct{}{}
		} else if _, replacement := h.replacementPath(module); !replacement && !h.hasCurrentUnpackedModule(mod) {
			touched[module] = struct{}{}
		}
	}
	controlled, err := h.reconciliationControlledModules(ctx, current, target, transcoder, desiredModules)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	moduleEntries, unpackPlan, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, touched), target, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	defer func() { _ = unpackPlan.cleanup() }()
	desiredDepEntries := make([]regapi.Entry, 0, len(desiredDeps))
	for _, dep := range desiredDeps {
		desiredDepEntries = append(desiredDepEntries, dep.entry)
	}
	combined := make([]regapi.Entry, 0, len(target)+len(moduleEntries))
	for _, entry := range target {
		if module := entryModule(entry); module != "" {
			if _, dependencyOwned := controlled[module]; dependencyOwned {
				if _, desired := desiredModules[module]; !desired {
					continue
				}
				if _, changed := touched[module]; changed {
					continue
				}
			}
		}
		combined = append(combined, entry)
	}
	combined = append(combined, moduleEntries...)

	pipeline := build.New(
		stages.Override(stages.WithMissingOverrideEntriesIgnored()),
		stages.Disable(),
		stages.Link(stages.WithDependencies(mergeLinkDependencies(desiredDepEntries, moduleEntries)), stages.WithStrictRequirementModules(sortedSetKeys(touched))),
		stages.Override(stages.WithMissingOverrideEntriesIgnored()),
	)
	if err := pipeline.Execute(ctx, &combined); err != nil {
		return regapi.DirectiveResult{}, NewDependencyPipelineError(err)
	}

	// Reconciliation owns the whole graph for deletes, but only artifacts whose
	// content identity or authored root parameters changed are mutable. A module
	// can be reloaded solely to repair its unpacked filesystem cache; relinking
	// that artifact must not turn harmless normalization into registry updates
	// and restart unrelated services during undo/redo (including the governance
	// worker itself).
	additional, err := (operationPlanner{resolver: h.resolver}).plan(target, combined, operationPlanOptions{
		controlledModules: controlled,
		mutableModules:    mutable,
	})
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	scoped := make([]regapi.ScopedOperation, 0, len(additional))
	for _, op := range additional {
		scoped = append(scoped, regapi.ScopedOperation{Operation: op, Scope: regapi.ScopeBaseline})
	}
	packEffect, err := h.buildEmbedPackEffect(ctx, resolved, controlled)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	var effects []regapi.Effect
	artifactEffect, err := h.buildArtifactEffect(ctx, resolved, combined)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	filesystemEffect, err := h.buildModuleFilesystemEffect(resolved, controlled, unpackPlan)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	if artifactEffect != nil {
		effects = append(effects, artifactEffect)
	}
	if filesystemEffect != nil {
		effects = append(effects, filesystemEffect)
	}
	if packEffect != nil {
		effects = append(effects, packEffect)
	}
	return regapi.DirectiveResult{
		Applied:    true,
		Resolution: effectiveResolution,
		Additional: scoped,
		Effects:    effects,
	}, nil
}

// replacementZeroVersion labels a replacement tree that declares no version
// and has never been recorded under one. Identity is the tree digest; the
// label only has to be a well-formed release.
const replacementZeroVersion = "0.0.0"

// touchedModuleIdentities returns the resolved modules this operation actually
// affects: those new or version-changed relative to the snapshot, plus the
// module of the dependency entry being changed in this operation. Modules
// already installed at the same version that this operation does not target are
// trusted — they were validated when installed — and are excluded from strict
// requirement enforcement, so a partial update does not re-validate
// dependencies it did not touch.
func touchedModuleIdentities(
	modules []ResolvedModule,
	installedVersions map[string]string,
	installedDigests map[string]string,
	opComponent string,
) []string {
	names := make([]string, 0, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		name := mod.Org + "/" + mod.Name
		version, known := installedVersions[name]
		digestMatches := mod.Digest == "" || artifactDigestsEqual(installedDigests[name], mod.Digest)
		if !known || version != mod.Version || !digestMatches || name == opComponent {
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
func sortedSetKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
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
func (h *DependencyHandler) shouldUnpackModules() bool {
	if h.lock == nil {
		return false
	}
	return h.lock.ShouldUnpackModules()
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
