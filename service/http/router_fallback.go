// SPDX-License-Identifier: MPL-2.0

package http

import (
	"net/http"
	"strings"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/service/http/middleware/firewall"
)

// probeMethods lists the methods checked when computing the Allow header.
var probeMethods = []string{
	http.MethodDelete,
	http.MethodGet,
	http.MethodHead,
	http.MethodOptions,
	http.MethodPatch,
	http.MethodPost,
	http.MethodPut,
	http.MethodTrace,
}

// routerFallback owns the subtree below a router prefix. Requests that reach it
// matched no route of the router, so it answers 404, or 405 when the path
// exists for other methods.
type routerFallback struct {
	probe *http.ServeMux
}

func (f *routerFallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var allowed []string
	probe := r.WithContext(r.Context()) // Only Method changes during the read-only mux lookups.
	for _, method := range probeMethods {
		probe.Method = method
		if _, pattern := f.probe.Handler(probe); pattern != "" {
			allowed = append(allowed, method)
		}
	}

	if len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		firewall.WriteJSONError(w, http.StatusMethodNotAllowed, false,
			"Method not allowed", "Method "+r.Method+" is not allowed for "+r.URL.Path)
		return
	}

	firewall.WriteJSONError(w, http.StatusNotFound, false,
		"Not found", "No route matches "+r.URL.Path)
}

// Fallbacks live in a separate mux so synthetic prefix patterns never compete
// with declared endpoint patterns. Concrete routes retain ServeMux precedence.
type routerFallbacks struct {
	*http.ServeMux
}

type prefixOwner struct {
	http.Handler
	prefix string
}

func buildRouterFallbacks(routers map[registry.ID]*RouterEntry) (*routerFallbacks, error) {
	counts := make(map[string]int)
	for _, router := range routers {
		counts[strings.TrimSuffix(router.prefix, "/")]++
	}
	var owners *routerFallbacks
	for id, router := range routers {
		prefix := strings.TrimSuffix(router.prefix, "/")
		// A wildcard prefix describes a set of paths, not a single owned
		// subtree. Its declared routes stay on the native mux.
		if prefix == "" || strings.ContainsAny(prefix, "{}") || counts[prefix] == 0 {
			continue
		}
		if owners == nil {
			owners = &routerFallbacks{http.NewServeMux()}
		}
		// Equivalent prefixes have no single middleware owner. Preserve native
		// routing for them, including precedence over a shorter prefix.
		handler := http.NotFoundHandler()
		if counts[prefix] == 1 && !ownsSubtree(router) {
			probe := http.NewServeMux()
			noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			for _, route := range router.routes {
				if err := registerPattern(probe, buildPattern(route.method, prefix, route.path), noop); err != nil {
					return nil, err
				}
				if route.method != "" && route.method != http.MethodOptions {
					_ = registerPattern(probe, buildPattern(http.MethodOptions, prefix, route.path), noop)
				}
			}
			handler = &prefixOwner{withRouteLabel(id.String(), applyMiddlewareChain(router.middleware, &routerFallback{probe: probe})), prefix}
		}
		delete(counts, prefix)
		for _, pattern := range []string{prefix, prefix + "/"} {
			if err := registerPattern(owners.ServeMux, pattern, handler); err != nil {
				return nil, err
			}
		}
	}
	return owners, nil
}

func (f *routerFallbacks) serveBelow(mount string, w http.ResponseWriter, r *http.Request) bool {
	handler, _ := f.Handler(r)
	owner, ok := handler.(*prefixOwner)
	if !ok || !strings.HasPrefix(owner.prefix, mount) {
		return false
	}
	owner.ServeHTTP(w, r)
	return true
}

func (f *routerFallbacks) beforeMount(pattern string, mount http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !f.serveBelow(pattern, w, r) {
			mount.ServeHTTP(w, r)
		}
	})
}

// ownsSubtree reports whether a method-agnostic route already matches every
// path below the prefix.
func ownsSubtree(router *RouterEntry) bool {
	for _, route := range router.routes {
		if route.method != "" {
			continue
		}
		if route.path == "/" {
			return true
		}
		if strings.HasPrefix(route.path, "/{") && strings.HasSuffix(route.path, "...}") &&
			strings.Count(route.path, "/") == 1 {
			return true
		}
	}
	return false
}
