// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/wapp"
)

// referenceFoldHandler builds a handler whose fake hub serves acme/a and
// acme/b at v1.0.0 so expansion can resolve without a network.
func referenceFoldHandler(t *testing.T) *DependencyHandler {
	t.Helper()
	vendorDir := t.TempDir()
	artifact := buildWappBytes(t, []wapp.Entry{{
		ID: wapp.NewID("acme.a", "entry"), Kind: regapi.EntryKind, Data: map[string]any{"module": "a"},
	}})
	hub := &fakeHub{
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: module, Version: "v1.0.0", URL: "memory://" + module}, nil
		},
		downloadFile: func(_ context.Context, _ string, dest string) error {
			require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
			return os.WriteFile(dest, artifact, 0o600)
		},
	}
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: hub, Logger: zap.NewNop(), VendorDir: vendorDir})
	require.NoError(t, err)
	return handler
}

func TestExpand_RecordsFoldedReferenceForInstalledComponent(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryCreate, Entry: reference},
		fixtureState(regapi.State{root, moduleEntry}),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Resolution)
	require.Len(t, result.Resolution.Roots, 1)
	assert.Equal(t, "app.deps:a", result.Resolution.Roots[0].ID)
	require.Len(t, result.Resolution.References, 1)
	assert.Equal(t, "acme.pkg:__dependency.acme.a", result.Resolution.References[0].ID)
	assert.Equal(t, "acme/a", result.Resolution.References[0].Component)
	require.True(t, result.Resolution.Valid())
}

func TestExpand_FreshDuplicateInstallKeepsConflict(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")

	// No module entry in the snapshot: the component is not installed, so a
	// second install attempt keeps the established planning conflict.
	_, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryCreate, Entry: reference},
		fixtureState(regapi.State{root}),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")
}

func TestExpand_UnsatisfiableReferenceConstraintFailsAtDeclaration(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=2.0.0")

	_, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryCreate, Entry: reference},
		fixtureState(regapi.State{root, moduleEntry}),
	)
	require.Error(t, err)
}

func TestExpand_DeletingControllerPromotesReference(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: root.ID}},
		fixtureState(regapi.State{root, reference, moduleEntry}),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Resolution)
	require.Len(t, result.Resolution.Roots, 1)
	assert.Equal(t, "acme.pkg:__dependency.acme.a", result.Resolution.Roots[0].ID)
	assert.Empty(t, result.Resolution.References)
	// The module stays installed under the promoted declaration.
	for _, op := range result.Additional {
		if op.Operation.Kind == regapi.EntryDelete && op.Operation.Entry.ID.String() == "acme.a:entry" {
			t.Fatalf("promotion must not uninstall the module")
		}
	}
}

func TestExpand_DeletingReferenceKeepsRootAndModule(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: reference.ID}},
		fixtureState(regapi.State{root, reference, moduleEntry}),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Resolution)
	require.Len(t, result.Resolution.Roots, 1)
	assert.Equal(t, "app.deps:a", result.Resolution.Roots[0].ID)
	assert.Empty(t, result.Resolution.References)
	for _, op := range result.Additional {
		if op.Operation.Kind == regapi.EntryDelete && op.Operation.Entry.ID.String() == "acme.a:entry" {
			t.Fatalf("removing a reference must not uninstall the module")
		}
	}
}

func TestFoldRootDependencyComponents_OrderIndependentDigest(t *testing.T) {
	a := desiredDependency{
		entry:      regapi.Entry{ID: regapi.NewID("app.deps", "a"), Kind: regapi.NamespaceDependency},
		definition: DependencyDefinition{Component: "acme/a", Version: ">=1.0.0"},
	}
	b := desiredDependency{
		entry:      regapi.Entry{ID: regapi.NewID("acme.pkg", "__dependency.acme.a"), Kind: regapi.NamespaceDependency},
		definition: DependencyDefinition{Component: "acme/a", Version: ">=1.0.0"},
	}
	c := desiredDependency{
		entry:      regapi.Entry{ID: regapi.NewID("app.deps", "b"), Kind: regapi.NamespaceDependency},
		definition: DependencyDefinition{Component: "acme/b", Version: "*"},
	}

	permutations := [][]desiredDependency{
		{a, b, c}, {c, b, a}, {b, a, c}, {b, c, a}, {c, a, b}, {a, c, b},
	}
	var digest string
	var refIDs []string
	for _, perm := range permutations {
		roots, refs, err := foldRootDependencyComponents(perm, nil, false)
		require.NoError(t, err)
		got := dependencyInputDigest(roots)
		ids := make([]string, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.entry.ID.String())
		}
		if digest == "" {
			digest, refIDs = got, ids
			continue
		}
		assert.Equal(t, digest, got, "input digest must be order independent")
		assert.Equal(t, refIDs, ids, "reference election must be order independent")
	}
}

