package json

import (
	"fmt"
	"testing"
)

var (
	simpleJSON = []byte(`{"name":"John","age":30,"active":true}`)

	complexJSON = []byte(`{
		"users": [
			{"id": 1, "name": "Alice", "email": "alice@example.com"},
			{"id": 2, "name": "Bob", "email": "bob@example.com"},
			{"id": 3, "name": "Charlie", "email": "charlie@example.com"}
		],
		"metadata": {
			"total": 3,
			"page": 1,
			"hasMore": false
		}
	}`)

	largeArrayJSON = func() []byte {
		b := []byte(`[`)
		for i := 0; i < 100; i++ {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, `{"id":`...)
			b = append(b, '0'+byte(i%10))
			b = append(b, `,"value":"item"}`...)
		}
		b = append(b, ']')
		return b
	}()

	// LLM chat completion request - realistic OpenAI/Anthropic API format
	llmRequestJSON = func() []byte {
		messages := `[`
		for i := 0; i < 20; i++ {
			if i > 0 {
				messages += `,`
			}
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			messages += fmt.Sprintf(`{"role":"%s","content":"This is message %d with some realistic content that might appear in a conversation. It includes questions, answers, code snippets, and various types of text that users typically send to LLMs. The content length varies but this represents a medium-sized message."}`, role, i)
		}
		messages += `]`

		return []byte(fmt.Sprintf(`{
			"model": "gpt-4-turbo",
			"messages": %s,
			"temperature": 0.7,
			"max_tokens": 4096,
			"top_p": 1.0,
			"frequency_penalty": 0.0,
			"presence_penalty": 0.0,
			"stream": false,
			"tools": [
				{
					"type": "function",
					"function": {
						"name": "search_database",
						"description": "Search the database for relevant information",
						"parameters": {
							"type": "object",
							"properties": {
								"query": {"type": "string", "description": "Search query"},
								"limit": {"type": "integer", "description": "Max results"},
								"filters": {
									"type": "object",
									"properties": {
										"category": {"type": "string"},
										"date_from": {"type": "string"},
										"date_to": {"type": "string"}
									}
								}
							},
							"required": ["query"]
						}
					}
				},
				{
					"type": "function",
					"function": {
						"name": "execute_code",
						"description": "Execute code in a sandboxed environment",
						"parameters": {
							"type": "object",
							"properties": {
								"language": {"type": "string", "enum": ["python", "javascript", "go"]},
								"code": {"type": "string"},
								"timeout": {"type": "integer"}
							},
							"required": ["language", "code"]
						}
					}
				}
			]
		}`, messages))
	}()

	// LLM streaming response chunks - what you'd receive from SSE
	llmResponseJSON = func() []byte {
		return []byte(`{
			"id": "chatcmpl-abc123def456",
			"object": "chat.completion",
			"created": 1701388800,
			"model": "gpt-4-turbo",
			"choices": [
				{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": "Here's a comprehensive answer to your question about optimizing JSON parsing in Go. The key considerations are:\n\n1. **Memory Allocation**: Each allocation has overhead. Using sync.Pool for reusable buffers reduces GC pressure significantly.\n\n2. **Token-based Parsing**: Instead of unmarshaling to intermediate types, parse tokens directly to your target representation.\n\n3. **String Interning**: For repeated keys, consider interning strings to reduce memory.\n\n4. **SIMD Optimization**: Modern JSON parsers use SIMD instructions for scanning.\n\nHere's example code:\n\n` + "```go\nfunc parseEfficiently(data []byte) error {\n    dec := jsontext.NewDecoder(bytes.NewReader(data))\n    for {\n        tok, err := dec.ReadToken()\n        if err == io.EOF {\n            break\n        }\n        // Process token directly\n    }\n    return nil\n}\n```" + `\n\nThis approach typically yields 2-3x performance improvements.",
						"tool_calls": [
							{
								"id": "call_abc123",
								"type": "function",
								"function": {
									"name": "search_database",
									"arguments": "{\"query\":\"json optimization techniques\",\"limit\":10}"
								}
							}
						]
					},
					"finish_reason": "tool_calls",
					"logprobs": null
				}
			],
			"usage": {
				"prompt_tokens": 1250,
				"completion_tokens": 489,
				"total_tokens": 1739
			},
			"system_fingerprint": "fp_abc123"
		}`)
	}()

	// Large dataset - 500 records like from a database query
	largeDatasetJSON = func() []byte {
		b := []byte(`{"records":[`)
		for i := 0; i < 500; i++ {
			if i > 0 {
				b = append(b, ',')
			}
			record := fmt.Sprintf(`{"id":%d,"uuid":"550e8400-e29b-%04d-a716-446655440000","name":"Record %d","email":"user%d@example.com","created_at":"2024-01-15T10:30:00Z","updated_at":"2024-12-01T15:45:30Z","metadata":{"source":"api","version":2,"tags":["active","verified","premium"]},"settings":{"notifications":true,"theme":"dark","language":"en"},"stats":{"views":%d,"clicks":%d,"conversions":%d}}`,
				i, i%10000, i, i, i*100, i*10, i)
			b = append(b, record...)
		}
		b = append(b, `],"total":500,"page":1,"per_page":500,"has_more":false}`...)
		return b
	}()

	// Deeply nested config - like webpack/vite config
	nestedConfigJSON = []byte(`{
		"compiler": {
			"options": {
				"target": "ES2022",
				"module": "ESNext",
				"lib": ["ES2022", "DOM", "DOM.Iterable"],
				"strict": true,
				"paths": {
					"@/*": ["./src/*"],
					"@components/*": ["./src/components/*"],
					"@utils/*": ["./src/utils/*"]
				}
			},
			"plugins": [
				{"name": "typescript", "options": {"transpileOnly": true}},
				{"name": "babel", "options": {"presets": ["@babel/preset-env"]}},
				{"name": "postcss", "options": {"plugins": ["autoprefixer", "cssnano"]}}
			]
		},
		"build": {
			"input": {"main": "./src/main.ts", "worker": "./src/worker.ts"},
			"output": {
				"dir": "./dist",
				"format": "esm",
				"sourcemap": true,
				"minify": {"js": true, "css": true, "html": false}
			},
			"optimization": {
				"splitChunks": {
					"chunks": "all",
					"minSize": 20000,
					"maxSize": 244000,
					"cacheGroups": {
						"vendor": {"test": "/node_modules/", "name": "vendors", "chunks": "all"},
						"common": {"minChunks": 2, "name": "common", "chunks": "all"}
					}
				},
				"runtimeChunk": "single",
				"moduleIds": "deterministic"
			}
		},
		"server": {
			"port": 3000,
			"host": "localhost",
			"https": {"key": "./certs/key.pem", "cert": "./certs/cert.pem"},
			"proxy": {
				"/api": {"target": "http://localhost:8080", "changeOrigin": true},
				"/ws": {"target": "ws://localhost:8080", "ws": true}
			}
		}
	}`)
)

