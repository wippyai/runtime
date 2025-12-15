package stages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"strings"
	"sync"
	"testing/fstest"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/boot/pack"
	glua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/bytecode"
	"github.com/yuin/gopher-lua/parse"
	"go.uber.org/zap"
)

// BytecodeFSID is the registry ID for the synthetic bytecode filesystem.
const BytecodeFSID = "internal:lua.bytecode"

var (
	bytecodeMu       sync.RWMutex
	bytecodeResource *pack.ResourceSpec
)

// kindMapping maps source kinds to bytecode kinds.
// Only includes kinds that have registered bytecode managers.
var kindMapping = map[registry.Kind]registry.Kind{
	luaapi.Function: luaapi.FunctionBytecode,
	luaapi.Library:  luaapi.LibraryBytecode,
	luaapi.Process:  luaapi.ProcessBytecode,
}

type bytecodeStage struct {
	patterns []string
}

// Bytecode creates a stage that compiles Lua source entries to bytecode.
// It transforms source-based entries (function.lua, library.lua, etc.) to
// bytecode-based entries (function.lua.bc, library.lua.bc, etc.).
//
// The compiled bytecode is stored in an in-memory filesystem that can be
// embedded in the pack file. Use GetBytecodeResource() to retrieve it.
//
// Patterns filter which entries to compile. If empty, all supported entries
// are compiled. Patterns match entry ID strings or names.
func Bytecode(patterns ...string) boot.Stage {
	return &bytecodeStage{
		patterns: patterns,
	}
}

func (s *bytecodeStage) Name() string {
	return "bytecode"
}

func (s *bytecodeStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	log := logs.GetLogger(ctx)
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return ErrTranscoderNotFound
	}

	// Collect entries to compile
	toCompile := s.collectEntries(*entries)
	if len(toCompile) == 0 {
		log.Info("no Lua entries to compile to bytecode")
		return nil
	}

	log.Info("compiling Lua entries to bytecode", zap.Int("count", len(toCompile)))

	// Build in-memory filesystem for bytecode files
	mapFS := make(fstest.MapFS)
	compiled := make(map[string]*compiledEntry)

	for _, idx := range toCompile {
		entry := &(*entries)[idx]
		result, err := s.compileEntry(entry, transcoder)
		if err != nil {
			log.Error("failed to compile entry",
				zap.String("id", entry.ID.String()),
				zap.Error(err))
			return NewBytecodeCompileError(entry.ID, err)
		}

		// Store bytecode in filesystem
		mapFS[result.path] = &fstest.MapFile{
			Data: result.bytecode,
			Mode: 0644,
		}
		compiled[entry.ID.String()] = result

		log.Debug("compiled entry to bytecode",
			zap.String("id", entry.ID.String()),
			zap.String("path", result.path),
			zap.Int("size", len(result.bytecode)))
	}

	// Transform entries
	for _, idx := range toCompile {
		entry := &(*entries)[idx]
		result := compiled[entry.ID.String()]
		if err := s.transformEntry(entry, result, transcoder); err != nil {
			return NewBytecodeTransformError(entry.ID, err)
		}
	}

	// Store resource for pack
	bytecodeMu.Lock()
	bytecodeResource = &pack.ResourceSpec{
		ID: registry.ParseID(BytecodeFSID),
		FS: mapFS,
	}
	bytecodeMu.Unlock()

	// Add fs.embed entry for the bytecode filesystem so it gets registered at runtime
	bcFSEntry := registry.Entry{
		ID:   registry.ParseID(BytecodeFSID),
		Kind: embedapi.Kind,
		Data: payload.New(embedapi.Config{}),
	}
	*entries = append(*entries, bcFSEntry)

	log.Info("bytecode compilation complete",
		zap.Int("compiled", len(toCompile)),
		zap.Int("files", len(mapFS)))

	return nil
}

// GetBytecodeResource retrieves the compiled bytecode resource.
// Returns nil if no bytecode was compiled.
func GetBytecodeResource() *pack.ResourceSpec {
	bytecodeMu.RLock()
	defer bytecodeMu.RUnlock()
	return bytecodeResource
}

// ClearBytecodeResource clears the stored bytecode resource.
// Useful for testing.
func ClearBytecodeResource() {
	bytecodeMu.Lock()
	bytecodeResource = nil
	bytecodeMu.Unlock()
}

type compiledEntry struct {
	path     string
	hash     string
	bytecode []byte
}

