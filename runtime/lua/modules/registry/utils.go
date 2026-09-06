// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"errors"
	"fmt"
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

// luaTableToEntry converts a Lua table to a registry entry
func luaTableToEntry(l *lua.LState, table *lua.LTable) (regapi.Entry, error) {
	var entry regapi.Entry

	// Extract ID
	idVal := table.RawGetString("id")
	switch idVal.Type() {
	case lua.LTTable:
		idTable := idVal.(*lua.LTable)
		var err error
		entry.ID, err = tableToID(l, idTable)
		if err != nil {
			return entry, err
		}
	case lua.LTString:
		entry.ID = regapi.ParseID(idVal.String())
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTInteger, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
		fallthrough
	default:
		return entry, errors.New("entry must have valid id field")
	}

	// Extract kind
	kindVal := table.RawGetString("kind")
	if kindVal.Type() != lua.LTString {
		return entry, errors.New("entry must have kind field")
	}
	entry.Kind = kindVal.String()

	// Extract metadata
	metaVal := table.RawGetString("meta")
	if metaVal.Type() == lua.LTTable {
		meta := attrs.Bag{}
		metaTable := metaVal.(*lua.LTable)

		metaTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				meta[string(kStr)] = value.ToGoAny(v)
			}
		})

		entry.Meta = meta
	} else {
		entry.Meta = attrs.Bag{}
	}

	// Extract deployment-root status. Ownership is assigned by the dependency
	// directive; callers may only select whether an ns.dependency is a root.
	rootVal := table.RawGetString("dependency_root")
	if rootVal != lua.LNil {
		root, ok := rootVal.(lua.LBool)
		if !ok {
			return entry, errors.New("entry dependency_root must be a boolean")
		}
		entry.Registry.Root = bool(root)
	}

	// Extract data
	dataVal := table.RawGetString("data")
	if dataVal != lua.LNil {
		entry.Data = payload.NewPayload(value.ToGoAny(dataVal), payload.Golang)
	}

	return entry, nil
}

// entryToLuaTable converts a registry Entry to a Lua table
func entryToLuaTable(l *lua.LState, entry regapi.Entry) (*lua.LTable, error) {
	entryTable := l.CreateTable(0, 4)

	// Convert ID
	entryTable.RawSetString("id", lua.LString(entry.ID.String()))

	// Add kind
	entryTable.RawSetString("kind", lua.LString(entry.Kind))

	// Convert metadata
	metaTable := l.CreateTable(0, len(entry.Meta))
	for k, v := range entry.Meta {
		luaValue, err := luaconv.GoToLua(v)
		if err != nil {
			return nil, fmt.Errorf("failed to convert metadata value: %w", err)
		}
		metaTable.RawSetString(k, luaValue)
	}
	entryTable.RawSetString("meta", metaTable)

	// Convert data payload using transcoder if available
	if entry.Data != nil {
		dtt := payload.GetTranscoder(l.Context())
		if dtt == nil {
			return nil, fmt.Errorf("failed to transcode entry data: no transcoder in context")
		}

		luaData, err := dtt.Transcode(entry.Data, payload.Lua)
		if err != nil {
			return nil, fmt.Errorf("failed to transcode entry data: %w", err)
		}

		if luaData != nil {
			if lv, ok := luaData.Data().(lua.LValue); ok {
				entryTable.RawSetString("data", lv)
			} else {
				entryTable.RawSetString("data", lua.LNil)
			}
		} else {
			entryTable.RawSetString("data", lua.LNil)
		}
	} else {
		entryTable.RawSetString("data", lua.LNil)
	}

	return entryTable, nil
}

// stateEntryToLuaTable includes registry-owned metadata for the state API.
// The ordinary entry APIs intentionally expose only the author-facing shape.
func stateEntryToLuaTable(l *lua.LState, entry regapi.Entry) (*lua.LTable, error) {
	entryTable, err := entryToLuaTable(l, entry)
	if err != nil {
		return nil, err
	}
	metadata := l.CreateTable(0, 2)
	metadata.RawSetString("owner", lua.LString(entry.Registry.Owner))
	metadata.RawSetString("root", lua.LBool(entry.Registry.Root))
	entryTable.RawSetString("registry", metadata)
	return entryTable, nil
}

func resolutionToLuaTable(l *lua.LState, resolution *regapi.DependencyResolution) *lua.LTable {
	table := l.CreateTable(0, 6)
	table.RawSetString("digest", lua.LString(resolution.Digest))
	table.RawSetString("input_digest", lua.LString(resolution.InputDigest))
	if resolution.BaselineDigest != "" {
		table.RawSetString("baseline_digest", lua.LString(resolution.BaselineDigest))
	}
	table.RawSetString("roots", dependencyRootsToLuaTable(l, resolution.Roots))
	if len(resolution.References) > 0 {
		table.RawSetString("references", dependencyRootsToLuaTable(l, resolution.References))
	}

	modules := l.CreateTable(len(resolution.Modules), 0)
	for i, module := range resolution.Modules {
		item := l.CreateTable(0, 7)
		item.RawSetString("name", lua.LString(module.Name))
		item.RawSetString("version", lua.LString(module.Version))
		if module.VersionID != "" {
			item.RawSetString("version_id", lua.LString(module.VersionID))
		}
		if module.Source != "" {
			item.RawSetString("source", lua.LString(module.Source))
		}
		if module.Digest != "" {
			item.RawSetString("digest", lua.LString(module.Digest))
		}
		if module.SizeBytes != 0 {
			item.RawSetString("size_bytes", lua.LNumber(module.SizeBytes))
		}
		if module.Protected {
			item.RawSetString("protected", lua.LTrue)
		}
		modules.RawSetInt(i+1, item)
	}
	table.RawSetString("modules", modules)
	return table
}

