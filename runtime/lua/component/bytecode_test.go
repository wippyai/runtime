package component

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/go-lua/compiler/parse"
	fsapi "github.com/wippyai/runtime/api/fs"
)

// Test Lua sources of varying complexity
var (
	// Simple function
	simpleLua = `
local function add(a, b)
    return a + b
end
return add
`

	// Medium complexity with loops and tables
	mediumLua = `
local utils = {}

function utils.sum(tbl)
    local total = 0
    for i, v in ipairs(tbl) do
        total = total + v
    end
    return total
end

function utils.map(tbl, fn)
    local result = {}
    for i, v in ipairs(tbl) do
        result[i] = fn(v)
    end
    return result
end

function utils.filter(tbl, predicate)
    local result = {}
    for i, v in ipairs(tbl) do
        if predicate(v) then
            result[#result + 1] = v
        end
    end
    return result
end

function utils.reduce(tbl, fn, initial)
    local acc = initial
    for i, v in ipairs(tbl) do
        acc = fn(acc, v)
    end
    return acc
end

return utils
`

	// Complex with nested functions and closures
	complexLua = `
local module = {}

local function createCounter(initial)
    local count = initial or 0
    return {
        increment = function(n)
            count = count + (n or 1)
            return count
        end,
        decrement = function(n)
            count = count - (n or 1)
            return count
        end,
        get = function()
            return count
        end,
        reset = function()
            count = initial or 0
            return count
        end
    }
end

local function memoize(fn)
    local cache = {}
    return function(...)
        local args = {...}
        local key = table.concat(args, ",")
        if cache[key] == nil then
            cache[key] = fn(...)
        end
        return cache[key]
    end
end

local function compose(...)
    local fns = {...}
    return function(x)
        local result = x
        for i = #fns, 1, -1 do
            result = fns[i](result)
        end
        return result
    end
end

local function curry(fn, arity)
    arity = arity or 2
    local function helper(args)
        if #args >= arity then
            return fn(table.unpack(args))
        end
        return function(...)
            local newArgs = {table.unpack(args)}
            for _, v in ipairs({...}) do
                newArgs[#newArgs + 1] = v
            end
            return helper(newArgs)
        end
    end
    return helper({})
end

function module.createCounter(initial)
    return createCounter(initial)
end

function module.memoize(fn)
    return memoize(fn)
end

function module.compose(...)
    return compose(...)
end

function module.curry(fn, arity)
    return curry(fn, arity)
end

function module.pipeline(value, ...)
    local fns = {...}
    local result = value
    for _, fn in ipairs(fns) do
        result = fn(result)
    end
    return result
end

function module.debounce(fn, delay)
    local lastCall = 0
    return function(...)
        local now = os.time()
        if now - lastCall >= delay then
            lastCall = now
            return fn(...)
        end
    end
end

return module
`
)

// compileSource compiles Lua source to FunctionProto
func compileSource(source string) (*glua.FunctionProto, error) {
	chunk, err := parse.Parse(strings.NewReader(source), "<test>")
	if err != nil {
		return nil, err
	}
	return glua.Compile(chunk, "<test>")
}

// dumpProto serializes FunctionProto to bytecode bytes
func dumpProto(proto *glua.FunctionProto) ([]byte, error) {
	return bytecode.Dump(proto)
}

// TestCompileVsUndump verifies that compiled and undumped protos are equivalent
func TestCompileVsUndump(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"simple", simpleLua},
		{"medium", mediumLua},
		{"complex", complexLua},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile from source
			compiled, err := compileSource(tt.source)
			require.NoError(t, err)
			require.NotNil(t, compiled)

			// Dump to bytecode
			bc, err := dumpProto(compiled)
			require.NoError(t, err)
			require.NotEmpty(t, bc)

			// Undump bytecode
			undumped, err := UndumpBytecode(bc)
			require.NoError(t, err)
			require.NotNil(t, undumped)

			// Verify key properties match
			assert.Equal(t, compiled.NumParameters, undumped.NumParameters)
			assert.Equal(t, compiled.IsVarArg, undumped.IsVarArg)
			assert.Equal(t, compiled.NumUpvalues, undumped.NumUpvalues)
			assert.Equal(t, len(compiled.Code), len(undumped.Code))
			assert.Equal(t, len(compiled.Constants), len(undumped.Constants))
		})
	}
}

