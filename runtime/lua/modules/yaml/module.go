package yaml

import (
	"fmt"
	"sort"
	"strings"

	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"gopkg.in/yaml.v3"
)

// YAMLOptions holds all formatting options for YAML encoding
//
//nolint:revive
type YAMLOptions struct {
	// Basic formatting options
	Indent        int      // Number of spaces for indentation (default: 2)
	FieldOrder    []string // Custom field ordering
	SortUnordered bool     // Sort fields not in FieldOrder alphabetically (default: false)

	// Style options for different node types
	ScalarStyle   string // Style for scalar values: "", "plain", "single", "double", "literal", "folded"
	MappingStyle  string // Style for mappings: "", "flow", "block"
	SequenceStyle string // Style for sequences: "", "flow", "block"

	// Special formatting helpers
	CompactSequences       bool // Make short sequences compact using flow style (default: false)
	CompactSequenceLimit   int  // Max items for a sequence to be considered "short" (default: 5)
	CompactNestedSequences bool // Apply compact style to sequences inside other sequences (default: true)
	EmitDefaults           bool // Emit zero/default values (opposite of omitempty) (default: true)
}

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
	mod := l.CreateTable(0, 2)
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

	// Create default options
	options := YAMLOptions{
		Indent:                 2,
		FieldOrder:             []string{},
		SortUnordered:          false,
		ScalarStyle:            "",
		MappingStyle:           "",
		SequenceStyle:          "",
		CompactSequences:       false,
		CompactSequenceLimit:   5,
		CompactNestedSequences: true,
		EmitDefaults:           true,
	}

	// Extract options from Lua table if provided
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		optionsTable := l.CheckTable(2)
		extractOptions(optionsTable, &options)
	}

	// Convert Lua value to Go
	goVal := luaconv.ToGoAny(luaVal)

	// Create YAML node
	node := yaml.Node{}
	err := node.Encode(goVal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("error encoding to YAML node: %v", err)))
		return 2
	}

	// Process node for formatting with initial depth of 0
	processNode(&node, &options, 0)

	// Marshal the processed node
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(options.Indent)

	err = encoder.Encode(&node)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("error marshaling YAML: %v", err)))
		return 2
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

// extractOptions converts a Lua options table to YAMLOptions struct
func extractOptions(table *lua.LTable, options *YAMLOptions) {
	// Helper functions for type extraction
	getBool := func(key string, defaultVal bool) bool {
		val := table.RawGetString(key)
		if val.Type() == lua.LTBool {
			return lua.LVAsBool(val)
		}
		return defaultVal
	}

	getInt := func(key string, defaultVal int) int {
		val := table.RawGetString(key)
		if val.Type() == lua.LTNumber {
			return int(lua.LVAsNumber(val))
		}
		return defaultVal
	}

	getString := func(key string, defaultVal string) string {
		val := table.RawGetString(key)
		if val.Type() == lua.LTString {
			return val.String()
		}
		return defaultVal
	}

	// Extract field_order array if it exists
	fieldOrderVal := table.RawGetString("field_order")
	if fieldOrderVal.Type() == lua.LTTable {
		fieldOrderTable := fieldOrderVal.(*lua.LTable)
		options.FieldOrder = tableToStringSlice(fieldOrderTable)
	}

	// Extract other options
	options.Indent = getInt("indent", options.Indent)
	options.SortUnordered = getBool("sort_unordered", options.SortUnordered)
	options.ScalarStyle = getString("scalar_style", options.ScalarStyle)
	options.MappingStyle = getString("mapping_style", options.MappingStyle)
	options.SequenceStyle = getString("sequence_style", options.SequenceStyle)
	options.CompactSequences = getBool("compact_sequences", options.CompactSequences)
	options.CompactSequenceLimit = getInt("compact_sequence_limit", options.CompactSequenceLimit)
	options.CompactNestedSequences = getBool("compact_nested_sequences", options.CompactNestedSequences)
	options.EmitDefaults = getBool("emit_defaults", options.EmitDefaults)
}

// tableToStringSlice converts a Lua table to a string slice
func tableToStringSlice(table *lua.LTable) []string {
	var result []string
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

	yamlStr := l.CheckString(1)
	if yamlStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("first argument must be a string"))
		return 2
	}

	var data interface{}
	err := yaml.Unmarshal([]byte(yamlStr), &data)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("error unmarshaling YAML: %v", err)))
		return 2
	}

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

