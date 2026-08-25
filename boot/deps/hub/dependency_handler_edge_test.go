// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

// --- resolveOperationEntry ---

func TestResolveOperationEntry_FullEntry(t *testing.T) {
	op := regapi.Operation{
		Kind: regapi.EntryCreate,
		Entry: regapi.Entry{
			ID:   regapi.NewID("app", "svc"),
			Kind: "service",
			Data: payload.New("data"),
		},
	}

	entry, ok := resolveOperationEntry(op, nil)
	assert.True(t, ok)
	assert.Equal(t, regapi.NewID("app", "svc"), entry.ID)
}

func TestResolveOperationEntry_FromSnapshot(t *testing.T) {
	op := regapi.Operation{
		Kind:  regapi.EntryUpdate,
		Entry: regapi.Entry{ID: regapi.NewID("app", "svc")},
	}
	snapshot := regapi.State{
		{ID: regapi.NewID("app", "other"), Kind: "other"},
		{ID: regapi.NewID("app", "svc"), Kind: "service", Data: payload.New("data")},
	}

	entry, ok := resolveOperationEntry(op, snapshot)
	assert.True(t, ok)
	assert.Equal(t, "service", entry.Kind)
}

func TestResolveOperationEntry_NotFound(t *testing.T) {
	op := regapi.Operation{
		Kind:  regapi.EntryUpdate,
		Entry: regapi.Entry{ID: regapi.NewID("app", "missing")},
	}

	_, ok := resolveOperationEntry(op, nil)
	assert.False(t, ok)
}

// --- entriesEqual ---

func TestEntriesEqual_BothNilData(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service"}
	b := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service"}
	assert.True(t, entriesEqual(a, b))
}

func TestEntriesEqual_DifferentIDs(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service"}
	b := regapi.Entry{ID: regapi.NewID("ns", "b"), Kind: "service"}
	assert.False(t, entriesEqual(a, b))
}

func TestEntriesEqual_DifferentKinds(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service"}
	b := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "handler"}
	assert.False(t, entriesEqual(a, b))
}

func TestEntriesEqual_OneNilData(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Data: payload.New("x")}
	b := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service"}
	assert.False(t, entriesEqual(a, b))
	assert.False(t, entriesEqual(b, a))
}

func TestEntriesEqual_DifferentFormats(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Data: payload.NewPayload("x", payload.JSON)}
	b := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Data: payload.NewPayload("x", payload.String)}
	assert.False(t, entriesEqual(a, b))
}

func TestEntriesEqual_DifferentData(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Data: payload.New("x")}
	b := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Data: payload.New("y")}
	assert.False(t, entriesEqual(a, b))
}

func TestEntriesEqual_SameData(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Data: payload.NewPayload([]byte(`{"k":"v"}`), payload.JSON)}
	b := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Data: payload.NewPayload([]byte(`{"k":"v"}`), payload.JSON)}
	assert.True(t, entriesEqual(a, b))
}

func TestEntriesEqual_DifferentMeta(t *testing.T) {
	a := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Meta: attrs.NewBagFrom(map[string]any{"x": 1})}
	b := regapi.Entry{ID: regapi.NewID("ns", "a"), Kind: "service", Meta: attrs.NewBagFrom(map[string]any{"x": 2})}
	assert.False(t, entriesEqual(a, b))
}

// --- entryConflict ---

func TestEntryConflict_NoModuleOnDesired(t *testing.T) {
	assert.False(t, entryConflict(regapi.EntryProvenance{}, regapi.EntryProvenance{}))
}

func TestEntryConflict_ExistingIsHostAuthored(t *testing.T) {
	assert.True(t, entryConflict(regapi.EntryProvenance{}, regapi.EntryProvenance{Module: "acme/http"}))
}

func TestEntryConflict_SameModule(t *testing.T) {
	assert.False(t, entryConflict(
		regapi.EntryProvenance{Module: "acme/http"},
		regapi.EntryProvenance{Module: "acme/http"},
	))
}

