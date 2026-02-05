package indexing

import (
	"context"
	"strings"

	"github.com/wippyai/go-lua/compiler/ast"
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
	"go.uber.org/zap"
)

const luaKindSuffix = ".lua"

// DiagnosticStore stores diagnostics for files.
type DiagnosticStore interface {
	StoreDiagnostics(fileID string, diagnostics []diag.Diagnostic)
}

// Indexer builds type information and populates LSP indexes.
type Indexer struct {
	log        *zap.Logger
	provider   Provider
	lspService *golualsp.Service
	symbols    *index.SymbolIndex
	callGraph  *index.CallGraph
	documents  *DocumentStore
	diagStore  DiagnosticStore
}

// NewIndexer creates a new indexer.
func NewIndexer(log *zap.Logger, provider Provider, lspService *golualsp.Service, symbols *index.SymbolIndex, callGraph *index.CallGraph, docs *DocumentStore, diagStore DiagnosticStore) *Indexer {
	if log == nil {
		log = zap.NewNop()
	}
	return &Indexer{
		log:        log.Named("indexer"),
		provider:   provider,
		lspService: lspService,
		symbols:    symbols,
		callGraph:  callGraph,
		documents:  docs,
		diagStore:  diagStore,
	}
}

// IndexAll indexes all Lua entries from the code manager.
func (idx *Indexer) IndexAll(ctx context.Context) error {
	checker := idx.newChecker()
	if idx.provider == nil || checker == nil {
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

		err := idx.indexEntryWithChecker(entry, checker)
		checker.ClearCache()

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

	checker := idx.newChecker()
	if idx.provider == nil || checker == nil {
		return nil
	}

	entry, err := idx.entryInfoForID(id)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil
	}

	err = idx.indexEntryWithChecker(entry, checker)
	checker.ClearCache()
	return err
}

func (idx *Indexer) entryInfoForID(id registry.ID) (*entryInfo, error) {
	if idx.provider == nil {
		return nil, nil
	}
	if overlay, ok := idx.overlaySource(id); ok && overlay != "" {
		return &entryInfo{ID: id, Source: overlay}, nil
	}
	node, err := idx.provider.Node(id)
	if err != nil {
		idx.invalidateEntry(id)
		return nil, err
	}
	if node.ID == (registry.ID{}) {
		idx.invalidateEntry(id)
		return nil, nil
	}
	if !isLuaKind(node.Kind) || node.Source == "" {
		if overlay, ok := idx.overlaySource(node.ID); ok && overlay != "" {
			return &entryInfo{ID: node.ID, Source: overlay}, nil
		}
		idx.invalidateEntry(node.ID)
		return nil, nil
	}

	source := node.Source
	if overlay, ok := idx.overlaySource(node.ID); ok {
		source = overlay
	}

	return &entryInfo{ID: node.ID, Source: source}, nil
}

func (idx *Indexer) invalidateEntry(id registry.ID) {
	if id == (registry.ID{}) {
		return
	}
	fileID := id.String()
	if idx.symbols != nil {
		idx.symbols.InvalidateFile(fileID)
	}
	if idx.callGraph != nil {
		idx.callGraph.InvalidateFile(fileID)
	}
	if idx.lspService != nil {
		idx.lspService.InvalidateFile(fileID)
	}
}

// entryInfo holds minimal entry data for indexing.
type entryInfo struct {
	ID     registry.ID
	Source string
}

// collectLuaEntries collects all Lua entries from the code manager graph.
func (idx *Indexer) collectLuaEntries() []*entryInfo {
	var entries []*entryInfo
	if idx.provider == nil {
		return entries
	}

	for _, node := range idx.provider.AllNodes() {
		if !isLuaKind(node.Kind) || node.Source == "" {
			continue
		}
		source := node.Source
		if overlay, ok := idx.overlaySource(node.ID); ok {
			source = overlay
		}
		entries = append(entries, &entryInfo{ID: node.ID, Source: source})
	}

	return entries
}

// indexEntryWithChecker indexes a single entry.
func (idx *Indexer) indexEntryWithChecker(entry *entryInfo, checker *lspChecker) error {
	if checker == nil {
		return nil
	}
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
	depManifests := idx.provider.DependencyManifests(entry.ID)
	manifest, diagnostics := checker.CheckParsed(stmts, fileID, depManifests)
	idx.registerManifest(manifest)
	idx.storeDiagnostics(fileID, diagnostics)
	hasErr := hasErrors(diagnostics)

	if hasErr {
		idx.log.Debug("lsp indexed with typecheck errors", zap.String("id", fileID))
		return nil
	}
	return nil
}

