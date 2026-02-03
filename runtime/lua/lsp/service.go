package lsp

import (
	"context"
	"sync"

	golualsp "github.com/wippyai/go-lua/lsp"
	"github.com/wippyai/go-lua/lsp/completion"
	"github.com/wippyai/go-lua/lsp/index"
	"github.com/wippyai/go-lua/lsp/signature"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"go.uber.org/zap"
)

const eventBufferSize = 100

// Service provides LSP functionality for the Lua runtime.
type Service struct {
	bus        event.Bus
	completion *completion.Provider
	log        *zap.Logger
	cm         *code.Manager
	server     *Server
	lspService *golualsp.Service
	signature  *signature.Provider
	indexer    *Indexer
	cancel     context.CancelFunc
	cfg        Config
	wg         sync.WaitGroup
	mu         sync.Mutex
	running    bool
}

// New creates a new LSP service.
func New(cfg Config, log *zap.Logger, bus event.Bus, cm *code.Manager) *Service {
	cache := index.NewDB()
	symbols := index.NewSymbolIndex()
	callGraph := index.NewCallGraph()
	lspSvc := golualsp.NewService(cache, symbols, callGraph)

	s := &Service{
		cfg:        cfg,
		log:        log.Named("lsp"),
		bus:        bus,
		cm:         cm,
		lspService: lspSvc,
		completion: completion.NewProvider(symbols),
		signature:  signature.NewProvider(symbols, callGraph),
	}

	s.indexer = NewIndexer(s.log, cm, lspSvc, symbols, callGraph)
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

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	// Subscribe to code invalidation events
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.subscribeEvents(runCtx)
	}()

	// Index all existing entries
	if err := s.indexer.IndexAll(runCtx); err != nil {
		s.log.Warn("initial indexing failed", zap.Error(err))
	}

	// Start JSON-RPC server if enabled
	if s.cfg.Enabled {
		s.server = NewServer(s.cfg, s.log, s)
		if err := s.server.Start(runCtx); err != nil {
			cancel()
			s.wg.Wait()
			return err
		}
	}

	s.running = true
	s.log.Info("lsp service started", zap.Bool("enabled", s.cfg.Enabled))

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

	events := make(chan event.Event, eventBufferSize)
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

	// Check if still running before spawning goroutine
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()

	go func(ids []registry.ID) {
		defer s.wg.Done()
		for _, id := range ids {
			select {
			case <-ctx.Done():
				return
			default:
			}

			fileID := id.String()

			// Check if node still exists (not deleted)
			if s.cm != nil {
				if _, err := s.cm.GetNode(id); err != nil {
					// Node deleted - just invalidate, don't re-index
					s.lspService.InvalidateFile(fileID)
					s.log.Debug("invalidated deleted entry", zap.String("id", fileID))
					continue
				}
			}

			// Re-index entry (indexer handles invalidation)
			if err := s.indexer.IndexEntry(ctx, id); err != nil {
				s.log.Debug("re-index failed", zap.String("id", fileID), zap.Error(err))
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