func TestEntryConflict_DifferentModules(t *testing.T) {
	assert.True(t, entryConflict(
		regapi.EntryProvenance{Module: "acme/http"},
		regapi.EntryProvenance{Module: "acme/grpc"},
	))
}

// --- deployment roots ---

func TestIsRootDependencyTruthTable(t *testing.T) {
	dependency := regapi.Entry{ID: regapi.NewID("workspace.modules", "search"), Kind: regapi.NamespaceDependency}
	other := regapi.Entry{ID: regapi.NewID("workspace.modules", "search"), Kind: "service"}

	cases := []struct {
		name   string
		entry  regapi.Entry
		record regapi.EntryProvenance
		root   bool
	}{
		{"selected root owned by a module", dependency, regapi.EntryProvenance{Module: "acme/app", Root: true}, true},
		{"selected root owned by no module", dependency, regapi.EntryProvenance{Root: true}, true},
		{"module-owned transitive", dependency, regapi.EntryProvenance{Module: "acme/app"}, false},
		{"registry-authored overlay", dependency, regapi.EntryProvenance{}, true},
		{"another kind is never a root", other, regapi.EntryProvenance{Root: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.root, isRootDependency(tc.entry, tc.record))
		})
	}
}

func TestCollectControlledModules_TruthTableOverProvenance(t *testing.T) {
	ctx := newTestContext()
	transcoder := payload.GetTranscoder(ctx)
	handler := &DependencyHandler{}

	dependency := func(name, component string) regapi.Entry {
		return regapi.Entry{
			ID:   regapi.NewID("app.deps", name),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{"component": component, "version": "v1.0.0"}),
		}
	}
	ownedRoot := dependency("owned_root", "acme/owned_root")
	unownedRoot := dependency("unowned_root", "acme/unowned_root")
	transitive := dependency("transitive", "acme/transitive")
	overlay := dependency("overlay", "acme/overlay")

	state := regapi.ProvenancedState{
		Entries: regapi.State{ownedRoot, unownedRoot, transitive, overlay},
		Prov: regapi.ProvMap{
			ownedRoot.ID:   {Module: "acme/deployment", Root: true},
			unownedRoot.ID: {Root: true},
			transitive.ID:  {Module: "acme/deployment"},
			overlay.ID:     {},
		},
	}

	controlled, err := handler.collectControlledModules(ctx, state, transcoder)
	require.NoError(t, err)
	assert.Contains(t, controlled, "acme/owned_root", "a selected root controls its component whoever owns the declaration")
	assert.Contains(t, controlled, "acme/unowned_root")
	assert.Contains(t, controlled, "acme/overlay", "a registry-authored declaration is a user overlay root")
	assert.NotContains(t, controlled, "acme/transitive",
		"a module-owned declaration extends the graph from its owner, which no root reaches here")

	missing := state
	missing.Prov = regapi.ProvMap{
		ownedRoot.ID:   {Module: "acme/deployment", Root: true},
		unownedRoot.ID: {Root: true},
		transitive.ID:  {Module: "acme/deployment"},
	}
	_, err = handler.collectControlledModules(ctx, missing, transcoder)
	require.ErrorIs(t, err, regapi.ErrMissingProvenance, "an entry without a record must fail loud, never read as host-authored")
}

// --- loader ownership rule ---

func TestClaimEntryProvenance_NamespaceOwnerWinsOverLoadingModule(t *testing.T) {
	entry := regapi.Entry{ID: regapi.NewID("acme.session", "delete_session_service")}
	owners := moduleOwnersByNamespace([]ResolvedModule{
		{Org: "acme", Name: "session", Version: "v1.0.0"},
		{Org: "example", Name: "uploads", Version: "v2.0.0"},
	})

	claim := claimEntryProvenance(
		entry,
		moduleOwner{name: "example/uploads", version: "v2.0.0"},
		false, loadedProvenance{}, false, nil, owners,
	)

	assert.Equal(t, "acme/session", claim.record.Module)
	assert.Equal(t, "v1.0.0", claim.record.Version)
}

