package remote

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDialRequiresEndpointAndCredentials(t *testing.T) {
	_, err := Dial(context.Background(), DialConfig{})
	require.Error(t, err)
	_, err = Dial(context.Background(), DialConfig{Endpoint: "localhost:9000"})
	require.Error(t, err)
}

func TestExplicitTokenFileDoesNotFallBackToToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	_, err := Dial(t.Context(), DialConfig{Endpoint: "localhost:9000", Token: "default-secret", TokenFile: path})
	require.ErrorContains(t, err, "read history token")
	require.NoError(t, os.WriteFile(path, []byte(" \n"), 0600))
	_, err = Dial(t.Context(), DialConfig{Endpoint: "localhost:9000", Token: "default-secret", TokenFile: path})
	require.ErrorContains(t, err, "history authentication is required")
	require.NotContains(t, err.Error(), "default-secret")
}
