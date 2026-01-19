package lsp

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/yuin/gopher-lua/compiler/check"
	"github.com/yuin/gopher-lua/compiler/parse"
	golualsp "github.com/yuin/gopher-lua/lsp"
	"github.com/yuin/gopher-lua/lsp/index"
	"github.com/yuin/gopher-lua/types/db"
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

	database := buildDBFromModules(idx.cm.GetModuleDefs())
	entries := idx.collectLuaEntries()
	idx.log.Debug("indexing entries", zap.Int("count", len(entries)))

	var failed int
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := idx.indexEntry(entry, database); err != nil {
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

	database := buildDBFromModules(idx.cm.GetModuleDefs())
	return idx.indexEntry(&entryInfo{ID: id, Source: node.Source}, database)
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
func (idx *Indexer) indexEntry(entry *entryInfo, database *db.DB) error {
	fileID := entry.ID.String()

	stmts, err := parse.Parse(strings.NewReader(entry.Source), fileID)
	if err != nil {
		return err
	}

	ctx := db.NewContext(database)
	checker := check.New(ctx)
	diags := checker.Check(stmts)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Build and register manifest
	idx.buildManifest(fileID, database)

	if check.HasError(diags) && len(check.FilterErrors(diags)) > 0 {
		return errors.New("type check errors")
	}
	return nil
}

// buildManifest builds and registers a Manifest for the file.
func (idx *Indexer) buildManifest(file string, database *db.DB) {
	if database == nil || idx.lspService == nil {
		return
	}

	manifest := io.NewManifest(file)
	idx.lspService.RegisterManifest(manifest)
}

// buildDBFromModules creates a type database from module definitions.
func buildDBFromModules(modules []*luaapi.ModuleDef) *db.DB {
	database := db.New()

	for _, mod := range modules {
		if mod == nil {
			continue
		}
		if mod.Types != nil {
			manifest := mod.Types()
			if manifest != nil {
				database.Connect(mod.Name, manifest)
			}
		}
	}

	return database
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

// lspObserver provides LSP indexing integration.
type lspObserver struct {
	file    string
	symbols *index.SymbolIndex
}
