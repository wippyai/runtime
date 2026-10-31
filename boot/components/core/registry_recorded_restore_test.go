// SPDX-License-Identifier: MPL-2.0

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	commonv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/common/v1"
	downloadv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1"
	downloadv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1/downloadv1connect"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// A replacement image contains its own baseline, but no artifacts installed by
// earlier UI operations. Boot materializes the recorded identities before the
// baseline transition, then the same image restarts without any Hub requests.
func TestRegistryComponentBootsRecordedGraphWithFreshVendor(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv(auth.EnvToken, "test-recorded-artifacts-token")
	artifacts := &recordedArtifacts{blobs: map[string][]byte{}, digests: map[string]string{}}
	baseline := regapi.State{}
	for _, module := range []string{"app", "base", "feature"} {
		for _, version := range []string{"1.0.0", "2.0.0"} {
			entries := []wapp.Entry{
				{ID: wapp.NewID("acme."+module, "definition"), Kind: regapi.NamespaceDefinition},
				{ID: wapp.NewID("acme."+module, "state"), Kind: regapi.EntryKind, Data: map[string]any{"version": version}},
			}
			var b bytes.Buffer
			require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{}, entries, &b))
			key := module + "@" + version
			artifacts.blobs[key] = b.Bytes()
			artifacts.digests[key] = fmt.Sprintf("sha256:%x", sha256.Sum256(b.Bytes()))
			if module != "feature" && version == "1.0.0" {
				for _, entry := range entries {
					baseline = append(baseline, regapi.Entry{ID: regapi.NewID(entry.ID.Namespace, entry.ID.Name), Kind: entry.Kind, Data: payload.New(entry.Data), Registry: regapi.EntryMetadata{Owner: "acme/" + module}})
				}
			}
		}
	}
	lockPath := filepath.Join(root, "wippy.lock")
	vendor := filepath.Join(root, "vendor")
	writeBaseline := func(version string) {
		l, err := lock.New(lockPath)
		require.NoError(t, err)
		l.SetDirectories(lock.Directories{Modules: "vendor"})
		for _, module := range []string{"app", "base"} {
			l.SetModule(lock.Module{Name: "acme/" + module, Version: version, Hash: artifacts.digests[module+"@"+version], Root: module == "app"})
			path := filepath.Join(vendor, "acme", module+"-"+version+".wapp")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, artifacts.blobs[module+"@"+version], 0o600))
		}
		require.NoError(t, l.Write())
	}
	writeBaseline("1.0.0")
	dbPath := filepath.Join(root, "registry.db")
	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryEnableHistory: true, RegistryHistoryType: "sqlite", RegistryHistoryPath: dbPath,
		RegistryDependencyLockPath: lockPath, RegistryDependencyVendorDir: vendor,
	}))
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
	require.NoError(t, err)
	ctx = regapi.WithDependencyAccess(ctx, regapi.DependencyAccessOnline)
	resolver := topology.NewResolver()
	handler, err := hub.NewDependencyHandler(hub.DependencyHandlerOptions{Hub: artifacts, LockPath: lockPath, VendorDir: vendor, Resolver: resolver, Logger: zap.NewNop()})
	require.NoError(t, err)
	require.NoError(t, handler.PrepareRestore(ctx, history))
	reg := registryimpl.NewRegistry(history, &recordedRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
		registryimpl.WithKindDirective(regapi.NamespaceDependency, expansion.NewDependencyDirective(handler.Expand).WithChangesExpansion(handler.ExpandChanges).WithResolutionTransition(handler.ReconcileResolution)))
	ctx = regapi.WithRegistry(ctx, reg)
	head, err := history.Head()
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, baseline, head))
	for _, module := range []string{"feature", "app"} {
		_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{
			ID: regapi.NewID("installed.deps", module), Kind: regapi.NamespaceDependency, Data: payload.New(map[string]any{"component": "acme/" + module, "version": ">=2.0.0"}),
		}}})
		require.NoError(t, err)
	}
	recorded := reg.Snapshot().Registry.Resolution
	require.NoError(t, history.Close())
	require.NoError(t, os.RemoveAll(vendor))
	writeBaseline("2.0.0")
	// New baseline entries match the shipped artifacts, just as the image loader supplies them.
	for i := range baseline {
		if baseline[i].Kind == regapi.EntryKind {
			baseline[i].Data = payload.New(map[string]any{"version": "2.0.0"})
		}
	}
	var calls atomic.Int32
	mux := http.NewServeMux()
	path, downloadHandler := downloadv1connect.NewDownloadServiceHandler(&recordedDownloadService{artifacts: artifacts})
	mux.Handle(path, downloadHandler)
	mux.HandleFunc("/artifact/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(artifacts.blobs[strings.TrimPrefix(r.URL.Path, "/artifact/")])
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); mux.ServeHTTP(w, r) }))
	defer server.Close()
	artifacts.url = server.URL
	t.Setenv(auth.EnvRegistry, server.URL)
	for restart := 0; restart < 2; restart++ {
		calls.Store(0)
		ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
		require.NoError(t, err)
		loader, err := bootpkg.NewLoader(Artifacts(), Registry())
		require.NoError(t, err)
		ctx, err = loader.Load(ctx)
		require.NoError(t, err)
		restored := regapi.GetRegistry(ctx)
		head, err := restored.History().Head()
		require.NoError(t, err)
		require.NoError(t, restored.LoadState(ctx, baseline, head))
		resolution := restored.Snapshot().Registry.Resolution
		for _, module := range recorded.Modules {
			if module.Name == "acme/base" {
				continue
			}
			require.Contains(t, resolution.Modules, module, "history selects the recorded identity")
		}
		for _, module := range resolution.Modules {
			require.Equal(t, artifacts.digests[strings.TrimPrefix(module.Name, "acme/")+"@2.0.0"], module.Digest)
		}
		for _, module := range []string{"feature", "app"} {
			_, err := restored.GetEntry(regapi.NewID("installed.deps", module))
			require.NoError(t, err)
		}
		require.Len(t, resolution.Modules, 2)
		require.Len(t, resolution.Deployment.Modules, 2)
		for _, module := range resolution.Deployment.Modules {
			require.Equal(t, "2.0.0", module.Version)
			require.Equal(t, artifacts.digests[strings.TrimPrefix(module.Name, "acme/")+"@2.0.0"], module.Digest)
		}
		baseEntry, err := restored.GetEntry(regapi.NewID("acme.base", "state"))
		require.NoError(t, err)
		require.Equal(t, "2.0.0", baseEntry.Data.Data().(map[string]any)["version"])
		require.Equal(t, recorded.Roots, resolution.Roots)
		if restart == 0 {
			require.EqualValues(t, 2, calls.Load(), "only the missing recorded feature is fetched")
		} else {
			require.Zero(t, calls.Load(), "warm restart makes no Hub calls")
		}
		require.NoError(t, loader.Shutdown(ctx))
	}
}

