package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const defaultHTTPPath = "/lsp"

// HTTPServer exposes the LSP JSON-RPC handler over HTTP for browser clients.
// It accepts POST requests with JSON-RPC payloads and returns JSON-RPC responses.
type HTTPServer struct {
	log         *zap.Logger
	handler     *Handler
	addr        string
	path        string
	maxBytes    int
	allowOrigin string

	server   *http.Server
	listener net.Listener
	cancel   context.CancelFunc
	mu       sync.Mutex
	running  bool
}

// NewHTTPServer creates a new HTTP LSP server.
func NewHTTPServer(addr, path string, log *zap.Logger, svc Service, maxMessageBytes int, allowOrigin string) *HTTPServer {
	if log == nil {
		log = zap.NewNop()
	}
	if path == "" {
		path = defaultHTTPPath
	}
	if maxMessageBytes <= 0 {
		maxMessageBytes = defaultMaxContentLength
	}
	return &HTTPServer{
		log:         log.Named("http"),
		handler:     NewHandler(svc),
		addr:        addr,
		path:        path,
		maxBytes:    maxMessageBytes,
		allowOrigin: allowOrigin,
	}
}

// Start begins the HTTP server.
func (s *HTTPServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	if s.addr == "" {
		return errors.New("http address required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	var lc net.ListenConfig
	listener, err := lc.Listen(runCtx, "tcp", s.addr)
	if err != nil {
		cancel()
		return err
	}
	s.listener = listener

	mux := http.NewServeMux()
	mux.HandleFunc(s.path, s.serveHTTP)
	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.running = true

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Warn("lsp http server stopped", zap.Error(err))
		}
	}()

	s.log.Info("lsp http server started", zap.String("addr", s.listener.Addr().String()), zap.String("path", s.path))
	return nil
}

// Stop terminates the HTTP server.
func (s *HTTPServer) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	cancel := s.cancel
	s.cancel = nil
	server := s.server
	s.server = nil
	listener := s.listener
	s.listener = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if listener != nil {
		_ = listener.Close()
	}
	if server != nil {
		ctx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = server.Shutdown(ctx)
	}
	return nil
}

// Addr returns the bound address if running.
func (s *HTTPServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}

func (s *HTTPServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if s.allowOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", s.allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if s.handler == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(s.maxBytes))
	defer func() {
		_ = r.Body.Close()
	}()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, nil, errInvalidContentLength)
		return
	}

	reqs, isBatch, err := decodeRequests(body)
	if err != nil {
		s.writeError(w, nil, err)
		return
	}

	responses := make([]*Response, 0, len(reqs))
	for i := range reqs {
		resp := s.handler.Handle(r.Context(), &reqs[i])
		if resp != nil {
			responses = append(responses, resp)
		}
	}

	if len(responses) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var payload any = responses[0]
	if isBatch {
		payload = responses
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *HTTPServer) writeError(w http.ResponseWriter, id any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := &Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &ResponseError{Code: ParseError, Message: err.Error()},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func decodeRequests(body []byte) ([]Request, bool, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, false, errors.New("empty body")
	}
	if strings.HasPrefix(trimmed, "[") {
		var batch []Request
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, true, err
		}
		return batch, true, nil
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, false, err
	}
	return []Request{req}, false, nil
}
