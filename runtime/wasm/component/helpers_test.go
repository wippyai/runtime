package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	fslib "io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
)

type mockFS struct {
	files map[string][]byte
}

func (m *mockFS) Open(name string) (fslib.File, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, fslib.ErrNotExist
	}
	return &mockFile{data: data}, nil
}

func (m *mockFS) Stat(name string) (fslib.FileInfo, error) {
	_, ok := m.files[name]
	if !ok {
		return nil, fslib.ErrNotExist
	}
	return nil, nil
}

func (m *mockFS) ReadDir(_ string) ([]fslib.DirEntry, error) {
	return nil, nil
}

func (m *mockFS) OpenFile(_ string, _ int, _ fslib.FileMode) (fs.File, error) {
	return nil, fslib.ErrNotExist
}

func (m *mockFS) Remove(_ string) error {
	return nil
}

func (m *mockFS) Mkdir(_ string, _ fslib.FileMode) error {
	return nil
}

type mockFile struct {
	data   []byte
	reader *bytes.Reader
}

func (f *mockFile) Read(p []byte) (n int, err error) {
	if f.reader == nil {
		f.reader = bytes.NewReader(f.data)
	}
	return f.reader.Read(p)
}

func (f *mockFile) Close() error {
	return nil
}

func (f *mockFile) Stat() (fslib.FileInfo, error) {
	return nil, nil
}

type mockFSRegistry struct {
	filesystems map[string]*mockFS
}

func newMockFSRegistry() *mockFSRegistry {
	return &mockFSRegistry{
		filesystems: make(map[string]*mockFS),
	}
}

func (r *mockFSRegistry) GetFS(id string) (fs.FS, bool) {
	f, ok := r.filesystems[id]
	return f, ok
}

func (r *mockFSRegistry) addFS(id string, files map[string][]byte) {
	r.filesystems[id] = &mockFS{files: files}
}

func setupTestContext() context.Context {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	return payload.WithTranscoder(ctx, transcoder)
}

func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

func TestVerifyHash_Valid(t *testing.T) {
	data := []byte("test data")
	hash := computeSHA256(data)

	err := VerifyHash(data, hash)

	assert.NoError(t, err)
}

func TestVerifyHash_Mismatch(t *testing.T) {
	data := []byte("test data")
	wrongHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	err := VerifyHash(data, wrongHash)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestVerifyHash_InvalidFormat(t *testing.T) {
	data := []byte("test data")

	err := VerifyHash(data, "invalidhash")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid hash format")
}

func TestVerifyHash_UnsupportedAlgorithm(t *testing.T) {
	data := []byte("test data")

	err := VerifyHash(data, "md5:abcd1234")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported hash algorithm")
}

func TestLoadWASM_Success(t *testing.T) {
	fsReg := newMockFSRegistry()
	wasmData := []byte{0x00, 0x61, 0x73, 0x6D} // WASM magic header
	fsReg.addFS("test:fs", map[string][]byte{
		"module.wasm": wasmData,
	})

	data, err := LoadWASM(fsReg, "test:fs", "module.wasm")

	require.NoError(t, err)
	assert.Equal(t, wasmData, data)
}

func TestLoadWASM_FSNotFound(t *testing.T) {
	fsReg := newMockFSRegistry()

	_, err := LoadWASM(fsReg, "nonexistent:fs", "module.wasm")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem not found")
}

func TestLoadWASM_FileNotFound(t *testing.T) {
	fsReg := newMockFSRegistry()
	fsReg.addFS("test:fs", map[string][]byte{})

	_, err := LoadWASM(fsReg, "test:fs", "nonexistent.wasm")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestLoadAndVerifyWASM_Success(t *testing.T) {
	fsReg := newMockFSRegistry()
	wasmData := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
	hash := computeSHA256(wasmData)
	fsReg.addFS("test:fs", map[string][]byte{
		"module.wasm": wasmData,
	})

	data, err := LoadAndVerifyWASM(fsReg, "test:fs", "module.wasm", hash)

	require.NoError(t, err)
	assert.Equal(t, wasmData, data)
}

func TestLoadAndVerifyWASM_HashMismatch(t *testing.T) {
	fsReg := newMockFSRegistry()
	wasmData := []byte{0x00, 0x61, 0x73, 0x6D}
	wrongHash := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	fsReg.addFS("test:fs", map[string][]byte{
		"module.wasm": wasmData,
	})

	_, err := LoadAndVerifyWASM(fsReg, "test:fs", "module.wasm", wrongHash)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

type testConfig struct {
	Name   string `json:"name"`
	Method string `json:"method"`
}

func (c *testConfig) Validate() error {
	if c.Name == "" {
		return io.EOF // Use any error for test
	}
	return nil
}

func TestUnpackConfig_Success(t *testing.T) {
	ctx := setupTestContext()

	data := `{"name": "test", "method": "main"}`
	entry := registry.Entry{
		Data: payload.NewPayload(data, payload.JSON),
	}

	cfg, err := UnpackConfig[testConfig](ctx, entry)

	require.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, "main", cfg.Method)
}

func TestUnpackConfig_ValidationError(t *testing.T) {
	ctx := setupTestContext()

	data := `{"name": "", "method": "main"}`
	entry := registry.Entry{
		Data: payload.NewPayload(data, payload.JSON),
	}

	_, err := UnpackConfig[testConfig](ctx, entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
}

func TestUnpackConfig_NoTranscoder(t *testing.T) {
	ctx := context.Background() // No transcoder

	entry := registry.Entry{
		Data: payload.NewPayload(`{}`, payload.JSON),
	}

	_, err := UnpackConfig[testConfig](ctx, entry)

	require.Error(t, err)
}

func TestUnpackConfig_InvalidJSON(t *testing.T) {
	ctx := setupTestContext()

	data := `{"invalid json`
	entry := registry.Entry{
		Data: payload.NewPayload(data, payload.JSON),
	}

	_, err := UnpackConfig[testConfig](ctx, entry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal")
}

func TestHandler_Pattern(t *testing.T) {
	h := NewHandler("test.kind", nil)

	pattern := h.Pattern()

	assert.Equal(t, "registry", string(pattern.System))
	assert.Contains(t, string(pattern.Kind), "create")
	assert.Contains(t, string(pattern.Kind), "update")
	assert.Contains(t, string(pattern.Kind), "delete")
}
