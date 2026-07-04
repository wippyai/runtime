// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	boothub "github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/wapp"
)

func buildWappWithResourceForHubTest(t *testing.T, entries []wapp.Entry, resID wapp.ID, files map[string]string) []byte {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, content := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(content)}
	}
	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.Pack(wapp.Metadata{"name": "dummy"}, entries, fsys, resID, wapp.Metadata{"role": "assets"}, &buf))
	return buf.Bytes()
}

func newReadSurfaceState(t *testing.T, fake *fakeArtifactClient) *lua.LState {
	t.Helper()
	mod := NewModule(Options{ArtifactClient: fake})
	l := lua.NewState()
	t.Cleanup(l.Close)
	l.SetContext(hubTestStoreContext(setupContext(), t))
	tbl, _ := mod.Build()
	l.SetGlobal(mod.Name, tbl)
	return l
}

// hubTestStoreContext attaches a resource store to ctx and closes it in test
// cleanup, so a package handle's frame-end AddCleanup runs and releases the open
// .wapp file before t.TempDir removal. On Windows an open handle blocks removal;
// this mirrors the request-end store close that happens in production.
func hubTestStoreContext(ctx context.Context, t *testing.T) context.Context {
	t.Helper()
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))
	t.Cleanup(func() { _ = store.Close() })
	return ctx
}

func artifactClientReturning(t *testing.T, artifact []byte, downloads *int) *fakeArtifactClient {
	t.Helper()
	sum := sha256.Sum256(artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	return &fakeArtifactClient{
		getDownloadFn: func(_ context.Context, _ *boothub.DownloadParams) (*boothub.DownloadInfo, error) {
			return &boothub.DownloadInfo{URL: "memory://dummy", Version: "v0.1.2", Digest: digest}, nil
		},
		downloadFn: func(_ context.Context, url, destPath string) error {
			if downloads != nil {
				*downloads++
			}
			require.Equal(t, "memory://dummy", url)
			return os.WriteFile(destPath, artifact, 0600)
		},
	}
}

func TestVersionsOpenInspectsPackage(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappWithResourceForHubTest(t,
		[]wapp.Entry{
			{
				ID:   wapp.NewID("wippy.dummy", "router"),
				Kind: "ns.requirement",
				Meta: wapp.Metadata{"description": "Router"},
				Data: map[string]any{"default": "app:router"},
			},
			{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"},
		},
		wapp.NewID("wippy.dummy", "assets"),
		map[string]string{"config/app.yaml": "name: dummy\n"},
	)

	var downloads int
	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, &downloads))

	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		if pkg.version ~= "v0.1.2" then error("version mismatch: " .. tostring(pkg.version)) end
		if pkg.digest:sub(1, 7) ~= "sha256:" then error("digest mismatch: " .. tostring(pkg.digest)) end
		if pkg.packed ~= true then error("packed mismatch") end

		local meta, merr = pkg:metadata()
		if merr then error(merr) end
		if meta.name ~= "dummy" then error("metadata name mismatch: " .. tostring(meta.name)) end

		local entries, eerr = pkg:entries()
		if eerr then error(eerr) end
		if #entries ~= 2 then error("entries count mismatch: " .. tostring(#entries)) end
		local router
		for _, e in ipairs(entries) do
			if e.id == "wippy.dummy:router" then router = e end
		end
		if router == nil then error("router entry missing") end
		if router.kind ~= "ns.requirement" then error("kind mismatch") end
		if router.meta.description ~= "Router" then error("meta mismatch") end
		if router.data.default ~= "app:router" then error("raw data mismatch") end

		local filtered, ferr = pkg:entries({ kind = "function.lua", include_data = false })
		if ferr then error(ferr) end
		if #filtered ~= 1 then error("filtered count mismatch: " .. tostring(#filtered)) end
		if filtered[1].id ~= "wippy.dummy:ping" then error("filtered id mismatch") end
		if filtered[1].data ~= nil then error("expected no data when include_data=false") end

		local resources, rerr = pkg:resources()
		if rerr then error(rerr) end
		if #resources ~= 1 then error("resources count mismatch: " .. tostring(#resources)) end
		if resources[1].id ~= "wippy.dummy:assets" then error("resource id mismatch: " .. tostring(resources[1].id)) end
		if resources[1].type ~= "tree" then error("resource type mismatch: " .. tostring(resources[1].type)) end
		if resources[1].file_count ~= 1 then error("file_count mismatch: " .. tostring(resources[1].file_count)) end

		local vfs, verr = pkg:fs("wippy.dummy:assets")
		if verr then error(verr) end
		local body, berr = vfs:read_file("config/app.yaml")
		if berr then error(berr) end
		if body ~= "name: dummy\n" then error("fs read mismatch: " .. tostring(body)) end

		local ok, cerr = pkg:close()
		if cerr then error(cerr) end
		if ok ~= true then error("close did not return true") end

		local _, closedErr = pkg:metadata()
		if closedErr == nil then error("expected error after close") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	require.Equal(t, 1, downloads)
}