func TestClaimEntryProvenance_ResidentOwnerWinsOverNamespaceOwner(t *testing.T) {
	id := regapi.NewID("acme.llm.openai_compat", "client")
	entry := regapi.Entry{ID: id}
	resident := provIndex{idKey(id): {Module: "acme/llm", Version: "v4.0.0"}}

	claim := claimEntryProvenance(
		entry,
		moduleOwner{name: "example/skills", version: "v2.0.0"},
		false, loadedProvenance{}, false, resident, nil,
	)

	assert.Equal(t, "acme/llm", claim.record.Module)
	assert.Equal(t, "v4.0.0", claim.record.Version)
}

func TestClaimEntryProvenance_ResidentOwnerRefreshesToLoadingIdentity(t *testing.T) {
	id := regapi.NewID("acme.llm", "client")
	entry := regapi.Entry{ID: id}
	resident := provIndex{idKey(id): {Module: "acme/llm", Version: "v4.0.0", Digest: "sha256:old"}}

	claim := claimEntryProvenance(
		entry,
		moduleOwner{name: "acme/llm", version: "v5.0.0", digest: "sha256:new"},
		false, loadedProvenance{}, false, resident, nil,
	)

	assert.Equal(t, regapi.EntryProvenance{Module: "acme/llm", Version: "v5.0.0", Digest: "sha256:new"}, claim.record)
}

func TestClaimEntryProvenance_ReplacementWinsUnconditionally(t *testing.T) {
	id := regapi.NewID("acme.llm", "client")
	entry := regapi.Entry{ID: id}
	resident := provIndex{idKey(id): {Module: "acme/other", Version: "v4.0.0"}}
	owners := moduleOwnersByNamespace([]ResolvedModule{{Org: "acme", Name: "llm", Version: "v9.0.0"}})

	claim := claimEntryProvenance(
		entry,
		moduleOwner{name: "acme/dev", digest: "sha256-tree-v1:abc"},
		true, loadedProvenance{}, false, resident, owners,
	)

	require.True(t, claim.replacement)
	assert.Equal(t, "acme/dev", claim.record.Module)
	assert.Empty(t, claim.record.Version, "an unpublished development tree has no version; the digest is its identity")
}

func TestClaimEntryProvenance_NonReplacementNeverDisplacesReplacement(t *testing.T) {
	id := regapi.NewID("acme.llm", "client")
	entry := regapi.Entry{ID: id}
	staged := loadedProvenance{
		record:      regapi.EntryProvenance{Module: "acme/dev", Digest: "sha256-tree-v1:abc"},
		replacement: true,
	}

	claim := claimEntryProvenance(
		entry,
		moduleOwner{name: "acme/llm", version: "v1.0.0"},
		false, staged, true, nil, nil,
	)

	assert.Equal(t, staged, claim)
}

func TestPreserveHostSnapshotEntry_KeepsExistingUnownedEntry(t *testing.T) {
	id := regapi.NewID("app", "api")
	existing := regapi.Entry{
		ID:   id,
		Kind: "http.router",
		Data: payload.NewPayload(`{"name":"host"}`, payload.JSON),
	}
	loaded := regapi.Entry{
		ID:   id,
		Kind: "http.router",
		Meta: attrs.NewBag(),
		Data: payload.NewPayload(`{"name":"packed"}`, payload.JSON),
	}

	result, ok := preserveHostSnapshotEntry(
		loaded,
		"acme/security",
		map[string]regapi.Entry{idKey(id): existing},
		provIndex{idKey(id): {}},
		map[string]struct{}{"acme/security": {}},
	)

	require.True(t, ok)
	assert.Equal(t, existing.Data, result.Data)
}