func BenchmarkDecode_Simple(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Decode(simpleJSON)
	}
}

func BenchmarkDecode_Complex(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Decode(complexJSON)
	}
}

func BenchmarkDecode_LargeArray(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Decode(largeArrayJSON)
	}
}

func BenchmarkDecode_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = Decode(complexJSON)
		}
	})
}

func BenchmarkDecode_LLMRequest(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(llmRequestJSON)))
	for i := 0; i < b.N; i++ {
		_, _ = Decode(llmRequestJSON)
	}
}

func BenchmarkDecode_LLMResponse(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(llmResponseJSON)))
	for i := 0; i < b.N; i++ {
		_, _ = Decode(llmResponseJSON)
	}
}

func BenchmarkDecode_LargeDataset(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(largeDatasetJSON)))
	for i := 0; i < b.N; i++ {
		_, _ = Decode(largeDatasetJSON)
	}
}

func BenchmarkDecode_NestedConfig(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(nestedConfigJSON)))
	for i := 0; i < b.N; i++ {
		_, _ = Decode(nestedConfigJSON)
	}
}

func BenchmarkDecode_LLMRequest_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(llmRequestJSON)))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = Decode(llmRequestJSON)
		}
	})
}

func BenchmarkDecode_LargeDataset_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(largeDatasetJSON)))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = Decode(largeDatasetJSON)
		}
	})
}