// hardeningReferencedResolution builds a stored graph with recorded references,
// the shape the live fold writes.
func hardeningReferencedResolution(roots []regapi.Entry, references []regapi.Entry) *regapi.DependencyResolution {
	toDeps := func(entries []regapi.Entry) []desiredDependency {
		deps := make([]desiredDependency, 0, len(entries))
		for _, entry := range entries {
			def, _ := entry.Data.Data().(map[string]any)
			component, _ := def["component"].(string)
			version, _ := def["version"].(string)
			deps = append(deps, desiredDependency{entry: entry, definition: DependencyDefinition{Component: component, Version: version}})
		}
		return deps
	}
	modules := make([]ResolvedModule, 0, len(roots))
	for _, root := range roots {
		def, _ := root.Data.Data().(map[string]any)
		component, _ := def["component"].(string)
		name, _ := graph.ParseName(component)
		modules = append(modules, ResolvedModule{
			Org: name.Organization, Name: name.Module, Version: "v1.0.0", Digest: hardeningDigest,
		})
	}
	return dependencyResolution(toDeps(roots), toDeps(references), modules)
}

func TestReconcile_ReplaysRecordedReferences(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{reference})

	state := regapi.State{root, reference, moduleEntry}
	result, err := handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), resolution)
	require.NoError(t, err)
	require.Equal(t, resolution.Digest, result.Resolution.Digest)
}

func TestReconcile_MissingReferenceEntryIsDrift(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{reference})

	state := regapi.State{root, moduleEntry}
	_, err = handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), resolution)
	require.ErrorContains(t, err, "root set")
}

func TestReconcile_UnrecordedDeclarationIsDrift(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	state := regapi.State{root, reference, moduleEntry}

	// Bind the stored graph to the current deployment baseline: within an
	// unchanged baseline an unrecorded declaration is drift, never an upgrade.
	transcoder := payload.GetTranscoder(ctx)
	baseline, err := handler.deploymentBaselineDigest(ctx, fixtureState(state), transcoder)
	require.NoError(t, err)
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, nil)
	resolution.BaselineDigest = baseline
	resolution = resolution.Canonical()

	_, err = handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), resolution)
	require.ErrorContains(t, err, "root set")
}

func TestReconcile_LegacyGraphUpgradesRegardlessOfDeclarationOrder(t *testing.T) {
	ctx := newTestContext()
	// The refresh path resolves online; the fake hub serves the module.
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	// A duplicate whose ID sorts AFTER the stored controller previously never
	// triggered the legacy refresh and wedged on the strict count check.
	late := hardeningRoot("zzz.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	legacy := hardeningReferencedResolution([]regapi.Entry{root}, nil)
	require.Empty(t, legacy.BaselineDigest)

	state := regapi.State{root, late, moduleEntry}
	result, err := handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), legacy)
	require.NoError(t, err)
	require.NotNil(t, result.Resolution)
	require.NotEmpty(t, result.Resolution.BaselineDigest, "legacy upgrade must bind the baseline")
	require.Len(t, result.Resolution.References, 1)
	require.Equal(t, "zzz.pkg:__dependency.acme.a", result.Resolution.References[0].ID)
}

func TestReconcile_ReferenceConstraintDriftIsRejected(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	recorded := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	drifted := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=0.5.0")
	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{recorded})

	state := regapi.State{root, drifted, moduleEntry}
	_, err = handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), resolution)
	require.ErrorContains(t, err, "stored dependency reference")
}

func TestReconcile_WildcardReferenceNormalizesAcrossReplay(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	// An absent constraint on disk is recorded as the explicit wildcard.
	bare := regapi.Entry{
		ID:   regapi.ParseID("acme.pkg:__dependency.acme.a"),
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/a"}),
	}
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{bare})
	require.Len(t, resolution.References, 1)
	require.Equal(t, "*", resolution.References[0].Version)

	state := regapi.State{root, bare, moduleEntry}
	result, err := handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), resolution)
	require.NoError(t, err)
	require.Equal(t, resolution.Digest, result.Resolution.Digest)
}

func TestReconcile_StoredSelectionMustSatisfyReferenceConstraint(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	// A graph pairing a v1.0.0 selection with a >=2.0.0 reference is invalid at
	// the model level: it cannot pass Valid(), so it cannot enter a durable
	// store, and a hand-fed copy is rejected before replay.
	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	tight := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=2.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{tight})
	require.False(t, resolution.Valid())

	state := regapi.State{root, tight, moduleEntry}
	_, err = handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), resolution)
	require.ErrorContains(t, err, "stored dependency resolution is invalid")

	// Exact-pin spellings carry hub semantics the model deliberately leaves
	// uninterpreted; the handler's satisfaction sweep still rejects a stored
	// selection that does not literally match such a declaration.
	pinnedRoot := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	pinnedRef := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", "2.0.0")
	pinned := hardeningReferencedResolution([]regapi.Entry{pinnedRoot}, []regapi.Entry{pinnedRef})
	require.True(t, pinned.Valid())

	pinnedState := regapi.State{pinnedRoot, pinnedRef, moduleEntry}
	_, err = handler.ReconcileResolution(ctx, fixtureState(pinnedState), fixtureState(pinnedState), pinned)
	require.ErrorContains(t, err, "does not satisfy")
}

