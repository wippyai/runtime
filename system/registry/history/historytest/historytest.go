// SPDX-License-Identifier: MPL-2.0

// Package historytest checks that a registry history driver has the canonical
// history semantics.
package historytest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
)

// History is the capability set that every durable driver implements.
type History interface {
	registry.ResolutionHeadCASHistory
	registry.HeadCASHistory
	registry.ChangeSetReplayer
	registry.VersionLookup
	registry.VersionIDBounds
}

// Run checks one new, empty history from open for each case.
func Run(t *testing.T, open func(t *testing.T) History) {
	t.Helper()
	cases := map[string]func(*testing.T, History){
		"save and read":              testSaveAndRead,
		"reject existing version":    testRejectExistingVersion,
		"save head conflict":         testSaveHeadConflict,
		"branch and set head":        testBranchAndSetHead,
		"compare and set head":       testCompareAndSetHead,
		"resolution inheritance":     testResolutionInheritance,
		"resolution checkpoint":      testResolutionCheckpoint,
		"resolution head conflict":   testResolutionHeadConflict,
		"resolution baseline rebase": testResolutionRebase,
		"replay lineage":             testReplayLineage,
		"reject invalid resolution":  testRejectInvalidResolution,
	}
	for name, check := range cases {
		t.Run(name, func(t *testing.T) { check(t, open(t)) })
	}
}

// Entry returns a registry entry with metadata, payload, and ownership data.
func Entry(name string, value any) registry.Entry {
	return registry.Entry{
		ID:       registry.ID{NS: "app", Name: name},
		Kind:     "registry.entry",
		Meta:     attrs.Bag{"comment": name, "tags": []any{"a", "b"}},
		Data:     payload.NewPayload(map[string]any{"value": value, "nested": map[string]any{"n": int64(1)}}, payload.Golang),
		Registry: registry.EntryMetadata{Root: true},
	}
}

// Changes returns a changeset that creates and updates one entry.
func Changes(name string, value any) registry.ChangeSet {
	original := Entry(name, "before")
	return registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: Entry(name, value)},
		{Kind: registry.EntryUpdate, Entry: Entry(name, value), OriginalEntry: &original},
	}
}

// Resolution returns a valid resolution for a baseline.
func Resolution(input, baseline string) *registry.DependencyResolution {
	return (&registry.DependencyResolution{
		InputDigest:    input,
		BaselineDigest: baseline,
		Modules:        []registry.ResolvedModule{{Name: "acme/" + input, Version: "v1.0.0", Digest: "sha256:" + input}},
	}).Canonical()
}

func child(parent registry.Version, id uint) registry.Version {
	return version.FromParent(parent, id)
}

func root() registry.Version { return version.New(registry.RootVersion) }

func requireChanges(t *testing.T, expected, actual registry.ChangeSet) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].Kind, actual[i].Kind)
		requireEntry(t, expected[i].Entry, actual[i].Entry)
		if expected[i].OriginalEntry == nil {
			require.Nil(t, actual[i].OriginalEntry)
			continue
		}
		require.NotNil(t, actual[i].OriginalEntry)
		requireEntry(t, *expected[i].OriginalEntry, *actual[i].OriginalEntry)
	}
}

func requireEntry(t *testing.T, expected, actual registry.Entry) {
	t.Helper()
	require.Equal(t, expected.ID, actual.ID)
	require.Equal(t, expected.Kind, actual.Kind)
	require.Equal(t, expected.Registry, actual.Registry)
	require.Equal(t, expected.Meta["comment"], actual.Meta["comment"])
	require.NotNil(t, actual.Data)
	require.Equal(t, expected.Data.Format(), actual.Data.Format())
	data, ok := actual.Data.Data().(map[string]any)
	require.True(t, ok)
	require.Equal(t, expected.Data.Data().(map[string]any)["value"], data["value"])
}

func requireHead(t *testing.T, h History, id uint) {
	t.Helper()
	head, err := h.Head()
	require.NoError(t, err)
	require.Equal(t, id, head.ID())
}

