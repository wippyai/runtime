package lsp

import (
	"context"
	"sync"

	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/completion"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/lsp/signature"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/lsp/indexing"
	"github.com/wippyai/runtime/runtime/lua/lsp/transport"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Service provides LSP functionality for the Lua runtime.
//
// It owns the transport server, indexing scheduler, and optional document
// overlay store for unsaved changes. When disabled, it stays fully inert.
type Service struct {
	completion        *completion.Provider
	log               *zap.Logger
	provider          indexing.Provider
	bus               event.Bus
	server            *transport.Server
	httpServer        *transport.HTTPServer
	lspService        *golualsp.Service
	signature         *signature.Provider
	indexer           *indexing.Indexer
	scheduler         *indexing.Scheduler
	documents         *indexing.DocumentStore
	diagnostics       map[string][]diag.Diagnostic
	globalTypes       map[string]typ.Type
	completionChecker *exprChecker
	indexedVersions   map[string]int
	cancel            context.CancelFunc
	sub               *eventbus.Subscriber
	cfg               Config
	mu                sync.RWMutex
	completionMu      sync.Mutex
	running           bool
}

// New creates a new LSP service.
func New(cfg Config, log *zap.Logger, bus event.Bus, cm *code.Manager) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	s := &Service{
		cfg:      cfg,
		log:      log.Named("lsp"),
		provider: indexing.NewManagerProvider(cm),
		bus:      bus,
	}

	return s
}

// Start begins the LSP service, wiring indexing and the transport server.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	if !s.cfg.Enabled {
		s.mu.Unlock()
		s.log.Info("lsp service disabled")
		return nil
	}

	s.running = true
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.initLocked()
	indexer := s.indexer
	provider := s.provider
	s.mu.Unlock()

	scheduler := indexing.NewScheduler(s.log, indexer, provider, 0)
	scheduler.Start(runCtx)

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		scheduler.Stop()
		s.resetState()
		return nil
	}
	s.scheduler = scheduler
	s.mu.Unlock()

	scheduler.EnqueueAllSync()
	if err := scheduler.WaitIdle(runCtx); err != nil {
		s.log.Warn("initial indexing interrupted", zap.Error(err))
	}
	if runCtx.Err() != nil {
		scheduler.Stop()
		s.mu.Lock()
		s.running = false
		s.resetStateLocked()
		s.mu.Unlock()
		return nil
	}

	sub := s.subscribeEvents(runCtx)
	if sub != nil {
		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			sub.Close()
			scheduler.Stop()
			s.resetState()
			return nil
		}
		s.sub = sub
		s.mu.Unlock()
	}

	server := transport.NewServer(s.cfg.Address, s.log, s, s.cfg.MaxMessageBytes)
	if err := server.Start(runCtx); err != nil {
		cancel()
		scheduler.Stop()
		if sub != nil {
			sub.Close()
		}
		s.mu.Lock()
		s.running = false
		s.resetStateLocked()
		s.mu.Unlock()
		return err
	}

	var httpServer *transport.HTTPServer
	if s.cfg.HTTPEnabled {
		httpServer = transport.NewHTTPServer(
			s.cfg.HTTPAddress,
			s.cfg.HTTPPath,
			s.log,
			s,
			s.cfg.MaxMessageBytes,
			s.cfg.HTTPAllowOrigin,
		)
		if err := httpServer.Start(runCtx); err != nil {
			_ = server.Stop()
			if sub != nil {
				sub.Close()
			}
			scheduler.Stop()
			s.mu.Lock()
			s.running = false
			s.resetStateLocked()
			s.mu.Unlock()
			return err
		}
	}

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		if httpServer != nil {
			_ = httpServer.Stop()
		}
		_ = server.Stop()
		if sub != nil {
			sub.Close()
		}
		scheduler.Stop()
		s.resetState()
		return nil
	}
	s.server = server
	s.httpServer = httpServer
	s.mu.Unlock()

	s.log.Info("lsp service started", zap.Bool("enabled", s.cfg.Enabled))

	return nil
}

// Stop terminates the LSP service and releases all LSP resources.
func (s *Service) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	cancel := s.cancel
	s.cancel = nil
	scheduler := s.scheduler
	server := s.server
	httpServer := s.httpServer
	sub := s.sub
	s.scheduler = nil
	s.server = nil
	s.httpServer = nil
	s.sub = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if scheduler != nil {
		scheduler.Stop()
	}

	if sub != nil {
		sub.Close()
	}

	if server != nil {
		if err := server.Stop(); err != nil {
			s.log.Warn("server stop error", zap.Error(err))
		}
	}
	if httpServer != nil {
		if err := httpServer.Stop(); err != nil {
			s.log.Warn("http server stop error", zap.Error(err))
		}
	}

	s.resetState()
	s.log.Info("lsp service stopped")

	return nil
}