// collectEntries returns indices of entries that should be compiled.
func (s *bytecodeStage) collectEntries(entries []registry.Entry) []int {
	var indices []int
	for i, entry := range entries {
		if _, ok := kindMapping[entry.Kind]; !ok {
			continue
		}

		// Check pattern filter
		if len(s.patterns) > 0 && !s.matchesPattern(entry) {
			continue
		}

		indices = append(indices, i)
	}
	return indices
}

func (s *bytecodeStage) matchesPattern(entry registry.Entry) bool {
	for _, pattern := range s.patterns {
		if entry.ID.String() == pattern || entry.ID.Name == pattern {
			return true
		}
		// Support namespace wildcard: "app:**" matches "app:anything"
		if strings.HasSuffix(pattern, ":**") {
			ns := strings.TrimSuffix(pattern, ":**")
			if entry.ID.NS == ns {
				return true
			}
		}
	}
	return false
}

func (s *bytecodeStage) compileEntry(entry *registry.Entry, transcoder payload.Transcoder) (*compiledEntry, error) {
	// Extract source from entry data
	source, err := s.extractSource(entry, transcoder)
	if err != nil {
		return nil, err
	}

	// Parse Lua source
	chunk, err := parse.Parse(strings.NewReader(source), entry.ID.String())
	if err != nil {
		return nil, NewBytecodeParseError(err)
	}

	// Compile to proto
	proto, err := glua.Compile(chunk, entry.ID.String())
	if err != nil {
		return nil, NewBytecodeCompileLuaError(err)
	}

	// Dump to bytecode
	bc, err := bytecode.Dump(proto)
	if err != nil {
		return nil, NewBytecodeDumpError(err)
	}

	// Calculate hash
	h := sha256.Sum256(bc)
	hash := "sha256:" + hex.EncodeToString(h[:])

	// Generate path: namespace/name.luac
	bcPath := s.generatePath(entry.ID)

	return &compiledEntry{
		path:     bcPath,
		hash:     hash,
		bytecode: bc,
	}, nil
}

func (s *bytecodeStage) extractSource(entry *registry.Entry, transcoder payload.Transcoder) (string, error) {
	if entry.Data == nil {
		return "", ErrBytecodeNoData
	}

	// Transcode to Golang format if needed
	data := entry.Data
	if data.Format() != payload.Golang {
		var err error
		data, err = transcoder.Transcode(data, payload.Golang)
		if err != nil {
			return "", NewBytecodeTranscodeError(err)
		}
	}

	// Extract source field
	m, ok := data.Data().(map[string]any)
	if !ok {
		return "", ErrBytecodeInvalidData
	}

	source, ok := m["source"].(string)
	if !ok || source == "" {
		return "", ErrBytecodeNoSource
	}

	return source, nil
}

func (s *bytecodeStage) generatePath(id registry.ID) string {
	// Use namespace/name.luac format
	if id.NS != "" {
		return path.Join(id.NS, id.Name+".luac")
	}
	return id.Name + ".luac"
}

func (s *bytecodeStage) transformEntry(entry *registry.Entry, result *compiledEntry, transcoder payload.Transcoder) error {
	// Get new kind
	newKind, ok := kindMapping[entry.Kind]
	if !ok {
		return ErrBytecodeUnsupportedKind
	}

	// Extract original config data
	data := entry.Data
	if data.Format() != payload.Golang {
		var err error
		data, err = transcoder.Transcode(data, payload.Golang)
		if err != nil {
			return NewBytecodeTranscodeError(err)
		}
	}

	m, ok := data.Data().(map[string]any)
	if !ok {
		return ErrBytecodeInvalidData
	}

	// Build new config preserving existing fields
	newConfig := map[string]any{
		"fs":   BytecodeFSID,
		"path": result.path,
		"hash": result.hash,
	}

	// Preserve method if present
	if method, ok := m["method"].(string); ok {
		newConfig["method"] = method
	}

	// Preserve imports if present
	if imports, ok := m["imports"]; ok {
		newConfig["imports"] = imports
	}

	// Preserve modules if present
	if modules, ok := m["modules"]; ok {
		newConfig["modules"] = modules
	}

	// Preserve pool if present (for functions)
	if pool, ok := m["pool"]; ok {
		newConfig["pool"] = pool
	}

	// Preserve meta if present
	if meta, ok := m["meta"]; ok {
		newConfig["meta"] = meta
	}

	// Update entry
	entry.Kind = newKind
	entry.Data = payload.New(newConfig)

	return nil
}

// BytecodeFS returns the compiled bytecode as an fs.FS.
// Returns nil if no bytecode was compiled.
func BytecodeFS() fs.FS {
	bytecodeMu.RLock()
	defer bytecodeMu.RUnlock()
	if bytecodeResource == nil {
		return nil
	}
	return bytecodeResource.FS
}