func testSaveAndRead(t *testing.T, h History) {
	v1 := child(root(), 1)
	changes := Changes("one", "first")
	require.NoError(t, h.Save(v1, changes, true))
	requireHead(t, h, 1)

	stored, err := h.Get(v1)
	require.NoError(t, err)
	requireChanges(t, changes, stored)

	lookup, err := h.GetVersion(1)
	require.NoError(t, err)
	require.Equal(t, uint(1), lookup.ID())
	require.Equal(t, registry.RootVersion, lookup.Previous().ID())

	maxID, err := h.MaxVersionID()
	require.NoError(t, err)
	require.Equal(t, uint(1), maxID)

	versions, err := h.Versions()
	require.NoError(t, err)
	ids := make([]uint, 0, len(versions))
	for _, v := range versions {
		ids = append(ids, v.ID())
	}
	require.Equal(t, []uint{0, 1}, ids)
	require.Equal(t, uint(0), versions[1].Previous().ID())

	_, err = h.GetVersion(9)
	require.Error(t, err)
	_, err = h.Get(child(root(), 9))
	require.Error(t, err)
}

func testRejectExistingVersion(t *testing.T, h History) {
	v1 := child(root(), 1)
	require.NoError(t, h.Save(v1, Changes("one", "first"), true))
	require.Error(t, h.Save(v1, Changes("one", "second"), false))
	stored, err := h.Get(v1)
	require.NoError(t, err)
	requireChanges(t, Changes("one", "first"), stored)
}

func testSaveHeadConflict(t *testing.T, h History) {
	v1 := child(root(), 1)
	require.NoError(t, h.Save(v1, Changes("one", "first"), true))
	err := h.Save(child(root(), 2), Changes("two", "second"), true)
	require.ErrorContains(t, err, "history head changed")
	_, err = h.GetVersion(2)
	require.Error(t, err)
	requireHead(t, h, 1)
}

func testBranchAndSetHead(t *testing.T, h History) {
	v1 := child(root(), 1)
	require.NoError(t, h.Save(v1, Changes("one", "first"), true))
	v2 := child(root(), 2)
	require.NoError(t, h.Save(v2, Changes("two", "second"), false))
	requireHead(t, h, 1)
	require.NoError(t, h.SetHead(v2))
	requireHead(t, h, 2)
	require.Error(t, h.SetHead(child(root(), 9)))
	requireHead(t, h, 2)
	require.NoError(t, h.SetHead(root()))
	requireHead(t, h, 0)
}

func testCompareAndSetHead(t *testing.T, h History) {
	v1 := child(root(), 1)
	v2 := child(v1, 2)
	require.NoError(t, h.Save(v1, Changes("one", "first"), true))
	require.NoError(t, h.Save(v2, Changes("two", "second"), true))
	require.ErrorContains(t, h.CompareAndSetHead(v1, root()), "history head changed")
	requireHead(t, h, 2)
	require.NoError(t, h.CompareAndSetHead(v2, v1))
	requireHead(t, h, 1)
	require.Error(t, h.CompareAndSetHead(v1, child(root(), 9)))
	requireHead(t, h, 1)
}

func testResolutionInheritance(t *testing.T, h History) {
	v1 := child(root(), 1)
	v2 := child(v1, 2)
	first := Resolution("first", "")
	_, err := h.GetDependencyResolution(root())
	require.ErrorIs(t, err, registry.ErrDependencyResolutionNotFound)
	require.NoError(t, h.SaveWithDependencyResolution(v1, Changes("one", "first"), first, true))
	require.NoError(t, h.Save(v2, Changes("two", "second"), true))
	for _, v := range []registry.Version{v1, v2} {
		stored, err := h.GetDependencyResolution(v)
		require.NoError(t, err)
		require.Equal(t, first.Digest, stored.Digest)
		require.Equal(t, first.Modules, stored.Modules)
	}
}

