// SPDX-License-Identifier: MPL-2.0

package topology

import (
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
)

// envVariableName follows the shortcut registered by system/env. Raw registry
// declarations and Go-authored typed configurations carry the same identity.
// Duplicate names retain runtime's first-registrant policy; static topology
// conservatively orders consumers after all declarants, not an invented winner.
func envVariableName(entry registry.Entry) string {
	if entry.Kind != "env.variable" {
		return ""
	}
	if entry.Data != nil {
		var name string
		switch data := entry.Data.Data().(type) {
		case map[string]any:
			name, _ = data["variable"].(string)
		case envapi.Variable:
			name = data.Name
		case *envapi.Variable:
			if data != nil {
				name = data.Name
			}
		}
		if name != "" {
			return name
		}
	}
	return entry.ID.String()
}

func environmentFallbackKey(namespace, name string) string {
	kind, value := parseDependency(name)
	switch kind {
	case "group":
		return groupPrefix + value
	case "namespace":
		return nsPrefix + value
	default:
		id := resolveDependencyID(namespace, value)
		return "id:" + id.String()
	}
}

// Candidate sorting needs resident aliases too: a variable outside the batch
// still shadows a relative ID inside it. The incremental path reads only the
// providers for referenced names, not every entry's payload. Rejected candidates
// never mutate this committed provider index.
func environmentVariablesForChanges(from registry.State, changes registry.ChangeSet, index *DepIndex, keys map[registry.ID]entryDepKeys) map[string][]registry.ID {
	var variables map[string][]registry.ID
	for _, declared := range keys {
		for _, name := range declared.env {
			if variables == nil {
				variables = make(map[string][]registry.ID)
			}
			variables[name] = nil
		}
	}
	if variables == nil {
		return nil
	}
	byID := make(map[registry.ID]string)
	if index != nil {
		for name := range variables {
			for id := range index.variableNames[name] {
				byID[id] = name
			}
		}
	} else {
		for _, entry := range from {
			if name := envVariableName(entry); name != "" {
				if _, referenced := variables[name]; !referenced {
					continue
				}
				byID[entry.ID] = name
			}
		}
	}
	// Same-ID replacement input is normalized by operation kind, not execution
	// order. Apply removals first so a replacement keeps its new declaration.
	for _, operation := range changes {
		if operation.Kind == registry.EntryDelete {
			delete(byID, operation.Entry.ID)
		}
	}
	for _, operation := range changes {
		if operation.Kind == registry.EntryDelete {
			continue
		}
		name := envVariableName(operation.Entry)
		if _, referenced := variables[name]; referenced {
			byID[operation.Entry.ID] = name
		} else {
			delete(byID, operation.Entry.ID)
		}
	}
	for id, name := range byID {
		variables[name] = append(variables[name], id)
	}
	return variables
}