// IndexSource indexes a single source string into LSP indexes.
// This is used for on-demand indexing of open documents that are not in the code manager.
func (idx *Indexer) IndexSource(ctx context.Context, id registry.ID, source string, deps map[string]*io.Manifest) error {
	if idx == nil || id == (registry.ID{}) || source == "" {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	checker := idx.newChecker()
	if checker == nil {
		return nil
	}

	fileID := id.String()
	stmts, err := parse.Parse(strings.NewReader(source), fileID)
	if err != nil {
		return err
	}

	if idx.symbols != nil {
		idx.symbols.InvalidateFile(fileID)
	}
	if idx.callGraph != nil {
		idx.callGraph.InvalidateFile(fileID)
	}

	manifest, diagnostics := checker.CheckParsed(stmts, fileID, deps)
	idx.registerManifest(manifest)
	idx.storeDiagnostics(fileID, diagnostics)
	hasErr := hasErrors(diagnostics)

	if hasErr {
		idx.log.Debug("lsp indexed with typecheck errors", zap.String("id", fileID))
		return nil
	}
	return nil
}

// registerManifest registers a Manifest for the file.
func (idx *Indexer) registerManifest(manifest *io.Manifest) {
	if idx.lspService == nil {
		return
	}

	if manifest == nil {
		return
	}
	idx.lspService.RegisterManifest(manifest)
}

// storeDiagnostics stores diagnostics for a file.
func (idx *Indexer) storeDiagnostics(fileID string, diagnostics []diag.Diagnostic) {
	if idx.diagStore == nil {
		return
	}
	idx.diagStore.StoreDiagnostics(fileID, diagnostics)
}

func (idx *Indexer) newChecker() *lspChecker {
	if idx == nil || idx.provider == nil {
		return nil
	}
	modules := idx.provider.ModuleDefs()
	builtinHash := idx.provider.BuiltinManifestHash()
	return newLSPChecker(modules, idx.symbols, idx.callGraph, builtinHash)
}

func (idx *Indexer) overlaySource(id registry.ID) (string, bool) {
	if idx.documents == nil {
		return "", false
	}
	doc, ok := idx.documents.Get(id)
	if !ok {
		return "", false
	}
	return doc.Text, true
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

type lspChecker struct {
	db          *db.DB
	checker     *check.Checker
	builtinHash string
}

func newLSPChecker(mods []*luaapi.ModuleDef, symbols *index.SymbolIndex, callGraph *index.CallGraph, builtinHash string) *lspChecker {
	builtins := make(map[string]typ.Type)
	manifests := make(map[string]*io.Manifest)

	for _, mod := range mods {
		if mod == nil || mod.Types == nil || mod.Name == "" {
			continue
		}
		manifest := mod.Types()
		if manifest == nil {
			continue
		}
		manifests[mod.Name] = manifest
		if manifest.Export != nil {
			builtins[mod.Name] = manifest.Export
		}
		for name, t := range manifest.AllGlobals() {
			builtins[name] = t
		}
	}

	base := scope.NewWithBuiltins()
	globalTypes := make(map[string]typ.Type)
	for name, t := range stdlib.Library() {
		globalTypes[name] = t
	}
	for name, t := range builtins {
		globalTypes[name] = t
	}

	database := db.New()
	for path, manifest := range manifests {
		database.Connect(path, manifest)
	}

	types := core.NewEngineWithStdlib(stdlib.EngineConfig())
	opts := []check.Option{
		hooks.WithAssign(),
		hooks.WithReturn(),
		hooks.WithCall(),
		hooks.WithField(),
	}
	if symbols != nil || callGraph != nil {
		lspIndexer := hooks.NewLSPIndexer(symbols, callGraph)
		opts = append(opts, hooks.WithLSPIndex(lspIndexer))
	}

	checker := check.NewChecker(database, check.Deps{
		Types:       types,
		Stdlib:      base,
		GlobalTypes: globalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, opts...)

	return &lspChecker{
		db:          database,
		checker:     checker,
		builtinHash: builtinHash,
	}
}

func (c *lspChecker) CheckParsed(chunk []ast.Stmt, entryID string, imports map[string]*io.Manifest) (*io.Manifest, []diag.Diagnostic) {
	if c == nil || c.checker == nil {
		return nil, nil
	}

	for alias, manifest := range imports {
		if manifest != nil {
			c.db.Connect(alias, manifest)
		}
	}

	sess := c.checker.CheckChunk(chunk, entryID)

	manifest := io.NewManifest(entryID)
	if exportType := sess.ExportType(); exportType != nil {
		manifest.SetExport(exportType)
	}
	if exportTypes := sess.ExportTypes(); exportTypes != nil {
		for name, t := range exportTypes {
			manifest.DefineType(name, t)
		}
	}

	diagnostics := sess.Diagnostics
	sess.Release()

	return manifest, diagnostics
}

func (c *lspChecker) ClearCache() {
	if c == nil || c.checker == nil {
		return
	}
	c.checker.ClearCache()
}