func TestExpandChanges_BatchCannotBypassFreshDuplicateGate(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	other := hardeningRoot("app.deps:b", "acme/b", "v1.0.0")

	// acme/a is NOT installed. A single-op duplicate create conflicts; hiding
	// the same create behind another root op in one changeset must conflict
	// identically instead of electing the fresh declaration as controller.
	_, err := handler.ExpandChanges(ctx, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: reference},
		{Kind: regapi.EntryCreate, Entry: other},
	}, fixtureState(regapi.State{root}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")

	// Reversed order must behave the same.
	_, err = handler.ExpandChanges(ctx, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: other},
		{Kind: regapi.EntryCreate, Entry: reference},
	}, fixtureState(regapi.State{root}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")

	// With the component installed, the same batch folds and records the
	// reference under the established root in either order.
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	for _, batch := range []regapi.ChangeSet{
		{{Kind: regapi.EntryCreate, Entry: reference}, {Kind: regapi.EntryCreate, Entry: other}},
		{{Kind: regapi.EntryCreate, Entry: other}, {Kind: regapi.EntryCreate, Entry: reference}},
	} {
		result, err := handler.ExpandChanges(ctx, batch, fixtureState(regapi.State{root, moduleEntry}))
		require.NoError(t, err)
		require.NotNil(t, result.Resolution)
		rootIDs := make([]string, 0, len(result.Resolution.Roots))
		for _, r := range result.Resolution.Roots {
			rootIDs = append(rootIDs, r.ID)
		}
		assert.Contains(t, rootIDs, "app.deps:a", "the established root must keep control")
		require.Len(t, result.Resolution.References, 1)
		assert.Equal(t, "acme.pkg:__dependency.acme.a", result.Resolution.References[0].ID)
	}
}

func TestReconcile_LegacyReferenceUpgradeWorksOffline(t *testing.T) {
	ctx := newTestContext()
	// The hub is unreachable: a cold restart must still upgrade a legacy graph
	// when the stored selection already satisfies every folded reference.
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	legacy := hardeningReferencedResolution([]regapi.Entry{root}, nil)
	require.Empty(t, legacy.BaselineDigest)

	state := regapi.State{root, reference, moduleEntry}
	result, err := handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), legacy)
	require.NoError(t, err)
	require.NotNil(t, result.Resolution)
	require.NotEmpty(t, result.Resolution.BaselineDigest, "offline upgrade must bind the baseline")
	require.Len(t, result.Resolution.References, 1)
	require.Equal(t, "acme.pkg:__dependency.acme.a", result.Resolution.References[0].ID)
	// The stored selection is retained exactly; no re-resolution happened.
	require.Equal(t, legacy.Modules, result.Resolution.Modules)
	require.True(t, result.Resolution.Valid())
}

func TestReconcile_LegacyUpgradeUnsatisfiedReferenceRequiresResolution(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	// The stored v1.0.0 selection cannot satisfy this reference; the upgrade
	// must go through a real resolution, which the unreachable hub fails.
	tight := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=2.0.0")
	legacy := hardeningReferencedResolution([]regapi.Entry{root}, nil)

	state := regapi.State{root, tight, moduleEntry}
	_, err = handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), legacy)
	require.Error(t, err)
}

// Adding parameters to one of two folded declarations transfers control to the
// carrier: parameters are link configuration, and carrying them claims the
// component deliberately. Lifecycle safety does not depend on which
// declaration controls — deleting either leaves the module installed while the
// other remains.
func TestFoldRootDependencyComponents_ParametersAddedLaterTransferControl(t *testing.T) {
	appRoot := desiredDependency{
		entry:      regapi.Entry{ID: regapi.NewID("app.deps", "a"), Kind: regapi.NamespaceDependency},
		definition: DependencyDefinition{Component: "acme/a", Version: ">=1.0.0"},
	}
	pkgRef := desiredDependency{
		entry:      regapi.Entry{ID: regapi.NewID("acme.pkg", "__dependency.acme.a"), Kind: regapi.NamespaceDependency},
		definition: DependencyDefinition{Component: "acme/a", Version: ">=1.0.0"},
	}

	// Parameterless established pair: the lowest canonical key controls.
	roots, _, err := foldRootDependencyComponents([]desiredDependency{appRoot, pkgRef}, nil, true)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Equal(t, "acme.pkg:__dependency.acme.a", roots[0].entry.ID.String())

	// An update adding parameters to the app declaration is not fresh; the
	// carrier wins the election and control transfers without a conflict.
	withParams := appRoot
	withParams.definition.Parameters = []Parameter{{Name: "db", Value: "app:db"}}
	roots, refs, err := foldRootDependencyComponents([]desiredDependency{withParams, pkgRef}, nil, true)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Equal(t, "app.deps:a", roots[0].entry.ID.String())
	require.Len(t, refs, 1)
	require.Equal(t, "acme.pkg:__dependency.acme.a", refs[0].entry.ID.String())
}

