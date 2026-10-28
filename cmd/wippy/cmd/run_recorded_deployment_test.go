// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/common/v1"
	downloadv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1"
	"github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1/downloadv1connect"
	manifestv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1"
	"github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1/manifestv1connect"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/internal/version"
	historypg "github.com/wippyai/runtime/system/registry/history/postgres"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestRunRecordedDeploymentWithoutLock(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, mode := range []string{"cold", "corrupt legacy", "altered local", "altered download", "wrong download size"} {
			t.Run(engine+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				t.Chdir(dir)
				t.Setenv("XDG_CONFIG_HOME", dir)
				t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
				t.Setenv("WIPPY_CACHE_DIR", filepath.Join(dir, "wippy-cache"))
				t.Setenv(auth.EnvToken, "recorded-test-token")
				server := &recordedRunHub{t: t, blobs: map[string][]byte{}}
				var modules []regapi.ResolvedModule
				for _, name := range []string{"app", "feature"} {
					var b bytes.Buffer
					require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{
						"namespace": "acme." + name, "name": name, "version": "1.0.0",
						"runtime.profiles.recorded.logger.level": "error",
					}, []wapp.Entry{{ID: wapp.NewID("acme."+name, "definition"), Kind: regapi.NamespaceDefinition}}, &b))
					server.blobs[name] = b.Bytes()
					modules = append(modules, regapi.ResolvedModule{Name: "acme/" + name, Version: "1.0.0", VersionID: "1.0.0", Source: "hub", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(b.Bytes())), SizeBytes: uint64(b.Len())})
				}
				history, config := recordedRunHistory(t, engine, dir)
				roots := []regapi.DependencyRoot{{ID: "app.deps:feature", Component: "acme/feature", Version: ">=1.0.0"}}
				encoded, err := json.Marshal(roots)
				require.NoError(t, err)
				resolution := (&regapi.DependencyResolution{InputDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), Roots: roots, Modules: modules[1:], Deployment: &regapi.Deployment{Root: "acme/app", Modules: modules[:1]}}).Canonical()
				root, err := history.Head()
				require.NoError(t, err)
				head := version.FromParent(root, 1)
				require.NoError(t, history.(regapi.ResolutionHistory).SaveWithDependencyResolution(head, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app.deps", "feature"), Kind: regapi.NamespaceDependency, Data: payload.New(map[string]any{"component": "acme/feature", "version": ">=1.0.0"})}}}, resolution, true))
				require.NoError(t, history.(interface{ Close() error }).Close())
				configPath := filepath.Join(dir, ".wippy.yaml")
				require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
				setTestConfigFiles(t, configPath)
				handler := http.NewServeMux()
				path, service := downloadv1connect.NewDownloadServiceHandler(server)
				handler.Handle(path, service)
				handler.HandleFunc("/artifact/", func(w http.ResponseWriter, r *http.Request) {
					data := server.blobs[strings.TrimPrefix(r.URL.Path, "/artifact/")]
					if mode == "altered download" {
						data = append([]byte(nil), data...)
						data[0] ^= 1
					}
					_, _ = w.Write(data)
				})
				httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { server.calls.Add(1); handler.ServeHTTP(w, r) }))
				defer httpServer.Close()
				server.url = httpServer.URL
				t.Setenv(auth.EnvRegistry, server.url)
				if mode == "wrong download size" {
					server.sizeDelta = 1
				}
				if mode == "altered local" {
					path := filepath.Join(".wippy", "vendor", "acme", "app-1.0.0."+strings.ReplaceAll(modules[0].Digest, ":", "-")+".wapp")
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
					require.NoError(t, os.WriteFile(path, []byte("altered"), 0o600))
				}
				legacyPath := filepath.Join(".wippy", "vendor", "acme", "feature-1.0.0.wapp")
				if mode == "corrupt legacy" {
					require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o700))
					require.NoError(t, os.WriteFile(legacyPath, []byte("corrupt"), 0o600))
				}
				for restart := 0; restart < 2; restart++ {
					server.calls.Store(0)
					command := &cobra.Command{}
					command.Flags().String("registry", server.url, "")
					command.Flags().StringArray("profile", []string{"recorded"}, "")
					command.SetContext(context.Background())
					err := runWithUseCase(command, []string{"acme/app"}, "test")
					if mode != "cold" && mode != "corrupt legacy" {
						require.ErrorContains(t, err, "integrity verification")
						if mode == "altered local" {
							require.Zero(t, server.calls.Load())
						}
						require.NoFileExists(t, "wippy.lock")
						break
					}
					require.ErrorContains(t, err, "no test entrypoint found", "the recorded deployment reaches entrypoint selection")
					require.NoFileExists(t, "wippy.lock")
					if mode == "corrupt legacy" {
						legacy, err := os.ReadFile(legacyPath)
						require.NoError(t, err)
						require.Equal(t, []byte("corrupt"), legacy)
					}
					if restart == 0 {
						require.EqualValues(t, 4, server.calls.Load())
					} else {
						require.Zero(t, server.calls.Load())
					}
				}
				cfg, err := bootconfig.Load(configPath)
				require.NoError(t, err)
				cfg, err = bootconfig.ResolveVariables(cfg)
				require.NoError(t, err)
				stored, closer, err := core.OpenRegistryHistory(cfg, zap.NewNop())
				require.NoError(t, err)
				storedHead, err := stored.Head()
				require.NoError(t, err)
				require.Equal(t, head.ID(), storedHead.ID())
				restored, err := stored.(regapi.ResolutionHistory).GetDependencyResolution(storedHead)
				require.NoError(t, err)
				require.Equal(t, resolution.Modules, restored.Modules)
				require.Equal(t, resolution.Deployment, restored.Deployment)
				require.NoError(t, closer.Close())
			})
		}
	}
}

