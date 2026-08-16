// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	hubsemver "github.com/wippyai/runtime/api/semver"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/artifact"
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
	resolved, err := h.resolveEffectiveModules(ctx, desiredRoots, lockedVersions)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	for _, ref := range refDeps {
		selected, ok := selectedModuleVersion(resolved, ref.definition.Component)
		if !ok || !storedVersionSatisfies(selected, ref.definition.Version) {
			return regapi.DirectiveResult{}, NewDependencyResolutionError(fmt.Errorf(
				"folded dependency reference %s requires %s@%s, selection is %s",
				ref.entry.ID.String(), ref.definition.Component, ref.definition.Version, selected,
			))
		}
	}

	opComponent := ""
	for _, dep := range desiredDeps {
		if idsEqual(dep.entry.ID, op.Entry.ID) {
			opComponent = dep.definition.Component
			break
		}
	}
	strictModules := touchedModuleIdentities(
		resolved,
		lockedVersions,
		snapshotModuleDigests(snapshot),
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

	moduleEntries, unpackPlan, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, touchedModules), resolved, snapshot, transcoder)
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
	packEffect, err := h.buildEmbedPackEffect(ctx, resolved, snapshot, controlledModules)
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

// applyOperationToState materializes the state an operation produces, so a
// recorded graph can bind to the deployment identity of its own version.
func applyOperationToState(snapshot regapi.State, op regapi.Operation) regapi.State {
	next := make(regapi.State, 0, len(snapshot)+1)
	replaced := false
	for _, entry := range snapshot {
		if idsEqual(entry.ID, op.Entry.ID) {
			if op.Kind == regapi.EntryCreate || op.Kind == regapi.EntryUpdate {
				next = append(next, op.Entry)
				replaced = true
			}
			continue
		}
		next = append(next, entry)
	}
	if !replaced && (op.Kind == regapi.EntryCreate || op.Kind == regapi.EntryUpdate) {
		next = append(next, op.Entry)
	}
	return next
}

func rootExpansionDriver(op regapi.Operation, snapshot regapi.State) (regapi.Operation, bool) {
	if entry, ok := resolveOperationEntry(op, snapshot); ok && isRootDependency(entry) {
		return op, true
	}
	for _, entry := range snapshot {
		if idsEqual(entry.ID, op.Entry.ID) && isRootDependency(entry) {
			return regapi.Operation{Kind: regapi.EntryDelete, Entry: entry}, true
		}
	}
	return regapi.Operation{}, false
}