// Committed parameter disagreement never wedges boot: reconcile folds
// leniently and replays the stored graph. The next dependency mutation runs
// the strict fold and surfaces the disagreement as a conflict naming both
// declarations, which is the operator's cue to reconcile the spellings.
func TestReconcile_ParameterDisagreementBootsThenConflictsOnNextOperation(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := paramRoot("app.deps:a", "acme/a", "v1.0.0", map[string]any{"db": "app:db"})
	disagreeing := paramRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0", map[string]any{"db": "other:db"})
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	moduleDefinition := hardeningModuleDefinition("acme.a", "acme/a", "v1.0.0")
	state := regapi.State{root, disagreeing, moduleDefinition, moduleEntry}

	transcoder := payload.GetTranscoder(ctx)
	baseline, err := handler.deploymentBaselineDigest(ctx, fixtureState(state), transcoder)
	require.NoError(t, err)
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{disagreeing})
	resolution.BaselineDigest = baseline
	resolution = resolution.Canonical()

	_, err = handler.ReconcileResolution(ctx, fixtureState(state), fixtureState(state), resolution)
	require.NoError(t, err, "committed parameter disagreement must not wedge boot")

	unrelated := hardeningRoot("app.deps:b", "acme/b", ">=1.0.0")
	_, err = handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryCreate, Entry: unrelated},
		fixtureState(state),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "acme/a")
	require.Contains(t, err.Error(), "app.deps:a")
	require.Contains(t, err.Error(), "acme.pkg:__dependency.acme.a")
}

// One changeset deletes the controller and creates a new duplicate. The
// established reference is promoted — a fresh declaration never takes control —
// the newcomer folds under it, and the module survives the batch.
func TestExpandChanges_DeleteControllerAndCreateReferenceInOneBatch(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	newcomer := hardeningRoot("other.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")

	for _, batch := range []regapi.ChangeSet{
		{{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: root.ID}}, {Kind: regapi.EntryCreate, Entry: newcomer}},
		{{Kind: regapi.EntryCreate, Entry: newcomer}, {Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: root.ID}}},
	} {
		result, err := handler.ExpandChanges(ctx, batch, fixtureState(regapi.State{root, reference, moduleEntry}))
		require.NoError(t, err)
		require.NotNil(t, result.Resolution)
		require.Len(t, result.Resolution.Roots, 1)
		assert.Equal(t, "acme.pkg:__dependency.acme.a", result.Resolution.Roots[0].ID)
		require.Len(t, result.Resolution.References, 1)
		assert.Equal(t, "other.pkg:__dependency.acme.a", result.Resolution.References[0].ID)
		for _, op := range result.Additional {
			if op.Operation.Kind == regapi.EntryDelete && op.Operation.Entry.ID.String() == "acme.a:entry" {
				t.Fatalf("batch must not uninstall a module that still has declarations")
			}
		}
	}
}

// Deleting and recreating the same declaration ID in one changeset is a
// replacement of an established declaration, never a fresh install attempt.
func TestExpandChanges_SameIDDeleteRecreateIsReplacement(t *testing.T) {
	ctx := newTestContext()
	handler := referenceFoldHandler(t)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	recreated := hardeningRoot("app.deps:a", "acme/a", ">=1.0.0")
	reference := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")

	result, err := handler.ExpandChanges(ctx, regapi.ChangeSet{
		{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: root.ID}},
		{Kind: regapi.EntryCreate, Entry: recreated},
	}, fixtureState(regapi.State{root, reference, moduleEntry}))
	require.NoError(t, err)
	require.NotNil(t, result.Resolution)
	require.Len(t, result.Resolution.Roots, 1)
	require.Len(t, result.Resolution.References, 1)
	ids := map[string]struct{}{
		result.Resolution.Roots[0].ID:      {},
		result.Resolution.References[0].ID: {},
	}
	require.Contains(t, ids, "app.deps:a")
	require.Contains(t, ids, "acme.pkg:__dependency.acme.a")
}
