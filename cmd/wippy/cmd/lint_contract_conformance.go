// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"sort"

	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/query/core"
	"github.com/wippyai/go-lua/types/subtype"
	"github.com/wippyai/go-lua/types/typ"
	regapi "github.com/wippyai/runtime/api/registry"
	contractmod "github.com/wippyai/runtime/runtime/lua/modules/contract"
)

type conformanceFinding struct {
	binding   string
	method    string
	function  string
	path      string
	message   string
	violation bool
}

func checkBindingConformance(catalog *contractCatalog, manifests map[regapi.ID]*io.Manifest, data map[regapi.ID]entryData, filters []string) []conformanceFinding {
	if catalog == nil {
		return nil
	}
	var findings []conformanceFinding
	bindingIDs := make([]string, 0, len(catalog.bindings))
	for id := range catalog.bindings {
		bindingIDs = append(bindingIDs, id)
	}
	sort.Strings(bindingIDs)
	for _, bindingID := range bindingIDs {
		if len(filters) != 0 && !matchesNSFilter(regapi.ParseID(bindingID).NS, filters) {
			continue
		}
		binding := catalog.bindings[bindingID]
		for _, bound := range binding.Contracts {
			definitionID := bound.Contract.String()
			definition := catalog.definitions[definitionID]
			if definition == nil {
				continue
			} // catalog coverage already reports this
			for _, method := range definition.Methods {
				functionID, mapped := bound.Methods[method.Name]
				if !mapped {
					findings = append(findings, conformanceFinding{binding: bindingID, method: method.Name, path: "contracts/methods", message: "method has no bound function", violation: true})
					continue
				}
				add := func(path, message string, violation bool) {
					findings = append(findings, conformanceFinding{bindingID, method.Name, functionID.String(), path, message, violation})
				}
				manifest := manifests[functionID]
				if manifest == nil || !manifest.BodyBacked {
					add("contracts/methods/"+method.Name, "cannot verify: body-backed implementation manifest unavailable", false)
					continue
				}
				functionMethod := data[functionID].Method
				if functionMethod == "" {
					add("contracts/methods/"+method.Name, "cannot verify: configured implementation method unavailable", false)
					continue
				}
				field, ok := core.FieldOrMethod(manifest.Export, functionMethod)
				if !ok || field == nil {
					add("contracts/methods/"+method.Name, "cannot verify: checked implementation signature unavailable", false)
					continue
				}
				fn, okFn := typ.UnwrapAnnotated(field).(*typ.Function)
				if !okFn {
					add("contracts/methods/"+method.Name, "cannot verify: checked implementation signature unavailable", false)
					continue
				}
				if len(method.InputSchemas) == 0 {
					add("input_schemas", "cannot verify input: positional schemas unspecified", false)
				} else {
					minArity := len(method.InputSchemas)
					for minArity > 0 {
						projection := contractmod.TranslateSchema(method.InputSchemas[minArity-1], nil)
						if !nilableSchemaType(projection.Type) {
							break
						}
						minArity--
					}
					requiredParams := 0
					for _, param := range fn.Params {
						if !param.Optional {
							requiredParams++
						}
					}
					if requiredParams > minArity {
						add("input_schemas", fmt.Sprintf("implementation requires %d arguments but schema permits %d", requiredParams, minArity), true)
					}
					for i, schema := range method.InputSchemas {
						var accepted typ.Type
						if i < len(fn.Params) {
							accepted = fn.Params[i].Type
						} else {
							accepted = fn.Variadic
						}
						if accepted == nil {
							continue
						} // Lua discards surplus positional arguments.
						projection := contractmod.TranslateSchema(schema, nil)
						path := fmt.Sprintf("input_schemas[%d]", i)
						if projection.Coverage != contractmod.SchemaComplete {
							add(path, "cannot verify input constraint from unknown/any or incomplete schema evidence", false)
						}
						if containsUnverifiable(projection.Type) || containsUnverifiable(accepted) {
							continue
						}
						if !subtype.IsSubtype(projection.Type, accepted) {
							add(path, fmt.Sprintf("implementation parameter %d cannot accept every schema-permitted value", i+1), true)
						}
					}
				}
				if len(method.OutputSchemas) == 0 {
					add("output_schemas", "cannot verify output: schema unspecified", false)
				}
				for outputIndex, schema := range method.OutputSchemas {
					projection := contractmod.TranslateSchema(schema, nil)
					path := fmt.Sprintf("output_schemas[%d]", outputIndex)
					schemaGap := projection.Coverage != contractmod.SchemaComplete || containsUnverifiable(projection.Type)
					if schemaGap {
						add(path, "cannot verify remaining output constraints from unshaped or incomplete schema evidence", false)
					}
					actual := typ.Type(typ.Nil)
					if len(fn.Returns) > outputIndex {
						actual = fn.Returns[outputIndex]
					}
					if containsUnverifiable(actual) {
						if missing := missingRequiredLiteralReturnField(data[functionID].Source, functionMethod, projection.Type); missing != "" {
							add(path+"/required", "successful result may omit required output field "+fmt.Sprintf("%q", missing), true)
						}
						add(path, "cannot verify successful output from unknown/any implementation evidence", false)
						continue
					}
					if nilableSchemaType(actual) && len(fn.Returns) > len(method.OutputSchemas) && !nilableSchemaType(projection.Type) {
						if success, proved := correlatedSuccessType(actual, data[functionID].Source, functionMethod); proved && outputIndex == 0 && len(method.OutputSchemas) == 1 {
							actual = success
						} else {
							add(path, "cannot verify whether nil result occurs only with an outer error", false)
							continue
						}
					}
					missing := missingOutputField(actual, projection.Type)
					if missing != "" {
						add(path+"/required", "successful result may omit required output field "+fmt.Sprintf("%q", missing), true)
					}
					if missing != "" {
						continue
					}
					if !subtype.IsSubtype(actual, projection.Type) {
						if provenOutputKindMismatch(actual, projection.Type) {
							add(path, "successful result does not satisfy output schema", true)
						} else {
							add(path, "cannot verify structural output assignability from inferred evidence", false)
						}
					}
				}
				errorIndex := len(method.OutputSchemas)
				if errorIndex == 0 {
					errorIndex = 1
				}
				if len(fn.Returns) > errorIndex {
					outer := fn.Returns[errorIndex]
					if containsUnverifiable(outer) {
						add("outer_error", "cannot verify outer error from unknown/any implementation evidence", false)
					} else if !subtype.IsSubtype(outer, typ.NewUnion(typ.String, typ.LuaError, typ.Nil)) {
						add("outer_error", "second return is not an error: the runtime ignores it", true)
					}
				}
			}
		}
	}
	return findings
}