// TestVerifyHash tests hash verification
func TestVerifyHash(t *testing.T) {
	data := []byte("test data for hashing")
	h := sha256.Sum256(data)
	validHash := "sha256:" + hex.EncodeToString(h[:])

	tests := []struct {
		name      string
		hash      string
		data      []byte
		expectErr bool
	}{
		{"valid hash", validHash, data, false},
		{"invalid hash", "sha256:0000000000000000000000000000000000000000000000000000000000000000", data, true},
		{"invalid format", "invalid", data, true},
		{"unsupported algorithm", "md5:abc123", data, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyHash(tt.data, tt.hash)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestUndumpBytecode tests bytecode undumping
func TestUndumpBytecode(t *testing.T) {
	// Create valid bytecode
	proto, err := compileSource(simpleLua)
	require.NoError(t, err)
	bc, err := dumpProto(proto)
	require.NoError(t, err)

	t.Run("valid bytecode", func(t *testing.T) {
		result, err := UndumpBytecode(bc)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("invalid bytecode", func(t *testing.T) {
		result, err := UndumpBytecode([]byte("invalid bytecode"))
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("empty bytecode", func(t *testing.T) {
		result, err := UndumpBytecode([]byte{})
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestBytecodeSize measures bytecode size vs source size
func TestBytecodeSize(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{"simple", simpleLua},
		{"medium", mediumLua},
		{"complex", complexLua},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto, err := compileSource(tt.source)
			require.NoError(t, err)

			bc, err := dumpProto(proto)
			require.NoError(t, err)

			sourceSize := len(tt.source)
			bytecodeSize := len(bc)
			ratio := float64(bytecodeSize) / float64(sourceSize) * 100

			t.Logf("Source: %d bytes, Bytecode: %d bytes (%.1f%% of source)",
				sourceSize, bytecodeSize, ratio)
		})
	}
}

// Benchmarks

// BenchmarkCompileSource benchmarks compiling Lua source to FunctionProto
func BenchmarkCompileSource(b *testing.B) {
	benchmarks := []struct {
		name   string
		source string
	}{
		{"simple", simpleLua},
		{"medium", mediumLua},
		{"complex", complexLua},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				proto, err := compileSource(bm.source)
				if err != nil {
					b.Fatal(err)
				}
				_ = proto
			}
		})
	}
}

// BenchmarkUndumpBytecode benchmarks loading FunctionProto from bytecode
func BenchmarkUndumpBytecode(b *testing.B) {
	benchmarks := []struct {
		name   string
		source string
	}{
		{"simple", simpleLua},
		{"medium", mediumLua},
		{"complex", complexLua},
	}

	for _, bm := range benchmarks {
		// Pre-compile and dump bytecode
		proto, err := compileSource(bm.source)
		if err != nil {
			b.Fatal(err)
		}
		bc, err := dumpProto(proto)
		if err != nil {
			b.Fatal(err)
		}

		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				proto, err := UndumpBytecode(bc)
				if err != nil {
					b.Fatal(err)
				}
				_ = proto
			}
		})
	}
}

// BenchmarkCompileVsUndump provides a direct comparison
func BenchmarkCompileVsUndump(b *testing.B) {
	sources := map[string]string{
		"simple":  simpleLua,
		"medium":  mediumLua,
		"complex": complexLua,
	}

	// Pre-compile bytecode for all sources
	bytecodes := make(map[string][]byte)
	for name, source := range sources {
		proto, err := compileSource(source)
		if err != nil {
			b.Fatal(err)
		}
		bc, err := dumpProto(proto)
		if err != nil {
			b.Fatal(err)
		}
		bytecodes[name] = bc
	}

	for name, source := range sources {
		bc := bytecodes[name]

		b.Run(name+"/compile", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				proto, _ := compileSource(source)
				_ = proto
			}
		})

		b.Run(name+"/undump", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				proto, _ := UndumpBytecode(bc)
				_ = proto
			}
		})
	}
}

// BenchmarkHashVerification benchmarks hash verification overhead
func BenchmarkHashVerification(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
	}

	for _, s := range sizes {
		data := make([]byte, s.size)
		for i := range data {
			data[i] = byte(i % 256)
		}
		h := sha256.Sum256(data)
		hash := "sha256:" + hex.EncodeToString(h[:])

		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = VerifyHash(data, hash)
			}
		})
	}
}

// BenchmarkFullPipeline benchmarks the complete load pipeline
func BenchmarkFullPipeline(b *testing.B) {
	sources := map[string]string{
		"simple":  simpleLua,
		"medium":  mediumLua,
		"complex": complexLua,
	}

	for name, source := range sources {
		// Pre-compile bytecode
		proto, err := compileSource(source)
		if err != nil {
			b.Fatal(err)
		}
		bc, err := dumpProto(proto)
		if err != nil {
			b.Fatal(err)
		}
		h := sha256.Sum256(bc)
		hash := "sha256:" + hex.EncodeToString(h[:])

		b.Run(name+"/compile_from_source", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				proto, _ := compileSource(source)
				_ = proto
			}
		})

		b.Run(name+"/verify_and_undump", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = VerifyHash(bc, hash)
				proto, _ := UndumpBytecode(bc)
				_ = proto
			}
		})
	}
}

// Mock filesystem implementations for testing LoadBytecode

type mockFile struct {
	*bytes.Reader
	name string
}

func (f *mockFile) Close() error { return nil }
func (f *mockFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{name: f.name, size: int64(f.Len())}, nil
}

type mockFileInfo struct {
	name string
	size int64
}

