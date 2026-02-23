// SPDX-License-Identifier: MPL-2.0

package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"
)

const (
	contentLengthHeader       = "Content-Length:"
	contentLengthHeaderPrefix = "Content-Length: "
)

var (
	errInvalidContentLength = errors.New("invalid Content-Length")
	errMissingContentLength = errors.New("missing or invalid Content-Length header")
	errContentTooLarge      = errors.New("content length exceeds limit")
)

const defaultMaxContentLength = 8 << 20

// Server handles JSON-RPC communication for LSP over TCP.
type Server struct {
	listener net.Listener
	log      *zap.Logger
	handler  *Handler
	conns    map[net.Conn]struct{}
	cancel   context.CancelFunc
	addr     string
	maxBytes int
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// NewServer creates a new LSP server.
func NewServer(addr string, log *zap.Logger, svc Service, maxMessageBytes int) *Server {
	if log == nil {
		log = zap.NewNop()
	}
	if maxMessageBytes <= 0 {
		maxMessageBytes = defaultMaxContentLength
	}
	return &Server{
		addr:     addr,
		log:      log.Named("server"),
		handler:  NewHandler(svc),
		conns:    make(map[net.Conn]struct{}),
		maxBytes: maxMessageBytes,
	}
}

// Start begins the LSP server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	var lc net.ListenConfig
	listener, err := lc.Listen(runCtx, "tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = listener
	s.running = true

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(runCtx)
	}()

	s.log.Info("lsp server started", zap.String("addr", s.listener.Addr().String()))
	return nil
}

// Stop terminates the LSP server.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false

	if s.cancel != nil {
		s.cancel()
	}

	if s.listener != nil {
		_ = s.listener.Close()
	}

	for conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = make(map[net.Conn]struct{})
	s.mu.Unlock()

	s.wg.Wait()
	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
				s.log.Error("accept failed", zap.Error(err))
				continue
			}
		}

		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()

		go func(c net.Conn) {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
				_ = c.Close()
			}()
			s.serveConn(ctx, c)
		}(conn)
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	reader := bufio.NewReader(conn)
	// Close on shutdown to unblock reads; avoid read deadlines to prevent framing desync.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := readMessage(reader, s.maxBytes)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			s.log.Debug("read error", zap.Error(err))
			return
		}

		resp := s.handler.Handle(ctx, msg)
		if resp != nil {
			if err := writeMessage(conn, resp); err != nil {
				s.log.Debug("write error", zap.Error(err))
			}
		}
	}
}

// JSON-RPC message types

// Request represents a JSON-RPC request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC response.
type Response struct {
	ID      any            `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
	JSONRPC string         `json:"jsonrpc"`
}

// ResponseError represents a JSON-RPC error.
type ResponseError struct {
	Data    any    `json:"data,omitempty"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// LSP error codes
const (
	ParseError           = -32700
	InvalidRequest       = -32600
	MethodNotFound       = -32601
	InvalidParams        = -32602
	InternalError        = -32603
	ServerNotInitialized = -32002
	RequestCancelled     = -32800
)

func readMessage(r *bufio.Reader, maxMessageBytes int) (*Request, error) {
	var contentLength int
	if maxMessageBytes <= 0 {
		maxMessageBytes = defaultMaxContentLength
	}

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)

		if line == "" {
			break
		}

		if strings.HasPrefix(line, contentLengthHeader) {
			val := strings.TrimSpace(strings.TrimPrefix(line, contentLengthHeader))
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return nil, errInvalidContentLength
			}
			if contentLength < 0 || contentLength > maxMessageBytes {
				return nil, errContentTooLarge
			}
		}
	}

	if contentLength <= 0 {
		return nil, errMissingContentLength
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}

	return &req, nil
}

func writeMessage(w io.Writer, resp *Response) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	header := strconv.AppendInt([]byte(contentLengthHeaderPrefix), int64(len(body)), 10)
	header = append(header, '\r', '\n', '\r', '\n')

	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
