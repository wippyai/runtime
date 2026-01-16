package lsp

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	golualsp "github.com/yuin/gopher-lua/lsp"
	"github.com/yuin/gopher-lua/lsp/completion"
	"github.com/yuin/gopher-lua/lsp/index"
	"github.com/yuin/gopher-lua/lsp/signature"
	"go.uber.org/zap"
)

// Service provides LSP functionality for the Lua runtime.
type Service struct {
	cfg    Config
	log    *zap.Logger
	bus    event.Bus
	cm     *code.Manager
	server *Server

	// LSP query service from go-lua
	lspService *golualsp.Service

	// Providers
	completion *completion.Provider
	signature  *signature.Provider

	// Indexer for building manifests
	indexer *Indexer

	mu      sync.RWMutex
	wg      sync.WaitGroup
	running bool
	cancel  context.CancelFunc
}

// New creates a new LSP service.
func New(cfg Config, log *zap.Logger, bus event.Bus, cm *code.Manager) *Service {
	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols)

	s := &Service{
		cfg:        cfg,
		log:        log.Named("lsp"),
		bus:        bus,
		cm:         cm,
		lspService: lspSvc,
		completion: completion.NewProvider(symbols),
		signature:  signature.NewProvider(symbols, callGraph),
	}

	s.indexer = NewIndexer(s.log, cm, lspSvc)
	s.completion.SetTypeFormatter(golualsp.FormatType)

	return s
}

// Start begins the LSP service.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Subscribe to code invalidation events
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.subscribeEvents(ctx)
	}()

	// Index all existing entries
	if err := s.indexer.IndexAll(ctx); err != nil {
		s.log.Warn("initial indexing failed", zap.Error(err))
	}

	// Start JSON-RPC server if enabled
	if s.cfg.Enabled {
		s.server = NewServer(s.cfg, s.log, s)
		if err := s.server.Start(ctx); err != nil {
			cancel()
			s.wg.Wait()
			return err
		}
	}

	s.running = true
	s.log.Info("lsp service started",
		zap.Bool("enabled", s.cfg.Enabled),
		zap.String("mode", s.cfg.Mode))

	return nil
}

// Stop terminates the LSP service.
func (s *Service) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	if s.server != nil {
		if err := s.server.Stop(); err != nil {
			s.log.Warn("server stop error", zap.Error(err))
		}
	}

	s.wg.Wait()
	s.log.Info("lsp service stopped")

	return nil
}

// subscribeEvents listens for code invalidation events.
func (s *Service) subscribeEvents(ctx context.Context) {
	if s.bus == nil {
		return
	}

	events := make(chan event.Event, 100)
	subID, err := s.bus.Subscribe(ctx, luaapi.System, events)
	if err != nil {
		s.log.Error("failed to subscribe to events", zap.Error(err))
		return
	}
	defer s.bus.Unsubscribe(context.Background(), subID)

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			s.handleEvent(ctx, ev)
		}
	}
}

// handleEvent processes incoming events.
func (s *Service) handleEvent(ctx context.Context, ev event.Event) {
	if ev.Kind != luaapi.InvalidateNodes {
		return
	}

	ids, ok := ev.Data.([]registry.ID)
	if !ok {
		return
	}

	if s.lspService == nil {
		return
	}

	for _, id := range ids {
		s.log.Debug("invalidating entry", zap.String("id", id.String()))
		s.lspService.InvalidateFile(id.String())
	}

	// Re-index affected entries - check if still running before spawning goroutine
	s.mu.RLock()
	if !s.running {
		s.mu.RUnlock()
		return
	}
	s.wg.Add(1)
	s.mu.RUnlock()

	go func(ids []registry.ID) {
		defer s.wg.Done()
		for _, id := range ids {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := s.indexer.IndexEntry(ctx, id); err != nil {
				s.log.Debug("re-index failed", zap.String("id", id.String()), zap.Error(err))
			}
		}
	}(ids)
}

// LSPService returns the underlying go-lua LSP service.
func (s *Service) LSPService() *golualsp.Service {
	return s.lspService
}

// Completion returns the completion provider.
func (s *Service) Completion() *completion.Provider {
	return s.completion
}

// Signature returns the signature provider.
func (s *Service) Signature() *signature.Provider {
	return s.signature
}

// Indexer returns the indexer.
func (s *Service) Indexer() *Indexer {
	return s.indexer
}