// refreshResolvedModules re-resolves the final declarations for a graph whose
// stored selection cannot be replayed, seeding the solver with stored,
// installed, and locked versions so an unchanged module keeps its selection.
func (h *DependencyHandler) refreshResolvedModules(
	ctx context.Context,
	current regapi.State,
	transcoder payload.Transcoder,
	resolution *regapi.DependencyResolution,
	desiredDeps []desiredDependency,
) ([]ResolvedModule, error) {
	lockedVersions := storedResolutionVersions(resolution)
	deploymentVersions, err := h.installedModuleVersions(ctx, transcoder, current)
	if err != nil {
		return nil, err
	}
	for module, version := range deploymentVersions {
		lockedVersions[module] = version
	}
	if h.lock != nil {
		for _, mod := range h.lock.GetModules() {
			if mod.Name != "" && mod.Version != "" {
				lockedVersions[mod.Name] = mod.Version
			}
		}
	}
	return h.resolveEffectiveModules(ctx, dependencyDefinitions(desiredDeps), lockedVersions)
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
		return regapi.DirectiveResult{}, NewDependencyResolutionError(fmt.Errorf("stored dependency resolution is invalid"))
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
			return regapi.DirectiveResult{}, NewDependencyResolutionError(fmt.Errorf(
				"stored dependency input digest does not match declarations: stored %s, current %s",
				resolution.InputDigest, got,
			))
		}
		resolved, err = resolvedModulesFromStored(resolution)
		if err != nil {
			return regapi.DirectiveResult{}, err
		}
	}
	for _, root := range desiredDeps {
		selected, ok := selectedModuleVersion(resolved, root.definition.Component)
		if !ok || !storedVersionSatisfies(selected, root.definition.Version) {
			return regapi.DirectiveResult{}, NewDependencyResolutionError(fmt.Errorf(
				"selected module %s@%s does not satisfy %s declared by %s",
				root.definition.Component, selected, root.definition.Version, root.entry.ID.String(),
			))
		}
	}
	if err := h.refreshReplacementModuleIdentities(resolved); err != nil {
		return regapi.DirectiveResult{}, err
	}
	// A local replacement is a mutable development source. Reconciliation uses
	// its current identity to decide which resident entries must be reloaded,
	// while effectiveResolution remains the immutable checkpoint for this
	// history version.

	desiredModules := resolvedModuleSet(resolved)
	// A stored graph is authoritative by content identity, not merely version.
	// Reload modules whose entries do not carry the exact stored digest. Entries
	// written before module_digest existed are deliberately reloaded once.
	installedDigests := snapshotModuleDigests(target)
	lockedDigests := h.lockedModuleDigests()
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
			// Legacy entries predate module_digest. The exact name@version lock
			// hash is still authoritative and prevents a history restore from
			// rewriting every resident module merely to backfill metadata.
			installedDigest = lockedDigests[module+"@"+mod.Version]
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

	moduleEntries, unpackPlan, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, touched), resolved, target, transcoder)
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
	packEffect, err := h.buildEmbedPackEffect(ctx, resolved, current, controlled)
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

// changedDependencyParameterModules returns the modules whose authored root
// parameters differ across a history transition. Dependency parameters belong
// to the referenced component's declared namespace and cannot mutate another
// module, including through a fully qualified requirement ID.
func changedDependencyParameterModules(
	ctx context.Context,
	current regapi.State,
	target regapi.State,
	transcoder payload.Transcoder,
) (map[string]struct{}, error) {
	type declaration struct {
		definition DependencyDefinition
		present    bool
	}

	decodeRoots := func(state regapi.State) (map[string]declaration, error) {
		roots := make(map[string]declaration)
		for _, entry := range state {
			if !isRootDependency(entry) {
				continue
			}
			definition, err := decodeDependency(ctx, transcoder, entry)
			if err != nil {
				return nil, err
			}
			roots[idKey(entry.ID)] = declaration{definition: definition, present: true}
		}
		return roots, nil
	}

	currentRoots, err := decodeRoots(current)
	if err != nil {
		return nil, err
	}
	targetRoots, err := decodeRoots(target)
	if err != nil {
		return nil, err
	}

	changed := make(map[string]struct{})
	rootIDs := make(map[string]struct{}, len(currentRoots)+len(targetRoots))
	for id := range currentRoots {
		rootIDs[id] = struct{}{}
	}
	for id := range targetRoots {
		rootIDs[id] = struct{}{}
	}
	for id := range rootIDs {
		before := currentRoots[id]
		after := targetRoots[id]
		if before.present == after.present && reflect.DeepEqual(before.definition.Parameters, after.definition.Parameters) {
			continue
		}
		for _, item := range []declaration{before, after} {
			if !item.present {
				continue
			}
			if item.definition.Component != "" {
				changed[item.definition.Component] = struct{}{}
			}
		}
	}
	return changed, nil
}

// storedVersionSatisfies validates selectors that can be checked without the
// resolver. A label is intentionally accepted here: the graph digest binds the
// authored label to the exact version selected when the graph was created, and
// resolving the moving label again would defeat deterministic offline restore.
func storedVersionSatisfies(version, constraint string) bool {
	if strings.HasPrefix(strings.TrimSpace(constraint), "@") {
		return strings.TrimSpace(version) != ""
	}
	return lockedVersionSatisfies(version, constraint)
}

func selectedModuleVersion(modules []ResolvedModule, component string) (string, bool) {
	for _, mod := range modules {
		if mod.Org+"/"+mod.Name == component {
			return mod.Version, true
		}
	}
	return "", false
}

