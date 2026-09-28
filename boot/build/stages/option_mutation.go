// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/system/entry"
)

// prepareOptionMutationTarget gives a stage-owned entry view to Mutator. A
// payload may arrive in packed or JSON form, so the existing option spelling
// must be inspected only after conversion to the same Go map Mutator edits.
func prepareOptionMutationTarget(target *registry.Entry, transcoder payload.Transcoder) error {
	if target.Data != nil {
		data := target.Data
		if data.Format() != payload.Golang {
			var err error
			data, err = transcoder.Transcode(data, payload.Golang)
			if err != nil {
				return fmt.Errorf("transcode entry data: %w", err)
			}
		}
		if m, ok := data.Data().(map[string]any); ok {
			owned := cloneOptionMap(m)
			if err := expandTargetOptionContainer(target.Kind, owned, "options"); err != nil {
				return err
			}
			target.Data = payload.New(owned)
		} else {
			target.Data = data
		}
	}
	if target.Meta != nil {
		owned := cloneOptionMap(map[string]any(target.Meta))
		if err := expandTargetOptionContainer(target.Kind, owned, "meta.options"); err != nil {
			return err
		}
		target.Meta = attrs.Bag(owned)
	}
	return nil
}

// Typed containers are expanded only for WASM options, using the API's own
// representation. Other entry kinds and arbitrary payload fields are untouched.
func expandTypedOptionContainer(kind registry.Kind, path string, value any) (any, error) {
	if kind != wasm.FunctionWASM && kind != wasm.FunctionWAT && kind != wasm.ProcessWASM {
		return value, nil
	}
	path = strings.TrimPrefix(path, ".")
	if path != "options" && path != "data.options" && path != "meta.options" {
		return value, nil
	}
	switch value.(type) {
	case wasm.FunctionOptions, *wasm.FunctionOptions, wasm.ProcessOptions, *wasm.ProcessOptions:
		return wasm.ExpandOptionObject(kind, value, strings.TrimPrefix(path, "data."))
	default:
		return value, nil
	}
}

func expandTargetOptionContainer(kind registry.Kind, object map[string]any, path string) error {
	value, exists := object["options"]
	if !exists {
		return nil
	}
	expanded, err := expandTypedOptionContainer(kind, path, value)
	if err != nil {
		return err
	}
	object["options"] = expanded
	return nil
}

func cloneOptionMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = cloneOptionValue(value)
	}
	return clone
}

func cloneOptionValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneOptionMap(typed)
	case attrs.Bag:
		// Mutator descends map[string]any. Converting nested Bag values avoids
		// replacing them wholesale and losing their sibling controls.
		return cloneOptionMap(map[string]any(typed))
	case []any:
		clone := make([]any, len(typed))
		for i, item := range typed {
			clone[i] = cloneOptionValue(item)
		}
		return clone
	default:
		return value
	}
}

// resolveWASMOverridePath delegates all alias knowledge to the API catalog.
// `data.meta.*` is a payload field, not registry metadata; only authored
// `meta.options.*` paths participate in the compatibility catalog.
func resolveWASMOverridePath(kind registry.Kind, path string) (wasm.OptionPathInfo, bool) {
	trimmed := strings.TrimPrefix(path, ".")
	candidate := trimmed
	if strings.HasPrefix(candidate, "data.") {
		candidate = strings.TrimPrefix(trimmed, "data.")
		if strings.HasPrefix(candidate, "meta.") {
			return wasm.OptionPathInfo{}, false
		}
	}
	return wasm.ResolveOptionPath(kind, candidate)
}

// optionControl identifies either a directly authored control path or a group
// inside a whole options object. ContainerKey is empty for a direct path.
type optionControl struct {
	ContainerKey string
	Info         wasm.OptionPathInfo
}

