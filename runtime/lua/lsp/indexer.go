package lsp

import (
	"context"
	"strings"
	"sync"

	"github.com/wippyai/go-lua/compiler/check"
	"github.com/wippyai/go-lua/compiler/check/hooks"
	"github.com/wippyai/go-lua/compiler/check/scope"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/compiler/stdlib"
	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/types/db"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/query/core"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"go.uber.org/zap"
)

const luaKindSuffix = ".lua"

// Indexer builds type information and populates LSP indexes.
type Indexer struct {
	log         *zap.Logger
	cm          *code.Manager
	lspService  *golualsp.Service
	symbols     *index.SymbolIndex
	callGraph   *index.CallGraph
	checker     *check.Checker
	builtinHash string
	mu          sync.Mutex
}

// NewIndexer creates a new indexer.
func NewIndexer(log *zap.Logger, cm *code.Manager, lspService *golualsp.Service, symbols *index.SymbolIndex, callGraph *index.CallGraph) *Indexer {
	idx := &Indexer{
		log:        log.Named("indexer"),
		cm:         cm,
		lspService: lspService,
		symbols:    symbols,
		callGraph:  callGraph,
	}
	if cm != nil {
		idx.checker = buildCheckerFromModules(cm.GetModuleDefs(), symbols, callGraph)
		idx.builtinHash = cm.BuiltinManifestHash()
	}
	return idx
}

// IndexAll indexes all Lua entries from the code manager.
func (idx *Indexer) IndexAll(ctx context.Context) error {
	idx.ensureChecker()
	if idx.cm == nil || idx.checker == nil {
		return nil
	}

	entries := idx.collectLuaEntries()
	idx.log.Debug("indexing entries", zap.Int("count", len(entries)))

	var failed int
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		idx.mu.Lock()
		err := idx.indexEntryLocked(entry)
		idx.checker.ClearCache()
		idx.mu.Unlock()

		if err != nil {
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
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	idx.ensureChecker()
	if idx.cm == nil || idx.checker == nil {
		return nil
	}

	node, err := idx.cm.GetNode(id)
	if err != nil {
		return err
	}

	if !isLuaKind(node.Kind) || node.Source == "" {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.checker.ClearCache()
	return idx.indexEntryLocked(&entryInfo{ID: id, Source: node.Source})
}

// entryInfo holds minimal entry data for indexing.
type entryInfo struct {
	ID     registry.ID
	Source string
}

// collectLuaEntries collects all Lua entries from the code manager graph.
func (idx *Indexer) collectLuaEntries() []*entryInfo {
	var entries []*entryInfo

	for _, node := range idx.cm.GetAllNodes() {
		if isLuaKind(node.Kind) && node.Source != "" {
			entries = append(entries, &entryInfo{ID: node.ID, Source: node.Source})
		}
	}

	return entries
}

// indexEntryLocked indexes a single entry. Must be called with idx.mu held.
func (idx *Indexer) indexEntryLocked(entry *entryInfo) error {
	fileID := entry.ID.String()

	stmts, err := parse.Parse(strings.NewReader(entry.Source), fileID)
	if err != nil {
		return err
	}

	// Invalidate old data after successful parse to avoid dropping last good index on parse errors.
	if idx.symbols != nil {
		idx.symbols.InvalidateFile(fileID)
	}
	if idx.callGraph != nil {
		idx.callGraph.InvalidateFile(fileID)
	}

	// Connect dependency manifests to the checker's database
	depManifests := idx.cm.GetNodeDependencyManifests(entry.ID)
	for alias, manifest := range depManifests {
		idx.checker.Database().Connect(alias, manifest)
	}

	sess := idx.checker.CheckChunk(stmts, fileID)

	// Build and register manifest
	idx.buildManifest(fileID, sess)

	hasErr := hasErrors(sess.Diagnostics)
	sess.Release()

	if hasErr {
		idx.log.Debug("lsp indexed with typecheck errors", zap.String("id", fileID))
		return nil
	}
	return nil
}

// buildManifest builds and registers a Manifest for the file.
func (idx *Indexer) buildManifest(file string, sess *check.Session) {
	if idx.lspService == nil {
		return
	}

	manifest := io.NewManifest(file)
	if exportType := sess.ExportType(); exportType != nil {
		manifest.SetExport(exportType)
	}
	idx.lspService.RegisterManifest(manifest)
}

// buildCheckerFromModules creates a type checker from module definitions with LSP hooks.
func buildCheckerFromModules(modules []*luaapi.ModuleDef, symbols *index.SymbolIndex, callGraph *index.CallGraph) *check.Checker {
	database := db.New()

	// Build type scope with builtins
	base := scope.NewWithBuiltins()

	// Build global types map for value namespace
	globalTypes := make(map[string]typ.Type)
	for name, t := range stdlib.Library() {
		globalTypes[name] = t
	}

	// Connect module manifests
	for _, mod := range modules {
		if mod == nil {
			continue
		}
		if mod.Types != nil {
			manifest := mod.Types()
			if manifest != nil {
				database.Connect(mod.Name, manifest)
				if manifest.Export != nil {
					globalTypes[mod.Name] = manifest.Export
				}
				for name, t := range manifest.AllGlobals() {
					globalTypes[name] = t
				}
			}
		}
	}

	// Create LSP indexer hook
	lspIndexer := hooks.NewLSPIndexer(symbols, callGraph)

	return check.NewChecker(database, check.Deps{
		Types:       core.NewEngineWithStdlib(stdlib.EngineConfig()),
		Stdlib:      base,
		GlobalTypes: globalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, hooks.WithAssign(), hooks.WithReturn(), hooks.WithCall(), hooks.WithField(), hooks.WithLSPIndex(lspIndexer))
}

func (idx *Indexer) ensureChecker() {
	if idx.cm == nil {
		return
	}
	hash := idx.cm.BuiltinManifestHash()
	if idx.checker == nil || hash != idx.builtinHash {
		idx.checker = buildCheckerFromModules(idx.cm.GetModuleDefs(), idx.symbols, idx.callGraph)
		idx.builtinHash = hash
	}
}

// hasErrors checks if any diagnostic is an error.
func hasErrors(diagnostics []diag.Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			return true
		}
	}
	return false
}

// isLuaKind checks if a kind represents Lua code.
func isLuaKind(kind registry.Kind) bool {
	return strings.HasSuffix(kind, luaKindSuffix) || strings.Contains(kind, luaKindSuffix+".")
}