func dependencyResolution(roots, references []desiredDependency, modules []ResolvedModule) *regapi.DependencyResolution {
	resolved := &regapi.DependencyResolution{
		InputDigest: dependencyInputDigest(roots),
		Roots:       dependencyRoots(roots),
		References:  dependencyReferenceRoots(references),
		Modules:     make([]regapi.ResolvedModule, 0, len(modules)),
	}
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" || mod.Version == "" {
			continue
		}
		resolved.Modules = append(resolved.Modules, regapi.ResolvedModule{
			Name:      mod.Org + "/" + mod.Name,
			Version:   mod.Version,
			VersionID: mod.VersionID,
			Source:    mod.Source,
			Digest:    mod.Digest,
			SizeBytes: mod.SizeBytes,
			Protected: mod.Protected,
		})
	}
	return resolved.Canonical()
}

// dependencyReferenceRoots renders folded references for the durable
// resolution; an absent constraint is recorded as the explicit wildcard so
// every stored reference carries a non-empty version.
func dependencyReferenceRoots(references []desiredDependency) []regapi.DependencyRoot {
	result := dependencyRoots(references)
	for i := range result {
		if strings.TrimSpace(result[i].Version) == "" {
			result[i].Version = "*"
		}
	}
	return result
}

func dependencyRoots(roots []desiredDependency) []regapi.DependencyRoot {
	result := make([]regapi.DependencyRoot, 0, len(roots))
	for _, root := range roots {
		result = append(result, regapi.DependencyRoot{
			ID: root.entry.ID.String(), Component: root.definition.Component, Version: root.definition.Version,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func dependencyInputDigest(roots []desiredDependency) string {
	canonical := dependencyRoots(roots)
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (h *DependencyHandler) collectResolutionDependencies(
	ctx context.Context,
	snapshot regapi.State,
	transcoder payload.Transcoder,
	roots []regapi.DependencyRoot,
	references []regapi.DependencyRoot,
) ([]desiredDependency, []desiredDependency, error) {
	byID := make(map[string]regapi.Entry, len(snapshot))
	for _, entry := range snapshot {
		if isRootDependency(entry) {
			byID[idKey(entry.ID)] = entry
		}
	}
	if len(byID) != len(roots)+len(references) {
		return nil, nil, NewDependencyResolutionError(fmt.Errorf(
			"stored dependency root set has %d entries, current declarations have %d",
			len(roots)+len(references), len(byID),
		))
	}
	deps := make([]desiredDependency, 0, len(roots))
	seenIDs := make(map[string]struct{}, len(roots)+len(references))
	seenComponents := make(map[string]string, len(roots))
	for _, root := range roots {
		rootKey := idKey(regapi.ParseID(root.ID))
		if rootKey == ":" {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf("stored dependency root has an empty id"))
		}
		if _, duplicate := seenIDs[rootKey]; duplicate {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf("duplicate stored dependency root %s", root.ID))
		}
		seenIDs[rootKey] = struct{}{}
		entry, ok := byID[rootKey]
		if !ok {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf("stored dependency root %s is missing", root.ID))
		}
		definition, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, nil, err
		}
		if definition.Component != root.Component || definition.Version != root.Version {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf(
				"stored dependency root %s expected %s@%s, got %s@%s",
				root.ID, root.Component, root.Version, definition.Component, definition.Version,
			))
		}
		if previousID, duplicate := seenComponents[definition.Component]; duplicate {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf(
				"duplicate stored dependency component %s in roots %s and %s", definition.Component, previousID, root.ID,
			))
		}
		seenComponents[definition.Component] = root.ID
		deps = append(deps, desiredDependency{entry: entry, definition: definition})
	}
	refs := make([]desiredDependency, 0, len(references))
	for _, reference := range references {
		refKey := idKey(regapi.ParseID(reference.ID))
		if refKey == ":" {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf("stored dependency reference has an empty id"))
		}
		if _, duplicate := seenIDs[refKey]; duplicate {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf("duplicate stored dependency declaration %s", reference.ID))
		}
		seenIDs[refKey] = struct{}{}
		entry, ok := byID[refKey]
		if !ok {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf("stored dependency reference %s is missing", reference.ID))
		}
		definition, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, nil, err
		}
		declared := strings.TrimSpace(definition.Version)
		if declared == "" {
			declared = "*"
		}
		if definition.Component != reference.Component || declared != reference.Version {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf(
				"stored dependency reference %s expected %s@%s, got %s@%s",
				reference.ID, reference.Component, reference.Version, definition.Component, declared,
			))
		}
		if _, anchored := seenComponents[definition.Component]; !anchored {
			return nil, nil, NewDependencyResolutionError(fmt.Errorf(
				"stored dependency reference %s has no root for component %s", reference.ID, definition.Component,
			))
		}
		refs = append(refs, desiredDependency{entry: entry, definition: definition})
	}
	return deps, refs, nil
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
	freshRoots map[string]struct{},
) ([]desiredDependency, []desiredDependency, error) {
	deps := make(map[string]desiredDependency)
	operationID := op.Entry.ID

	current, err := h.collectSnapshotDependencies(ctx, snapshot, transcoder)
	if err != nil {
		return nil, nil, err
	}
	for _, dep := range current {
		deps[idKey(dep.entry.ID)] = dep
	}

	switch op.Kind {
	case regapi.EntryDelete:
		delete(deps, idKey(op.Entry.ID))
	case regapi.EntryCreate, regapi.EntryUpdate:
		entry, ok := resolveOperationEntry(op, snapshot)
		if !ok {
			return nil, nil, NewDependencyEntryMissingError(op.Entry.ID.String())
		}
		if !isRootDependency(entry) {
			break
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, nil, err
		}
		deps[idKey(entry.ID)] = desiredDependency{
			entry:      entry,
			definition: def,
		}
	}

	result := make([]desiredDependency, 0, len(deps))
	for id, dep := range deps {
		if dep.definition.Component == "" {
			return nil, nil, NewDependencyEntryInvalidError(id, "component is required", "")
		}
		result = append(result, dep)
	}
	fresh := make(map[string]struct{}, 1)
	if op.Kind == regapi.EntryCreate {
		fresh[idKey(operationID)] = struct{}{}
	}
	for key := range freshRoots {
		fresh[key] = struct{}{}
	}
	return foldRootDependencyComponents(result, fresh, true)
}