func provenOutputKindMismatch(actual, expected typ.Type) bool {
	actual = typ.UnwrapAnnotated(actual)
	expected = typ.UnwrapAnnotated(expected)
	if _, record := expected.(*typ.Record); record {
		return false
	}
	if _, union := expected.(*typ.Union); union {
		return false
	}
	if _, optional := expected.(*typ.Optional); optional {
		return false
	}
	return !subtype.IsSubtype(actual, expected)
}

// A literal table returned by the configured function is direct evidence for
// required-field absence even when imported calls make the checked aggregate
// return type unknown. Nested functions are not traversed.
func missingRequiredLiteralReturnField(source, method string, expected typ.Type) string {
	want, ok := typ.UnwrapAnnotated(expected).(*typ.Record)
	if !ok || source == "" || method == "" {
		return ""
	}
	statements, err := parse.ParseString(source, "contract-conformance")
	if err != nil {
		return ""
	}
	var body []ast.Stmt
	for _, stmt := range statements {
		switch s := stmt.(type) {
		case *ast.FuncDefStmt:
			if s.Name != nil && s.Func != nil {
				if s.Name.Method == method {
					body = s.Func.Stmts
				}
				if ident, ok := s.Name.Func.(*ast.IdentExpr); ok && ident.Value == method {
					body = s.Func.Stmts
				}
			}
		case *ast.LocalAssignStmt:
			for i, name := range s.Names {
				if name == method && i < len(s.Exprs) {
					if fn, ok := s.Exprs[i].(*ast.FunctionExpr); ok {
						body = fn.Stmts
					}
				}
			}
		}
	}
	var missing string
	walkRequireNodes(body, nil, func(stmt ast.Stmt) {
		if missing != "" {
			return
		}
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Exprs) == 0 {
			return
		}
		table, ok := ret.Exprs[0].(*ast.TableExpr)
		if !ok {
			return
		}
		fields := make(map[string]bool, len(table.Fields))
		for _, field := range table.Fields {
			name := ast.KeyName(field.Key)
			if name == "" {
				// A computed key may provide any required field.
				return
			}
			fields[name] = true
		}
		for _, field := range want.Fields {
			if !field.Optional && !fields[field.Name] {
				missing = field.Name
				return
			}
		}
	}, nil)
	return missing
}

