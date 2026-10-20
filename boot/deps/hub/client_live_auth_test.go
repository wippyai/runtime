// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	downloadv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1"
	downloadv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1/downloadv1connect"
	manifestv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1/manifestv1connect"
	modulev1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1/modulev1connect"
)

type authDownloadService struct {
	downloadv1connect.UnimplementedDownloadServiceHandler
}

func (*authDownloadService) GetDownloadURL(context.Context, *connect.Request[downloadv1.GetDownloadURLRequest]) (*connect.Response[downloadv1.GetDownloadURLResponse], error) {
	return connect.NewResponse(&downloadv1.GetDownloadURLResponse{Version: "1.0.0"}), nil
}

func TestClientTokenSourceCoversHubRequestPaths(t *testing.T) {
	root := t.TempDir()
	digest := strings.Repeat("a", 64)
	archive := wappFixture(t)
	headers := make(chan string, 16)
	mux := http.NewServeMux()
	path, handler := modulev1connect.NewModuleServiceHandler(&privateModuleService{})
	mux.Handle(path, handler)
	path, handler = manifestv1connect.NewManifestServiceHandler(&privateManifestService{})
	mux.Handle(path, handler)
	path, handler = downloadv1connect.NewDownloadServiceHandler(&authDownloadService{})
	mux.Handle(path, handler)
	mux.HandleFunc("/api/v1/account/modules", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"module-1"}`))
	})
	mux.HandleFunc("/api/v1/publish/upload", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"publish_id":"publish-1"}`))
	})
	mux.HandleFunc("/api/v1/wapp/"+digest, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("archive"))
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	var token atomic.Value
	token.Store("")
	client, err := NewClient(Options{
		BaseURL:     server.URL,
		Token:       "must-not-fall-back-to-static-token",
		TokenSource: func() string { return token.Load().(string) },
	})
	require.NoError(t, err)
	calls := []struct {
		call func() error
		name string
	}{
		{name: "versions", call: func() error { _, err := client.ListAllVersions(t.Context(), "acme", "private"); return err }},
		{name: "manifest", call: func() error { _, err := client.GetManifest(t.Context(), "acme", "private", "1.0.0"); return err }},
		{name: "download-url", call: func() error {
			_, err := client.GetDownloadURL(t.Context(), &DownloadParams{Org: "acme", Module: "private"})
			return err
		}},
		{name: "register", call: func() error {
			_, err := client.RegisterModule(t.Context(), &RegisterModuleParams{Org: "acme", Name: "private"})
			return err
		}},
		{name: "download", call: func() error { return client.DownloadViaHub(t.Context(), digest, filepath.Join(root, "module.wapp")) }},
		{name: "publish", call: func() error {
			_, err := client.PublishViaHub(t.Context(), UploadInput{Org: "acme", Module: "private", FilePath: archive})
			return err
		}},
	}
	for _, current := range []string{"test-login", "test-rotated", ""} {
		token.Store(current)
		want := ""
		if current != "" {
			want = "Bearer " + current
		}
		for _, call := range calls {
			err := call.call()
			if call.name == "publish" && current == "" {
				require.ErrorIs(t, err, ErrNotAuthenticated)
				require.Empty(t, headers, "anonymous publish must not send a request")
				continue
			}
			require.NoError(t, err, call.name)
			require.Equal(t, want, <-headers, call.name)
		}
	}
}

func TestClientTokenSourceDoesNotReachArtifactStorage(t *testing.T) {
	headers := make(chan string, 1)
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte("archive"))
	}))
	t.Cleanup(storage.Close)
	client, err := NewClient(Options{
		BaseURL:     "https://registry.example.com",
		TokenSource: func() string { return "test-registry-secret" },
	})
	require.NoError(t, err)
	destination := filepath.Join(t.TempDir(), "module.wapp")
	require.NoError(t, client.DownloadToFile(t.Context(), storage.URL+"/signed-artifact", destination))
	require.Empty(t, <-headers, "Hub credentials must not be attached to storage URLs")
}

func TestHubDownloadRereadsTokenOnRetry(t *testing.T) {
	var token atomic.Value
	token.Store("test-before-retry")
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			if r.Header.Get("Authorization") != "Bearer test-before-retry" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			token.Store("test-after-retry")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-after-retry" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("archive"))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(Options{BaseURL: server.URL, TokenSource: func() string { return token.Load().(string) }})
	require.NoError(t, err)
	destination := filepath.Join(t.TempDir(), "module.wapp")
	require.NoError(t, client.DownloadViaHub(t.Context(), strings.Repeat("a", 64), destination))
	require.Equal(t, int32(2), attempts.Load())
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "archive", string(content))
}
