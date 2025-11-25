package json

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func BenchmarkValidate_WithCache(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(true), WithCapacity(100))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}, "age": {"type": "number"}}, "required": ["name"]}'
			local data = {name = "John", age = 30}
			local ok, err = json.validate(schema, data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate_WithoutCache(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(false))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}, "age": {"type": "number"}}, "required": ["name"]}'
			local data = {name = "John", age = 30}
			local ok, err = json.validate(schema, data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate_CacheHit(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(true), WithCapacity(100))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	err = vm.DoString(context.Background(), `
		local json = require("json")
		local schema = '{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}'
		local data = {name = "John"}
		json.validate(schema, data)
	`, "warmup")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}'
			local data = {name = "Alice"}
			local ok, err = json.validate(schema, data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate_CacheMiss(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(true), WithCapacity(10))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = string.format('{"type": "object", "properties": {"field%d": {"type": "string"}}}', math.random(1, 1000))
			local data = {name = "John"}
			local ok, err = json.validate(schema, data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate_StringSchema(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(true), WithCapacity(100))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "string", "minLength": 5, "maxLength": 100}'
			local data = "hello world"
			local ok, err = json.validate(schema, data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate_TableSchema(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(true), WithCapacity(100))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = {
				type = "object",
				properties = {
					name = {type = "string", minLength = 1},
					age = {type = "number", minimum = 0}
				},
				required = {"name"}
			}
			local data = {name = "John", age = 30}
			local ok, err = json.validate(schema, data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateString(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(true), WithCapacity(100))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = '{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}'
			local jsonStr = '{"name": "Bob", "age": 35}'
			local ok, err = json.validate_string(schema, jsonStr)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidate_ComplexSchema(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule(WithCache(true), WithCapacity(100))
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local schema = {
				type = "object",
				properties = {
					id = {type = "string", pattern = "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"},
					email = {type = "string", format = "email"},
					age = {type = "integer", minimum = 0, maximum = 150},
					tags = {type = "array", items = {type = "string"}, minItems = 1},
					address = {
						type = "object",
						properties = {
							street = {type = "string"},
							city = {type = "string"},
							zipcode = {type = "string", pattern = "^[0-9]{5}$"}
						},
						required = {"city"}
					}
				},
				required = {"id", "email", "age"}
			}
			local data = {
				id = "550e8400-e29b-41d4-a716-446655440000",
				email = "test@example.com",
				age = 30,
				tags = {"developer", "golang"},
				address = {city = "New York", zipcode = "10001"}
			}
			local ok, err = json.validate(schema, data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode_SimpleObject(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule()
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local data = {name = "John", age = 30, active = true}
			local result = json.encode(data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode_ComplexNested(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule()
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local data = {
				id = "550e8400-e29b-41d4-a716-446655440000",
				user = {
					name = "John Doe",
					email = "john@example.com",
					roles = {"admin", "developer"}
				},
				metadata = {
					created_at = "2024-01-01T00:00:00Z",
					tags = {"important", "urgent"},
					settings = {
						notifications = true,
						theme = "dark"
					}
				}
			}
			local result = json.encode(data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode_LargeArray(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule()
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local data = {}
			for i = 1, 1000 do
				data[i] = {id = i, name = "Item " .. i, value = i * 10}
			end
			local result = json.encode(data)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode_SimpleObject(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule()
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local jsonStr = '{"name":"John","age":30,"active":true}'
			local result = json.decode(jsonStr)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode_ComplexNested(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule()
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local jsonStr = '{"id":"550e8400-e29b-41d4-a716-446655440000","user":{"name":"John Doe","email":"john@example.com","roles":["admin","developer"]},"metadata":{"created_at":"2024-01-01T00:00:00Z","tags":["important","urgent"],"settings":{"notifications":true,"theme":"dark"}}}'
			local result = json.decode(jsonStr)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode_LargeArray(b *testing.B) {
	logger := zap.NewNop()
	mod := NewJSONModule()
	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	if err != nil {
		b.Fatal(err)
	}
	defer vm.Close()

	// Create large JSON array once
	err = vm.DoString(context.Background(), `
		local json = require("json")
		local data = {}
		for i = 1, 1000 do
			data[i] = {id = i, name = "Item " .. i, value = i * 10}
		end
		_G.largeJSON = json.encode(data)
	`, "setup")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = vm.DoString(context.Background(), `
			local json = require("json")
			local result = json.decode(_G.largeJSON)
		`, "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}