func dependencyRootsToLuaTable(l *lua.LState, roots []regapi.DependencyRoot) *lua.LTable {
	table := l.CreateTable(len(roots), 0)
	for i, root := range roots {
		item := l.CreateTable(0, 3)
		item.RawSetString("id", lua.LString(root.ID))
		item.RawSetString("component", lua.LString(root.Component))
		item.RawSetString("version", lua.LString(root.Version))
		table.RawSetInt(i+1, item)
	}
	return table
}

// filterOperators are the finder operator prefixes a metadata selector may carry.
const filterOperators = "~*^$"

// rootFieldPrefix marks a finder root selector such as ".kind".
const rootFieldPrefix = "."

// metaFieldPrefix marks a finder metadata selector such as "meta.type".
const metaFieldPrefix = "meta."

// rootSelectorList spells the accepted root selectors for error messages.
const rootSelectorList = `".kind", ".ns", ".name" or ".id"`

// rootFilterFields are the entry fields a root selector addresses.
var rootFilterFields = map[string]struct{}{
	"kind": {},
	"ns":   {},
	"name": {},
	"id":   {},
}

// splitFilterOperator separates a leading finder operator from the field name.
func splitFilterOperator(key string) (operator, field string) {
	if key != "" && strings.ContainsRune(filterOperators, rune(key[0])) {
		return key[:1], key[1:]
	}
	return "", key
}

// checkFilterKey validates a top-level filter key. A key is a selector: it
// either starts with "." and names an entry field, or carries an optional
// operator and names a metadata field as "meta.<field>".
func checkFilterKey(key string) error {
	if strings.HasPrefix(key, rootFieldPrefix) {
		field := strings.TrimPrefix(key, rootFieldPrefix)
		if _, ok := rootFilterFields[field]; ok {
			return nil
		}
		return fmt.Errorf("filter key %q is not a root selector: use %s for entry fields or %q for metadata",
			key, rootSelectorList, metaFieldPrefix+field)
	}

	operator, field := splitFilterOperator(key)
	if strings.HasPrefix(field, metaFieldPrefix) {
		if field == metaFieldPrefix {
			return fmt.Errorf("filter key %q names no metadata field", key)
		}
		return nil
	}

	metaForm := operator + metaFieldPrefix + field
	if _, ok := rootFilterFields[field]; ok && operator == "" {
		return fmt.Errorf("filter key %q is not a selector: use %q for the entry %s or %q for metadata",
			key, rootFieldPrefix+field, field, metaForm)
	}
	return fmt.Errorf("filter key %q is not a selector: use %s for entry fields or %q for metadata",
		key, rootSelectorList, metaForm)
}

// checkMetaFilterKey validates a key of the nested meta table. Those keys name
// metadata fields directly, so they carry no prefix beyond an operator.
func checkMetaFilterKey(key string) error {
	operator, field := splitFilterOperator(key)
	switch {
	case field == "":
		return fmt.Errorf("filter key %q in the meta table names no metadata field", key)
	case strings.HasPrefix(field, metaFieldPrefix):
		return fmt.Errorf("filter key %q in the meta table repeats the %q prefix: use %q",
			key, metaFieldPrefix, operator+strings.TrimPrefix(field, metaFieldPrefix))
	case strings.HasPrefix(field, rootFieldPrefix):
		return fmt.Errorf("filter key %q in the meta table must name a metadata field", key)
	}
	return nil
}

// convertFilterToMetadata converts a Lua filter table to finder search criteria.
// Top-level keys are selectors; the nested meta table names metadata fields.
func convertFilterToMetadata(_ *lua.LState, filterTable *lua.LTable) (attrs.Bag, error) {
	meta := attrs.Bag{}
	var keyErr error

	nested, hasNested := filterTable.RawGetString("meta").(*lua.LTable)

	filterTable.ForEach(func(k, v lua.LValue) {
		if keyErr != nil {
			return
		}

		kStr, ok := k.(lua.LString)
		if !ok {
			keyErr = fmt.Errorf("filter keys must be strings, got %s", k.Type())
			return
		}

		key := string(kStr)
		if key == "meta" {
			if !hasNested {
				keyErr = errors.New(`filter key "meta" must be a table of metadata selectors`)
			}
			return
		}

		if err := checkFilterKey(key); err != nil {
			keyErr = err
			return
		}

		meta[key] = value.ToGoAny(v)
	})

	if hasNested {
		nested.ForEach(func(k, v lua.LValue) {
			if keyErr != nil {
				return
			}

			kStr, ok := k.(lua.LString)
			if !ok {
				keyErr = fmt.Errorf("meta filter keys must be strings, got %s", k.Type())
				return
			}

			key := string(kStr)
			if err := checkMetaFilterKey(key); err != nil {
				keyErr = err
				return
			}

			operator, field := splitFilterOperator(key)
			meta[operator+metaFieldPrefix+field] = value.ToGoAny(v)
		})
	}

	if keyErr != nil {
		return nil, keyErr
	}

	return meta, nil
}
