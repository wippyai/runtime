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
	"github.com/wippyai/runtime/api/boot"
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

func TestRunRecordedDeploymentRestoration(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, mode := range []string{"cold", "explicit registry", "history redirect", "partial cache", "cached offline", "corrupt legacy", "altered local", "altered download", "wrong download size", "unavailable Hub", "cancelled", "wrong root", "different version", "mutable selector"} {
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
					metadata := wapp.Metadata{
						"namespace": "acme." + name, "name": name, "version": "1.0.0",
						"runtime.profiles.recorded.logger.level": "error",
					}
					if mode == "history redirect" && name == "app" {
						if engine == "sqlite" {
							metadata["runtime.profiles.recorded.registry.history_path"] = filepath.Join(dir, "redirected.db")
						} else {
							metadata["runtime.profiles.recorded.registry.history_schema"] = "redirected_history"
						}
					}
					require.NoError(t, wapp.NewWriter().PackEntries(metadata, []wapp.Entry{{ID: wapp.NewID("acme."+name, "definition"), Kind: regapi.NamespaceDefinition}}, &b))
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
				var offline atomic.Bool
				offline.Store(mode == "cached offline" || mode == "unavailable Hub")
				httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					server.calls.Add(1)
					if offline.Load() {
						http.Error(w, "Hub unavailable", http.StatusServiceUnavailable)
						return
					}
					handler.ServeHTTP(w, r)
				}))
				defer httpServer.Close()
				server.url = httpServer.URL
				t.Setenv(auth.EnvRegistry, server.url)
				if mode == "explicit registry" {
					fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						t.Error("explicit --registry must override the default registry during recorded restore")
						http.Error(w, "wrong registry", http.StatusNotFound)
					}))
					defer fallback.Close()
					t.Setenv(auth.EnvRegistry, fallback.URL)
				}
				if mode == "wrong download size" {
					server.sizeDelta = 1
				}
				if mode == "altered local" {
					path := filepath.Join(".wippy", "vendor", "acme", "app-1.0.0."+strings.ReplaceAll(modules[0].Digest, ":", "-")+".wapp")
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
					require.NoError(t, os.WriteFile(path, []byte("altered"), 0o600))
				}
				if mode == "partial cache" || mode == "cached offline" {
					for _, module := range modules {
						if mode != "cached offline" && module.Name != "acme/app" {
							continue
						}
						name := strings.TrimPrefix(module.Name, "acme/")
						path := filepath.Join(".wippy", "vendor", "acme", name+"-1.0.0."+strings.ReplaceAll(module.Digest, ":", "-")+".wapp")
						require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
						require.NoError(t, os.WriteFile(path, server.blobs[name], 0o600))
					}
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
					ref := "acme/app"
					expectedError := ""
					switch mode {
					case "altered local", "altered download", "wrong download size":
						expectedError = "integrity verification"
					case "unavailable Hub":
						expectedError = "503 Service Unavailable"
					case "history redirect":
						expectedError = "recorded deployment history"
					case "cancelled":
						ctx, cancel := context.WithCancel(context.Background())
						cancel()
						command.SetContext(ctx)
						expectedError = "context canceled"
					case "wrong root":
						ref, expectedError = "acme/other", "not acme/other"
					case "different version":
						ref, expectedError = "acme/app@2.0.0", "not @2.0.0"
					case "mutable selector":
						ref, expectedError = "acme/app@latest", "requires Hub resolution"
					}
					err := runWithUseCase(command, []string{ref}, "test")
					if expectedError != "" {
						require.ErrorContains(t, err, expectedError)
						if mode == "altered local" || mode == "cancelled" || mode == "wrong root" || mode == "different version" || mode == "mutable selector" {
							require.Zero(t, server.calls.Load())
						}
						require.NoFileExists(t, "wippy.lock")
						if mode == "history redirect" {
							require.NoFileExists(t, filepath.Join(dir, "redirected.db"))
						}
						break
					}
					require.ErrorContains(t, err, "no test entrypoint found", "the recorded deployment reaches entrypoint selection")
					require.NoFileExists(t, "wippy.lock")
					if mode == "corrupt legacy" {
						legacy, err := os.ReadFile(legacyPath)
						require.NoError(t, err)
						require.Equal(t, []byte("corrupt"), legacy)
					}
					if restart == 0 && mode == "partial cache" {
						require.EqualValues(t, 2, server.calls.Load(), "only the missing recorded module is fetched")
					} else if restart == 0 && mode != "cached offline" {
						require.EqualValues(t, 4, server.calls.Load())
					} else {
						require.Zero(t, server.calls.Load())
					}
					offline.Store(true)
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
	version   string
	calls     atomic.Int32
	sizeDelta uint64
}