// LSPService returns the underlying go-lua LSP service.
func (s *Service) LSPService() *golualsp.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lspService
}

// Completion returns the completion provider.
func (s *Service) Completion() *completion.Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.completion
}

// Signature returns the signature provider.
func (s *Service) Signature() *signature.Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.signature
}

// StoreDiagnostics stores diagnostics for a file.
func (s *Service) StoreDiagnostics(fileID string, diagnostics []diag.Diagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diagnostics == nil {
		s.diagnostics = make(map[string][]diag.Diagnostic)
	}
	if len(diagnostics) == 0 {
		delete(s.diagnostics, fileID)
	} else {
		s.diagnostics[fileID] = diagnostics
	}
}

// GetDiagnostics returns diagnostics for a file.
func (s *Service) GetDiagnostics(id string) []transport.Diagnostic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.diagnostics == nil {
		return nil
	}
	return transport.ConvertDiagnostics(s.diagnostics[id])
}

func (s *Service) DocumentText(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	parsed := registry.ParseID(id)
	if parsed == (registry.ID{}) {
		return "", false
	}
	s.mu.RLock()
	documents := s.documents
	provider := s.provider
	s.mu.RUnlock()
	if documents != nil {
		if doc, ok := documents.Get(parsed); ok {
			return doc.Text, true
		}
	}
	if provider != nil {
		node, err := provider.Node(parsed)
		if err == nil {
			return node.Source, node.Source != ""
		}
	}
	return "", false
}

// EnsureIndexed ensures the latest open document snapshot is indexed for LSP queries.
// It only reindexes when the overlay version (or content hash) has changed.
func (s *Service) EnsureIndexed(ctx context.Context, fileID string) {
	if fileID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id := registry.ParseID(fileID)
	if id == (registry.ID{}) {
		return
	}

	s.mu.RLock()
	documents := s.documents
	indexer := s.indexer
	provider := s.provider
	lspSvc := s.lspService
	s.mu.RUnlock()
	if documents == nil || indexer == nil || provider == nil {
		return
	}

	if doc, ok := documents.Get(id); ok && doc.Text != "" {
		if doc.Version > 0 {
			s.mu.RLock()
			seen := s.indexedVersions
			last := 0
			if seen != nil {
				last = seen[fileID]
			}
			s.mu.RUnlock()
			if last >= doc.Version {
				return
			}
		}

		deps := provider.DependencyManifests(id)
		if err := indexer.IndexSource(ctx, id, doc.Text, deps); err != nil {
			s.log.Debug("lsp ensure index failed", zap.String("id", fileID), zap.Error(err))
		}

		if doc.Version > 0 {
			s.mu.Lock()
			if s.indexedVersions == nil {
				s.indexedVersions = make(map[string]int)
			}
			s.indexedVersions[fileID] = doc.Version
			s.mu.Unlock()
		}
		return
	}

	if lspSvc != nil {
		if syms := lspSvc.Symbols().SymbolsInFile(fileID); len(syms) > 0 {
			return
		}
	}

	if err := indexer.IndexEntry(ctx, id); err != nil {
		s.log.Debug("lsp ensure index failed", zap.String("id", fileID), zap.Error(err))
	}
}

// Indexer returns the indexer.
func (s *Service) Indexer() *indexing.Indexer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexer
}

// subscribeEvents wires invalidation events to the indexing scheduler.
func (s *Service) subscribeEvents(ctx context.Context) *eventbus.Subscriber {
	if s.bus == nil {
		return nil
	}

	sub, err := eventbus.NewSubscriber(ctx, s.bus, luaapi.System, luaapi.InvalidateNodes, s.handleEvent)
	if err != nil {
		s.log.Warn("lsp event subscription failed", zap.Error(err))
		return nil
	}
	return sub
}

func (s *Service) handleEvent(evt event.Event) {
	if evt.Kind != luaapi.InvalidateNodes {
		return
	}
	ids, ok := evt.Data.([]registry.ID)
	if !ok {
		return
	}
	s.mu.RLock()
	scheduler := s.scheduler
	running := s.running
	s.mu.RUnlock()
	if !running || scheduler == nil {
		return
	}
	scheduler.Enqueue(ids)
}

