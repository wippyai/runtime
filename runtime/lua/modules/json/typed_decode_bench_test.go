// SPDX-License-Identifier: MPL-2.0

package json

import (
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/typ"
)

func typedBenchCases() []struct {
	name   string
	data   []byte
	target *lua.LType
} {
	idName := typ.NewRecord().Field("id", typ.Integer).Field("name", typ.String).Field("email", typ.String).Build()
	meta := typ.NewRecord().Field("total", typ.Integer).Field("page", typ.Integer).Field("hasMore", typ.Boolean).Build()
	large := make([]byte, 0, 300000)
	large = append(large, '[')
	for i := 0; i < 10000; i++ {
		if i > 0 {
			large = append(large, ',')
		}
		large = append(large, fmt.Sprintf(`{"id":%d,"name":"item"}`, i)...)
	}
	large = append(large, ']')
	mapData := make([]byte, 0, 6000)
	mapData = append(mapData, '{')
	for i := 0; i < 100; i++ {
		if i > 0 {
			mapData = append(mapData, ',')
		}
		mapData = append(mapData, fmt.Sprintf(`"k%d":{"id":%d,"name":"item"}`, i, i)...)
	}
	mapData = append(mapData, '}')
	toolCall := typ.NewRecord().Field("id", typ.String).Field("type", typ.String).Field("function", typ.NewRecord().Field("name", typ.String).Field("arguments", typ.String).Build()).Build()
	message := typ.NewRecord().Field("role", typ.String).Field("content", typ.String).Field("tool_calls", typ.NewArray(toolCall)).Build()
	choice := typ.NewRecord().Field("index", typ.Integer).Field("message", message).Field("finish_reason", typ.String).OptField("logprobs", typ.Nil).Build()
	usage := typ.NewRecord().Field("prompt_tokens", typ.Integer).Field("completion_tokens", typ.Integer).Field("total_tokens", typ.Integer).Build()
	return []struct {
		name   string
		data   []byte
		target *lua.LType
	}{
		{"small_record", simpleJSON, lua.NewLType(typ.NewRecord().Field("name", typ.String).Field("age", typ.Integer).Field("active", typ.Boolean).Build())},
		{"nested_arrays", complexJSON, lua.NewLType(typ.NewRecord().Field("users", typ.NewArray(idName)).Field("metadata", meta).Build())},
		{"array_10k", large, lua.NewLType(typ.NewArray(typ.NewRecord().Field("id", typ.Integer).Field("name", typ.String).Build()))},
		{"map_records", mapData, lua.NewLType(typ.NewMap(typ.String, typ.NewRecord().Field("id", typ.Integer).Field("name", typ.String).Build()))},
		{"llm_response", benchValidJSON(llmResponseJSON), lua.NewLType(typ.NewRecord().Field("id", typ.String).Field("object", typ.String).Field("created", typ.Integer).Field("model", typ.String).Field("choices", typ.NewArray(choice)).Field("usage", usage).Field("system_fingerprint", typ.String).Build())},
	}
}

// The shared fixture embeds a raw code block newline inside a JSON string.
func benchValidJSON(src []byte) []byte {
	out := make([]byte, 0, len(src)+16)
	inString, escaped := false, false
	for _, c := range src {
		if inString && c == '\n' {
			out = append(out, '\\', 'n')
			continue
		}
		out = append(out, c)
		if escaped {
			escaped = false
		} else if c == '\\' && inString {
			escaped = true
		} else if c == '"' {
			inString = !inString
		}
	}
	return out
}

func BenchmarkTypedDecodeComparison(b *testing.B) {
	for _, tc := range typedBenchCases() {
		b.Run(tc.name, func(b *testing.B) {
			l := lua.NewState()
			defer l.Close()
			raw := string(tc.data)
			b.Run("decode", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_, _ = Decode(tc.data)
				}
			})
			b.Run("decode_is", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					v, _ := Decode(tc.data)
					_ = tc.target.Validate(l, v)
				}
			})
			b.Run("typed", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_, _ = decodeTypedString(raw, tc.target, l)
				}
			})
		})
	}
}

func TestTypedBenchFixtures(t *testing.T) {
	for _, tc := range typedBenchCases() {
		t.Run(tc.name, func(t *testing.T) {
			checkTypedDifferential(t, tc.target, string(tc.data))
			v, e := Decode(tc.data)
			if e != nil || !tc.target.Validate(nil, v) {
				t.Fatalf("benchmark target does not match fixture: %v", e)
			}
		})
	}
}