func TestPreserveHostSnapshotEntry_ModuleOwnedResidentEntryIsNotHost(t *testing.T) {
	id := regapi.NewID("app", "api")
	existing := regapi.Entry{ID: id, Kind: "http.router", Data: payload.NewPayload(`{"name":"resident"}`, payload.JSON)}
	loaded := regapi.Entry{ID: id, Kind: "http.router", Data: payload.NewPayload(`{"name":"packed"}`, payload.JSON)}

	_, ok := preserveHostSnapshotEntry(
		loaded,
		"acme/security",
		map[string]regapi.Entry{idKey(id): existing},
		provIndex{idKey(id): {Module: "acme/security", Version: "v1.0.0"}},
		map[string]struct{}{"acme/security": {}},
	)

	assert.False(t, ok, "a module-owned resident entry is not a host entry")
}

// --- parseExpectedDigest ---

func TestParseExpectedDigest_Empty(t *testing.T) {
	_, _, err := parseExpectedDigest("")
	assert.Error(t, err)
}

func TestParseExpectedDigest_Whitespace(t *testing.T) {
	_, _, err := parseExpectedDigest("   ")
	assert.Error(t, err)
}

func TestParseExpectedDigest_BareHash(t *testing.T) {
	alg, val, err := parseExpectedDigest("abcdef1234")
	require.NoError(t, err)
	assert.Equal(t, "sha256", alg)
	assert.Equal(t, "abcdef1234", val)
}

func TestParseExpectedDigest_WithAlgorithm(t *testing.T) {
	alg, val, err := parseExpectedDigest("sha256:abcdef1234")
	require.NoError(t, err)
	assert.Equal(t, "sha256", alg)
	assert.Equal(t, "abcdef1234", val)
}

func TestParseExpectedDigest_UppercaseAlgorithm(t *testing.T) {
	alg, val, err := parseExpectedDigest("SHA256:ABCDEF1234")
	require.NoError(t, err)
	assert.Equal(t, "sha256", alg)
	assert.Equal(t, "ABCDEF1234", val)
}

func TestParseExpectedDigest_EmptyParts(t *testing.T) {
	_, _, err := parseExpectedDigest(":")
	assert.Error(t, err)
}

func TestParseExpectedDigest_EmptyValue(t *testing.T) {
	_, _, err := parseExpectedDigest("sha256:")
	assert.Error(t, err)
}

// --- unwrapPayloadData ---

func TestUnwrapPayloadData_NonMap(t *testing.T) {
	assert.Equal(t, "hello", unwrapPayloadData("hello"))
	assert.Equal(t, 42, unwrapPayloadData(42))
	assert.Nil(t, unwrapPayloadData(nil))
}

func TestUnwrapPayloadData_MapWithoutDataFormat(t *testing.T) {
	m := map[string]any{"key": "value"}
	assert.Equal(t, m, unwrapPayloadData(m))
}

func TestUnwrapPayloadData_MapWithDataFormat(t *testing.T) {
	m := map[string]any{
		"Data":   "inner-data",
		"Format": "json",
	}
	assert.Equal(t, "inner-data", unwrapPayloadData(m))
}

func TestUnwrapPayloadData_MapWithExtraKeys(t *testing.T) {
	m := map[string]any{
		"Data":   "inner-data",
		"Format": "json",
		"Extra":  "ignored",
	}
	// 3 keys, not exactly 2, so not unwrapped
	assert.Equal(t, m, unwrapPayloadData(m))
}

// --- operationPlanner ---

func planTestOperations(
	current regapi.State,
	desired []regapi.Entry,
	originalID regapi.ID,
	controlledModules map[string]struct{},
	mutableModules map[string]struct{},
) ([]regapi.Operation, error) {
	return (operationPlanner{}).plan(fixtureState(current), fixtureState(desired), operationPlanOptions{
		originalKey:       idKey(originalID),
		controlledModules: controlledModules,
		mutableModules:    mutableModules,
	})
}

