// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"reflect"
	"sort"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
)

func TestStorage_Versions(t *testing.T) {
	storage := New()

	v0 := version.New(0)
	v1 := version.New(1)
	v2 := version.New(2)
	v3 := version.New(3)

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

	expectedVersions := []registry.Version{v0, v1, v2, v3}
	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("Expected versions: %v, got: %v", expectedVersions, versions)
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
	if err := storage.ReplayChanges(v3, func(cs registry.ChangeSet) error {
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

func TestStorage_Get(t *testing.T) {
	storage := New()
	v2 := version.New(2)

	actions := registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

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
	v1 := version.New(1)
	v2 := version.New(2)

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

	expectedVersions := []registry.Version{v0, v1, v2}
	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("Expected versions: %v, got: %v", expectedVersions, versions)
	}

	retrievedActions, _ := storage.Get(v2)
	if !reflect.DeepEqual(retrievedActions, actions) {
		t.Errorf("Expected actions: %v, got: %v", actions, retrievedActions)
	}
}

func TestStorage_Head(t *testing.T) {
	storage := New()

	_, err := storage.Head()
	if err == nil {
		t.Errorf("Expected error when getting head of empty history, got nil")
	}

	v1 := version.New(1)
	_ = storage.Save(v1, registry.ChangeSet{}, true)

	head, err := storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if !reflect.DeepEqual(head, v1) {
		t.Errorf("Expected head to be v1 (%v), got: %v", v1, head)
	}

	v2 := version.New(2)
	_ = storage.Save(v2, registry.ChangeSet{}, false)

	head, err = storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if !reflect.DeepEqual(head, v1) {
		t.Errorf("Expected head to remain v1 (%v), got: %v", v1, head)
	}

	v3 := version.New(3)
	_ = storage.Save(v3, registry.ChangeSet{}, true)

	head, err = storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if !reflect.DeepEqual(head, v3) {
		t.Errorf("Expected head to be v3 (%v), got: %v", v3, head)
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
