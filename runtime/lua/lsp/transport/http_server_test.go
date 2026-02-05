package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPServer_RequestResponse(t *testing.T) {
	srv := NewHTTPServer("127.0.0.1:0", "/lsp", zap.NewNop(), nil, 1024*1024, "*")
	require.NoError(t, srv.Start(context.Background()))
	defer srv.Stop()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	resp, err := http.Post("http://"+srv.Addr()+"/lsp", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var payload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, "2.0", payload["jsonrpc"])
	require.Equal(t, float64(1), payload["id"])
	require.NotNil(t, payload["result"])
}

func TestHTTPServer_Notification(t *testing.T) {
	srv := NewHTTPServer("127.0.0.1:0", "/lsp", zap.NewNop(), nil, 1024*1024, "*")
	require.NoError(t, srv.Start(context.Background()))
	defer srv.Stop()

	body := []byte(`{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	resp, err := http.Post("http://"+srv.Addr()+"/lsp", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestHTTPServer_CORS(t *testing.T) {
	srv := NewHTTPServer("127.0.0.1:0", "/lsp", zap.NewNop(), nil, 1024*1024, "*")
	require.NoError(t, srv.Start(context.Background()))
	defer srv.Stop()

	req, err := http.NewRequest(http.MethodOptions, "http://"+srv.Addr()+"/lsp", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
}
