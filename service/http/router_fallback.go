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
	for _, method := range probeMethods {
		probe := r.Clone(r.Context())
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

// routerFallbackPatterns returns the patterns that make a router own its whole
// prefix, with the handler serving them. Root routers own no prefix. A router
// whose own routes already cover the entire subtree needs no fallback.
func routerFallbackPatterns(id registry.ID, router *RouterEntry) (patterns []string, handler http.Handler, err error) {
	prefix := strings.TrimSuffix(router.prefix, "/")
	if prefix == "" || ownsSubtree(router) {
		return nil, nil, nil
	}

	probe := http.NewServeMux()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, route := range router.routes {
		if err := registerPattern(probe, buildPattern(route.method, router.prefix, route.path), noop); err != nil {
			return nil, nil, err
		}
		if route.method != "" && route.method != http.MethodOptions {
			// Duplicate OPTIONS registrations are tolerated; the main mux reports real conflicts.
			_ = registerPattern(probe, buildPattern(http.MethodOptions, router.prefix, route.path), noop)
		}
	}

	handler = http.Handler(&routerFallback{probe: probe})
	if len(router.middleware) > 0 {
		handler = applyMiddlewareChain(router.middleware, handler)
	}
	handler = withRouteLabel(id.String(), handler)

	return []string{prefix, prefix + "/"}, handler, nil
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