func TestOperationPlanner_EmptyBoth(t *testing.T) {
	ops, err := planTestOperations(nil, nil, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestOperationPlanner_NewEntries(t *testing.T) {
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{ID: regapi.NewID("app", "svc"), Kind: "service", Data: payload.New("data")},
	}

	ops, err := planTestOperations(nil, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1) // dep is excluded (originalID)
	assert.Equal(t, regapi.EntryCreate, ops[0].Kind)
	assert.Equal(t, regapi.NewID("app", "svc"), ops[0].Entry.ID)
}

func TestOperationPlanner_DeletedEntries(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{
			ID:   regapi.NewID("app", "old-svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "acme/http"}),
		},
	}
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
	}

	ops, err := planTestOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryDelete, ops[0].Kind)
	assert.Equal(t, regapi.NewID("app", "old-svc"), ops[0].Entry.ID)
}

func TestOperationPlanner_DeletesOnlyControlledModules(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{
			ID:   regapi.NewID("app", "old-svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "acme/http"}),
		},
		{
			ID:   regapi.NewID("example.tools", "dependencies"),
			Kind: "function.lua",
			Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "example/keeper"}),
		},
	}
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
	}
	controlled := map[string]struct{}{"acme/http": {}}

	ops, err := planTestOperations(current, desired, regapi.NewID("app", "dep"), controlled, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryDelete, ops[0].Kind)
	assert.Equal(t, regapi.NewID("app", "old-svc"), ops[0].Entry.ID)
}

func TestOperationPlanner_UpdatedEntries(t *testing.T) {
	current := regapi.State{
		{
			ID:   regapi.NewID("app", "svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "acme/http"}),
			Data: payload.New("old"),
		},
	}
	desired := []regapi.Entry{
		{
			ID:   regapi.NewID("app", "svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "acme/http"}),
			Data: payload.New("new"),
		},
	}

	ops, err := planTestOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
}

func TestOperationPlanner_ReconcileSkipsUntouchedImmutableModuleUpdates(t *testing.T) {
	moduleMeta := attrs.NewBagFrom(map[string]any{
		fixtureModuleKey:        "acme/http",
		fixtureModuleVersionKey: "v1.0.0",
	})
	current := regapi.State{{
		ID:   regapi.NewID("app", "svc"),
		Kind: "service",
		Meta: moduleMeta,
		Data: payload.New("resident"),
	}}
	desired := []regapi.Entry{{
		ID:   regapi.NewID("app", "svc"),
		Kind: "service",
		Meta: moduleMeta,
		Data: payload.New("normalized"),
	}}

	ops, err := planTestOperations(current, desired, regapi.ID{}, map[string]struct{}{"acme/http": {}}, map[string]struct{}{})
	require.NoError(t, err)
	require.Empty(t, ops, "an unchanged immutable artifact must not be rewritten during history reconciliation")

	ops, err = planTestOperations(current, desired, regapi.ID{}, map[string]struct{}{"acme/http": {}}, map[string]struct{}{"acme/http": {}})
	require.NoError(t, err)
	require.Len(t, ops, 1, "an explicitly mutable artifact must still update")
	assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
}

func TestOperationPlanner_KindChangeUsesDeleteCreate(t *testing.T) {
	id := regapi.NewID("ui", "assets")
	current := regapi.State{{
		ID:   id,
		Kind: "fs.embed",
		Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "acme/ui"}),
		Data: payload.New(map[string]any{}),
	}}
	desired := []regapi.Entry{{
		ID:   id,
		Kind: "fs.directory",
		Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "acme/ui"}),
		Data: payload.New(map[string]any{"directory": "assets", "base": "module"}),
	}}

	ops, err := planTestOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	require.Len(t, ops, 2)
	assert.Equal(t, regapi.EntryDelete, ops[0].Kind)
	assert.Equal(t, regapi.Kind("fs.embed"), ops[0].Entry.Kind)
	assert.Equal(t, regapi.EntryCreate, ops[1].Kind)
	assert.Equal(t, regapi.Kind("fs.directory"), ops[1].Entry.Kind)
}

