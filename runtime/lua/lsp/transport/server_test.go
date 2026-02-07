package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		wantErr error
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
			wantErr: errMissingContentLength,
		},
		{
			name:    "invalid content length",
			input:   "Content-Length: abc\r\n\r\n{}",
			wantErr: errInvalidContentLength,
		},
		{
			name:    "too large content length",
			input:   "Content-Length: 99999999\r\n\r\n{}",
			wantErr: errContentTooLarge,
		},
		{
			name:    "truncated body",
			input:   "Content-Length: 100\r\n\r\n{}",
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name:  "invalid json",
			input: "Content-Length: 10\r\n\r\n{invalid}!",
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
			req, err := readMessage(reader, defaultMaxContentLength)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			if tt.method == "" {
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

func TestResponseError_Codes(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"ParseError", ParseError},
		{"InvalidRequest", InvalidRequest},
		{"MethodNotFound", MethodNotFound},
		{"InvalidParams", InvalidParams},
		{"InternalError", InternalError},
		{"ServerNotInitialized", ServerNotInitialized},
		{"RequestCancelled", RequestCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Less(t, tt.code, 0, "%s should be negative", tt.name)
		})
	}
}

func TestServer_StartStopTCP(t *testing.T) {
	log := zap.NewNop()
	server := NewServer("127.0.0.1:0", log, nil, defaultMaxContentLength)

	ctx := context.Background()

	err := server.Start(ctx)
	require.NoError(t, err)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_StartTwice(t *testing.T) {
	log := zap.NewNop()
	server := NewServer("127.0.0.1:0", log, nil, defaultMaxContentLength)

	ctx := context.Background()

	err := server.Start(ctx)
	require.NoError(t, err)

	err = server.Start(ctx)
	require.NoError(t, err)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_StopWithoutStart(t *testing.T) {
	log := zap.NewNop()
	server := NewServer("127.0.0.1:0", log, nil, defaultMaxContentLength)

	err := server.Stop()
	require.NoError(t, err)
}

func TestServer_TCPConnection(t *testing.T) {
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr := listener.Addr().String()
	listener.Close()

	log := zap.NewNop()
	server := NewServer(addr, log, nil, defaultMaxContentLength)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = server.Start(ctx)
	require.NoError(t, err)

	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(req), req)

	_, err = conn.Write([]byte(msg))
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_MultipleConnections(t *testing.T) {
	log := zap.NewNop()
	server := NewServer("127.0.0.1:0", log, nil, defaultMaxContentLength)

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
			conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", addr)
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
	log := zap.NewNop()
	server := NewServer("127.0.0.1:0", log, nil, defaultMaxContentLength)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	addr := server.listener.Addr().String()

	conns := make([]net.Conn, 5)
	for i := 0; i < 5; i++ {
		d := net.Dialer{}
		conn, err := d.DialContext(context.Background(), "tcp", addr)
		require.NoError(t, err)
		conns[i] = conn
	}

	time.Sleep(50 * time.Millisecond)

	server.mu.Lock()
	connCount := len(server.conns)
	server.mu.Unlock()
	assert.Equal(t, 5, connCount)

	err = server.Stop()
	require.NoError(t, err)

	server.mu.Lock()
	connCount = len(server.conns)
	server.mu.Unlock()
	assert.Equal(t, 0, connCount)

	for _, conn := range conns {
		conn.Close()
	}
}

func TestServer_ContextCancellation(t *testing.T) {
	log := zap.NewNop()
	server := NewServer("127.0.0.1:0", log, nil, defaultMaxContentLength)

	ctx, cancel := context.WithCancel(context.Background())

	err := server.Start(ctx)
	require.NoError(t, err)

	addr := server.listener.Addr().String()

	d := net.Dialer{}
	conn, err := d.DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	cancel()

	time.Sleep(200 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)
}