// processNode walks through the YAML node tree and applies formatting options
func processNode(node *yaml.Node, options *YAMLOptions, depth int) {
	if node == nil {
		return
	}

	// Apply style based on node kind
	switch node.Kind {
	case yaml.DocumentNode:
		// Document nodes don't have style options, just process content
		for _, content := range node.Content {
			processNode(content, options, depth)
		}

	case yaml.MappingNode:
		// Apply mapping style
		switch options.MappingStyle {
		case "flow":
			node.Style = yaml.FlowStyle
		case "block":
			node.Style = 0 // Default block style
		}

		// Apply field ordering if provided
		if len(options.FieldOrder) > 0 || options.SortUnordered {
			orderMappingNode(node, options.FieldOrder, options.SortUnordered)
		}

		// Process all child nodes with increased depth
		for i := 0; i < len(node.Content); i += 2 {
			if i+1 < len(node.Content) {
				processNode(node.Content[i], options, depth+1)   // Key
				processNode(node.Content[i+1], options, depth+1) // Value
			}
		}

	case yaml.SequenceNode:
		// Determine if this is a nested sequence
		// A sequence is nested if it's deeper than level 1
		// Level 0 is the document node, level 1 is top-level nodes
		isNested := depth > 1

		// Apply sequence style
		switch {
		case options.SequenceStyle == "flow":
			node.Style = yaml.FlowStyle
		case options.SequenceStyle == "block":
			node.Style = 0 // Default block style
		case options.CompactSequences &&
			(options.CompactNestedSequences || !isNested) &&
			isSimpleSequence(node, options):
			// Apply flow style for short, simple sequences
			// Only apply to nested sequences if CompactNestedSequences is true
			node.Style = yaml.FlowStyle
		}

		// Process each sequence item with increased depth
		for _, content := range node.Content {
			processNode(content, options, depth+1)
		}

	case yaml.ScalarNode:
		// Apply scalar style based on content and options
		switch options.ScalarStyle {
		case "single":
			node.Style = yaml.SingleQuotedStyle
		case "double":
			node.Style = yaml.DoubleQuotedStyle
		case "literal":
			node.Style = yaml.LiteralStyle
		case "folded":
			node.Style = yaml.FoldedStyle
		case "plain":
			node.Style = 0 // Plain style
		default:
			// Auto style - use literal for multiline strings
			if strings.Contains(node.Value, "\n") {
				node.Style = yaml.LiteralStyle
			}
		}

	case yaml.AliasNode:
		// Alias nodes refer to anchor nodes, so we don't need to process them further
		// The actual content is in the referenced node
		if node.Alias != nil {
			// Only process the alias target if it hasn't been processed yet
			processNode(node.Alias, options, depth)
		}
	}
}

// isSimpleSequence checks if a sequence is short and contains only simple items
func isSimpleSequence(node *yaml.Node, options *YAMLOptions) bool {
	if node.Kind != yaml.SequenceNode {
		return false
	}

	// Check against customizable limit
	if len(node.Content) > options.CompactSequenceLimit {
		return false
	}

	// Check if all items are simple scalars (no nested structures)
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return false
		}
		// Avoid flow style for multiline items
		if strings.Contains(item.Value, "\n") {
			return false
		}
	}
	return true
}

// orderMappingNode reorders mapping node fields based on field order
func orderMappingNode(node *yaml.Node, fieldOrder []string, sortUnordered bool) {
	if node == nil || len(node.Content) < 2 {
		return
	}

	// Build a map of field names to their index in fieldOrder
	orderIndexMap := make(map[string]int)
	for i, fieldName := range fieldOrder {
		orderIndexMap[fieldName] = i
	}

	// Extract key-value pairs
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

	// Sort key-value pairs
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

		// Otherwise, keep original order
		return false
	})

	// Rebuild the node content
	newContent := make([]*yaml.Node, 0, len(keyValuePairs)*2)
	for _, pair := range keyValuePairs {
		newContent = append(newContent, pair[0], pair[1])
	}
	node.Content = newContent
}
