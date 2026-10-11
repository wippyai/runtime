// SPDX-License-Identifier: MPL-2.0

package requirements

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/registry"
)

func AddressIndex(namespace string, ids []string) map[string][]string {
	owned := make(map[string][]string)
	for _, id := range ids {
		ns, name, _ := strings.Cut(id, ":")
		if ns != namespace && !strings.HasPrefix(ns, namespace+".") {
			continue
		}
		owned[name] = append(owned[name], id)
		owned[id] = append(owned[id], id)
	}
	return owned
}

func Resolve[T any](name string, all map[string]T, owned map[string][]string) []string {
	if strings.Contains(name, ":") {
		if _, exists := all[name]; exists {
			return []string{name}
		}
		return nil
	}
	return owned[name]
}

// ModuleNamespaces returns the canonical registry namespace exported
// by each loaded module. Publishing requires exactly one ns.definition per
// module, and the loader records the containing module on that entry. This is
// the authoritative bridge between a Hub component name (org/module) and its
// registry namespace; neither spelling nor pluralization is inferred.
func ModuleNamespaces(entries []registry.Entry) (map[string]string, error) {
	namespaces := make(map[string]string)
	owners := make(map[string]string)
	for _, entry := range entries {
		if entry.Kind != registry.NamespaceDefinition {
			continue
		}
		module := entry.Registry.Owner
		if module == "" {
			continue
		}
		namespace := strings.TrimSpace(entry.ID.NS)
		if namespace == "" {
			continue
		}
		if existing := namespaces[module]; existing != "" && existing != namespace {
			return nil, fmt.Errorf(
				"module %s declares multiple namespaces: %s and %s",
				module,
				existing,
				namespace,
			)
		}
		if owner := owners[namespace]; owner != "" && owner != module {
			return nil, fmt.Errorf(
				"namespace %s is declared by multiple modules: %s and %s",
				namespace,
				owner,
				module,
			)
		}
		namespaces[module] = namespace
		owners[namespace] = module
	}
	return namespaces, nil
}
