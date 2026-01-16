package lsp

import (
	"context"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/yuin/gopher-lua/compiler/parse"
	golualsp "github.com/yuin/gopher-lua/lsp"
	"github.com/yuin/gopher-lua/lsp/index"
	"github.com/yuin/gopher-lua/types"
	"github.com/yuin/gopher-lua/types/diag"
	"github.com/yuin/gopher-lua/types/io"
	"go.uber.org/zap"
)

// Indexer builds type information and populates LSP indexes.
type Indexer struct {
	log        *zap.Logger
	cm         *code.Manager
	lspService *golualsp.Service
	mu         sync.RWMutex
}

// NewIndexer creates a new indexer.
func NewIndexer(log *zap.Logger, cm *code.Manager, lspService *golualsp.Service) *Indexer {
	return &Indexer{
		log:        log.Named("indexer"),
		cm:         cm,
		lspService: lspService,
	}
}

// IndexAll indexes all Lua entries from the code manager.
func (idx *Indexer) IndexAll(ctx context.Context) error {
	if idx.cm == nil {
		return nil
	}

	modules := idx.cm.GetModuleDefs()
	env := buildEnvFromModules(modules)

	entries := idx.collectLuaEntries()
	idx.log.Debug("indexing entries", zap.Int("count", len(entries)))

	var failed int
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := idx.indexEntry(entry, env); err != nil {
			failed++
			idx.log.Debug("index failed", zap.String("id", entry.ID.String()), zap.Error(err))
		}
	}

	if failed > 0 {
		idx.log.Warn("indexing completed with errors",
			zap.Int("total", len(entries)),
			zap.Int("failed", failed))
	} else {
		idx.log.Debug("indexing completed", zap.Int("total", len(entries)))
	}

	return nil
}

// IndexEntry indexes a single entry by ID.
func (idx *Indexer) IndexEntry(ctx context.Context, id registry.ID) error {
	if idx.cm == nil {
		return nil
	}

	node, err := idx.cm.GetNode(id)
	if err != nil {
		return err
	}

	if !isLuaKind(node.Kind) || node.Source == "" {
		return nil
	}

	modules := idx.cm.GetModuleDefs()
	env := buildEnvFromModules(modules)

	return idx.indexEntry(&entryInfo{ID: id, Source: node.Source}, env)
}

// entryInfo holds minimal entry data for indexing.
type entryInfo struct {
	ID     registry.ID
	Source string
}

// collectLuaEntries collects all Lua entries from the code manager graph.
func (idx *Indexer) collectLuaEntries() []*entryInfo {
	var entries []*entryInfo

	for _, mod := range idx.cm.GetModules() {
		id := registry.NewID("", mod.Name)
		node, err := idx.cm.GetNode(id)
		if err != nil {
			continue
		}

		if isLuaKind(node.Kind) && node.Source != "" {
			entries = append(entries, &entryInfo{ID: id, Source: node.Source})
		}
	}

	return entries
}

// indexEntry indexes a single entry.
func (idx *Indexer) indexEntry(entry *entryInfo, env *types.Env) error {
	fileID := entry.ID.String()

	chunk, err := parse.ParseString(entry.Source, fileID)
	if err != nil {
		return err
	}

	// Type check without observer for now
	result := types.CheckChunkWithContext(chunk,
		types.WithStdlib(),
		types.WithEnv(env),
		types.WithSource(fileID),
	)
	// Release session resources after type checking
	defer result.Context.Release()

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Build and register manifest
	idx.buildManifest(fileID, result.Context)

	return nil
}

// buildManifest builds and registers a TypeManifest for the file.
func (idx *Indexer) buildManifest(file string, ctx *types.Context) {
	if ctx == nil || idx.lspService == nil {
		return
	}

	manifest := io.NewManifest(file)
	manifest.WithDebug()

	idx.lspService.RegisterManifest(manifest)
}

// buildEnvFromModules creates a type environment from module definitions.
func buildEnvFromModules(modules []*luaapi.ModuleDef) *types.Env {
	env := types.NewEnv()

	// Add standard library
	stdlib := types.StandardLibrary()
	for name, typ := range stdlib {
		env = env.WithSymbol(name, typ)
	}

	// Add module types
	for _, mod := range modules {
		if mod == nil {
			continue
		}
		if mod.Types != nil {
			manifest := mod.Types()
			if manifest != nil && manifest.Export != nil {
				env = env.WithSymbol(mod.Name, manifest.Export)
			}
		}
	}

	return env
}

// isLuaKind checks if a kind represents Lua code.
func isLuaKind(kind registry.Kind) bool {
	luaPrefixes := []string{"function.lua.", "library.lua.", "process.lua.", "workflow.lua"}
	for _, prefix := range luaPrefixes {
		if strings.HasPrefix(kind, prefix) {
			return true
		}
	}
	return false
}

// lspObserver implements types.Observer for LSP indexing.
type lspObserver struct {
	file    string
	symbols *index.SymbolIndex
}

func (o *lspObserver) OnEnterFunction(name string, pos interface{}) {}
func (o *lspObserver) OnExitFunction(name string)                   {}

func (o *lspObserver) OnSymbolDef(name string, kind types.SymbolKind, typ types.Type, span diag.Span, scope string) {
	if o.symbols == nil {
		return
	}
	indexKind := convertTypesSymbolKind(kind)
	o.symbols.AddDefinition(o.file, name, indexKind, typ, span, scope)
}

func (o *lspObserver) OnSymbolRef(name string, typ types.Type, pos interface{}) {}
func (o *lspObserver) OnTypeResolved(expr interface{}, typ types.Type)          {}
func (o *lspObserver) OnSymbolEscape(name string)                               {}

// convertTypesSymbolKind converts types.SymbolKind to index.SymbolKind.
func convertTypesSymbolKind(k types.SymbolKind) index.SymbolKind {
	switch k {
	case types.SymbolVariable:
		return index.SymbolVariable
	case types.SymbolFunction:
		return index.SymbolFunction
	case types.SymbolParameter:
		return index.SymbolParameter
	case types.SymbolType:
		return index.SymbolType
	case types.SymbolField:
		return index.SymbolField
	default:
		return index.SymbolVariable
	}
}
