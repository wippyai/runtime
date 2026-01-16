package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var timeNow = time.Now

// Server handles JSON-RPC communication for LSP.
type Server struct {
	cfg     Config
	log     *zap.Logger
	handler *Handler

	mu       sync.RWMutex
	wg       sync.WaitGroup
	running  bool
	listener net.Listener
	conns    map[net.Conn]struct{}
	cancel   context.CancelFunc
}

// NewServer creates a new LSP server.
func NewServer(cfg Config, log *zap.Logger, svc *Service) *Server {
	return &Server{
		cfg:     cfg,
		log:     log.Named("server"),
		handler: NewHandler(svc, log),
		conns:   make(map[net.Conn]struct{}),
	}
}

// Start begins the LSP server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	switch s.cfg.Mode {
	case "tcp":
		return s.startTCP(ctx)
	default:
		return s.startStdio(ctx)
	}
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

func (s *Server) startStdio(ctx context.Context) error {
	s.running = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.serveConn(ctx, stdioPipe{os.Stdin, os.Stdout}, nil)
	}()
	s.log.Info("lsp server started", zap.String("mode", "stdio"))
	return nil
}

func (s *Server) startTCP(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return err
	}
	s.listener = listener
	s.running = true

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(ctx)
	}()
	s.log.Info("lsp server started", zap.String("mode", "tcp"), zap.String("addr", s.cfg.Address))
	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
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
			s.serveConn(ctx, c, c)
		}(conn)
	}
}

func (s *Server) serveConn(ctx context.Context, rw io.ReadWriter, conn net.Conn) {
	reader := bufio.NewReader(rw)
	var writeMu sync.Mutex

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if conn != nil {
			_ = conn.SetReadDeadline(timeNow().Add(100 * time.Millisecond))
		}

		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			s.log.Debug("read error", zap.Error(err))
			continue
		}

		resp := s.handler.Handle(ctx, msg)
		if resp != nil {
			writeMu.Lock()
			err := writeMessage(rw, resp)
			writeMu.Unlock()
			if err != nil {
				s.log.Debug("write error", zap.Error(err))
			}
		}
	}
}

// stdioPipe wraps stdin/stdout as a single ReadWriter.
type stdioPipe struct {
	r io.Reader
	w io.Writer
}

func (p stdioPipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p stdioPipe) Write(b []byte) (int, error) { return p.w.Write(b) }

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
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
}

// ResponseError represents a JSON-RPC error.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
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

func readMessage(r *bufio.Reader) (*Request, error) {
	var contentLength int

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)

		if line == "" {
			break
		}

		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, err = strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %s", val)
			}
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
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

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := w.Write([]byte(header)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
