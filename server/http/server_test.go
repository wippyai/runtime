package http

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	config "github.com/ponyruntime/pony/api/server/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	cfg := config.ServerConfig{
		Addr: ":8080",
		HTTP: config.HTTPConfig{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  10 * time.Second,
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	server := NewServer(cfg, handler)

	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
	assert.NotNil(t, server.router)
}

func TestServer_Router(t *testing.T) {
	cfg := config.ServerConfig{Addr: ":8080"}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	server := NewServer(cfg, handler)

	router := server.Router()
	assert.NotNil(t, router)
	// Router should be the same instance each time
	assert.Equal(t, router, server.Router())
}

func TestServer_UpdateConfig(t *testing.T) {
	initialCfg := config.ServerConfig{
		Addr: ":8080",
		HTTP: config.HTTPConfig{
			ReadTimeout: 5 * time.Second,
		},
	}

	server := NewServer(initialCfg, nil)

	newCfg := config.ServerConfig{
		Addr: ":9090",
		HTTP: config.HTTPConfig{
			ReadTimeout: 10 * time.Second,
		},
	}

	server.UpdateConfig(newCfg)
	assert.Equal(t, newCfg, server.config)
}

func TestServer_StopWithoutServe(t *testing.T) {
	server := NewServer(config.ServerConfig{}, nil)
	err := server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestSimpleHTTP(t *testing.T) {
	// Create server with a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})

	server := NewServer(config.ServerConfig{
		Addr: "localhost:8123", // using fixed port for simplicity
	}, handler)

	// Add a route for our handler
	err := server.Router().AddEndpoint("test", config.EndpointConfig{
		Path:   "/",
		Method: "GET",
	})
	require.NoError(t, err)

	// StartComponent server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Serve(ctx)
	}()

	// Wait a bit for server to start
	time.Sleep(time.Second)

	// Make request
	resp, err := http.Get("http://localhost:8123")
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, "hello", string(body))

	// Clean shutdown
	cancel()
}
