// SPDX-License-Identifier: MPL-2.0

package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
)

func TestRouteManager_PrefixedRouterComposesWithRootWildcard(t *testing.T) {
	rm, err := NewRouteManager()
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	root := registry.NewID("test", "root")
	api := registry.NewID("test", "api")
	if err := rm.AddRouter(root, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := rm.AddRoute(root, registry.NewID("test", "page"), "GET", "/{slug}", root, handler); err != nil {
		t.Fatal(err)
	}
	if err := rm.AddRouter(api, "/api", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := rm.AddRoute(api, registry.NewID("test", "users"), "GET", "/users", api, handler); err != nil {
		t.Fatal(err)
	}
	require.NoError(t, rm.Build())
	require.Equal(t, http.StatusOK, serveWithFrame(rm, "GET", "/api").Code)
	require.Equal(t, http.StatusOK, serveWithFrame(rm, "GET", "/api/users").Code)
	require.Equal(t, http.StatusNotFound, serveWithFrame(rm, "GET", "/api/missing").Code)
}

func TestRouteManager_NormalizedPrefixesHaveCompleteAllow(t *testing.T) {
	rm, err := NewRouteManager()
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	for _, route := range []struct{ id, prefix, method, path string }{
		{"first", "/api", "GET", "/a"},
		{"second", "/api/", "POST", "/b"},
	} {
		id := registry.NewID("test", route.id)
		if err := rm.AddRouter(id, route.prefix, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := rm.AddRoute(id, id, route.method, route.path, id, handler); err != nil {
			t.Fatal(err)
		}
	}
	if err := rm.Build(); err != nil {
		t.Fatal(err)
	}
	for path, allow := range map[string]string{"/api/a": "GET, HEAD, OPTIONS", "/api/b": "OPTIONS, POST"} {
		rec := serveWithFrame(rm, "DELETE", path)
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		require.Equal(t, allow, rec.Header().Get("Allow"))
	}

	// With no unique middleware owner, the existing mount still handles misses.
	require.NoError(t, rm.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})))
	require.NoError(t, rm.Build())
	require.Equal(t, http.StatusAccepted, serveWithFrame(rm, "DELETE", "/api/a").Code)
	require.Equal(t, http.StatusOK, serveWithFrame(rm, "GET", "/api/a").Code)
}

func TestRouteManager_OverlappingWildcardPrefixesKeepNativeRoutes(t *testing.T) {
	rm, err := NewRouteManager()
	require.NoError(t, err)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.PathValue("part")))
	})
	for _, route := range []struct{ prefix, path string }{
		{"/{part}/fixed", "/left"},
		{"/fixed/{part}", "/right"},
	} {
		id := registry.NewID("test", route.path[1:])
		require.NoError(t, rm.AddRouter(id, route.prefix, nil, nil))
		require.NoError(t, rm.AddRoute(id, id, http.MethodGet, route.path, id, handler))
	}
	require.NoError(t, rm.AddRouter(registry.NewID("test", "api"), "/api", nil, nil))
	require.NoError(t, rm.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})))
	require.NoError(t, rm.Build())
	require.Equal(t, http.StatusAccepted, serveWithFrame(rm, "GET", "/one/fixed/missing").Code)
	require.Equal(t, http.StatusNotFound, serveWithFrame(rm, "GET", "/api/missing").Code)
	require.Equal(t, "one", serveWithFrame(rm, "GET", "/one/fixed/left").Body.String())
	require.Equal(t, "two", serveWithFrame(rm, "GET", "/fixed/two/right").Body.String())
}
