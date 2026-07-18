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
		regapi.State{root, moduleEntry},
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
		regapi.State{root},
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
		regapi.State{root, moduleEntry},
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
		regapi.State{root, reference, moduleEntry},
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
		regapi.State{root, reference, moduleEntry},
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
	result, err := handler.ReconcileResolution(ctx, state, state, resolution)
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
	_, err = handler.ReconcileResolution(ctx, state, state, resolution)
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
	baseline, err := handler.deploymentBaselineDigest(ctx, state, transcoder)
	require.NoError(t, err)
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, nil)
	resolution.BaselineDigest = baseline
	resolution = resolution.Canonical()

	_, err = handler.ReconcileResolution(ctx, state, state, resolution)
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
	result, err := handler.ReconcileResolution(ctx, state, state, legacy)
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
	_, err = handler.ReconcileResolution(ctx, state, state, resolution)
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
	result, err := handler.ReconcileResolution(ctx, state, state, resolution)
	require.NoError(t, err)
	require.Equal(t, resolution.Digest, result.Resolution.Digest)
}

func TestReconcile_StoredSelectionMustSatisfyReferenceConstraint(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	tight := hardeningRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=2.0.0")
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	// The stored graph inconsistently pairs a v1.0.0 selection with a >=2.0.0
	// reference; the satisfaction sweep must reject it.
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{tight})

	state := regapi.State{root, tight, moduleEntry}
	_, err = handler.ReconcileResolution(ctx, state, state, resolution)
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
	}, regapi.State{root})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")

	// Reversed order must behave the same.
	_, err = handler.ExpandChanges(ctx, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: other},
		{Kind: regapi.EntryCreate, Entry: reference},
	}, regapi.State{root})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")

	// With the component installed, the same batch folds and records the
	// reference under the established root in either order.
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	for _, batch := range []regapi.ChangeSet{
		{{Kind: regapi.EntryCreate, Entry: reference}, {Kind: regapi.EntryCreate, Entry: other}},
		{{Kind: regapi.EntryCreate, Entry: other}, {Kind: regapi.EntryCreate, Entry: reference}},
	} {
		result, err := handler.ExpandChanges(ctx, batch, regapi.State{root, moduleEntry})
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
