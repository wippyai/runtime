// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/contract"
)

// SchemaDialect is the dialect used for contract schemas. OpenAPI's nullable
// keyword is deliberately not part of this dialect.
const SchemaDialect = "https://json-schema.org/draft/2020-12/schema"

type SchemaCoverage uint8

const (
	SchemaComplete SchemaCoverage = iota
	SchemaUnconstrained
	SchemaIncomplete
)

type SchemaDiagnostic struct {
	Path    string
	Message string
}

type SchemaProjection struct {
	Type        typ.Type
	Coverage    SchemaCoverage
	Diagnostics []SchemaDiagnostic
}

// SchemaFormatTranslator owns projection semantics and a cache version for one
// schema format. Registrations are shared by manifests and conformance checks.
type SchemaFormatTranslator interface {
	Version() string
	Translate(definition any, resources map[string]any) SchemaProjection
}

var schemaFormats = struct {
	sync.RWMutex
	translators map[string]SchemaFormatTranslator
}{translators: map[string]SchemaFormatTranslator{"application/schema+json": jsonSchemaTranslator{}}}

func RegisterSchemaTranslator(format string, translator SchemaFormatTranslator) {
	schemaFormats.Lock()
	defer schemaFormats.Unlock()
	schemaFormats.translators[format] = translator
}

func SchemaTranslatorVersion(format string) string {
	schemaFormats.RLock()
	translator := schemaFormats.translators[format]
	schemaFormats.RUnlock()
	if translator == nil {
		return ""
	}
	return translator.Version()
}

type jsonSchemaTranslator struct{}

func (jsonSchemaTranslator) Version() string { return "json-schema-2020-12-v1" }

// TranslateSchema projects the validation constraints that Lua's structural
// checker can prove. resources are explicitly supplied JSON Schema documents,
// indexed by URI. Resolution never fetches from the network.
func TranslateSchema(schema api.SchemaDefinition, resources map[string]any) SchemaProjection {
	schemaFormats.RLock()
	translator := schemaFormats.translators[schema.Format]
	schemaFormats.RUnlock()
	if translator == nil {
		return SchemaProjection{Type: typ.Unknown, Coverage: SchemaIncomplete, Diagnostics: []SchemaDiagnostic{{"$", "schema format " + schema.Format + " is not supported"}}}
	}
	return translator.Translate(schema.Definition, resources)
}

func (jsonSchemaTranslator) Translate(definition any, resources map[string]any) SchemaProjection {
	result := SchemaProjection{Type: typ.Unknown, Coverage: SchemaUnconstrained}
	root, err := schemaDocument(definition)
	if err != nil {
		result.Coverage = SchemaIncomplete
		result.Diagnostics = []SchemaDiagnostic{{"$", err.Error()}}
		return result
	}
	tr := schemaTranslator{root: root, resources: resources, visiting: map[string]bool{}}
	result.Type = tr.translate(root, "$", 0)
	result.Diagnostics = tr.diagnostics
	if len(result.Diagnostics) != 0 {
		result.Coverage = SchemaIncomplete
	} else if result.Type != typ.Unknown {
		result.Coverage = SchemaComplete
	}
	return result
}

func schemaDocument(raw any) (any, error) {
	var b []byte
	switch v := raw.(type) {
	case json.RawMessage:
		b = v
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		var err error
		b, err = json.Marshal(raw)
		if err != nil {
			return nil, err
		}
	}
	var value any
	if err := json.Unmarshal(b, &value); err != nil {
		return nil, fmt.Errorf("invalid JSON Schema: %w", err)
	}
	// YAML contract entries commonly quote the JSON document. After the
	// entry is decoded through SchemaConfig's RawMessage, that document is a
	// JSON string literal and needs one more decode before projection.
	if encoded, ok := value.(string); ok {
		if err := json.Unmarshal([]byte(encoded), &value); err != nil {
			return nil, fmt.Errorf("invalid JSON Schema: %w", err)
		}
	}
	return value, nil
}

type schemaTranslator struct {
	root        any
	resources   map[string]any
	visiting    map[string]bool
	diagnostics []SchemaDiagnostic
}