// initLocked lazily allocates LSP indexes and providers.
// Caller must hold s.mu.
func (s *Service) initLocked() {
	if s.lspService != nil {
		return
	}

	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	s.lspService = lspSvc
	s.completion = completion.NewProvider(symbols)
	s.signature = signature.NewProvider(symbols, callGraph)
	s.completion.SetTypeFormatter(golualsp.FormatType)
	s.globalTypes = buildGlobalTypes(s.provider)

	s.documents = indexing.NewDocumentStore()
	s.diagnostics = make(map[string][]diag.Diagnostic)
	s.indexer = indexing.NewIndexer(s.log, s.provider, lspSvc, symbols, callGraph, s.documents, s)
}

// resetState releases all LSP resources. Caller must stop background workers first.
func (s *Service) resetState() {
	s.mu.Lock()
	s.resetStateLocked()
	s.mu.Unlock()
}

func (s *Service) resetStateLocked() {
	if s.documents != nil {
		s.documents.Reset()
	}
	s.documents = nil
	s.indexer = nil
	s.scheduler = nil
	s.lspService = nil
	s.completion = nil
	s.signature = nil
	s.diagnostics = nil
	s.globalTypes = nil
	s.completionChecker = nil
	s.indexedVersions = nil
	s.server = nil
	s.httpServer = nil
	s.sub = nil
	s.cancel = nil
}

// ApplyDocumentOpen registers an opened document snapshot.
func (s *Service) ApplyDocumentOpen(id string, text string, version int) {
	s.applyDocumentSet(registry.ParseID(id), text, version, true)
}

// ApplyDocumentChange registers a full document update.
func (s *Service) ApplyDocumentChange(id string, text string, version int) {
	s.applyDocumentSet(registry.ParseID(id), text, version, false)
}

// ApplyDocumentClose unregisters a document snapshot.
func (s *Service) ApplyDocumentClose(id string) {
	s.applyDocumentDelete(registry.ParseID(id))
}

// applyDocumentSet updates the in-memory overlay and queues a reindex.
func (s *Service) applyDocumentSet(id registry.ID, text string, version int, reset bool) {
	if id == (registry.ID{}) {
		return
	}
	s.mu.RLock()
	documents := s.documents
	scheduler := s.scheduler
	running := s.running
	s.mu.RUnlock()
	if !running || documents == nil {
		return
	}
	if reset {
		documents.Delete(id)
	}
	documents.Set(id, text, version)
	if scheduler != nil {
		scheduler.Enqueue([]registry.ID{id})
	}
}

// applyDocumentDelete removes the in-memory overlay and queues a reindex.
func (s *Service) applyDocumentDelete(id registry.ID) {
	if id == (registry.ID{}) {
		return
	}
	s.mu.RLock()
	documents := s.documents
	scheduler := s.scheduler
	running := s.running
	s.mu.RUnlock()
	if !running || documents == nil {
		return
	}
	documents.Delete(id)
	s.mu.Lock()
	if s.indexedVersions != nil {
		delete(s.indexedVersions, id.String())
	}
	s.mu.Unlock()
	if scheduler != nil {
		scheduler.Enqueue([]registry.ID{id})
	}
}

func (s *Service) resolveIdentifierType(fileID string, name string) typ.Type {
	if name == "" {
		return nil
	}
	s.mu.RLock()
	lspSvc := s.lspService
	globals := s.globalTypes
	s.mu.RUnlock()
	if lspSvc != nil {
		if sym := lspSvc.Symbols().LookupByName(fileID, name); sym != nil {
			if t, ok := sym.Type.(typ.Type); ok {
				return t
			}
		}
	}
	if globals != nil {
		if t, ok := globals[name]; ok {
			return t
		}
	}
	return nil
}

func isIdentStart(b byte) bool {
	if b == '_' {
		return true
	}
	if b >= 'a' && b <= 'z' {
		return true
	}
	return b >= 'A' && b <= 'Z'
}

func isIdentChar(b byte) bool {
	if isIdentStart(b) {
		return true
	}
	return b >= '0' && b <= '9'
}

func buildGlobalTypes(provider indexing.Provider) map[string]typ.Type {
	if provider == nil {
		return nil
	}
	mods := provider.ModuleDefs()
	if len(mods) == 0 {
		return nil
	}
	globals := make(map[string]typ.Type)
	for _, mod := range mods {
		if mod == nil || mod.Types == nil || mod.Name == "" {
			continue
		}
		manifest := mod.Types()
		if manifest == nil {
			continue
		}
		if manifest.Export != nil {
			globals[mod.Name] = manifest.Export
		}
		for name, t := range manifest.AllGlobals() {
			globals[name] = t
		}
	}
	if len(globals) == 0 {
		return nil
	}
	return globals
}
