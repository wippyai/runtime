// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
)

func TestStorageCanonicalizesSavedAncestryAndHead(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	if err := storage.Save(v1, nil, false); err != nil {
		t.Fatal(err)
	}

	// The caller supplies the right parent ID with a deliberately truncated
	// parent chain. Storage must link to its canonical v1, not retain this object.
	v2 := version.FromParent(version.New(1), 2)
	if err := storage.Save(v2, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetHead(version.New(2)); err != nil {
		t.Fatal(err)
	}
	head, err := storage.Head()
	if err != nil {
		t.Fatal(err)
	}
	if head.Previous() == nil || head.Previous().ID() != 1 || head.Previous().Previous() == nil || head.Previous().Previous().ID() != 0 {
		t.Fatalf("head ancestry was not canonicalized: %#v", head)
	}
}

func TestStorage_Versions(t *testing.T) {
	storage := New()

	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v2, 3)

	_ = storage.Save(v1, registry.ChangeSet{}, false)
	_ = storage.Save(v2, registry.ChangeSet{}, false)
	_ = storage.Save(v3, registry.ChangeSet{}, false)

	versions, err := storage.Versions()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].ID() < versions[j].ID()
	})

	expectedIDs := []uint{v0.ID(), v1.ID(), v2.ID(), v3.ID()}
	if got := versionIDs(versions); !reflect.DeepEqual(got, expectedIDs) {
		t.Errorf("Expected version IDs: %v, got: %v", expectedIDs, got)
	}
}