func (s *recordedRunHub) GetDownloadURL(_ context.Context, req *connect.Request[downloadv1.GetDownloadURLRequest]) (*connect.Response[downloadv1.GetDownloadURLResponse], error) {
	expectedVersion := s.version
	if expectedVersion == "" {
		expectedVersion = "1.0.0"
	}
	require.Equal(s.t, expectedVersion, req.Msg.Version.GetVersion(), "restore downloads only the recorded version")
	name := req.Msg.Module.GetName().Name
	data := s.blobs[name]
	return connect.NewResponse(&downloadv1.GetDownloadURLResponse{Version: expectedVersion, Download: &commonv1.DownloadInfo{Url: s.url + "/artifact/" + name, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), SizeBytes: uint64(len(data)) + s.sizeDelta}}), nil
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
	for _, engine := range []string{"sqlite", "postgres", "default", "memory", "nil", "disabled"} {
		t.Run(engine, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
			t.Setenv("WIPPY_CACHE_DIR", filepath.Join(dir, "wippy-cache"))
			var config string
			if engine == "sqlite" || engine == "postgres" {
				history, historyConfig := recordedRunHistory(t, engine, dir)
				require.NoError(t, history.(interface{ Close() error }).Close())
				config = historyConfig
			} else {
				config = "version: '1.0'\n"
				switch engine {
				case "memory", "nil":
					config += fmt.Sprintf("registry:\n  history_type: %s\n", engine)
				case "disabled":
					config += "registry:\n  enable_history: false\n"
				}
			}
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
			var offline atomic.Bool
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if offline.Load() {
					http.Error(w, "Hub unavailable during cached restart", http.StatusServiceUnavailable)
					return
				}
				mux.ServeHTTP(w, r)
			}))
			defer server.Close()
			path, downloadHandler := downloadv1connect.NewDownloadServiceHandler(&recordedRunHub{t: t, blobs: artifacts, url: server.URL, version: "2.0.0"})
			mux.Handle(path, downloadHandler)
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
			lockBytes, err := os.ReadFile("wippy.lock")
			require.NoError(t, err)
			if engine == "sqlite" || engine == "postgres" {
				// Seed through the actual CLI first, then lose a locked artifact.
				// The restart must use --registry for the exact saved selection.
				feature, ok := locked.GetModule("acme/feature")
				require.True(t, ok)
				legacy := filepath.Join(".wippy", "vendor", "acme", "feature-2.0.0.wapp")
				immutable := filepath.Join(".wippy", "vendor", "acme", "feature-2.0.0."+strings.ReplaceAll(feature.Hash, ":", "-")+".wapp")
				for _, path := range []string{legacy, immutable} {
					if err := os.Remove(path); err != nil {
						require.True(t, os.IsNotExist(err), "%v", err)
					}
				}
				fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					t.Error("locked restore must use the selected --registry")
					http.Error(w, "wrong registry", http.StatusNotFound)
				}))
				defer fallback.Close()
				t.Setenv(auth.EnvRegistry, fallback.URL)
				calls.Store(0)
				err = runWithUseCase(command, []string{"acme/app"}, "test")
				require.ErrorContains(t, err, "no test entrypoint found")
				require.EqualValues(t, 2, calls.Load(), "only the missing locked artifact needs download metadata and bytes")
			}
			offline.Store(true)
			calls.Store(0)
			err = runWithUseCase(command, []string{"acme/app"}, "test")
			require.ErrorContains(t, err, "no test entrypoint found")
			require.Zero(t, calls.Load(), "cached restart must work without the Hub")
			after, err := os.ReadFile("wippy.lock")
			require.NoError(t, err)
			require.Equal(t, lockBytes, after)
			if engine != "sqlite" && engine != "postgres" {
				return
			}
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

func TestLoadRecordedDeploymentDefaultsWithoutHistory(t *testing.T) {
	for name, cfg := range map[string]boot.Config{
		"nil config":  nil,
		"default":     boot.NewConfig(),
		"memory":      boot.NewConfig(boot.WithSection("registry", map[string]any{"history_type": "memory"})),
		"nil history": boot.NewConfig(boot.WithSection("registry", map[string]any{"history_type": "nil"})),
		"disabled":    boot.NewConfig(boot.WithSection("registry", map[string]any{"enable_history": false})),
	} {
		t.Run(name, func(t *testing.T) {
			defaults, deployment, err := loadRecordedDeploymentDefaults(context.Background(), cfg, "", "", zap.NewNop())
			require.NoError(t, err)
			require.Nil(t, defaults)
			require.Nil(t, deployment)
		})
	}
}

func TestRecordedHistoryConfigComposition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	for _, test := range []struct {
		host     map[string]any
		override map[string]any
		name     string
		reject   bool
	}{
		{name: "unrelated application settings", host: map[string]any{"history_type": "sqlite", "history_path": path}, override: map[string]any{"event_wait_timeout": "1m"}},
		{name: "equivalent SQLite path", host: map[string]any{"history_type": "sqlite", "history_path": path}, override: map[string]any{"history_path": filepath.Dir(path) + "/./history.db"}},
		{name: "SQLite path from changed profile variable", host: map[string]any{"history_type": "sqlite", "history_path": "${history_path}"}, override: map[string]any{"history_path": path + ".other"}, reject: true},
		{name: "backend switch", host: map[string]any{"history_type": "sqlite", "history_path": path}, override: map[string]any{"history_type": "memory"}, reject: true},
		{name: "disabled history", host: map[string]any{"history_type": "sqlite", "history_path": path}, override: map[string]any{"enable_history": false}, reject: true},
		{name: "Postgres schema switch", host: map[string]any{"history_type": "postgres", "history_dsn": "original-dsn", "history_schema": "original"}, override: map[string]any{"history_schema": "other"}, reject: true},
		{name: "Postgres database switch", host: map[string]any{"history_type": "postgres", "history_dsn": "original-dsn", "history_schema": "original"}, override: map[string]any{"history_dsn": "other-dsn"}, reject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := boot.NewConfig(boot.WithSection("registry", test.host), boot.WithSection("vars", map[string]any{"history_path": path}))
			effective, err := bootconfig.ResolveVariables(bootconfig.Merge(host, boot.NewConfig(boot.WithSection("registry", test.override))))
			require.NoError(t, err)
			err = validateRecordedHistoryConfig(host, effective)
			if test.reject {
				require.ErrorContains(t, err, "recorded deployment history")
				require.NotContains(t, err.Error(), "original-dsn")
				require.NotContains(t, err.Error(), "other-dsn")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