type recordedArtifacts struct {
	blobs   map[string][]byte
	digests map[string]string
	url     string
}

func (a *recordedArtifacts) GetManifest(_ context.Context, org, module, _ string) (*hub.ModuleManifest, error) {
	key := module + "@2.0.0"
	return &hub.ModuleManifest{Org: org, Name: module, Version: "2.0.0", VersionID: "2.0.0", Digest: a.digests[key], SizeBytes: uint64(len(a.blobs[key])), URL: "fixture://" + key}, nil
}
func (*recordedArtifacts) ListAllVersions(context.Context, string, string) ([]hub.VersionInfo, error) {
	return []hub.VersionInfo{{Version: "2.0.0", ID: "2.0.0"}}, nil
}
func (a *recordedArtifacts) DownloadToFile(_ context.Context, url, dest string) error {
	return os.WriteFile(dest, a.blobs[strings.TrimPrefix(url, "fixture://")], 0o600)
}

// Seed operations carry URLs; only startup uses the real Hub protocol below.
func (*recordedArtifacts) GetDownloadURL(context.Context, *hub.DownloadParams) (*hub.DownloadInfo, error) {
	return nil, fmt.Errorf("unexpected seed download metadata request")
}

type recordedDownloadService struct {
	downloadv1connect.UnimplementedDownloadServiceHandler
	artifacts *recordedArtifacts
}

func (s *recordedDownloadService) GetDownloadURL(_ context.Context, req *connect.Request[downloadv1.GetDownloadURLRequest]) (*connect.Response[downloadv1.GetDownloadURLResponse], error) {
	if req.Header().Get("Authorization") != "Bearer test-recorded-artifacts-token" {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("missing credential"))
	}
	name := req.Msg.Module.GetName()
	key := name.Name + "@" + req.Msg.Version.GetVersion()
	blob, ok := s.artifacts.blobs[key]
	if name.Org != "acme" || !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("unknown artifact"))
	}
	return connect.NewResponse(&downloadv1.GetDownloadURLResponse{Version: req.Msg.Version.GetVersion(), Download: &commonv1.DownloadInfo{Url: s.artifacts.url + "/artifact/" + key, Digest: s.artifacts.digests[key], SizeBytes: uint64(len(blob))}}), nil
}

type recordedRunner struct{}

func (*recordedRunner) Transition(_ context.Context, state regapi.State, changes regapi.ChangeSet, _ func(context.Context)) (regapi.State, error) {
	entries := topology.NewStateMap(state)
	for _, op := range changes {
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			entries[op.Entry.ID] = op.Entry
		case regapi.EntryDelete:
			delete(entries, op.Entry.ID)
		}
	}
	return topology.StateMapToSlice(entries), nil
}
