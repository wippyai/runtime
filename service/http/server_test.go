package http

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
	sup "github.com/ponyruntime/pony/system/supervisor"

	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	cfg := config.ServerConfig{
		Addr: ":8080",
		Timeouts: config.TimeoutConfig{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  10 * time.Second,
		},
	}

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := NewServer(cfg, handler)

	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
	assert.NotNil(t, server.router)
}

func TestServer_Router(t *testing.T) {
	cfg := config.ServerConfig{Addr: ":8080"}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := NewServer(cfg, handler)

	router := server.router
	assert.NotNil(t, router)
	// Router should be the same instance each time
	assert.Equal(t, router, server.router)
}

func TestServer_UpdateConfig(t *testing.T) {
	initialCfg := config.ServerConfig{
		Addr: ":8080",
		Timeouts: config.TimeoutConfig{
			ReadTimeout: 5 * time.Second,
		},
	}

	server := NewServer(initialCfg, nil)

	newCfg := config.ServerConfig{
		Addr: ":9090",
		Timeouts: config.TimeoutConfig{
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
	// Spawn service with a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})

	server := NewServer(config.ServerConfig{Addr: "localhost:8123"}, handler)
	err := server.router.AddEndpoint("test", config.EndpointConfig{
		Path:   "/",
		Method: "GET",
	})
	require.NoError(t, err)

	// Start service
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status, err := server.Start(ctx)
	require.NoError(t, err)

	// Spawn the actually assigned port from the service
	port := server.server.Addr

	// wait for the success message from the status channel
	select {
	case <-status:
	case <-ctx.Done():
		t.Fatal("timeout waiting for service to start")
	}

	// Spawn request
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + port) //nolint:noctx
	require.NoError(t, err)
	defer func() {
		errcl := resp.Body.Close()
		require.NoError(t, errcl)
	}()

	// Read response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, "hello", string(body))

	// Clean shutdown
	err = server.Stop(ctx)
	require.NoError(t, err)
}

func TestHTTPServerUnderSupervisor(t *testing.T) {
	// Define a simple Timeouts handler for testing
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello from supervised service"))
	})

	// Spawn a new Timeouts service
	httpServer := NewServer(config.ServerConfig{Addr: "localhost:8124"}, handler)
	err := httpServer.router.AddEndpoint("test", config.EndpointConfig{
		Path:   "/",
		Method: "GET",
	})
	require.NoError(t, err)

	// Spawn a supervisor for the Timeouts service
	hsup := sup.NewController(
		context.Background(),
		httpServer,
		supervisor.LifecycleConfig{
			StartTimeout: 5 * time.Second,
			StopTimeout:  5 * time.Second,
			RetryPolicy: supervisor.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: 100 * time.Millisecond,
			},
		},
		func(supervisor.Status, any) {},
	)

	// Start the supervisor
	err = hsup.Start()
	require.NoError(t, err)

	// Ensure the service is running
	state := hsup.State()
	if state.Status != supervisor.Running {
		t.Fatalf("Expected supervisor status to be Running, got %v", state.Status)
	}

	// Give the service a moment to fully start
	time.Sleep(200 * time.Millisecond)

	// Spawn a request to the Timeouts service
	port := httpServer.server.Addr
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + port) //nolint:noctx
	require.NoError(t, err)
	defer func() {
		errcl := resp.Body.Close()
		require.NoError(t, errcl)
	}()

	// Read the response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Verify the response
	assert.Equal(t, "hello from supervised service", string(body))

	// stop the supervisor
	err = hsup.Stop()
	require.NoError(t, err)

	// Verify the supervisor is stopped
	state = hsup.State()
	if state.Status != supervisor.Stopped {
		t.Fatalf("Expected supervisor status to be Stopped, got %v", state.Status)
	}
}