// foldRootDependencyComponents partitions root declarations into one
// controlling root per component plus folded references, independent of any
// operation: a declaration carrying parameters controls (several carriers must
// agree), ties break on the lowest canonical entry key, and — in the live path
// — a declaration introduced by the current changeset (`fresh`) never controls
// while an established one exists. The choice is therefore reconstructible
// from the declarations alone and stable across later evaluations.
//
// strict mode (live expansion) conflicts on parameter disagreement and on a
// fresh parameter-carrying duplicate that would have to seize control.
// Lenient mode (reconciliation of committed state) never conflicts: replay is
// anchored by the stored root/reference partition, and parameter drift is
// handled by the parameter reconciliation sweep, so a disagreement must not
// wedge boot.
//
// Folded reference constraints are normalized here (trimmed, absent becomes
// the explicit wildcard) so the durable record, the solver input, and strict
// replay all see one spelling.
func foldRootDependencyComponents(deps []desiredDependency, fresh map[string]struct{}, strict bool) (roots, references []desiredDependency, err error) {
	sort.SliceStable(deps, func(i, j int) bool {
		return idKey(deps[i].entry.ID) < idKey(deps[j].entry.ID)
	})

	groups := make(map[string][]desiredDependency, len(deps))
	order := make([]string, 0, len(deps))
	for _, dep := range deps {
		component := dep.definition.Component
		if _, seen := groups[component]; !seen {
			order = append(order, component)
		}
		groups[component] = append(groups[component], dep)
	}

	isFresh := func(dep desiredDependency) bool {
		if len(fresh) == 0 {
			return false
		}
		_, ok := fresh[idKey(dep.entry.ID)]
		return ok
	}
	hasParams := func(dep desiredDependency) bool { return len(dep.definition.Parameters) > 0 }

	roots = make([]desiredDependency, 0, len(deps))
	for _, component := range order {
		group := groups[component]

		// Election: parameter carriers first, established before fresh, then
		// the lowest canonical key. The input is already key-sorted, so the
		// first matching declaration wins deterministically.
		pick := func(accept func(desiredDependency) bool) (desiredDependency, bool) {
			for _, dep := range group {
				if accept(dep) {
					return dep, true
				}
			}
			return desiredDependency{}, false
		}
		controller, elected := pick(func(d desiredDependency) bool { return hasParams(d) && !isFresh(d) })
		if !elected {
			controller, elected = pick(func(d desiredDependency) bool { return !isFresh(d) })
		}
		if !elected {
			controller, elected = pick(hasParams)
		}
		if !elected {
			controller = group[0]
		}

		for _, dep := range group {
			if idsEqual(dep.entry.ID, controller.entry.ID) {
				// The controlling declaration itself, possibly observed through
				// two ID spellings; every such shape stays a root.
				roots = append(roots, dep)
				continue
			}
			if strict && hasParams(dep) && !dependencyParametersEqual(dep.definition.Parameters, controller.definition.Parameters) {
				return nil, nil, NewDependencyRootConflictError(component, controller.entry.ID.String(), dep.entry.ID.String())
			}
			reference := dep
			reference.definition.Version = strings.TrimSpace(reference.definition.Version)
			if reference.definition.Version == "" {
				reference.definition.Version = "*"
			}
			references = append(references, reference)
		}
	}
	return roots, references, nil
}