type recordedRunHub struct {
	downloadv1connect.UnimplementedDownloadServiceHandler
	t         *testing.T
	blobs     map[string][]byte
	url       string
	calls     atomic.Int32
	sizeDelta uint64
}

func (s *recordedRunHub) GetDownloadURL(_ context.Context, req *connect.Request[downloadv1.GetDownloadURLRequest]) (*connect.Response[downloadv1.GetDownloadURLResponse], error) {
	require.Equal(s.t, "1.0.0", req.Msg.Version.GetVersion(), "restore downloads only the recorded version")
	name := req.Msg.Module.GetName().Name
	data := s.blobs[name]
	return connect.NewResponse(&downloadv1.GetDownloadURLResponse{Version: "1.0.0", Download: &commonv1.DownloadInfo{Url: s.url + "/artifact/" + name, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), SizeBytes: uint64(len(data)) + s.sizeDelta}}), nil
}
func recordedRunHistory(t *testing.T, engine, dir string) (regapi.History, string) {
	t.Helper()
	if engine == "sqlite" {
		path := filepath.Join(dir, "history.db")
		h, err := historysqlite.NewSQLite(path, zap.NewNop())
		require.NoError(t, err)
		return h, fmt.Sprintf("version: '1.0'\nregistry:\n  history_type: sqlite\n  history_path: %s\n", path)
	}
	dsn := os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
	}
	schema := fmt.Sprintf("recorded_run_%d", time.Now().UnixNano())
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA %q CASCADE", schema))
		require.NoError(t, err)
		require.NoError(t, db.Close())
	})
	h, err := historypg.NewPostgres(dsn, schema, zap.NewNop())
	require.NoError(t, err)
	return h, fmt.Sprintf("version: '1.0'\nvars:\n  history_dsn: %s\nregistry:\n  history_type: postgres\n  history_dsn: ${history_dsn}\n  history_schema: %s\n", dsn, schema)
}

func TestRunEmptyHistoryWithoutLockSeedsHubDeployment(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
			t.Setenv("WIPPY_CACHE_DIR", filepath.Join(dir, "wippy-cache"))
			history, config := recordedRunHistory(t, engine, dir)
			require.NoError(t, history.(interface{ Close() error }).Close())
			configPath := filepath.Join(dir, ".wippy.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
			setTestConfigFiles(t, configPath)

			artifacts := map[string][]byte{}
			for _, name := range []string{"app", "feature"} {
				var blob bytes.Buffer
				entries := []wapp.Entry{{ID: wapp.NewID("acme."+name, "definition"), Kind: regapi.NamespaceDefinition}}
				if name == "app" {
					entries = append(entries, wapp.Entry{ID: wapp.NewID("acme.app", "feature"), Kind: regapi.NamespaceDependency, Data: map[string]any{"component": "acme/feature", "version": "2.0.0"}})
				}
				require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{"namespace": "acme." + name, "name": name, "version": "2.0.0"}, entries, &blob))
				artifacts[name] = blob.Bytes()
			}
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()
			var manifests atomic.Int32
			mux.Handle(manifestv1connect.ManifestServiceGetManifestProcedure, connect.NewUnaryHandler(manifestv1connect.ManifestServiceGetManifestProcedure, func(_ context.Context, req *connect.Request[manifestv1.GetManifestRequest]) (*connect.Response[manifestv1.GetManifestResponse], error) {
				manifests.Add(1)
				name := req.Msg.GetModule().GetName().GetName()
				data := artifacts[name]
				manifest := &manifestv1.ModuleManifest{Org: "acme", Name: name, Version: "2.0.0", VersionId: "2.0.0", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), SizeBytes: uint64(len(data)), Download: &commonv1.DownloadInfo{Url: server.URL + "/artifact/" + name}}
				if name == "app" {
					manifest.Dependencies = []*manifestv1.ResolvedDependency{{Org: "acme", Name: "feature", Version: "2.0.0", Constraint: "2.0.0"}}
				}
				return connect.NewResponse(&manifestv1.GetManifestResponse{Manifest: manifest}), nil
			}))
			mux.HandleFunc("/artifact/", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(artifacts[strings.TrimPrefix(r.URL.Path, "/artifact/")])
			})

			t.Setenv(auth.EnvRegistry, server.URL)
			command := &cobra.Command{}
			command.Flags().String("registry", server.URL, "")
			command.SetContext(context.Background())
			err := runWithUseCase(command, []string{"acme/app"}, "test")
			require.ErrorContains(t, err, "no test entrypoint found")
			require.Positive(t, manifests.Load(), "empty history resolves the seed")
			locked, err := lock.New("wippy.lock")
			require.NoError(t, err)
			root, ok := locked.GetModule("acme/app")
			require.True(t, ok)
			require.Equal(t, "2.0.0", root.Version)
			cfg, err := bootconfig.Load(configPath)
			require.NoError(t, err)
			cfg, err = bootconfig.ResolveVariables(cfg)
			require.NoError(t, err)
			stored, closer, err := core.OpenRegistryHistory(cfg, zap.NewNop())
			require.NoError(t, err)
			defer func() { require.NoError(t, closer.Close()) }()
			head, err := stored.Head()
			require.NoError(t, err)
			resolution, err := stored.(regapi.ResolutionHistory).GetDependencyResolution(head)
			require.NoError(t, err)
			require.Equal(t, "acme/app", resolution.Deployment.Root)
			require.Equal(t, "2.0.0", resolution.Deployment.Modules[0].Version)
		})
	}
}