func (fi *mockFileInfo) Name() string       { return fi.name }
func (fi *mockFileInfo) Size() int64        { return fi.size }
func (fi *mockFileInfo) Mode() fs.FileMode  { return 0644 }
func (fi *mockFileInfo) ModTime() time.Time { return time.Now() }
func (fi *mockFileInfo) IsDir() bool        { return false }
func (fi *mockFileInfo) Sys() any           { return nil }

type mockBytecodeFS struct {
	files map[string][]byte
}

func (m *mockBytecodeFS) Open(path string) (fs.File, error) {
	if data, ok := m.files[path]; ok {
		return &mockFile{Reader: bytes.NewReader(data), name: path}, nil
	}
	return nil, fs.ErrNotExist
}

func (m *mockBytecodeFS) Stat(path string) (fs.FileInfo, error) {
	if data, ok := m.files[path]; ok {
		return &mockFileInfo{name: path, size: int64(len(data))}, nil
	}
	return nil, fs.ErrNotExist
}

func (m *mockBytecodeFS) ReadDir(_ string) ([]fs.DirEntry, error) {
	return nil, errors.New("not implemented")
}

func (m *mockBytecodeFS) OpenFile(_ string, _ int, _ fs.FileMode) (fsapi.File, error) {
	return nil, errors.New("not implemented")
}
func (m *mockBytecodeFS) Remove(_ string) error               { return errors.New("not implemented") }
func (m *mockBytecodeFS) Mkdir(_ string, _ fs.FileMode) error { return errors.New("not implemented") }
func (m *mockBytecodeFS) Rename(_, _ string) error            { return errors.New("not implemented") }
func (m *mockBytecodeFS) Truncate(_ string, _ int64) error    { return errors.New("not implemented") }
func (m *mockBytecodeFS) Chtimes(_ string, _, _ time.Time) error {
	return errors.New("not implemented")
}

func (m *mockBytecodeFS) Lstat(name string) (fs.FileInfo, error) {
	return m.Stat(name)
}

type mockBytecodeRegistry struct {
	filesystems map[string]fsapi.FS
}

func (r *mockBytecodeRegistry) GetFS(id string) (fsapi.FS, bool) {
	fsys, ok := r.filesystems[id]
	return fsys, ok
}

// TestLoadBytecode tests loading bytecode from filesystem
func TestLoadBytecode(t *testing.T) {
	// Create valid bytecode
	proto, err := compileSource(simpleLua)
	require.NoError(t, err)
	bc, err := dumpProto(proto)
	require.NoError(t, err)

	mockFS := &mockBytecodeFS{files: map[string][]byte{
		"/test.luac": bc,
	}}
	registry := &mockBytecodeRegistry{filesystems: map[string]fsapi.FS{
		"code": mockFS,
	}}

	t.Run("successful load", func(t *testing.T) {
		data, err := LoadBytecode(registry, "code", "/test.luac")
		assert.NoError(t, err)
		assert.Equal(t, bc, data)
	})

	t.Run("filesystem not found", func(t *testing.T) {
		_, err := LoadBytecode(registry, "nonexistent", "/test.luac")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadBytecode(registry, "code", "/nonexistent.luac")
		assert.Error(t, err)
	})
}

// TestLoadAndVerifyBytecode tests loading and verifying bytecode
func TestLoadAndVerifyBytecode(t *testing.T) {
	// Create valid bytecode
	proto, err := compileSource(simpleLua)
	require.NoError(t, err)
	bc, err := dumpProto(proto)
	require.NoError(t, err)

	// Calculate correct hash
	h := sha256.Sum256(bc)
	validHash := "sha256:" + hex.EncodeToString(h[:])

	mockFS := &mockBytecodeFS{files: map[string][]byte{
		"/test.luac": bc,
	}}
	registry := &mockBytecodeRegistry{filesystems: map[string]fsapi.FS{
		"code": mockFS,
	}}

	t.Run("successful load and verify", func(t *testing.T) {
		result, err := LoadAndVerifyBytecode(registry, "code", "/test.luac", validHash)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("filesystem not found", func(t *testing.T) {
		_, err := LoadAndVerifyBytecode(registry, "nonexistent", "/test.luac", validHash)
		assert.Error(t, err)
	})

	t.Run("hash mismatch", func(t *testing.T) {
		wrongHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		_, err := LoadAndVerifyBytecode(registry, "code", "/test.luac", wrongHash)
		assert.Error(t, err)
	})

	t.Run("invalid bytecode after load", func(t *testing.T) {
		// Create filesystem with invalid bytecode
		invalidFS := &mockBytecodeFS{files: map[string][]byte{
			"/invalid.luac": []byte("not valid bytecode"),
		}}
		invalidReg := &mockBytecodeRegistry{filesystems: map[string]fsapi.FS{
			"code": invalidFS,
		}}
		invalidData := []byte("not valid bytecode")
		h := sha256.Sum256(invalidData)
		invalidHash := "sha256:" + hex.EncodeToString(h[:])

		_, err := LoadAndVerifyBytecode(invalidReg, "code", "/invalid.luac", invalidHash)
		assert.Error(t, err)
	})
}
