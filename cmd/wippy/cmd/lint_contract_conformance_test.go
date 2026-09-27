// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"testing"

	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/contract"
	regapi "github.com/wippyai/runtime/api/registry"
)

func conformanceFixture(input, output string, fn *typ.Function) []conformanceFinding {
	return conformanceFixtureOutputs(input, []string{output}, fn)
}

func conformanceFixtureOutputs(input string, outputs []string, fn *typ.Function) []conformanceFinding {
	definitionID := regapi.NewID("sample", "service")
	bindingID := regapi.NewID("sample", "binding")
	functionID := regapi.NewID("sample", "handle")
	outputSchemas := make([]api.SchemaDefinition, len(outputs))
	for i, output := range outputs {
		outputSchemas[i] = api.SchemaDefinition{Format: "application/schema+json", Definition: output}
	}
	definitions := map[string]*api.Definition{definitionID.String(): {Methods: []api.MethodDef{{Name: "query", InputSchemas: []api.SchemaDefinition{{Format: "application/schema+json", Definition: input}}, OutputSchemas: outputSchemas}}}}
	bindings := map[string]*api.Binding{bindingID.String(): {Contracts: []api.BoundContract{{Contract: definitionID, Methods: map[string]regapi.ID{"query": functionID}}}}}
	catalog := &contractCatalog{definitions: definitions, bindings: bindings}
	manifest := io.NewManifest(functionID.String())
	manifest.BodyBacked = true
	manifest.SetExport(typ.NewRecord().Field("handle", fn).Build())
	return checkBindingConformance(catalog, map[regapi.ID]*io.Manifest{functionID: manifest}, map[regapi.ID]entryData{functionID: {Method: "handle"}}, nil)
}

func TestBindingConformanceMultipleOutputsAndOuterError(t *testing.T) {
	outputs := []string{`{"type":"string"}`, `{"type":"number"}`}
	for _, tc := range []struct {
		name     string
		returns  []typ.Type
		wantPath string
	}{
		{"matching", []typ.Type{typ.String, typ.Number, typ.NewOptional(typ.LuaError)}, ""},
		{"wrong second value", []typ.Type{typ.String, typ.Boolean, typ.Nil}, "output_schemas[1]"},
		{"ignored outer error", []typ.Type{typ.String, typ.Number, typ.Boolean}, "outer_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := typ.Func().Param("request", typ.String).Returns(tc.returns...).Build()
			findings := conformanceFixtureOutputs(`{"type":"string"}`, outputs, fn)
			for _, finding := range findings {
				if finding.violation && finding.path == tc.wantPath {
					return
				}
			}
			if tc.wantPath != "" {
				t.Fatalf("missing %s violation: %+v", tc.wantPath, findings)
			}
			for _, finding := range findings {
				if finding.violation {
					t.Fatalf("unexpected violation: %+v", findings)
				}
			}
		})
	}
}

func TestBindingConformanceClassifiesViolationsAndGaps(t *testing.T) {
	input := `{"type":"string"}`
	output := `{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`
	goodResult := typ.NewRecord().Field("ok", typ.Boolean).Build()
	for _, tt := range []struct {
		name                   string
		fn                     *typ.Function
		wantViolation, wantGap bool
	}{
		{"wider input passes", typ.Func().Param("request", typ.NewUnion(typ.String, typ.Integer)).Returns(goodResult).Build(), false, false},
		{"narrower input fails", typ.Func().Param("request", typ.LiteralString("fixed")).Returns(goodResult).Build(), true, false},
		{"wrong success fails", typ.Func().Param("request", typ.String).Returns(typ.NewRecord().Field("error", typ.String).Build()).Build(), true, false},
		{"outer error needs correlation proof", typ.Func().Param("request", typ.String).Returns(typ.NewOptional(goodResult), typ.NewOptional(typ.LuaError)).Build(), false, true},
		{"unknown result gap", typ.Func().Param("request", typ.String).Returns(typ.Unknown).Build(), false, true},
		{"any input gap", typ.Func().Param("request", typ.Any).Returns(goodResult).Build(), false, true},
		{"arity violation", typ.Func().Param("request", typ.String).Param("extra", typ.String).Returns(goodResult).Build(), true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			findings := conformanceFixture(input, output, tt.fn)
			var violation, gap bool
			for _, finding := range findings {
				if finding.violation {
					violation = true
				} else {
					gap = true
				}
			}
			if violation != tt.wantViolation || gap != tt.wantGap {
				t.Fatalf("findings=%+v", findings)
			}
		})
	}
}

func TestApplicationFailureEnvelopeIsCheckedAsSuccessValue(t *testing.T) {
	input := `{"type":"object"}`
	output := `{"type":"object","properties":{"success":{"type":"boolean"},"components":{"type":"array"}},"required":["success","components"]}`
	impl := typ.Func().Param("request", typ.NewRecord().SetOpen(true).Build()).Returns(typ.NewUnion(
		typ.NewRecord().Field("success", typ.False).Field("error", typ.String).Build(),
		typ.NewRecord().Field("success", typ.True).Field("components", typ.NewArray(typ.String)).Build(),
	)).Build()
	findings := conformanceFixture(input, output, impl)
	var requiredViolation, arrayGap bool
	for _, finding := range findings {
		if finding.violation && finding.path == "output_schemas[0]/required" {
			requiredViolation = true
		}
		if !finding.violation && finding.path == "output_schemas[0]" {
			arrayGap = true
		}
	}
	if !requiredViolation || !arrayGap {
		t.Fatalf("required-field violation and unshaped-array gap must stay distinct, findings=%+v", findings)
	}
}

func TestOuterFailureCorrelationRequiresProof(t *testing.T) {
	value := typ.NewRecord().Field("ok", typ.Boolean).Build()
	actual := typ.NewOptional(value)
	good := `local function handle(request) if request == "bad" then return nil, "failure" end return {ok=true} end return {handle=handle}`
	projected, proved := correlatedSuccessType(actual, good, "handle")
	if !proved || !typ.TypeEquals(projected, value) {
		t.Fatalf("literal outer failure not proved: %v %v", projected, proved)
	}
	bad := `local function handle(request) if request == "bad" then return nil, nil end return {ok=true} end return {handle=handle}`
	if _, proved := correlatedSuccessType(actual, bad, "handle"); proved {
		t.Fatal("nil second result cannot establish an outer failure")
	}
}