func testResolutionCheckpoint(t *testing.T, h History) {
	v1 := child(root(), 1)
	require.NoError(t, h.Save(v1, Changes("one", "first"), true))
	_, err := h.GetDependencyResolution(v1)
	require.ErrorIs(t, err, registry.ErrDependencyResolutionNotFound)
	first := Resolution("first", "")
	require.NoError(t, h.CheckpointDependencyResolution(v1, first))
	require.NoError(t, h.CheckpointDependencyResolution(v1, first))
	require.Error(t, h.CheckpointDependencyResolution(v1, Resolution("second", "")))
	require.Error(t, h.CheckpointDependencyResolution(child(root(), 9), first))
	stored, err := h.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, first.Digest, stored.Digest)
}

func testResolutionHeadConflict(t *testing.T, h History) {
	v1 := child(root(), 1)
	v2 := child(v1, 2)
	first := Resolution("first", "base")
	require.NoError(t, h.SaveWithDependencyResolution(v1, Changes("one", "first"), first, true))
	require.NoError(t, h.Save(v2, Changes("two", "second"), true))
	require.ErrorContains(t, h.CompareAndSetHeadWithDependencyResolution(v1, v1, first), "history head changed")
	requireHead(t, h, 2)
	require.NoError(t, h.CompareAndSetHeadWithDependencyResolution(v2, v1, first))
	requireHead(t, h, 1)
	require.Error(t, h.CompareAndSetHeadWithDependencyResolution(v1, v2, Resolution("other", "base")))
	requireHead(t, h, 1)
	stored, err := h.GetDependencyResolution(v2)
	require.NoError(t, err)
	require.Equal(t, first.Digest, stored.Digest)
	require.ErrorIs(t, h.CompareAndSetHeadWithDependencyResolution(v1, v2, nil), registry.ErrDependencyResolutionNotFound)
}

func testResolutionRebase(t *testing.T, h History) {
	v1 := child(root(), 1)
	require.NoError(t, h.SaveWithDependencyResolution(v1, Changes("one", "first"), Resolution("first", "old"), true))
	next := Resolution("first", "new")
	require.NoError(t, h.CompareAndSetHeadWithDependencyResolution(v1, v1, next))
	stored, err := h.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, next.Digest, stored.Digest)
}

func testReplayLineage(t *testing.T, h History) {
	v1 := child(root(), 1)
	v2 := child(root(), 2)
	v3 := child(v1, 3)
	require.NoError(t, h.Save(v1, Changes("one", "first"), true))
	require.NoError(t, h.Save(v2, Changes("two", "second"), false))
	require.NoError(t, h.Save(v3, Changes("three", "third"), true))

	var replayed []registry.ChangeSet
	require.NoError(t, h.ReplayChanges(context.Background(), v3, func(changes registry.ChangeSet) error {
		replayed = append(replayed, changes)
		return nil
	}))
	require.Len(t, replayed, 2)
	requireChanges(t, Changes("one", "first"), replayed[0])
	requireChanges(t, Changes("three", "third"), replayed[1])

	stop := errors.New("stop")
	require.ErrorIs(t, h.ReplayChanges(context.Background(), v3, func(registry.ChangeSet) error { return stop }), stop)
	require.Error(t, h.ReplayChanges(context.Background(), child(root(), 9), func(registry.ChangeSet) error { return nil }))

	lookup, err := h.GetVersion(3)
	require.NoError(t, err)
	require.Equal(t, uint(1), lookup.Previous().ID())
}

func testRejectInvalidResolution(t *testing.T, h History) {
	malformed := (&registry.DependencyResolution{Modules: []registry.ResolvedModule{
		{Name: "duplicate", Version: "1.0.0"},
		{Name: "duplicate", Version: "2.0.0"},
	}}).Canonical()
	v1 := child(root(), 1)
	require.ErrorIs(t, h.SaveWithDependencyResolution(v1, Changes("one", "first"), malformed, true), registry.ErrInvalidDependencyResolution)
	_, err := h.GetVersion(1)
	require.Error(t, err)
}
