// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/hub"
)

type fakeModuleSearchClient struct {
	search func(context.Context, *hub.SearchParams) (*hub.SearchResult, error)
	get    func(context.Context, string, string) (*hub.ModuleInfo, error)
}

func (f fakeModuleSearchClient) SearchModules(ctx context.Context, params *hub.SearchParams) (*hub.SearchResult, error) {
	return f.search(ctx, params)
}

func (f fakeModuleSearchClient) GetModule(ctx context.Context, org, name string) (*hub.ModuleInfo, error) {
	return f.get(ctx, org, name)
}

func TestSearchModulesQualifiedNameUsesExactLookup(t *testing.T) {
	want := &hub.ModuleInfo{Org: "wippy", Name: "migration", LatestVersion: "0.3.19"}
	client := fakeModuleSearchClient{
		search: func(context.Context, *hub.SearchParams) (*hub.SearchResult, error) {
			t.Fatal("qualified name must not use full-text search")
			return nil, nil
		},
		get: func(_ context.Context, org, name string) (*hub.ModuleInfo, error) {
			require.Equal(t, "wippy", org)
			require.Equal(t, "migration", name)
			return want, nil
		},
	}

	got, err := searchModules(context.Background(), client, "wippy/migration", 20)
	require.NoError(t, err)
	require.Equal(t, int32(1), got.TotalCount)
	require.Equal(t, []*hub.ModuleInfo{want}, got.Modules)
}

func TestSearchModulesQualifiedNameNotFound(t *testing.T) {
	client := fakeModuleSearchClient{
		get: func(context.Context, string, string) (*hub.ModuleInfo, error) {
			return nil, hub.ErrModuleNotFound
		},
	}

	got, err := searchModules(context.Background(), client, "wippy/missing", 20)
	require.NoError(t, err)
	require.Empty(t, got.Modules)
}

func TestSearchModulesQualifiedNamePreservesOtherErrors(t *testing.T) {
	want := errors.New("hub unavailable")
	client := fakeModuleSearchClient{
		get: func(context.Context, string, string) (*hub.ModuleInfo, error) {
			return nil, want
		},
	}

	_, err := searchModules(context.Background(), client, "wippy/migration", 20)
	require.ErrorIs(t, err, want)
}

func TestSearchModulesTextUsesFullTextSearch(t *testing.T) {
	want := &hub.SearchResult{Modules: []*hub.ModuleInfo{{Org: "wippy", Name: "migration"}}}
	client := fakeModuleSearchClient{
		search: func(_ context.Context, params *hub.SearchParams) (*hub.SearchResult, error) {
			require.Equal(t, "migration", params.Query)
			require.Equal(t, int32(10), params.PageSize)
			return want, nil
		},
		get: func(context.Context, string, string) (*hub.ModuleInfo, error) {
			t.Fatal("unqualified query must not use exact lookup")
			return nil, nil
		},
	}

	got, err := searchModules(context.Background(), client, "migration", 10)
	require.NoError(t, err)
	require.Same(t, want, got)
}
