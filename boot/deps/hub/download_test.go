// SPDX-License-Identifier: MPL-2.0

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

func TestExpectedArtifactDigestPreservesPinnedDigest(t *testing.T) {
	digest, err := ExpectedArtifactDigest("sha256:abc", "sha256:ABC")
	require.NoError(t, err)
	require.Equal(t, "sha256:abc", digest)
}

func TestExpectedArtifactDigestAcceptsLegacyBareDigest(t *testing.T) {
	digest, err := ExpectedArtifactDigest("abc", "sha256:ABC")
	require.NoError(t, err)
	require.Equal(t, "abc", digest)
}

func TestExpectedArtifactDigestRejectsServerDrift(t *testing.T) {
	_, err := ExpectedArtifactDigest("sha256:abc", "sha256:def")
	require.EqualError(t, err, "artifact digest mismatch: lock pins sha256:abc, download reports sha256:def")
}

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
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, os.WriteFile(dest, []byte("existing"), 0o600))
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
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, os.WriteFile(dest, []byte("existing"), 0o600))
	err := client.DownloadToFile(context.Background(), server.URL, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 403")
	require.Equal(t, 1, attempts)
	data, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	require.Equal(t, "existing", string(data))
}