// dependencyParametersEqual compares parameter sets by name over their
// canonical JSON forms, so transcoder-specific value typing cannot split
// equal declarations.
func dependencyParametersEqual(a, b []Parameter) bool {
	if len(a) != len(b) {
		return false
	}
	canonical := func(params []Parameter) string {
		pairs := make([]string, 0, len(params))
		for _, p := range params {
			value, err := json.Marshal(p.Value)
			if err != nil {
				value = []byte(fmt.Sprintf("%#v", p.Value))
			}
			pairs = append(pairs, p.Name+"="+string(value))
		}
		sort.Strings(pairs)
		return strings.Join(pairs, "\x00")
	}
	return canonical(a) == canonical(b)
}

func (h *DependencyHandler) installedModuleVersions(ctx context.Context, transcoder payload.Transcoder, snapshot regapi.State) (map[string]string, error) {
	versions := snapshotModuleVersions(snapshot)
	if h.lock == nil {
		return versions, nil
	}
	installedRoots, err := rootDependencyModules(ctx, transcoder, snapshot)
	if err != nil {
		return nil, err
	}
	for _, mod := range h.lock.GetModules() {
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
	if regapi.DependencyAccessFromContext(ctx) == regapi.DependencyAccessVerifiedOffline {
		return nil, NewDependencyOfflineError("resolve", "")
	}

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

// resolveEffectiveModules returns the complete module selection controlled by
// the current deployment plus authored registry roots. Lock-selected root
// modules are implicit deployment inputs: a history overlay may replace one,
// but removing that overlay must reveal the locked root again rather than
// uninstalling the deployment itself.
func (h *DependencyHandler) resolveEffectiveModules(
	ctx context.Context,
	deps []DependencyDefinition,
	lockedVersions map[string]string,
) ([]ResolvedModule, error) {
	if regapi.DependencyAccessFromContext(ctx) == regapi.DependencyAccessVerifiedOffline {
		if resolved, ok := h.lockedResolution(deps, lockedVersions); ok {
			if h.logger != nil {
				h.logger.Debug("using locked dependency resolution",
					zap.Int("modules", len(resolved)),
					zap.Int("roots", len(deps)))
			}
			return resolved, nil
		}
		return nil, NewDependencyOfflineError("resolve", "")
	}

	resolved, err := h.resolveModules(ctx, deps, lockedVersions)
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
		return fmt.Errorf("invalid module name %q: organization and module must be lowercase alphanumeric with hyphens", name.String())
	}
	if _, err := hubsemver.ParseVersion(strings.TrimSpace(version)); err != nil {
		return fmt.Errorf("invalid exact module version %q for %s", version, name.String())
	}
	if digest == "" {
		return nil // Older hubs and local replacements did not always provide one.
	}
	algorithm, value, err := parseExpectedDigest(digest)
	if err != nil || algorithm != "sha256" || len(value) != sha256.Size*2 {
		return fmt.Errorf("invalid sha256 digest for %s@%s", name.String(), version)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("invalid sha256 digest for %s@%s", name.String(), version)
	}
	return nil
}

func validateStoredModuleArtifactIdentity(name graph.Name, version, source, digest string) error {
	if err := validateModuleArtifactIdentity(name, version, ""); err != nil {
		return err
	}
	if digest == "" {
		return fmt.Errorf("stored module %s@%s has no content digest", name.String(), version)
	}
	algorithm, value, err := parseExpectedDigest(digest)
	wantAlgorithm := "sha256"
	if source == moduleSourceReplacementTreeV1 {
		wantAlgorithm = "sha256-tree-v1"
	} else if source != "" && source != moduleSourceHub {
		return fmt.Errorf("stored module %s@%s has unsupported source %q", name.String(), version, source)
	}
	if err != nil || algorithm != wantAlgorithm || len(value) != sha256.Size*2 {
		return fmt.Errorf("invalid %s digest for %s@%s", wantAlgorithm, name.String(), version)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("invalid %s digest for %s@%s", wantAlgorithm, name.String(), version)
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
			return NewDependencyIntegrityError(modKey(*mod), err, mod.Digest, mod.SizeBytes)
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
			return "", 0, fmt.Errorf("extracted module has no content digest")
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

// replacementManifestProvider loads entries and declared dependencies from a
// local replacement tree. A replacement with no explicit source version still
// delegates release availability to the Hub; a satisfying lock avoids that call.
// Non-replaced modules delegate to the base provider.
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
	return p.lockedVersions[name]
}

func isExactModuleVersion(value string) bool {
	_, err := hubsemver.ParseVersion(strings.TrimSpace(value))
	return err == nil
}

func (p *replacementManifestProvider) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	name := org + "/" + module
	if path, ok := p.handler.replacementPath(name); ok {
		version := p.replacementVersion(name, constraint)
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

func loadReplacementEntries(
	ctx context.Context,
	path string,
	logger *zap.Logger,
	transcoder payload.Transcoder,
) ([]regapi.Entry, error) {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return nil, nil
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
		// for which released versions are available to satisfy live ranges.
		if version := p.handler.replacementModuleVersion(name); version != "" {
			return []VersionInfo{{Version: version}}, nil
		}
		return p.base.ListAllVersions(ctx, org, module)
	}
	return p.base.ListAllVersions(ctx, org, module)
}

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

func (h *DependencyHandler) loadModuleEntries(ctx context.Context, modules []ResolvedModule, ownerModules []ResolvedModule, snapshot regapi.State, transcoder payload.Transcoder) ([]regapi.Entry, *unpackPlan, error) {
	entries := make([]regapi.Entry, 0)
	plan := &unpackPlan{}
	owners := moduleOwnersByNamespace(ownerModules)
	snapshotOwners := moduleOwnersByEntryID(snapshot)
	snapshotByID := entriesByID(snapshot)
	installedRoots, err := rootDependencyModules(ctx, transcoder, snapshot)
	if err != nil {
		return nil, nil, err
	}

	for _, mod := range modules {
		moduleName := mod.Org + "/" + mod.Name
		deploymentRootModule := h.lock != nil && h.lock.IsRootModule(moduleName)
		moduleEntries, staged, err := h.loadEntriesForModulePlan(ctx, transcoder, mod)
		if err != nil {
			_ = plan.cleanup()
			return nil, nil, err
		}
		plan.add(staged)
		for i := range moduleEntries {
			// Cold boot marks dependency declarations from the selected root
			// application as deployment roots. Loading that same application
			// through a live Hub update must produce the identical topology;
			// otherwise the update silently turns its application dependencies
			// into transitive module entries and the next update loses their
			// host bindings.
			if deploymentRootModule && moduleEntries[i].Kind == regapi.NamespaceDependency {
				moduleEntries[i].DependencyRoot = true
			}
			if keep, ok := preserveHostSnapshotEntry(moduleEntries[i], moduleName, snapshotByID, installedRoots); ok {
				moduleEntries[i] = keep
				continue
			}
			moduleEntries[i] = markModuleIdentityForGraph(moduleEntries[i], moduleName, mod.Version, mod.Digest, owners, snapshotOwners)
		}
		entries = append(entries, moduleEntries...)
	}

	return entries, plan, nil
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

func (h *DependencyHandler) loadEntriesForModule(ctx context.Context, transcoder payload.Transcoder, mod ResolvedModule) ([]regapi.Entry, error) {
	entries, staged, err := h.loadEntriesForModulePlan(ctx, transcoder, mod)
	if staged != nil {
		_ = os.RemoveAll(staged.stagingDir)
	}
	return entries, err
}

func (h *DependencyHandler) loadEntriesForModulePlan(ctx context.Context, transcoder payload.Transcoder, mod ResolvedModule) ([]regapi.Entry, *stagedModuleDirectory, error) {
	modulePath, staged, err := h.materializeModuleForLoad(ctx, mod)
	if err != nil {
		return nil, nil, err
	}
	var entries []regapi.Entry
	if mod.Source == moduleSourceReplacementTreeV1 {
		entries, err = loadReplacementEntries(ctx, modulePath, h.logger, transcoder)
	} else {
		entries, err = loadRawEntriesFromPaths(ctx, []string{modulePath}, h.logger, transcoder)
	}
	if err != nil {
		if staged != nil {
			_ = os.RemoveAll(staged.stagingDir)
		}
		return nil, nil, err
	}
	entries, err = h.applyModuleConfigFilters(ctx, modulePath, entries)
	if err != nil {
		if staged != nil {
			_ = os.RemoveAll(staged.stagingDir)
		}
		return nil, nil, err
	}
	if mod.Source == moduleSourceReplacementTreeV1 {
		digest, size, digestErr := digestReplacementTree(modulePath)
		if digestErr != nil {
			return nil, nil, NewDependencyIntegrityError(modKey(mod), digestErr, mod.Digest, mod.SizeBytes)
		}
		if !strings.EqualFold(digest, mod.Digest) || (mod.SizeBytes > 0 && size != mod.SizeBytes) {
			return nil, nil, NewDependencyIntegrityError(modKey(mod), fmt.Errorf("replacement changed while it was being loaded"), mod.Digest, mod.SizeBytes)
		}
	}
	return entries, staged, nil
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
	filtered, err := stages.FilterModuleEntries(ctx, cfg, entries)
	if err != nil {
		return nil, NewDependencyLoadError(modulePath, err)
	}
	return filtered, nil
}

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
			return "", NewDependencyLoadError(replacementPath, fmt.Errorf("replacement path is not a directory"))
		}
		digest, size, err := digestReplacementTree(replacementPath)
		if err != nil {
			return "", NewDependencyIntegrityError(modKey(mod), err, mod.Digest, mod.SizeBytes)
		}
		if mod.Digest != "" && !strings.EqualFold(mod.Digest, digest) {
			return "", NewDependencyIntegrityError(modKey(mod), fmt.Errorf("replacement content digest mismatch"), mod.Digest, mod.SizeBytes)
		}
		if mod.SizeBytes > 0 && mod.SizeBytes != size {
			return "", NewDependencyIntegrityError(modKey(mod), fmt.Errorf("replacement content size mismatch"), mod.Digest, mod.SizeBytes)
		}
		return replacementPath, nil
	}
	if mod.Source == moduleSourceReplacementTreeV1 {
		return "", NewDependencyLoadError(moduleName, fmt.Errorf("stored local replacement is not configured"))
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
	if regapi.DependencyAccessFromContext(ctx) == regapi.DependencyAccessVerifiedOffline {
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

	downloadErr := h.hub.DownloadToFile(downloadCtx, url, destination)
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
			downloadErr = h.hub.DownloadToFile(retryCtx, info.URL, destination)
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
		return "", fmt.Errorf("artifact path %q is absolute", relative)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve vendor directory: %w", err)
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, relative))
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path %q escapes vendor directory", relative)
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
			return "", fmt.Errorf("inspect artifact path %q: %w", relative, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("artifact path %q traverses symlink %q", relative, cursor)
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
			return fmt.Errorf("download version mismatch: expected %s, got %s", mod.Version, info.Version)
		}
	}
	if mod.Digest != "" && info.Digest != "" {
		wantAlgorithm, want, wantErr := parseExpectedDigest(mod.Digest)
		gotAlgorithm, got, gotErr := parseExpectedDigest(info.Digest)
		if wantErr != nil || gotErr != nil || wantAlgorithm != gotAlgorithm || !strings.EqualFold(want, got) {
			return fmt.Errorf("download digest mismatch: expected %s, got %s", mod.Digest, info.Digest)
		}
	}
	if mod.SizeBytes > 0 && info.Size > 0 && mod.SizeBytes != info.Size {
		return fmt.Errorf("download size mismatch: expected %d bytes, got %d bytes", mod.SizeBytes, info.Size)
	}
	return nil
}

