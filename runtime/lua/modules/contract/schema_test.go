// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"encoding/json"
	"testing"

	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/contract"
)

func projectJSON(document string) SchemaProjection {
	return TranslateSchema(api.SchemaDefinition{Format: "application/schema+json", Definition: document}, nil)
}

func TestSchemaProjection(t *testing.T) {
	tests := []struct {
		name, schema string
		want         typ.Type
		coverage     SchemaCoverage
	}{
		{"string", `{"type":"string"}`, typ.String, SchemaComplete},
		{"integer", `{"type":"integer"}`, typ.Integer, SchemaComplete},
		{"number", `{"type":"number"}`, typ.Number, SchemaComplete},
		{"boolean", `{"type":"boolean"}`, typ.Boolean, SchemaComplete},
		{"null", `{"type":"null"}`, typ.Nil, SchemaComplete},
		{"description-only", `{"description":"settings.value is arbitrary"}`, typ.Unknown, SchemaUnconstrained},
		{"missing-type", `{"properties":{"x":{"type":"string"}}}`, typ.Unknown, SchemaIncomplete},
		{"true", `true`, typ.Unknown, SchemaUnconstrained},
		{"false", `false`, typ.Never, SchemaComplete},
		{"nullable", `{"type":["string","null"]}`, typ.NewOptional(typ.String), SchemaComplete},
		{"array", `{"type":"array","items":{"type":"boolean"}}`, typ.NewArray(typ.Boolean), SchemaComplete},
		{"unshaped-array", `{"type":"array"}`, typ.NewArray(typ.Unknown), SchemaComplete},
		{"const", `{"const":"ready"}`, typ.LiteralString("ready"), SchemaComplete},
		{"enum", `{"enum":["a","b"]}`, typ.NewUnion(typ.LiteralString("a"), typ.LiteralString("b")), SchemaComplete},
		{"anyOf", `{"anyOf":[{"type":"string"},{"type":"integer"}]}`, typ.NewUnion(typ.String, typ.Integer), SchemaComplete},
		{"oneOf", `{"oneOf":[{"type":"string"},{"type":"integer"}]}`, typ.NewUnion(typ.String, typ.Integer), SchemaIncomplete},
		{"unsupported", `{"type":"string","minLength":3}`, typ.String, SchemaIncomplete},
		{"unsupported-branch", `{"anyOf":[{"type":"string"},{"minLength":3}]}`, typ.Unknown, SchemaIncomplete},
		{"tuple", `{"type":"array","prefixItems":[{"type":"string"}],"items":{"type":"boolean"}}`, typ.NewArray(typ.Unknown), SchemaIncomplete},
		{"composite-const", `{"type":"object","const":{"x":1}}`, typ.NewRecord().SetDeclared(true).SetOpen(true).Build(), SchemaIncomplete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectJSON(tt.schema)
			if !typ.TypeEquals(got.Type, tt.want) || got.Coverage != tt.coverage {
				t.Fatalf("got %s coverage %v diagnostics %v; want %s coverage %v", got.Type, got.Coverage, got.Diagnostics, tt.want, tt.coverage)
			}
		})
	}
}

func TestSchemaObjectPropertiesAndReferences(t *testing.T) {
	got := projectJSON(`{"$defs":{"text":{"type":"string"}},"type":"object","properties":{"id":{"$ref":"#/$defs/text"},"value":{"description":"arbitrary"}},"required":["id","missing"],"additionalProperties":{"type":"integer"}}`)
	record, ok := got.Type.(*typ.Record)
	if !ok {
		t.Fatalf("type = %T", got.Type)
	}
	if got.Coverage != SchemaComplete || record.GetField("id").Type != typ.String || record.GetField("id").Optional || record.GetField("value").Type != typ.Unknown || !record.GetField("value").Optional || record.GetField("missing").Type != typ.Integer || record.MapKey != typ.String || record.MapValue != typ.Integer {
		t.Fatalf("unexpected projection: %s, coverage %v, diagnostics %v", got.Type, got.Coverage, got.Diagnostics)
	}
	open := projectJSON(`{"type":"object","properties":{"id":{"type":"string"}}}`)
	if !open.Type.(*typ.Record).Open {
		t.Fatal("default additionalProperties must remain open")
	}
	closed := projectJSON(`{"type":"object","additionalProperties":false}`)
	if closed.Type.(*typ.Record).Open || closed.Coverage != SchemaIncomplete {
		t.Fatalf("closed object coverage = %v, type = %s", closed.Coverage, closed.Type)
	}
	remote := TranslateSchema(api.SchemaDefinition{Format: "application/schema+json", Definition: `{"$ref":"other#/$defs/value"}`}, map[string]any{"other": `{"$defs":{"value":{"type":"number"}}}`})
	if remote.Type != typ.Number || remote.Coverage != SchemaComplete {
		t.Fatalf("resource projection: %+v", remote)
	}
	cycle := projectJSON(`{"$defs":{"node":{"type":"object","properties":{"next":{"$ref":"#/$defs/node"}}}},"$ref":"#/$defs/node"}`)
	if cycle.Coverage != SchemaIncomplete {
		t.Fatalf("recursive edge requires diagnostic: %+v", cycle)
	}
	unresolved := projectJSON(`{"$ref":"missing"}`)
	if unresolved.Type != typ.Unknown || unresolved.Coverage != SchemaIncomplete {
		t.Fatalf("unresolved reference: %+v", unresolved)
	}
}

func TestSchemaRejectsLegacyNullable(t *testing.T) {
	got := projectJSON(`{"type":"string","nullable":true}`)
	if got.Type != typ.String || got.Coverage != SchemaIncomplete {
		t.Fatalf("legacy nullable: %+v", got)
	}
	legacy := projectJSON(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"string"}`)
	if legacy.Type != typ.Unknown || legacy.Coverage != SchemaIncomplete {
		t.Fatalf("legacy dialect: %+v", legacy)
	}
}

func TestSchemaProjectionFromQuotedRawDocument(t *testing.T) {
	encoded, err := json.Marshal(`{"type":"object","properties":{"id":{"type":"string"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	got := TranslateSchema(api.SchemaDefinition{Format: "application/schema+json", Definition: json.RawMessage(encoded)}, nil)
	if got.Coverage != SchemaComplete {
		t.Fatalf("quoted schema projection = %+v", got)
	}
	record, ok := got.Type.(*typ.Record)
	if !ok || record.GetField("id") == nil || record.GetField("id").Type != typ.String {
		t.Fatalf("quoted schema type = %s", got.Type)
	}
}