func TestVersionsOpenResourcesAndFileRead(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappWithResourceForHubTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}},
		wapp.NewID("wippy.dummy", "assets"),
		map[string]string{"data/readme.txt": "hello"},
	)

	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))

	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end

		local resources, rserr = pkg:resources()
		if rserr then error(rserr) end
		if #resources ~= 1 then error("resources count mismatch") end
		if resources[1].id ~= "wippy.dummy:assets" then error("resource id mismatch") end

		local vfs, verr = pkg:fs("wippy.dummy:assets")
		if verr then error(verr) end

		local body, rerr = vfs:read_file("data/readme.txt")
		if rerr then error(rerr) end
		if body ~= "hello" then error("read_file mismatch: " .. tostring(body)) end

		local slashBody, serr = vfs:read_file("/data/readme.txt")
		if serr then error(serr) end
		if slashBody ~= "hello" then error("leading slash read mismatch") end

		local _, missErr = vfs:read_file("data/missing.txt")
		if missErr == nil then error("expected error for missing file") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func writeCacheArtifact(t *testing.T, vendorDir, org, module, version string) string {
	t.Helper()
	dir := filepath.Join(vendorDir, org)
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, module+"-"+version+".wapp")
	require.NoError(t, os.WriteFile(path, []byte("cached artifact bytes"), 0600))
	return path
}

func writeLockFile(t *testing.T, dir string, modules ...[2]string) {
	t.Helper()
	content := "directories:\n  modules: .wippy\n  src: .\n"
	if len(modules) > 0 {
		content += "modules:\n"
		for _, mod := range modules {
			content += "  - name: " + mod[0] + "\n    version: " + mod[1] + "\n"
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wippy.lock"), []byte(content), 0600))
}

func TestCacheListRemovePrune(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")

	pinnedPath := writeCacheArtifact(t, vendorDir, "wippy", "pinned", "v1.0.0")
	orphanPath := writeCacheArtifact(t, vendorDir, "wippy", "orphan", "v2.0.0")
	writeLockFile(t, root, [2]string{"wippy/pinned", "v1.0.0"})

	l := newReadSurfaceState(t, artifactClientReturning(t, nil, nil))

	if err := l.DoString(`
		local list, err = hub.cache.list()
		if err then error(err) end
		if #list ~= 2 then error("cache list count mismatch: " .. tostring(#list)) end

		local pinned, orphan
		for _, e in ipairs(list) do
			if e.module == "wippy/pinned" then pinned = e end
			if e.module == "wippy/orphan" then orphan = e end
		end
		if pinned == nil then error("pinned entry missing") end
		if orphan == nil then error("orphan entry missing") end
		if pinned.pinned ~= true then error("pinned flag wrong") end
		if pinned.version ~= "v1.0.0" then error("pinned version mismatch: " .. tostring(pinned.version)) end
		if orphan.pinned ~= false then error("orphan pinned flag wrong") end
		if orphan.version ~= "v2.0.0" then error("orphan version mismatch: " .. tostring(orphan.version)) end

		local _, refuseErr = hub.cache.remove("wippy/pinned", "v1.0.0")
		if refuseErr == nil then error("expected refusal removing pinned artifact") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	require.FileExists(t, pinnedPath)
	require.FileExists(t, orphanPath)

	if err := l.DoString(`
		local ok, err = hub.cache.remove("wippy/orphan", "v2.0.0")
		if err then error(err) end
		if ok ~= true then error("remove did not return true") end

		local forced, ferr = hub.cache.remove("wippy/pinned", "v1.0.0", { force = true })
		if ferr then error(ferr) end
		if forced ~= true then error("forced remove did not return true") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	require.NoFileExists(t, orphanPath)
	require.NoFileExists(t, pinnedPath)
}

func TestCachePruneDryRun(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")

	pinnedPath := writeCacheArtifact(t, vendorDir, "wippy", "pinned", "v1.0.0")
	orphanPath := writeCacheArtifact(t, vendorDir, "wippy", "orphan", "v2.0.0")
	writeLockFile(t, root, [2]string{"wippy/pinned", "v1.0.0"})

	l := newReadSurfaceState(t, artifactClientReturning(t, nil, nil))

	if err := l.DoString(`
		local dry, derr = hub.cache.prune({ dry_run = true })
		if derr then error(derr) end
		if #dry ~= 1 then error("dry run prune count mismatch: " .. tostring(#dry)) end
		if dry[1].module ~= "wippy/orphan" then error("dry run module mismatch") end
	`); err != nil {
		t.Fatalf("lua dry-run error: %v", err)
	}

	require.FileExists(t, orphanPath, "dry run must not delete")

	if err := l.DoString(`
		local pruned, perr = hub.cache.prune()
		if perr then error(perr) end
		if #pruned ~= 1 then error("prune count mismatch: " .. tostring(#pruned)) end
		if pruned[1].module ~= "wippy/orphan" then error("prune module mismatch") end
	`); err != nil {
		t.Fatalf("lua prune error: %v", err)
	}

	require.NoFileExists(t, orphanPath)
	require.FileExists(t, pinnedPath, "pinned artifact must survive prune")
}
