package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReadMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		method  string
	}{
		{
			name:   "valid request",
			input:  "Content-Length: 46\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}",
			method: "initialize",
		},
		{
			name:    "missing content length",
			input:   "\r\n{\"jsonrpc\":\"2.0\"}",
			wantErr: true,
		},
		{
			name:    "invalid content length",
			input:   "Content-Length: abc\r\n\r\n{}",
			wantErr: true,
		},
		{
			name:    "truncated body",
			input:   "Content-Length: 100\r\n\r\n{}",
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   "Content-Length: 10\r\n\r\n{invalid}!",
			wantErr: true,
		},
		{
			name:   "with content type header",
			input:  "Content-Length: 46\r\nContent-Type: application/json\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}",
			method: "initialize",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			req, err := readMessage(reader)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.method, req.Method)
		})
	}
}

func TestWriteMessage(t *testing.T) {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  "test",
	}

	var buf bytes.Buffer
	err := writeMessage(&buf, resp)
	require.NoError(t, err)

	output := buf.String()

	require.True(t, strings.HasPrefix(output, "Content-Length:"))

	parts := strings.SplitN(output, "\r\n\r\n", 2)
	require.Len(t, parts, 2)

	var parsed Response
	err = json.Unmarshal([]byte(parts[1]), &parsed)
	require.NoError(t, err)

	assert.Equal(t, "2.0", parsed.JSONRPC)
}

func TestStdioPipe(t *testing.T) {
	input := "test input"
	r := strings.NewReader(input)
	var w bytes.Buffer

	pipe := stdioPipe{r, &w}

	buf := make([]byte, len(input))
	n, err := pipe.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read error: %v", err)
	}
	assert.Equal(t, input, string(buf[:n]))

	output := "test output"
	n, err = pipe.Write([]byte(output))
	require.NoError(t, err)
	assert.Equal(t, len(output), n)
	assert.Equal(t, output, w.String())
}

func TestResponseError_Codes(t *testing.T) {
	tests := []struct {
		code int
		name string
	}{
		{ParseError, "ParseError"},
		{InvalidRequest, "InvalidRequest"},
		{MethodNotFound, "MethodNotFound"},
		{InvalidParams, "InvalidParams"},
		{InternalError, "InternalError"},
		{ServerNotInitialized, "ServerNotInitialized"},
		{RequestCancelled, "RequestCancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Less(t, tt.code, 0, "%s should be negative", tt.name)
		})
	}
}

func TestServer_StartStopTCP(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Mode:    "tcp",
		Address: "127.0.0.1:0",
	}
	log := zap.NewNop()
	svc := &Service{}

	server := NewServer(cfg, log, svc)

	ctx := context.Background()

	err := server.Start(ctx)
	require.NoError(t, err)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_StartTwice(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Mode:    "tcp",
		Address: "127.0.0.1:0",
	}
	log := zap.NewNop()
	svc := &Service{}

	server := NewServer(cfg, log, svc)

	ctx := context.Background()

	err := server.Start(ctx)
	require.NoError(t, err)

	err = server.Start(ctx)
	require.NoError(t, err)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_StopWithoutStart(t *testing.T) {
	cfg := DefaultConfig()
	log := zap.NewNop()
	svc := &Service{}

	server := NewServer(cfg, log, svc)

	err := server.Stop()
	require.NoError(t, err)
}

func TestServer_TCPConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	cfg := Config{
		Enabled: true,
		Mode:    "tcp",
		Address: listener.Addr().String(),
	}
	listener.Close()

	log := zap.NewNop()
	svc := &Service{}

	server := NewServer(cfg, log, svc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = server.Start(ctx)
	require.NoError(t, err)

	conn, err := net.Dial("tcp", cfg.Address)
	require.NoError(t, err)
	defer conn.Close()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	msg := "Content-Length: " + string(rune(len(req))) + "\r\n\r\n" + req

	_, err = conn.Write([]byte(msg))
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_MultipleConnections(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Mode:    "tcp",
		Address: "127.0.0.1:0",
	}
	log := zap.NewNop()
	svc := &Service{}

	server := NewServer(cfg, log, svc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	addr := server.listener.Addr().String()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			defer conn.Close()
			time.Sleep(50 * time.Millisecond)
		}()
	}

	time.Sleep(100 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)

	wg.Wait()
}

func TestServer_ConnectionCleanupOnStop(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Mode:    "tcp",
		Address: "127.0.0.1:0",
	}
	log := zap.NewNop()
	svc := &Service{}

	server := NewServer(cfg, log, svc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	addr := server.listener.Addr().String()

	conns := make([]net.Conn, 5)
	for i := 0; i < 5; i++ {
		conn, err := net.Dial("tcp", addr)
		require.NoError(t, err)
		conns[i] = conn
	}

	time.Sleep(50 * time.Millisecond)

	server.mu.RLock()
	connCount := len(server.conns)
	server.mu.RUnlock()
	assert.Equal(t, 5, connCount)

	err = server.Stop()
	require.NoError(t, err)

	server.mu.RLock()
	connCount = len(server.conns)
	server.mu.RUnlock()
	assert.Equal(t, 0, connCount)

	for _, conn := range conns {
		conn.Close()
	}
}

func TestServer_ContextCancellation(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Mode:    "tcp",
		Address: "127.0.0.1:0",
	}
	log := zap.NewNop()
	svc := &Service{}

	server := NewServer(cfg, log, svc)

	ctx, cancel := context.WithCancel(context.Background())

	err := server.Start(ctx)
	require.NoError(t, err)

	addr := server.listener.Addr().String()

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	cancel()

	time.Sleep(200 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)
}