func TestOperationPlanner_UnchangedEntries(t *testing.T) {
	entry := regapi.Entry{
		ID:   regapi.NewID("app", "svc"),
		Kind: "service",
		Data: payload.NewPayload([]byte(`{"ok":true}`), payload.JSON),
	}
	current := regapi.State{entry}
	desired := []regapi.Entry{entry}

	ops, err := planTestOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestOperationPlanner_ConflictError(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "svc"), Kind: "service", Data: payload.New("local")},
	}
	desired := []regapi.Entry{
		{
			ID:   regapi.NewID("app", "svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{fixtureModuleKey: "acme/http"}),
			Data: payload.New("module"),
		},
	}

	_, err := planTestOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestOperationPlanner_SkipsHostAuthoredDeletes(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{ID: regapi.NewID("app", "local-svc"), Kind: "service", Data: payload.New("local")},
	}
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
	}

	ops, err := planTestOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, ops, "no module owns local-svc, so dependency reconciliation cannot delete it")
}

func TestOperationPlanner_MissingProvenanceIsAHardError(t *testing.T) {
	orphan := regapi.Entry{ID: regapi.NewID("app", "orphan"), Kind: "service", Data: payload.New("orphan")}
	current := regapi.ProvenancedState{
		Entries: regapi.State{orphan},
		Prov:    regapi.ProvMap{},
	}

	_, err := (operationPlanner{}).plan(current, regapi.ProvenancedState{}, operationPlanOptions{
		controlledModules: map[string]struct{}{"acme/http": {}},
	})
	require.ErrorIs(t, err, regapi.ErrMissingProvenance,
		"an entry without a record is neither host-authored nor deletable; it is an invariant violation")
	assert.Contains(t, err.Error(), orphan.ID.String())
}

// --- formatResolutionErrors ---

func TestFormatResolutionErrors_Empty(t *testing.T) {
	assert.Empty(t, formatResolutionErrors(nil))
}

func TestFormatResolutionErrors_Single(t *testing.T) {
	errs := []ResolutionError{
		{Org: "acme", Name: "http", Constraint: "^1.0.0", Message: "no match"},
	}
	result := formatResolutionErrors(errs)
	assert.Contains(t, result, "no match")
}

func TestFormatResolutionErrors_Multiple(t *testing.T) {
	errs := []ResolutionError{
		{Org: "acme", Name: "http", Message: "no match"},
		{Org: "acme", Name: "grpc", Message: "conflict"},
	}
	result := formatResolutionErrors(errs)
	assert.Contains(t, result, "no match")
	assert.Contains(t, result, "; ")
	assert.Contains(t, result, "conflict")
}

// --- NewDependencyResolutionErrors auth hint ---

func TestNewDependencyResolutionErrors_AuthHint(t *testing.T) {
	errs := []ResolutionError{
		{Org: "acme", Name: "http", Constraint: "^1.0.0", Message: ErrNotAuthenticated.Error(), Err: ErrNotAuthenticated},
	}

	apiErr := NewDependencyResolutionErrors(errs)
	assert.Equal(t, apierror.Conflict, apiErr.Kind())
	assert.Equal(t, registryAuthHint, apiErr.Details().GetString("hint", ""))
}

func TestNewDependencyResolutionErrors_AuthHintWrapped(t *testing.T) {
	wrapped := fmt.Errorf("fetch manifest: %w", ErrNotAuthenticated)
	errs := []ResolutionError{
		{Org: "acme", Name: "http", Message: wrapped.Error(), Err: wrapped},
	}

	apiErr := NewDependencyResolutionErrors(errs)
	assert.Equal(t, registryAuthHint, apiErr.Details().GetString("hint", ""))
}

func TestNewDependencyResolutionErrors_NoHintWithoutAuthCause(t *testing.T) {
	errs := []ResolutionError{
		{Org: "acme", Name: "http", Message: "no match", Err: errors.New("no match")},
	}

	apiErr := NewDependencyResolutionErrors(errs)
	assert.Empty(t, apiErr.Details().GetString("hint", ""))
}

func TestNewDependencyResolutionErrors_MessageIncludesCause(t *testing.T) {
	errs := []ResolutionError{
		{Org: "acme", Name: "research", Constraint: "0.1.0", Message: "module not found", Err: errors.New("module not found")},
	}

	apiErr := NewDependencyResolutionErrors(errs)
	assert.Contains(t, apiErr.Error(), "acme/research@0.1.0")
	assert.Contains(t, apiErr.Error(), "module not found")
}