func TestStorage_ReplayChangesFollowsOnlyTargetAncestry(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v1, 3)
	entry := func(name string) registry.ChangeSet {
		return registry.ChangeSet{{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("test", name)}}}
	}
	if err := storage.Save(v1, entry("one"), false); err != nil {
		t.Fatal(err)
	}
	if err := storage.Save(v2, entry("branch_a"), false); err != nil {
		t.Fatal(err)
	}
	if err := storage.Save(v3, entry("branch_b"), false); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := storage.ReplayChanges(context.Background(), v3, func(cs registry.ChangeSet) error {
		for _, op := range cs {
			names = append(names, op.Entry.ID.Name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"one", "branch_b"}) {
		t.Fatalf("replayed names = %v", names)
	}
}

func TestStorageReplayHonorsCanceledContextAtRoot(t *testing.T) {
	storage := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := storage.ReplayChanges(ctx, version.New(registry.RootVersion), func(registry.ChangeSet) error {
		t.Fatal("root replay must not invoke callback")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestStorageReplayDoesNotHoldLockDuringCallback(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	require.NoError(t, storage.Save(v1, nil, false))
	v2 := version.FromParent(v1, 2)

	done := make(chan error, 1)
	go func() {
		done <- storage.ReplayChanges(context.Background(), v1, func(registry.ChangeSet) error {
			return storage.Save(v2, nil, false)
		})
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("replay callback could not re-enter history")
	}
	_, err := storage.Get(v2)
	require.NoError(t, err)
}

func TestStorageRejectsMalformedResolutionWithoutMutation(t *testing.T) {
	storage := New()
	malformed := (&registry.DependencyResolution{Modules: []registry.ResolvedModule{
		{Name: "duplicate", Version: "1.0.0"},
		{Name: "duplicate", Version: "2.0.0"},
	}}).Canonical()
	v1 := version.FromParent(version.New(0), 1)

	require.ErrorIs(t, storage.SaveWithDependencyResolution(v1, nil, malformed, true), registry.ErrInvalidDependencyResolution)
	_, err := storage.GetVersion(v1.ID())
	require.Error(t, err)
	_, err = storage.Head()
	require.Error(t, err)
}

func TestStorage_Get(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	actions := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	_ = storage.Save(v1, nil, false)
	_ = storage.Save(v2, actions, false)

	retrievedActions, err := storage.Get(v2)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(retrievedActions, actions) {
		t.Errorf("Expected actions: %v, got: %v", actions, retrievedActions)
	}

	_, err = storage.Get(version.New(3))
	if err == nil {
		t.Errorf("Expected error for non-existent version")
	}
}

func TestStorage_Save(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	v2 := version.FromParent(v1, 2)

	actions := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	err := storage.Save(v1, registry.ChangeSet{}, false)
	if err != nil {
		t.Fatalf("Unexpected error saving v1: %v", err)
	}

	err = storage.Save(v2, actions, false)
	if err != nil {
		t.Fatalf("Unexpected error saving v2: %v", err)
	}

	versions, _ := storage.Versions()
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].ID() < versions[j].ID()
	})

	expectedIDs := []uint{v0.ID(), v1.ID(), v2.ID()}
	if got := versionIDs(versions); !reflect.DeepEqual(got, expectedIDs) {
		t.Errorf("Expected version IDs: %v, got: %v", expectedIDs, got)
	}

	retrievedActions, _ := storage.Get(v2)
	if !reflect.DeepEqual(retrievedActions, actions) {
		t.Errorf("Expected actions: %v, got: %v", actions, retrievedActions)
	}
}

func versionIDs(versions []registry.Version) []uint {
	ids := make([]uint, len(versions))
	for i, stored := range versions {
		ids[i] = stored.ID()
	}
	return ids
}

func TestStorage_Head(t *testing.T) {
	storage := New()
	v0 := version.New(0)

	_, err := storage.Head()
	if err == nil {
		t.Errorf("Expected error when getting head of empty history, got nil")
	}

	v1 := version.FromParent(v0, 1)
	_ = storage.Save(v1, registry.ChangeSet{}, true)

	head, err := storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if head.ID() != v1.ID() {
		t.Errorf("Expected head to be v1 (%v), got: %v", v1, head)
	}

	v2 := version.FromParent(v1, 2)
	_ = storage.Save(v2, registry.ChangeSet{}, false)

	head, err = storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if head.ID() != v1.ID() {
		t.Errorf("Expected head to remain v1 (%v), got: %v", v1, head)
	}

	v3 := version.FromParent(v1, 3)
	_ = storage.Save(v3, registry.ChangeSet{}, true)

	head, err = storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if head.ID() != v3.ID() {
		t.Errorf("Expected head to be v3 (%v), got: %v", v3, head)
	}
}

func TestStorageRejectsParentlessNonRootVersion(t *testing.T) {
	storage := New()
	err := storage.Save(version.New(1), nil, false)
	if err == nil {
		t.Fatal("expected parentless non-root version to be rejected")
	}
	versions, versionsErr := storage.Versions()
	if versionsErr != nil {
		t.Fatal(versionsErr)
	}
	if len(versions) != 1 || versions[0].ID() != registry.RootVersion {
		t.Fatalf("rejected version was partially persisted: %v", versions)
	}
}

func TestStorage_DependencyResolutionRoundTripAndInheritance(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	resolution := (&registry.DependencyResolution{
		InputDigest: "roots",
		Modules: []registry.ResolvedModule{{
			Name: "acme/crm", Version: "v1.6.0", Digest: "sha256:crm",
		}},
	}).Canonical()

	if err := storage.SaveWithDependencyResolution(v1, nil, resolution, true); err != nil {
		t.Fatalf("save resolution: %v", err)
	}
	got, err := storage.GetDependencyResolution(v1)
	if err != nil {
		t.Fatalf("get resolution: %v", err)
	}
	if got.Digest != resolution.Digest {
		t.Fatalf("expected digest %s, got %s", resolution.Digest, got.Digest)
	}

	v2 := version.FromParent(v1, 2)
	if err := storage.Save(v2, nil, true); err != nil {
		t.Fatalf("save inherited version: %v", err)
	}
	inherited, err := storage.GetDependencyResolution(v2)
	if err != nil {
		t.Fatalf("get inherited resolution: %v", err)
	}
	if inherited.Digest != resolution.Digest {
		t.Fatalf("expected inherited digest %s, got %s", resolution.Digest, inherited.Digest)
	}
}

func TestStorage_SaveSnapshotsMutableChangesAndUsesHeadCAS(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	data := map[string]any{"version": "v1", "nested": []any{"original"}}
	meta := map[string]any{"nested": map[string]any{"enabled": true}}
	v1 := version.FromParent(v0, 1)
	if err := storage.Save(v1, registry.ChangeSet{{
		Kind:  registry.EntryCreate,
		Entry: registry.Entry{ID: registry.NewID("app", "entry"), Data: payload.New(data), Meta: meta},
	}}, true); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	data["version"] = "mutated"
	data["nested"].([]any)[0] = "mutated"
	meta["nested"].(map[string]any)["enabled"] = false

	got, err := storage.Get(v1)
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	gotData := got[0].Entry.Data.Data().(map[string]any)
	if gotData["version"] != "v1" || gotData["nested"].([]any)[0] != "original" {
		t.Fatalf("saved payload was mutated through caller alias: %#v", gotData)
	}
	if got[0].Entry.Meta["nested"].(map[string]any)["enabled"] != true {
		t.Fatalf("saved metadata was mutated through caller alias: %#v", got[0].Entry.Meta)
	}

	gotData["version"] = "mutated-through-get"
	again, err := storage.Get(v1)
	if err != nil || again[0].Entry.Data.Data().(map[string]any)["version"] != "v1" {
		t.Fatalf("Get returned mutable storage aliases: %#v, %v", again, err)
	}

	v2 := version.FromParent(v1, 2)
	if err := storage.Save(v2, nil, true); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	stale := version.FromParent(v1, 3)
	if err := storage.Save(stale, nil, true); err == nil {
		t.Fatal("expected stale-parent head CAS failure")
	}
	if _, err := storage.Get(stale); err == nil {
		t.Fatal("failed CAS must not retain a partial version")
	}
}

func TestStorage_DependencyResolutionCheckpointIsImmutable(t *testing.T) {
	storage := New()
	v0 := version.New(0)
	v1 := version.FromParent(v0, 1)
	if err := storage.Save(v1, nil, true); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	first := (&registry.DependencyResolution{InputDigest: "first"}).Canonical()
	second := (&registry.DependencyResolution{InputDigest: "second"}).Canonical()
	if err := storage.CheckpointDependencyResolution(v1, first); err != nil {
		t.Fatalf("first checkpoint: %v", err)
	}
	if err := storage.CheckpointDependencyResolution(v1, first); err != nil {
		t.Fatalf("idempotent checkpoint: %v", err)
	}
	if err := storage.CheckpointDependencyResolution(v1, second); err == nil {
		t.Fatal("expected immutable resolution reference conflict")
	}
}
