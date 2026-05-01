// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
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
	existing := regapi.Entry{ID: regapi.NewID("ns", "a")}
	desired := regapi.Entry{ID: regapi.NewID("ns", "a")}
	assert.False(t, entryConflict(existing, desired))
}

func TestEntryConflict_ExistingHasNoModule(t *testing.T) {
	existing := regapi.Entry{ID: regapi.NewID("ns", "a")}
	desired := regapi.Entry{
		ID:   regapi.NewID("ns", "a"),
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
	}
	assert.True(t, entryConflict(existing, desired))
}

func TestEntryConflict_SameModule(t *testing.T) {
	existing := regapi.Entry{
		ID:   regapi.NewID("ns", "a"),
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
	}
	desired := regapi.Entry{
		ID:   regapi.NewID("ns", "a"),
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
	}
	assert.False(t, entryConflict(existing, desired))
}

func TestEntryConflict_DifferentModules(t *testing.T) {
	existing := regapi.Entry{
		ID:   regapi.NewID("ns", "a"),
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
	}
	desired := regapi.Entry{
		ID:   regapi.NewID("ns", "a"),
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/grpc"}),
	}
	assert.True(t, entryConflict(existing, desired))
}

// --- entryModule ---

func TestEntryModule_NilMeta(t *testing.T) {
	assert.Empty(t, entryModule(regapi.Entry{}))
}

func TestEntryModule_NoModuleKey(t *testing.T) {
	e := regapi.Entry{Meta: attrs.NewBagFrom(map[string]any{"other": "val"})}
	assert.Empty(t, entryModule(e))
}

func TestEntryModule_NonStringModule(t *testing.T) {
	e := regapi.Entry{Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: 42})}
	assert.Empty(t, entryModule(e))
}

func TestEntryModule_ValidModule(t *testing.T) {
	e := regapi.Entry{Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"})}
	assert.Equal(t, "acme/http", entryModule(e))
}

// --- markModuleMeta ---

func TestMarkModuleMeta_NilMeta(t *testing.T) {
	e := regapi.Entry{ID: regapi.NewID("ns", "a")}
	result := markModuleMeta(e, "acme/http", "v1.0.0")
	assert.Equal(t, "acme/http", entryModule(result))
	assert.Equal(t, "v1.0.0", result.Meta.GetString(metaModuleVersionKey, ""))
}

func TestMarkModuleMeta_ExistingMeta(t *testing.T) {
	e := regapi.Entry{
		ID:   regapi.NewID("ns", "a"),
		Meta: attrs.NewBagFrom(map[string]any{"existing": true}),
	}
	result := markModuleMeta(e, "acme/http", "v2.0.0")
	assert.Equal(t, "acme/http", entryModule(result))
	assert.Equal(t, true, result.Meta.GetBool("existing", false))
}

func TestMarkModuleMeta_EmptyVersion(t *testing.T) {
	e := regapi.Entry{ID: regapi.NewID("ns", "a")}
	result := markModuleMeta(e, "acme/http", "")
	assert.Equal(t, "acme/http", entryModule(result))
	assert.Empty(t, result.Meta.GetString(metaModuleVersionKey, ""))
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

// --- buildOperations ---

func TestBuildOperations_EmptyBoth(t *testing.T) {
	ops, err := buildOperations(nil, nil, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestBuildOperations_NewEntries(t *testing.T) {
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{ID: regapi.NewID("app", "svc"), Kind: "service", Data: payload.New("data")},
	}

	ops, err := buildOperations(nil, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1) // dep is excluded (originalID)
	assert.Equal(t, regapi.EntryCreate, ops[0].Kind)
	assert.Equal(t, regapi.NewID("app", "svc"), ops[0].Entry.ID)
}

func TestBuildOperations_DeletedEntries(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{
			ID:   regapi.NewID("app", "old-svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
		},
	}
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
	}

	ops, err := buildOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryDelete, ops[0].Kind)
	assert.Equal(t, regapi.NewID("app", "old-svc"), ops[0].Entry.ID)
}

func TestBuildOperations_DeletesOnlyControlledModules(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{
			ID:   regapi.NewID("app", "old-svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
		},
		{
			ID:   regapi.NewID("keeper.hub.tools", "dependencies"),
			Kind: "function.lua",
			Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "keeper/keeper"}),
		},
	}
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
	}
	controlled := map[string]struct{}{"acme/http": {}}

	ops, err := buildOperations(current, desired, regapi.NewID("app", "dep"), controlled, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryDelete, ops[0].Kind)
	assert.Equal(t, regapi.NewID("app", "old-svc"), ops[0].Entry.ID)
}

func TestBuildOperations_UpdatedEntries(t *testing.T) {
	current := regapi.State{
		{
			ID:   regapi.NewID("app", "svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
			Data: payload.New("old"),
		},
	}
	desired := []regapi.Entry{
		{
			ID:   regapi.NewID("app", "svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
			Data: payload.New("new"),
		},
	}

	ops, err := buildOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
}

func TestBuildOperations_UnchangedEntries(t *testing.T) {
	entry := regapi.Entry{
		ID:   regapi.NewID("app", "svc"),
		Kind: "service",
		Data: payload.NewPayload([]byte(`{"ok":true}`), payload.JSON),
	}
	current := regapi.State{entry}
	desired := []regapi.Entry{entry}

	ops, err := buildOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestBuildOperations_ConflictError(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "svc"), Kind: "service", Data: payload.New("local")},
	}
	desired := []regapi.Entry{
		{
			ID:   regapi.NewID("app", "svc"),
			Kind: "service",
			Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/http"}),
			Data: payload.New("module"),
		},
	}

	_, err := buildOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
}

func TestBuildOperations_SkipsNonModuleDeletes(t *testing.T) {
	current := regapi.State{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
		{ID: regapi.NewID("app", "local-svc"), Kind: "service", Data: payload.New("local")},
	}
	desired := []regapi.Entry{
		{ID: regapi.NewID("app", "dep"), Kind: "ns.dependency"},
	}

	ops, err := buildOperations(current, desired, regapi.NewID("app", "dep"), nil, nil)
	require.NoError(t, err)
	// local-svc has no module meta, so it won't be deleted
	assert.Empty(t, ops)
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
	_, err := h.Expand(newTestContext(), regapi.Operation{}, nil)
	require.Error(t, err)
}

func TestDependencyHandler_Expand_NilHub(t *testing.T) {
	h := &DependencyHandler{}
	_, err := h.Expand(newTestContext(), regapi.Operation{}, nil)
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

	result, err := h.Expand(newTestContext(), op, nil)
	require.NoError(t, err)
	assert.False(t, result.Applied)
}

func TestDependencyHandler_Expand_EntryNotFound(t *testing.T) {
	h := &DependencyHandler{hub: &fakeHub{}}

	op := regapi.Operation{
		Kind:  regapi.EntryUpdate,
		Entry: regapi.Entry{ID: regapi.NewID("app", "missing")},
	}

	result, err := h.Expand(newTestContext(), op, nil)
	require.NoError(t, err)
	assert.False(t, result.Applied)
}
