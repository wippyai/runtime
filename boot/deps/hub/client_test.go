// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_EmptyBaseURL(t *testing.T) {
	_, err := NewClient(Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base URL")
}

func TestNewClient_InvalidURL(t *testing.T) {
	_, err := NewClient(Options{BaseURL: "://invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestNewClient_HTTPNotAllowed(t *testing.T) {
	_, err := NewClient(Options{BaseURL: "http://example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTPS")
}

func TestNewClient_HTTPLocalhost(t *testing.T) {
	c, err := NewClient(Options{BaseURL: "http://localhost:8080"})
	require.NoError(t, err)
	assert.NotNil(t, c)
	assert.NotNil(t, c.Publish)
	assert.NotNil(t, c.Module)
	assert.NotNil(t, c.Download)
	assert.NotNil(t, c.Manifest)
}

func TestNewClient_HTTP127(t *testing.T) {
	c, err := NewClient(Options{BaseURL: "http://127.0.0.1:8080"})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_HTTPS(t *testing.T) {
	c, err := NewClient(Options{BaseURL: "https://hub.wippy.ai"})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_WithToken(t *testing.T) {
	c, err := NewClient(Options{BaseURL: "https://hub.wippy.ai", Token: "tok123"})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_TrailingSlash(t *testing.T) {
	c, err := NewClient(Options{BaseURL: "https://hub.wippy.ai/"})
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewClient_ReusesBoundedTransport(t *testing.T) {
	first, err := NewClient(Options{BaseURL: "https://hub.wippy.ai", Token: "first"})
	require.NoError(t, err)
	second, err := NewClient(Options{BaseURL: "https://hub.wippy.ai", Token: "second"})
	require.NoError(t, err)

	assert.Same(t, first.httpClient.Transport, second.httpClient.Transport)
	transport, ok := first.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, defaultMaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, defaultMaxIdlePerHost, transport.MaxIdleConnsPerHost)
	assert.Equal(t, defaultIdleConnTimeout, transport.IdleConnTimeout)
}

func TestNewClient_ReusesConnectionAcrossClients(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	defer sharedTransport.CloseIdleConnections()

	first, err := NewClient(Options{BaseURL: server.URL, Token: "first"})
	require.NoError(t, err)
	second, err := NewClient(Options{BaseURL: server.URL, Token: "second"})
	require.NoError(t, err)

	for _, client := range []*Client{first, second} {
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, requestErr)
		resp, requestErr := client.httpClient.Do(request)
		require.NoError(t, requestErr)
		_, requestErr = io.Copy(io.Discard, resp.Body)
		require.NoError(t, requestErr)
		require.NoError(t, resp.Body.Close())
	}

	assert.Equal(t, int32(1), connections.Load())
}
