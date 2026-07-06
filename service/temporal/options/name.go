// SPDX-License-Identifier: MPL-2.0

package options

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// WorkflowName resolves the Temporal workflow type name from an explicit
// "workflow.name" option, falling back to the qualified form of the fallback ID.
func WorkflowName(options attrs.Attributes, fallback registry.ID) string {
	if name, ok := explicitName(options, OptionWorkflowName); ok {
		return name
	}
	return WorkflowNameFromID(fallback)
}

// WorkflowNameFromID resolves the Temporal workflow type name for call sites that
// carry no options bag, using the qualified form of the ID.
func WorkflowNameFromID(id registry.ID) string {
	return qualify(id)
}

// ActivityName resolves the Temporal activity type name from an explicit
// "activity.name" option, falling back to the qualified form of the fallback ID.
func ActivityName(options attrs.Attributes, fallback registry.ID) string {
	if name, ok := explicitName(options, OptionActivityName); ok {
		return name
	}
	return ActivityNameFromID(fallback)
}

// ActivityNameFromID resolves the Temporal activity type name for call sites that
// carry no options bag, using the qualified form of the ID.
func ActivityNameFromID(id registry.ID) string {
	return qualify(id)
}

// qualify renders a registry ID as a Temporal boundary type name: the bare name
// when the namespace is empty, "ns:name" otherwise. It never emits a leading
// colon, so a namespace-less name matches what an external worker registers.
func qualify(id registry.ID) string {
	if id.NS == "" {
		return id.Name
	}
	return id.NS + ":" + id.Name
}

func explicitName(options attrs.Attributes, key string) (string, bool) {
	raw, ok := optionGet(options, key)
	if !ok {
		return "", false
	}
	name, err := parseString(key, raw)
	if err != nil || name == "" {
		return "", false
	}
	return name, true
}
