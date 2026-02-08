package hub

import (
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