// freshDownloadInfo fetches a current presigned download URL for a module.
// Used both when the resolved manifest carries no URL and to refresh a URL
// that expired before the artifact could be downloaded.
func (h *DependencyHandler) freshDownloadInfo(ctx context.Context, mod ResolvedModule) (*DownloadInfo, error) {
	if regapi.DependencyAccessFromContext(ctx) == regapi.DependencyAccessVerifiedOffline {
		return nil, NewDependencyOfflineError("fetch artifact metadata", modKey(mod))
	}
	downloadURLCtx, cancel := withOptionalTimeout(ctx, h.downloadTimeout)
	defer cancel()

	return h.hub.GetDownloadURL(downloadURLCtx, &DownloadParams{
		Org:       mod.Org,
		Module:    mod.Name,
		Version:   mod.Version,
		VersionID: mod.VersionID,
	})
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
		return fmt.Errorf("hash extracted module: %w", err)
	}
	data, err := yaml.Marshal(extractedModuleMetadata{Digest: digest, Size: size, TreeDigest: treeDigest})
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
	if meta.TreeDigest == "" {
		return fmt.Errorf("extracted module has no tree digest")
	}
	treeDigest, _, err := digestDirectoryTree(dirPath)
	if err != nil {
		return fmt.Errorf("hash extracted module: %w", err)
	}
	if !strings.EqualFold(meta.TreeDigest, treeDigest) {
		return fmt.Errorf("extracted module tree digest mismatch: expected %s, got %s", meta.TreeDigest, treeDigest)
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

func (h *DependencyHandler) lockedModuleDigests() map[string]string {
	if h.lock == nil {
		return nil
	}
	modules := h.lock.GetModules()
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

// NewDependencyOfflineError reports unavailable verified dependency evidence.
func NewDependencyOfflineError(operation, module string) apierror.Error {
	details := map[string]any{
		"operation": operation,
		"hint":      "run an explicit wippy update/install while online, then retry startup",
	}
	if module != "" {
		details["module"] = module
	}
	return apierror.New(apierror.Invalid, "verified dependency evidence is unavailable during offline startup").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(details))
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