// --- verifyDownloadedArtifact ---

func TestVerifyDownloadedArtifact_NonExistentFile(t *testing.T) {
	err := verifyDownloadedArtifact("/nonexistent/path.wapp", "", 0)
	assert.Error(t, err)
}

func TestVerifyDownloadedArtifact_SizeMismatch(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.wapp")
	require.NoError(t, os.WriteFile(tmpFile, []byte("hello"), 0600))

	err := verifyDownloadedArtifact(tmpFile, "", 999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "size mismatch")
}

func TestVerifyDownloadedArtifact_EmptyDigest(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.wapp")
	require.NoError(t, os.WriteFile(tmpFile, []byte("hello"), 0600))

	err := verifyDownloadedArtifact(tmpFile, "", 0)
	assert.NoError(t, err)
}

func TestVerifyDownloadedArtifact_UnsupportedAlgorithm(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.wapp")
	require.NoError(t, os.WriteFile(tmpFile, []byte("hello"), 0600))

	err := verifyDownloadedArtifact(tmpFile, "md5:abc", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported digest algorithm")
}

func TestVerifyDownloadedArtifact_DigestMismatch(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.wapp")
	require.NoError(t, os.WriteFile(tmpFile, []byte("hello"), 0600))

	err := verifyDownloadedArtifact(tmpFile, "sha256:0000000000000000000000000000000000000000000000000000000000000000", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "digest mismatch")
}

func TestVerifyDownloadedArtifact_ValidDigest(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.wapp")
	data := []byte("hello")
	require.NoError(t, os.WriteFile(tmpFile, data, 0600))

	hash, err := sha256FileHex(tmpFile)
	require.NoError(t, err)

	err = verifyDownloadedArtifact(tmpFile, "sha256:"+hash, uint64(len(data)))
	assert.NoError(t, err)
}

// --- modKey ---

func TestModKey(t *testing.T) {
	assert.Equal(t, "acme/http@v1.0.0", modKey(ResolvedModule{Org: "acme", Name: "http", Version: "v1.0.0"}))
}

// --- exists ---

func TestExists_True(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0600))
	assert.True(t, exists(tmpFile))
}

func TestExists_False(t *testing.T) {
	assert.False(t, exists("/nonexistent/path"))
}

// --- DependencyHandler.Expand edge cases ---

func TestDependencyHandler_Expand_NilHandler(t *testing.T) {
	var h *DependencyHandler
	_, err := h.Expand(newTestContext(), regapi.Operation{}, regapi.ProvenancedState{})
	require.Error(t, err)
}

func TestDependencyHandler_Expand_NilHub(t *testing.T) {
	h := &DependencyHandler{}
	_, err := h.Expand(newTestContext(), regapi.Operation{}, regapi.ProvenancedState{})
	require.Error(t, err)
}

func TestDependencyHandler_Expand_NonDependencyKind(t *testing.T) {
	h := &DependencyHandler{hub: &fakeHub{}}

	entry := regapi.Entry{
		ID:   regapi.NewID("app", "svc"),
		Kind: "service",
		Data: payload.New("data"),
	}
	op := regapi.Operation{Kind: regapi.EntryCreate, Entry: entry}

	result, err := h.Expand(newTestContext(), op, regapi.ProvenancedState{})
	require.NoError(t, err)
	assert.False(t, result.Applied)
}

func TestDependencyHandler_Expand_EntryNotFound(t *testing.T) {
	h := &DependencyHandler{hub: &fakeHub{}}

	op := regapi.Operation{
		Kind:  regapi.EntryUpdate,
		Entry: regapi.Entry{ID: regapi.NewID("app", "missing")},
	}

	result, err := h.Expand(newTestContext(), op, regapi.ProvenancedState{})
	require.NoError(t, err)
	assert.False(t, result.Applied)
}
