// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"errors"
	"fmt"
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/logs"
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

// convertFilterToMetadata converts a Lua filter table to registry metadata
func convertFilterToMetadata(l *lua.LState, filterTable *lua.LTable) attrs.Bag {
	meta := attrs.Bag{}
	deprecated := false

	filterTable.ForEach(func(k, v lua.LValue) {
		if kStr, ok := k.(lua.LString); ok {
			key := string(kStr)

			if key == "meta" {
				deprecated = true
				return
			}

			if !supportedFilterSelector(key) {
				deprecated = true
			}
			meta[key] = value.ToGoAny(v)
		} else {
			deprecated = true
		}
	})

	// Process nested metadata table
	metaVal := filterTable.RawGetString("meta")
	if metaVal.Type() == lua.LTTable {
		metaTable := metaVal.(*lua.LTable)
		metaTable.ForEach(func(k, v lua.LValue) {
			if kStr, ok := k.(lua.LString); ok {
				key := string(kStr)
				meta[key] = value.ToGoAny(v)
			}
		})
	}

	// Diagnose legacy filters without changing their criteria, precedence, or
	// result shape. In particular, nested meta keys retain their old flattening.
	if deprecated {
		logs.GetLogger(l.Context()).Warn("deprecated registry.find/snapshot:find filter syntax; use flat selectors .kind, .ns, .name, .id or meta.<field> (optional ~, *, ^, $ metadata operator); legacy behavior is preserved and unsupported keys may leave the query unfiltered")
	}
	return meta
}

func supportedFilterSelector(key string) bool {
	switch key {
	case ".kind", ".ns", ".name", ".id":
		return true
	}
	if len(key) > 0 && strings.ContainsRune("~*^$", rune(key[0])) {
		key = key[1:]
	}
	return strings.HasPrefix(key, "meta.") && len(key) > len("meta.")
}
