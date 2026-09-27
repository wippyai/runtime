// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"

	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/contract"
)

type ManifestDiagnostic struct {
	Definition string
	Method     string
	Path       string
	Message    string
}

// BuildTypedManifest synthesizes ordinary go-lua types from lock-loaded
// contract definitions. Schemas and their source locations remain host data.
// The broad get overload preserves dynamic dispatch for non-singleton IDs.
func BuildTypedManifest(definitions map[string]*api.Definition, resources map[string]any) (*io.Manifest, []ManifestDiagnostic) {
	m := io.NewManifest("contract")
	m.DefineType("MethodDefinition", methodDefinitionType)
	m.DefineType("SchemaDefinition", schemaDefinitionType)
	m.DefineType("Contract", contractType)

	ids := make([]string, 0, len(definitions))
	for id := range definitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	get := make([]typ.Type, 0, len(ids)+1)
	var diagnostics []ManifestDiagnostic
	for _, id := range ids {
		def := definitions[id]
		if def == nil {
			continue
		}
		instance := typedInstance(id, def, resources, m, &diagnostics)
		wrapper := typedWrapper(id, instance)
		prefix := manifestTypePrefix(id)
		m.DefineType(prefix+"Contract", wrapper)
		m.DefineType(prefix+"Instance", instance)
		get = append(get, typ.Func().Param("name", typ.LiteralString(id)).Returns(wrapper, typ.NewOptional(typ.LuaError)).Build())
	}
	get = append(get, typ.Func().Param("name", typ.String).Returns(contractType, typ.NewOptional(typ.LuaError)).Build())
	module := typ.NewRecord().SetDeclared(true).
		Field("get", typ.NewIntersection(get...)).
		Field("open", typ.Func().Param("name", typ.String).OptParam("scope", typ.Any).OptParam("options", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()).
		Field("find_implementations", typ.Func().Param("name", typ.String).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()).
		Field("is", typ.Func().Param("value", typ.Any).Param("name", typ.String).Returns(typ.Boolean).Build()).Build()
	m.SetExport(module)
	return m, diagnostics
}

func typedInstance(id string, def *api.Definition, resources map[string]any, m *io.Manifest, diagnostics *[]ManifestDiagnostic) typ.Type {
	return typ.NewRecursive("Instance<"+id+">", func(self typ.Type) typ.Type {
		methods := typ.NewRecord().SetDeclared(true)
		for _, method := range def.Methods {
			fn := typ.Func().Param("self", self)
			prefix := manifestTypePrefix(id) + manifestTypePrefix(method.Name)
			if len(method.InputSchemas) == 0 {
				fn.Variadic(typ.Any)
				*diagnostics = append(*diagnostics, ManifestDiagnostic{id, method.Name, "input_schemas", "input schemas unspecified; argument conformance cannot be verified"})
			} else {
				inputs := make([]typ.Type, len(method.InputSchemas))
				for i, schema := range method.InputSchemas {
					path := "input_schemas[" + strconv.Itoa(i) + "]"
					projection := TranslateSchema(schema, resources)
					inputs[i] = projection.Type
					m.DefineType(prefix+"Input"+strconv.Itoa(i+1), projection.Type)
					appendProjectionDiagnostics(diagnostics, id, method.Name, path, projection)
				}
				firstOptional := len(inputs)
				for firstOptional > 0 && schemaAcceptsNil(inputs[firstOptional-1]) {
					firstOptional--
				}
				for i, input := range inputs {
					name := "arg" + strconv.Itoa(i+1)
					if i >= firstOptional {
						fn.OptParam(name, input)
					} else {
						fn.Param(name, input)
					}
				}
			}
			output := typ.Type(typ.Unknown)
			switch len(method.OutputSchemas) {
			case 0:
				*diagnostics = append(*diagnostics, ManifestDiagnostic{id, method.Name, "output_schemas", "output schema unspecified; result conformance cannot be verified"})
			case 1:
				projection := TranslateSchema(method.OutputSchemas[0], resources)
				output = projection.Type
				m.DefineType(prefix+"Output", output)
				appendProjectionDiagnostics(diagnostics, id, method.Name, "output_schemas[0]", projection)
			default:
				*diagnostics = append(*diagnostics, ManifestDiagnostic{id, method.Name, "output_schemas", "multiple output schemas are ambiguous; result widened to unknown"})
			}
			methods.Field(method.Name, fn.Returns(output, typ.NewOptional(typ.LuaError)).Build())
		}
		return methods.Build()
	})
}

func typedWrapper(id string, instance typ.Type) typ.Type {
	return typ.NewRecursive("Contract<"+id+">", func(self typ.Type) typ.Type {
		// A no-binding open uses the definition's promised surface. A broad
		// explicit binding is dynamic until its membership is proved.
		open := typ.NewIntersection(
			typ.Func().Param("self", self).Returns(instance, typ.NewOptional(typ.LuaError)).Build(),
			typ.Func().Param("self", self).Param("name", typ.String).OptParam("scope", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build(),
		)
		return typ.NewRecord().SetDeclared(true).
			Field("id", typ.Func().Param("self", self).Returns(typ.String).Build()).
			Field("methods", typ.Func().Param("self", self).Returns(typ.NewArray(methodDefinitionType)).Build()).
			Field("method", typ.Func().Param("self", self).Param("name", typ.String).Returns(methodDefinitionType, typ.NewOptional(typ.LuaError)).Build()).
			Field("implementations", typ.Func().Param("self", self).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()).
			Field("open", open).
			Field("with_context", typ.Func().Param("self", self).Param("ctx", typ.Any).Returns(self).Build()).
			Field("with_actor", typ.Func().Param("self", self).Param("actor", typ.Any).Returns(self).Build()).
			Field("with_scope", typ.Func().Param("self", self).Param("scope", typ.Any).Returns(self).Build()).
			Field("with_options", typ.Func().Param("self", self).Param("options", typ.Any).Returns(self).Build()).Build()
	})
}

func appendProjectionDiagnostics(into *[]ManifestDiagnostic, id, method, path string, projection SchemaProjection) {
	for _, d := range projection.Diagnostics {
		*into = append(*into, ManifestDiagnostic{id, method, path + d.Path, d.Message})
	}
	if projection.Coverage == SchemaUnconstrained {
		*into = append(*into, ManifestDiagnostic{id, method, path, "schema is unconstrained; conformance cannot be verified"})
	}
}

func schemaAcceptsNil(t typ.Type) bool {
	if t == typ.Nil {
		return true
	}
	if _, ok := t.(*typ.Optional); ok {
		return true
	}
	if u, ok := t.(*typ.Union); ok {
		for _, member := range u.Members {
			if member == typ.Nil {
				return true
			}
		}
	}
	return false
}

func manifestTypePrefix(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("T%s", hex.EncodeToString(h[:4]))
}
