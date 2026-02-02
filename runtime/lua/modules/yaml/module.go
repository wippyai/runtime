package yaml

import (
	"sort"
	"strings"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/wippyai/go-lua"
	"gopkg.in/yaml.v3"
)

// Options holds formatting options for YAML encoding
type Options struct {
	FieldOrder    []string
	SortUnordered bool
}

// Module is the yaml module definition.
var Module = &luaapi.ModuleDef{
	Name:        "yaml",
	Description: "YAML encoding and decoding",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 2)
		mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
		mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func encodeFunc(l *lua.LState) int {
	if l.GetTop() < 1 {
		return invalidError(l, "table expected")
	}

	luaVal := l.Get(1)
	if luaVal.Type() != lua.LTTable {
		return invalidError(l, "table expected")
	}

	options := Options{
		FieldOrder:    []string{},
		SortUnordered: false,
	}

	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		optionsTable := l.CheckTable(2)
		extractOptions(optionsTable, &options)
	}

	goVal := value.ToGoAny(luaVal)

	if len(options.FieldOrder) > 0 || options.SortUnordered {
		node := yaml.Node{}
		err := node.Encode(goVal)
		if err != nil {
			return internalError(l, err, "encode to node failed")
		}
		processNode(&node, &options)

		var buf strings.Builder
		encoder := yaml.NewEncoder(&buf)
		encoder.SetIndent(2)
		if err = encoder.Encode(&node); err != nil {
			return internalError(l, err, "encode failed")
		}
		l.Push(lua.LString(buf.String()))
	} else {
		data, err := yaml.Marshal(goVal)
		if err != nil {
			return internalError(l, err, "encode failed")
		}
		l.Push(lua.LString(data))
	}

	l.Push(lua.LNil)
	return 2
}

func extractOptions(table *lua.LTable, options *Options) {
	if val := table.RawGetString("sort_unordered"); val.Type() == lua.LTBool {
		options.SortUnordered = lua.LVAsBool(val)
	}

	if val := table.RawGetString("field_order"); val.Type() == lua.LTTable {
		options.FieldOrder = tableToStringSlice(val.(*lua.LTable))
	}
}

func tableToStringSlice(table *lua.LTable) []string {
	var result []string
	maxN := table.MaxN()

	if maxN > 0 {
		for i := 1; i <= maxN; i++ {
			if v := table.RawGetInt(i); v.Type() == lua.LTString {
				result = append(result, v.String())
			}
		}
	}

	return result
}

func decodeFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		return invalidError(l, "string expected")
	}

	if str == "" {
		return invalidError(l, "input cannot be empty")
	}

	var data interface{}
	if err := yaml.Unmarshal([]byte(str), &data); err != nil {
		return internalError(l, err, "decode failed")
	}

	lv, err := luaconv.GoToLua(data)
	if err != nil {
		return internalError(l, err, "convert to Lua failed")
	}

	l.Push(lv)
	l.Push(lua.LNil)
	return 2
}

func processNode(node *yaml.Node, options *Options) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.DocumentNode:
		for _, content := range node.Content {
			processNode(content, options)
		}

	case yaml.MappingNode:
		orderMappingNode(node, options.FieldOrder, options.SortUnordered)
		for i := 0; i < len(node.Content); i += 2 {
			if i+1 < len(node.Content) {
				processNode(node.Content[i+1], options)
			}
		}

	case yaml.SequenceNode:
		for _, content := range node.Content {
			processNode(content, options)
		}
	}
}

func orderMappingNode(node *yaml.Node, fieldOrder []string, sortUnordered bool) {
	if node == nil || len(node.Content) < 2 {
		return
	}

	orderIndexMap := make(map[string]int)
	for i, fieldName := range fieldOrder {
		orderIndexMap[fieldName] = i
	}

	keyValuePairs := make([][2]*yaml.Node, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			if keyNode.Kind == yaml.ScalarNode {
				keyValuePairs = append(keyValuePairs, [2]*yaml.Node{keyNode, valueNode})
			}
		}
	}

	sort.SliceStable(keyValuePairs, func(i, j int) bool {
		keyNameI := keyValuePairs[i][0].Value
		keyNameJ := keyValuePairs[j][0].Value

		orderI, existsI := orderIndexMap[keyNameI]
		orderJ, existsJ := orderIndexMap[keyNameJ]

		if existsI && existsJ {
			return orderI < orderJ
		}
		if existsI {
			return true
		}
		if existsJ {
			return false
		}
		if sortUnordered {
			return keyNameI < keyNameJ
		}
		return false
	})

	newContent := make([]*yaml.Node, 0, len(keyValuePairs)*2)
	for _, pair := range keyValuePairs {
		newContent = append(newContent, pair[0], pair[1])
	}
	node.Content = newContent
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}
