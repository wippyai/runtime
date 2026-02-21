package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDownloadToFileRetriesTransientStatus(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporary"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("payload"))
	}))
	t.Cleanup(server.Close)

	client := &Client{
		httpClient: server.Client(),
	}

	dest := filepath.Join(t.TempDir(), "cache", "module.wapp")
	err := client.DownloadToFile(context.Background(), server.URL, dest)
	require.NoError(t, err)
	require.Equal(t, 3, attempts)

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "payload", string(data))
}

func TestDownloadToFileDoesNotRetryForbidden(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	t.Cleanup(server.Close)

	client := &Client{
		httpClient: server.Client(),
	}

	dest := filepath.Join(t.TempDir(), "cache", "module.wapp")
	err := client.DownloadToFile(context.Background(), server.URL, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 403")
	require.Equal(t, 1, attempts)
}