func (tr *schemaTranslator) gap(path, message string) {
	tr.diagnostics = append(tr.diagnostics, SchemaDiagnostic{path, message})
}

func (tr *schemaTranslator) translate(raw any, path string, depth int) typ.Type {
	if depth > 64 {
		tr.gap(path, "schema nesting exceeds limit")
		return typ.Unknown
	}
	if b, ok := raw.(bool); ok {
		if b {
			return typ.Unknown
		}
		return typ.Never
	}
	m, ok := raw.(map[string]any)
	if !ok {
		tr.gap(path, "schema must be an object or boolean")
		return typ.Unknown
	}
	for k := range m {
		if !schemaKnownKeyword[k] {
			tr.gap(path+"/"+k, "unsupported validation keyword "+k)
		}
	}
	if dialect, ok := m["$schema"].(string); ok && dialect != SchemaDialect {
		tr.gap(path+"/$schema", "unsupported JSON Schema dialect "+dialect)
		return typ.Unknown
	}
	if _, ok := m["nullable"]; ok {
		tr.gap(path+"/nullable", "use 2020-12 type union with null")
	}
	if _, typed := m["type"]; !typed {
		for _, keyword := range []string{"properties", "required", "additionalProperties", "items", "prefixItems"} {
			if _, constrained := m[keyword]; constrained {
				tr.gap(path+"/"+keyword, "constraint applies conditionally without a type declaration")
			}
		}
	}

	var base typ.Type = typ.Unknown
	if ref, ok := m["$ref"].(string); ok {
		if tr.visiting[ref] {
			tr.gap(path+"/$ref", "recursive reference widened to unknown")
			return typ.Unknown
		}
		tr.visiting[ref] = true
		resolved, found := tr.resolve(ref)
		if found {
			base = tr.translate(resolved, path+"/$ref", depth+1)
		} else {
			tr.gap(path+"/$ref", "unresolved reference "+ref)
		}
		delete(tr.visiting, ref)
	}
	if rawType, exists := m["type"]; exists {
		var typed typ.Type = typ.Unknown
		switch v := rawType.(type) {
		case string:
			typed = tr.typed(v, m, path, depth)
		case []any:
			members := make([]typ.Type, 0, len(v))
			for i, member := range v {
				name, ok := member.(string)
				if !ok {
					tr.gap(path+"/type/"+strconv.Itoa(i), "invalid type member")
					return typ.Unknown
				}
				members = append(members, tr.typed(name, m, path, depth))
			}
			typed = schemaUnion(members)
		default:
			tr.gap(path+"/type", "invalid type declaration")
		}
		if base == typ.Unknown {
			base = typed
		} else if typed != typ.Unknown {
			base = typ.NewIntersection(base, typed)
		}
	}
	if value, ok := m["const"]; ok {
		base = tr.narrowLiteral(base, value, path+"/const")
	}
	if values, ok := m["enum"].([]any); ok {
		members := make([]typ.Type, 0, len(values))
		for i, v := range values {
			members = append(members, tr.narrowLiteral(base, v, path+"/enum/"+strconv.Itoa(i)))
		}
		base = schemaUnion(members)
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		if branches, ok := m[key].([]any); ok {
			members := make([]typ.Type, 0, len(branches))
			for i, branch := range branches {
				members = append(members, tr.translate(branch, path+"/"+key+"/"+strconv.Itoa(i), depth+1))
			}
			branchType := schemaUnion(members)
			if key == "oneOf" {
				tr.gap(path+"/oneOf", "exclusive branch matching cannot be verified statically")
			}
			if base == typ.Unknown {
				base = branchType
			} else if branchType != typ.Unknown {
				base = typ.NewIntersection(base, branchType)
			}
		}
	}
	return base
}

func schemaUnion(members []typ.Type) typ.Type {
	for _, member := range members {
		if member == typ.Unknown {
			return typ.Unknown
		}
	}
	return typ.NewUnion(members...)
}

