package yaml

import (
	"fmt"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"gopkg.in/yaml.v3"
	"sort"
)

// Module represents a YAML Lua module
type Module struct{}

// NewYAMLModule creates and returns a new instance of the YAML Module
func NewYAMLModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "yaml"
}

// Loader registers the module's functions into Lua state
func (m *Module) Loader(l *lua.LState) int {
	mod := l.CreateTable(0, 2) // Pre-allocate for exactly 2 functions

	// Register only encode and decode functions
	mod.RawSetString("encode", l.NewFunction(m.encode))
	mod.RawSetString("decode", l.NewFunction(m.decode))

	l.Push(mod)
	return 1
}

// encode serializes a Lua table to a YAML string
func (m *Module) encode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("missing input table"))
		return 2
	}

	// Get the input table
	luaVal := l.Get(1)
	if luaVal.Type() != lua.LTTable {
		l.Push(lua.LNil)
		l.Push(lua.LString("first argument must be a table"))
		return 2
	}

	// Get optional field order table (second argument)
	var fieldOrder []string
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		orderTable := l.CheckTable(2)
		fieldOrder = tableToStringSlice(orderTable)
	}

	// Get optional sort_unordered parameter (third argument)
	sortUnordered := false
	if l.GetTop() >= 3 {
		sortUnordered = lua.LVAsBool(l.Get(3))
	}

	// Convert Lua value to Go using the provided conversion function
	goVal := luaconv.ToGoAny(luaVal)

	// Create YAML node
	node := yaml.Node{}
	err := node.Encode(goVal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("error encoding to YAML node: %v", err)))
		return 2
	}

	// Process node to set multiline strings to use literal style
	// and apply field ordering if provided
	processNode(&node, fieldOrder, sortUnordered)

	// Marshal the processed node
	yamlData, err := yaml.Marshal(&node)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("error marshaling YAML: %v", err)))
		return 2
	}

	l.Push(lua.LString(string(yamlData)))
	l.Push(lua.LNil)
	return 2
}

// tableToStringSlice converts a Lua table to a string slice
func tableToStringSlice(table *lua.LTable) []string {
	result := []string{}

	// First check if the table has an array part
	maxN := table.MaxN()
	if maxN > 0 {
		// Process array part
		for i := 1; i <= maxN; i++ {
			value := table.RawGetInt(i)
			if value.Type() == lua.LTString {
				result = append(result, value.String())
			}
		}
	} else {
		// Process hash part
		table.ForEach(func(_, value lua.LValue) {
			if value.Type() == lua.LTString {
				result = append(result, value.String())
			}
		})
	}

	return result
}

// decode deserializes a YAML string to a Lua table
func (m *Module) decode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("missing input YAML string"))
		return 2
	}

	// Get the input YAML string
	yamlStr := l.CheckString(1)
	if yamlStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("first argument must be a string"))
		return 2
	}

	// Unmarshal YAML to Go map
	var data interface{}
	err := yaml.Unmarshal([]byte(yamlStr), &data)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("error unmarshaling YAML: %v", err)))
		return 2
	}

	// Convert Go value to Lua using the provided conversion function
	lv, err := luaconv.GoToLua(data)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("error converting to Lua: %v", err)))
		return 2
	}

	l.Push(lv)
	l.Push(lua.LNil)
	return 2
}

// processNode walks through the YAML node tree, marks multiline strings as literal,
// and applies field ordering if provided
func processNode(node *yaml.Node, fieldOrder []string, sortUnordered bool) {
	if node == nil {
		return
	}

	// Mark multiline strings for literal style
	if node.Kind == yaml.ScalarNode && containsNewline(node.Value) {
		node.Style = yaml.LiteralStyle
	}

	// Process based on node kind
	switch node.Kind {
	case yaml.DocumentNode:
		// Process the document content
		for _, content := range node.Content {
			processNode(content, fieldOrder, sortUnordered)
		}
	case yaml.MappingNode:
		// Apply field ordering to mapping nodes if field order is provided
		if len(fieldOrder) > 0 || sortUnordered {
			orderMappingNode(node, fieldOrder, sortUnordered)
		} else {
			// Process child nodes without reordering
			for i := 0; i < len(node.Content); i += 2 {
				if i+1 < len(node.Content) {
					processNode(node.Content[i], fieldOrder, sortUnordered)
					processNode(node.Content[i+1], fieldOrder, sortUnordered)
				}
			}
		}
	case yaml.SequenceNode:
		// Process each item in the sequence
		for _, content := range node.Content {
			processNode(content, fieldOrder, sortUnordered)
		}
	}
}

// orderMappingNode reorders mapping node fields based on the provided field order
// If sortUnordered is true, fields not in fieldOrder will be sorted alphabetically
func orderMappingNode(node *yaml.Node, fieldOrder []string, sortUnordered bool) {
	if node == nil || len(node.Content) < 2 {
		return
	}

	// Build a map of field names to their index in the fieldOrder slice
	orderIndexMap := make(map[string]int)
	if fieldOrder != nil {
		for i, fieldName := range fieldOrder {
			orderIndexMap[fieldName] = i
		}
	}

	// Create a map of field name to key-value pair
	keyValuePairs := make([][2]*yaml.Node, 0, len(node.Content)/2)

	// Extract key-value pairs and their field names
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]

			if keyNode.Kind == yaml.ScalarNode {
				keyValuePairs = append(keyValuePairs, [2]*yaml.Node{keyNode, valueNode})
			}
		}
	}

	// Sort key-value pairs based on field order and/or alphabetically
	sort.SliceStable(keyValuePairs, func(i, j int) bool {
		keyNameI := keyValuePairs[i][0].Value
		keyNameJ := keyValuePairs[j][0].Value

		orderI, existsI := orderIndexMap[keyNameI]
		orderJ, existsJ := orderIndexMap[keyNameJ]

		// If both keys have a defined order, sort by order
		if existsI && existsJ {
			return orderI < orderJ
		}

		// If only one has defined order, prioritize it
		if existsI {
			return true
		}
		if existsJ {
			return false
		}

		// If neither has defined order and sortUnordered is true, sort alphabetically
		if sortUnordered {
			return keyNameI < keyNameJ
		}

		// Otherwise, keep original order (handled by SliceStable)
		return false
	})

	// Rebuild the node content with the ordered key-value pairs
	newContent := make([]*yaml.Node, 0, len(keyValuePairs)*2)
	for _, pair := range keyValuePairs {
		newContent = append(newContent, pair[0], pair[1])
	}

	// Update the node content
	node.Content = newContent

	// Process child nodes recursively
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			valueNode := node.Content[i+1]
			processNode(valueNode, fieldOrder, sortUnordered)
		}
	}
}

// Check if string contains newline characters
func containsNewline(s string) bool {
	for _, c := range s {
		if c == '\n' {
			return true
		}
	}
	return false
}