// correlatedSuccessType removes nil only when every explicit nil return has
// a literal outer error, every other return is syntactically nonnil, and the
// function ends in an explicit return. The checked manifest still supplies
// the successful value's structural type.
func correlatedSuccessType(actual typ.Type, source, method string) (typ.Type, bool) {
	if source == "" || method == "" {
		return nil, false
	}
	statements, err := parse.ParseString(source, "contract-conformance")
	if err != nil {
		return nil, false
	}
	var body []ast.Stmt
	for _, stmt := range statements {
		switch s := stmt.(type) {
		case *ast.FuncDefStmt:
			if s.Name != nil && s.Func != nil {
				if s.Name.Method == method {
					body = s.Func.Stmts
				}
				if ident, ok := s.Name.Func.(*ast.IdentExpr); ok && ident.Value == method {
					body = s.Func.Stmts
				}
			}
		case *ast.LocalAssignStmt:
			for i, name := range s.Names {
				if name == method && i < len(s.Exprs) {
					if fn, ok := s.Exprs[i].(*ast.FunctionExpr); ok {
						body = fn.Stmts
					}
				}
			}
		}
	}
	if len(body) == 0 {
		return nil, false
	}
	if _, endsWithReturn := body[len(body)-1].(*ast.ReturnStmt); !endsWithReturn {
		return nil, false
	}
	seenFailure, seenSuccess, valid := false, false, true
	walkRequireNodes(body, nil, func(stmt ast.Stmt) {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok {
			return
		}
		if len(ret.Exprs) == 0 {
			valid = false
			return
		}
		if _, nilReturn := ret.Exprs[0].(*ast.NilExpr); nilReturn {
			if len(ret.Exprs) < 2 {
				valid = false
				return
			}
			if _, literalError := ret.Exprs[1].(*ast.StringExpr); !literalError {
				valid = false
				return
			}
			seenFailure = true
			return
		}
		switch ret.Exprs[0].(type) {
		case *ast.TableExpr, *ast.StringExpr, *ast.NumberExpr, *ast.TrueExpr, *ast.FalseExpr, *ast.FunctionExpr:
			seenSuccess = true
		default:
			valid = false
		}
	}, nil)
	if !valid || !seenFailure || !seenSuccess {
		return nil, false
	}
	if optional, ok := actual.(*typ.Optional); ok {
		return optional.Inner, true
	}
	if union, ok := actual.(*typ.Union); ok {
		members := make([]typ.Type, 0, len(union.Members))
		for _, member := range union.Members {
			if member != typ.Nil {
				members = append(members, member)
			}
		}
		return typ.NewUnion(members...), true
	}
	return nil, false
}

func nilableSchemaType(t typ.Type) bool {
	if t == nil || t == typ.Nil {
		return true
	}
	if _, ok := t.(*typ.Optional); ok {
		return true
	}
	if union, ok := t.(*typ.Union); ok {
		for _, member := range union.Members {
			if member == typ.Nil {
				return true
			}
		}
	}
	return false
}

func containsUnverifiable(t typ.Type) bool {
	seen := map[typ.Type]bool{}
	var visit func(typ.Type) bool
	visit = func(current typ.Type) bool {
		if current == nil || current == typ.Unknown || current == typ.Any {
			return true
		}
		if seen[current] {
			return false
		}
		seen[current] = true
		switch v := typ.UnwrapAnnotated(current).(type) {
		case *typ.Optional:
			return visit(v.Inner)
		case *typ.Union:
			for _, member := range v.Members {
				if visit(member) {
					return true
				}
			}
		case *typ.Intersection:
			for _, member := range v.Members {
				if visit(member) {
					return true
				}
			}
		case *typ.Array:
			return visit(v.Element)
		case *typ.Map:
			return visit(v.Key) || visit(v.Value)
		case *typ.Record:
			for _, field := range v.Fields {
				if visit(field.Type) {
					return true
				}
			}
			if v.HasMapComponent() {
				return visit(v.MapKey) || visit(v.MapValue)
			}
		case *typ.Alias:
			return visit(v.Target)
		case *typ.Recursive:
			return visit(v.Body)
		}
		return false
	}
	return visit(t)
}

func missingOutputField(actual, expected typ.Type) string {
	if optional, ok := actual.(*typ.Optional); ok {
		return missingOutputField(optional.Inner, expected)
	}
	if union, ok := actual.(*typ.Union); ok {
		for _, member := range union.Members {
			if name := missingOutputField(member, expected); name != "" {
				return name
			}
		}
		return ""
	}
	got, okGot := typ.UnwrapAnnotated(actual).(*typ.Record)
	want, okWant := typ.UnwrapAnnotated(expected).(*typ.Record)
	if !okGot || !okWant {
		return ""
	}
	for _, field := range want.Fields {
		if field.Optional {
			continue
		}
		present := got.GetField(field.Name)
		if present == nil || present.Optional {
			return field.Name
		}
	}
	return ""
}

func appendConformanceFindings(result *LintResult, findings []conformanceFinding, strict bool, minSeverity severity) {
	for _, finding := range findings {
		sev := severityWarning
		code := "C0003"
		if finding.violation || strict {
			sev = severityError
		}
		if finding.violation {
			code = "C0002"
		}
		if sev < minSeverity {
			continue
		}
		message := fmt.Sprintf("contract.binding %s, %s -> %s: %s (%s)", finding.binding, finding.method, finding.function, finding.message, finding.path)
		result.Diagnostics = append(result.Diagnostics, Diagnostic{EntryID: finding.binding, Code: code, Severity: sev.String(), Message: message, Line: 1, Column: 1})
		richSeverity := diag.SeverityWarning
		if sev == severityError {
			richSeverity = diag.SeverityError
		}
		result.RichDiagnostics = append(result.RichDiagnostics, RichDiagnostic{EntryID: finding.binding, displayCode: code, Diag: diag.Diagnostic{Position: diag.Position{File: finding.binding, Line: 1, Column: 1}, Severity: richSeverity, Message: message}})
		if sev == severityError {
			result.ErrorCount++
		} else {
			result.WarningCount++
		}
	}
}