func (tr *schemaTranslator) typed(name string, m map[string]any, path string, depth int) typ.Type {
	switch name {
	case "string":
		return typ.String
	case "boolean":
		return typ.Boolean
	case "integer":
		return typ.Integer
	case "number":
		return typ.Number
	case "null":
		return typ.Nil // Lua cannot distinguish JSON null from an absent value.
	case "array":
		if _, exists := m["prefixItems"]; exists {
			tr.gap(path+"/prefixItems", "tuple positions are not represented")
			return typ.NewArray(typ.Unknown)
		}
		items, exists := m["items"]
		if !exists || items == true {
			return typ.NewArray(typ.Unknown)
		}
		return typ.NewArray(tr.translate(items, path+"/items", depth+1))
	case "object":
		builder := typ.NewRecord().SetDeclared(true)
		properties, _ := m["properties"].(map[string]any)
		required := map[string]bool{}
		if names, ok := m["required"].([]any); ok {
			for _, raw := range names {
				if name, ok := raw.(string); ok {
					required[name] = true
				}
			}
		}
		keys := make([]string, 0, len(properties))
		for name := range properties {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			field := tr.translate(properties[name], path+"/properties/"+name, depth+1)
			if required[name] {
				builder.Field(name, field)
			} else {
				builder.OptField(name, field)
			}
			delete(required, name)
		}
		missing := make([]string, 0, len(required))
		for name := range required {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		for _, name := range missing {
			field := typ.Type(typ.Unknown)
			if extras, ok := m["additionalProperties"].(map[string]any); ok {
				field = tr.translate(extras, path+"/additionalProperties", depth+1)
			}
			builder.Field(name, field)
		}
		switch extras := m["additionalProperties"].(type) {
		case nil, bool:
			if extras != false {
				builder.SetOpen(true)
			} else {
				tr.gap(path+"/additionalProperties", "exact rejection of extra keys cannot be verified structurally")
			}
		default:
			builder.MapComponent(typ.String, tr.translate(extras, path+"/additionalProperties", depth+1))
		}
		return builder.Build()
	default:
		tr.gap(path+"/type", "unsupported type "+name)
		return typ.Unknown
	}
}

func (tr *schemaTranslator) narrowLiteral(base typ.Type, value any, path string) typ.Type {
	var literal typ.Type
	switch v := value.(type) {
	case string:
		literal = typ.LiteralString(v)
	case bool:
		literal = typ.LiteralBool(v)
	case nil:
		literal = typ.Nil
	case float64:
		if v == float64(int64(v)) {
			literal = typ.LiteralInt(int64(v))
		} else {
			literal = typ.LiteralNumber(v)
		}
	default:
		tr.gap(path, "composite equality cannot be verified statically")
		return base
	}
	if base == typ.Unknown {
		return literal
	}
	return typ.NewIntersection(base, literal)
}

func (tr *schemaTranslator) resolve(ref string) (any, bool) {
	uri, fragment, hasFragment := strings.Cut(ref, "#")
	var doc any
	if uri == "" {
		doc = tr.root
	} else {
		var found bool
		doc, found = tr.resources[uri]
		if !found {
			return nil, false
		}
		var err error
		doc, err = schemaDocument(doc)
		if err != nil {
			return nil, false
		}
	}
	if !hasFragment || fragment == "" {
		return doc, true
	}
	if !strings.HasPrefix(fragment, "/") {
		return nil, false
	}
	for _, piece := range strings.Split(fragment[1:], "/") {
		piece = strings.ReplaceAll(strings.ReplaceAll(piece, "~1", "/"), "~0", "~")
		if m, ok := doc.(map[string]any); ok {
			doc, ok = m[piece]
			if !ok {
				return nil, false
			}
			continue
		}
		if a, ok := doc.([]any); ok {
			i, err := strconv.Atoi(piece)
			if err != nil || i < 0 || i >= len(a) {
				return nil, false
			}
			doc = a[i]
			continue
		}
		return nil, false
	}
	return doc, true
}

var schemaKnownKeyword = map[string]bool{
	"$schema": true, "$id": true, "$defs": true, "$ref": true, "type": true,
	"properties": true, "required": true, "additionalProperties": true,
	"items": true, "prefixItems": true, "enum": true, "const": true,
	"anyOf": true, "oneOf": true, "description": true, "title": true,
	"default": true, "examples": true, "$comment": true, "readOnly": true,
	"writeOnly": true, "deprecated": true,
}
