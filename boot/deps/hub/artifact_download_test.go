// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDownloadToFileInterruptedBodyPreservesExistingDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	dest := filepath.Join(dir, "module.wapp")
	require.NoError(t, os.WriteFile(dest, []byte("known-good"), 0o600))
	client := &Client{httpClient: server.Client()}

	err := client.DownloadToFile(context.Background(), server.URL, dest)
	require.ErrorContains(t, err, "write file")
	data, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	require.Equal(t, "known-good", string(data))
	parts, globErr := filepath.Glob(filepath.Join(dir, "module.wapp.part-*"))
	require.NoError(t, globErr)
	require.Empty(t, parts, "failed downloads must not leak private partial files")
}

func TestCompleteResolvedModuleIdentitiesNormalizesHubDigest(t *testing.T) {
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	modules := []ResolvedModule{{
		Org:     "acme",
		Name:    "module",
		Version: "v1.2.3",
		Digest:  "SHA256:" + strings.Repeat("AB", 32),
	}}
	require.NoError(t, handler.completeResolvedModuleIdentities(context.Background(), modules))
	require.Equal(t, moduleSourceHub, modules[0].Source)
	require.Equal(t, "sha256:"+strings.Repeat("ab", 32), modules[0].Digest)
}
