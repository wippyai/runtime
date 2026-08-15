// SPDX-License-Identifier: MPL-2.0

package json

import "testing"

var benchmarkEnumSchema = []byte(`{
	"type":"object",
	"required":["state"],
	"properties":{"state":{"type":"string","enum":["ready","running","done"]}}
}`)

func BenchmarkValidateEnumSchemaCached(b *testing.B) {
	initSchemaCache()
	data := map[string]any{"state": "running"}

	if _, err := compileSchema(benchmarkEnumSchema); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		schema, err := compileSchema(benchmarkEnumSchema)
		if err != nil {
			b.Fatal(err)
		}
		if result := schema.ValidateMap(data); !result.IsValid() {
			b.Fatal("valid enum value was rejected")
		}
	}
}
