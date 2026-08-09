// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
)

type testSource struct {
	info api.SourceInfo
}

func (s *testSource) Info() api.SourceInfo { return s.info }

func (s *testSource) Subscribe(context.Context, api.StreamOptions) (api.Stream, error) {
	return nil, nil
}

func newTestSource(name string) *testSource {
	return &testSource{info: api.SourceInfo{Name: name}}
}

func TestRegistryCanonicalIDAndDuplicateProtection(t *testing.T) {
	r := NewRegistry(nil)
	id := registry.NewID("app", "events")
	representation := registry.ID{NS: "app", Name: "events"}
	source := newTestSource("wrong-name")

	require.NoError(t, r.Register(id, source, "db.cdc.test"))
	got, ok := r.Get(representation)
	require.True(t, ok)
	require.Same(t, source, got)
	require.ErrorIs(t, r.Register(id, newTestSource("other"), "db.cdc.test"), ErrSourceExists)
}

func TestRegistryReplaceIsAtomicAndReturnsOld(t *testing.T) {
	r := NewRegistry(nil)
	id := registry.NewID("app", "events")
	old := newTestSource("old")
	newSource := newTestSource("new")
	require.NoError(t, r.Register(id, old, "db.cdc.old"))

	previous, ok, err := r.Replace(registry.ParseID(id.String()), newSource, "db.cdc.new")
	require.NoError(t, err)
	require.True(t, ok)
	require.Same(t, old, previous)
	current, ok := r.Get(id)
	require.True(t, ok)
	require.Same(t, newSource, current)

	missing := registry.NewID("app", "missing")
	_, replaced, err := r.Replace(missing, newTestSource("candidate"), "db.cdc.test")
	require.ErrorIs(t, err, ErrSourceMissing)
	require.False(t, replaced)
	_, exists := r.Get(missing)
	require.False(t, exists)
}

func TestRegistryListIsSortedAndOverlaysIdentity(t *testing.T) {
	r := NewRegistry(nil)
	require.NoError(t, r.Register(registry.NewID("app", "z"), newTestSource("z"), "db.cdc.z"))
	require.NoError(t, r.Register(registry.NewID("app", "a"), newTestSource("a"), "db.cdc.a"))

	infos := r.List()
	require.Len(t, infos, 2)
	require.Equal(t, "app:a", infos[0].ID.String())
	require.Equal(t, registry.Kind("db.cdc.a"), infos[0].Kind)
	require.Equal(t, "app:a", infos[0].Name)
	require.Equal(t, "app:z", infos[1].ID.String())
}

func TestRegistryConcurrentReplaceAndGet(t *testing.T) {
	r := NewRegistry(nil)
	id := registry.NewID("app", "events")
	require.NoError(t, r.Register(id, newTestSource("initial"), "db.cdc.test"))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				candidate := newTestSource("candidate")
				_, _, _ = r.Replace(id, candidate, registry.Kind("db.cdc.test"))
				_, _ = r.Get(id)
				_ = i
			}
		}(i)
	}
	wg.Wait()
	_, ok := r.Get(id)
	require.True(t, ok)
}
