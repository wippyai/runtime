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
	return BuildTypedCatalogManifest(definitions, nil, resources)
}

// BuildTypedCatalogManifest also specializes known binding IDs. Binding
// surfaces are merged from the definitions present in the same lint catalog.
func BuildTypedCatalogManifest(definitions map[string]*api.Definition, bindings map[string]*api.Binding, resources map[string]any) (*io.Manifest, []ManifestDiagnostic) {
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
	instances := make(map[string]typ.Type, len(ids))
	for _, id := range ids {
		def := definitions[id]
		if def == nil {
			continue
		}
		instance := typedInstance(id, def, resources, m, &diagnostics)
		instances[id] = instance
	}
	bindingInstances := typedBindingInstances(bindings, instances, m, &diagnostics)
	for _, id := range ids {
		instance := instances[id]
		if instance == nil {
			continue
		}
		wrapper := typedWrapper(id, instance, bindings, bindingInstances)
		prefix := manifestTypePrefix(id)
		m.DefineType(prefix+"Contract", wrapper)
		m.DefineType(prefix+"Instance", instance)
		get = append(get, typ.Func().Param("name", typ.LiteralString(id)).Returns(wrapper, typ.NewOptional(typ.LuaError)).Build())
	}
	get = append(get, typ.Func().Param("name", typ.String).Returns(contractType, typ.NewOptional(typ.LuaError)).Build())
	open := make([]typ.Type, 0, len(bindingInstances)+1)
	bindingIDs := make([]string, 0, len(bindingInstances))
	for id := range bindingInstances {
		bindingIDs = append(bindingIDs, id)
	}
	sort.Strings(bindingIDs)
	for _, id := range bindingIDs {
		open = append(open, typ.Func().Param("name", typ.LiteralString(id)).OptParam("scope", typ.Any).OptParam("options", typ.Any).Returns(bindingInstances[id], typ.NewOptional(typ.LuaError)).Build())
	}
	open = append(open, typ.Func().Param("name", typ.String).OptParam("scope", typ.Any).OptParam("options", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build())
	module := typ.NewRecord().SetDeclared(true).
		Field("get", typ.NewIntersection(get...)).
		Field("open", typ.NewIntersection(open...)).
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

func typedWrapper(id string, instance typ.Type, bindings map[string]*api.Binding, bindingInstances map[string]typ.Type) typ.Type {
	return typ.NewRecursive("Contract<"+id+">", func(self typ.Type) typ.Type {
		// A no-binding open uses the definition's promised surface. A broad
		// explicit binding preserves only that surface after runtime membership
		// checking. Literal bindings can expose their complete merged surface.
		openMembers := []typ.Type{typ.Func().Param("self", self).Returns(instance, typ.NewOptional(typ.LuaError)).Build()}
		bindingIDs := make([]string, 0, len(bindingInstances))
		for bindingID := range bindingInstances {
			bindingIDs = append(bindingIDs, bindingID)
		}
		sort.Strings(bindingIDs)
		for _, bindingID := range bindingIDs {
			if !bindingImplements(bindings[bindingID], id) {
				openMembers = append(openMembers, typ.Func().Param("self", self).Param("name", typ.LiteralString(bindingID)).OptParam("scope", typ.Any).Returns(typ.Nil, typ.LuaError).Build())
				continue
			}
			openMembers = append(openMembers, typ.Func().Param("self", self).Param("name", typ.LiteralString(bindingID)).OptParam("scope", typ.Any).Returns(bindingInstances[bindingID], typ.NewOptional(typ.LuaError)).Build())
		}
		openMembers = append(openMembers, typ.Func().Param("self", self).Param("name", typ.String).OptParam("scope", typ.Any).Returns(instance, typ.NewOptional(typ.LuaError)).Build())
		open := typ.NewIntersection(openMembers...)
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

func bindingImplements(binding *api.Binding, definitionID string) bool {
	if binding == nil {
		return false
	}
	for _, bound := range binding.Contracts {
		if bound.Contract.String() == definitionID {
			return true
		}
	}
	return false
}

func typedBindingInstances(bindings map[string]*api.Binding, instances map[string]typ.Type, manifest *io.Manifest, diagnostics *[]ManifestDiagnostic) map[string]typ.Type {
	result := make(map[string]typ.Type)
	ids := make([]string, 0, len(bindings))
	for id := range bindings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		binding := bindings[id]
		if binding == nil {
			continue
		}
		methodTypes := make(map[string]*typ.Function)
		conflicts := make(map[string]bool)
		incomplete := false
		for _, bound := range binding.Contracts {
			definitionID := bound.Contract.String()
			instance := instances[definitionID]
			if instance == nil {
				incomplete = true
				*diagnostics = append(*diagnostics, ManifestDiagnostic{id, "", "contracts", "contract definition unavailable in this lint catalog: " + definitionID})
				continue
			}
			recursive, ok := instance.(*typ.Recursive)
			if !ok {
				continue
			}
			record, ok := recursive.Body.(*typ.Record)
			if !ok {
				continue
			}
			for _, field := range record.Fields {
				fn, ok := field.Type.(*typ.Function)
				if !ok {
					continue
				}
				if old, duplicate := methodTypes[field.Name]; duplicate {
					if !equivalentMethodSignature(old, fn) {
						conflicts[field.Name] = true
						*diagnostics = append(*diagnostics, ManifestDiagnostic{id, field.Name, "contracts", "conflicting method signatures on binding"})
					}
					continue
				}
				methodTypes[field.Name] = fn
			}
		}
		result[id] = typ.NewRecursive("Binding<"+id+">", func(self typ.Type) typ.Type {
			builder := typ.NewRecord().SetDeclared(true).SetOpen(incomplete)
			names := make([]string, 0, len(methodTypes))
			for name := range methodTypes {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if conflicts[name] {
					builder.Field(name, typ.Unknown)
					continue
				}
				fn := methodTypes[name]
				params := append([]typ.Param(nil), fn.Params...)
				params[0].Type = self
				builder.Field(name, fn.WithParams(params))
			}
			return builder.Build()
		})
		manifest.DefineType(manifestTypePrefix(id)+"BindingInstance", result[id])
	}
	return result
}

func equivalentMethodSignature(a, b *typ.Function) bool {
	if a == nil || b == nil || len(a.Params) != len(b.Params) || len(a.Returns) != len(b.Returns) {
		return false
	}
	for i := 1; i < len(a.Params); i++ {
		if a.Params[i].Optional != b.Params[i].Optional || !typ.TypeEquals(a.Params[i].Type, b.Params[i].Type) {
			return false
		}
	}
	if (a.Variadic == nil) != (b.Variadic == nil) {
		return false
	}
	if a.Variadic != nil && !typ.TypeEquals(a.Variadic, b.Variadic) {
		return false
	}
	for i := range a.Returns {
		if !typ.TypeEquals(a.Returns[i], b.Returns[i]) {
			return false
		}
	}
	return true
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