func optionControls(kind registry.Kind, path string, value any) []optionControl {
	if info, ok := resolveWASMOverridePath(kind, path); ok {
		return []optionControl{{Info: info}}
	}
	trimmed := strings.TrimPrefix(path, ".")
	if trimmed != "options" && trimmed != "data.options" && trimmed != "meta.options" {
		return nil
	}
	var container map[string]any
	switch v := value.(type) {
	case map[string]any:
		container = v
	case attrs.Bag:
		container = map[string]any(v)
	default:
		return nil
	}
	keys := make([]string, 0, len(container))
	for key := range container {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var controls []optionControl
	for _, key := range keys {
		if info, ok := resolveWASMOverridePath(kind, path+"."+key); ok {
			controls = append(controls, optionControl{Info: info, ContainerKey: key})
		}
	}
	return controls
}

// setOptionValue preserves ordinary whole-object replacement, including
// legacy invocation defaults. A control already authored through another alias
// is updated at that existing location; it is removed from the replacement
// object so admission never receives a second copy of the same group.
func setOptionValue(mutator *entry.Mutator, target *registry.Entry, path string, value any) error {
	var err error
	value, err = expandTypedOptionContainer(target.Kind, path, value)
	if err != nil {
		return err
	}
	if err := checkAuthoredOptionAliases(target); err != nil {
		return err
	}
	controls := optionControls(target.Kind, path, value)
	replacement := cloneOptionValue(value)
	if len(controls) == 0 || controls[0].ContainerKey == "" {
		return mutator.Set(target, resolveWASMOverrideApplicationPath(target, path), replacement)
	}
	object := replacement.(map[string]any)
	type relocatedControl struct {
		value any
		path  string
	}
	var relocated []relocatedControl
	for _, control := range controls {
		destination := resolveWASMOverrideApplicationPath(target, path+"."+control.ContainerKey)
		physical, _ := resolveWASMOverridePath(target.Kind, destination)
		if physical.AuthoredGroup != control.Info.AuthoredGroup {
			relocated = append(relocated, relocatedControl{path: destination, value: object[control.ContainerKey]})
			delete(object, control.ContainerKey)
		}
	}
	if err := mutator.Set(target, path, object); err != nil {
		return err
	}
	for _, control := range relocated {
		if err := mutator.Set(target, control.path, control.value); err != nil {
			return err
		}
	}
	return nil
}

func conflictingAliasPath(groups map[string]map[string][]string, info wasm.OptionPathInfo) (string, bool) {
	spellings := groups[info.CanonicalGroup]
	for spelling, leaves := range spellings {
		if spelling != info.AuthoredGroup {
			return spelling, true
		}
		for _, prior := range leaves {
			if prior == info.CanonicalPath || prior == info.CanonicalGroup || info.CanonicalPath == info.CanonicalGroup {
				return prior, true
			}
		}
	}
	return "", false
}

func rememberAliasPath(seen map[*registry.Entry]map[string]map[string][]string, target *registry.Entry, info wasm.OptionPathInfo) {
	if seen[target] == nil {
		seen[target] = make(map[string]map[string][]string)
	}
	if seen[target][info.CanonicalGroup] == nil {
		seen[target][info.CanonicalGroup] = make(map[string][]string)
	}
	seen[target][info.CanonicalGroup][info.AuthoredGroup] = append(seen[target][info.CanonicalGroup][info.AuthoredGroup], info.CanonicalPath)
}

// resolveWASMOverrideApplicationPath writes into an already-authored group
// when present. No alias table lives here: the catalog supplies the complete
// group alias list and the stage merely observes which map path exists.
func resolveWASMOverrideApplicationPath(target *registry.Entry, path string) string {
	info, ok := resolveWASMOverridePath(target.Kind, path)
	if !ok {
		return path
	}
	for _, alias := range info.GroupAliases {
		if strings.HasPrefix(alias, "meta.") {
			if hasOptionPath(map[string]any(target.Meta), strings.Split(strings.TrimPrefix(alias, "meta."), ".")) {
				return "meta." + strings.TrimPrefix(alias, "meta.") + info.Suffix
			}
			continue
		}
		if target.Data != nil && target.Data.Format() == payload.Golang {
			if data, isMap := target.Data.Data().(map[string]any); isMap && hasOptionPath(data, strings.Split(alias, ".")) {
				return "data." + alias + info.Suffix
			}
		}
	}
	return path
}

func hasOptionPath(data map[string]any, segments []string) bool {
	if len(segments) == 0 {
		return false
	}
	var current any = data
	for _, segment := range segments {
		m, ok := current.(map[string]any)
		if !ok {
			return false
		}
		var exists bool
		current, exists = m[segment]
		if !exists {
			return false
		}
	}
	return true
}

// checkAuthoredOptionAliases rejects an ambiguous source declaration before
// an override could erase the evidence. It checks presence only: link inputs
// may still need to supply values before the runtime performs type validation.
func checkAuthoredOptionAliases(target *registry.Entry) error {
	var controls []optionControl
	if target.Data != nil && target.Data.Format() == payload.Golang {
		if data, ok := target.Data.Data().(map[string]any); ok {
			keys := make([]string, 0, len(data))
			for key := range data {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				controls = append(controls, optionControls(target.Kind, "data."+key, data[key])...)
			}
		}
	}
	if target.Meta != nil {
		controls = append(controls, optionControls(target.Kind, "meta.options", target.Meta["options"])...)
	}
	seen := make(map[string]string)
	for _, control := range controls {
		info := control.Info
		if prior, ok := seen[info.CanonicalGroup]; ok {
			return fmt.Errorf("duplicate option group %s declared at %s and %s", info.CanonicalGroup, prior, info.AuthoredGroup)
		}
		seen[info.CanonicalGroup] = info.AuthoredGroup
	}
	return nil
}
