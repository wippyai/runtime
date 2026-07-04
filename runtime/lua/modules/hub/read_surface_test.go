// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// buildWappWithMetadataForHubTest packs an entries-only artifact with arbitrary
// top-level metadata so metadata fidelity can be asserted across many keys.
func buildWappWithMetadataForHubTest(t *testing.T, meta wapp.Metadata, entries []wapp.Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackEntries(meta, entries, &buf))
	return buf.Bytes()
}

// newStrictReadSurfaceState mirrors newReadSurfaceState but installs a strict
// security context so permission checks deny by default.
func newStrictReadSurfaceState(t *testing.T, fake *fakeArtifactClient) *lua.LState {
	t.Helper()
	mod := NewModule(Options{ArtifactClient: fake})
	l := lua.NewState()
	t.Cleanup(l.Close)
	l.SetContext(hubTestStoreContext(setupStrictContext(), t))
	tbl, _ := mod.Build()
	l.SetGlobal(mod.Name, tbl)
	return l
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

func TestVersionsOpenArgumentAndPermissionErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappBytesForHubModuleTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}})

	// Malformed module ref and missing/empty version each fail before any download.
	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/", "v0.1.2")
		if err == nil then error("expected error for malformed module ref") end
		if pkg ~= nil then error("expected nil pkg for malformed module ref") end

		local pkg2, err2 = hub.versions.open("wippy/dummy")
		if err2 == nil then error("expected error for missing version") end
		if pkg2 ~= nil then error("expected nil pkg for missing version") end

		local pkg3, err3 = hub.versions.open("wippy/dummy", "")
		if err3 == nil then error("expected error for empty version") end
		if pkg3 ~= nil then error("expected nil pkg for empty version") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	// Strict context with no grant denies open even for a well-formed call.
	strict := newStrictReadSurfaceState(t, artifactClientReturning(t, artifact, nil))
	if err := strict.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err == nil then error("expected permission denied") end
		if pkg ~= nil then error("expected nil pkg on permission denied") end
	`); err != nil {
		t.Fatalf("lua strict error: %v", err)
	}
}

func TestVersionsOpenDownloadFailureSurfaced(t *testing.T) {
	t.Chdir(t.TempDir())
	fake := &fakeArtifactClient{
		getDownloadFn: func(_ context.Context, _ *boothub.DownloadParams) (*boothub.DownloadInfo, error) {
			return &boothub.DownloadInfo{URL: "memory://dummy", Version: "v0.1.2"}, nil
		},
		downloadFn: func(_ context.Context, _, _ string) error {
			return errors.New("network unreachable")
		},
	}

	l := newReadSurfaceState(t, fake)
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err == nil then error("expected download error") end
		if pkg ~= nil then error("expected nil pkg on download failure") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsOpenMetadataAllKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappWithMetadataForHubTest(t,
		wapp.Metadata{
			"name":        "dummy",
			"version":     "v0.1.2",
			"description": "a dummy module",
		},
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}})

	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		local meta, merr = pkg:metadata()
		if merr then error(merr) end
		if meta.name ~= "dummy" then error("name mismatch: " .. tostring(meta.name)) end
		if meta.version ~= "v0.1.2" then error("version mismatch: " .. tostring(meta.version)) end
		if meta.description ~= "a dummy module" then error("description mismatch: " .. tostring(meta.description)) end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsOpenEntriesKindFilterAndRawData(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappBytesForHubModuleTest(t, []wapp.Entry{
		{
			ID:   wapp.NewID("wippy.dummy", "router"),
			Kind: "ns.requirement",
			Meta: wapp.Metadata{"description": "Router"},
			Data: map[string]any{
				"url":          "${env:SECRET}",
				"password_env": "DB_PASS",
			},
		},
		{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"},
		{ID: wapp.NewID("wippy.dummy", "bare"), Kind: "library.lua"},
	})

	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end

		-- Array of kinds returns the union.
		local union, uerr = pkg:entries({ kind = {"function.lua", "ns.requirement"} })
		if uerr then error(uerr) end
		if #union ~= 2 then error("union count mismatch: " .. tostring(#union)) end
		local seen = {}
		for _, e in ipairs(union) do seen[e.kind] = true end
		if not seen["function.lua"] then error("union missing function.lua") end
		if not seen["ns.requirement"] then error("union missing ns.requirement") end

		-- A kind matching nothing yields an empty array.
		local none, nerr = pkg:entries({ kind = {"does.not.exist"} })
		if nerr then error(nerr) end
		if #none ~= 0 then error("expected empty array, got " .. tostring(#none)) end

		-- Default (no opts) includes data.
		local all, aerr = pkg:entries()
		if aerr then error(aerr) end
		if #all ~= 3 then error("all count mismatch: " .. tostring(#all)) end

		-- RAW sealing: placeholder + *_env stay literal, never resolved.
		local router
		for _, e in ipairs(all) do
			if e.id == "wippy.dummy:router" then router = e end
		end
		if router == nil then error("router entry missing") end
		if router.data.url ~= "${env:SECRET}" then error("env placeholder resolved: " .. tostring(router.data.url)) end
		if router.data.password_env ~= "DB_PASS" then error("password_env mismatch: " .. tostring(router.data.password_env)) end

		-- Entry with no meta and no data decodes cleanly.
		local ping
		for _, e in ipairs(all) do
			if e.id == "wippy.dummy:ping" then ping = e end
		end
		if ping == nil then error("ping entry missing") end
		if ping.data ~= nil and next(ping.data) ~= nil then error("expected no data for bare entry") end
		if ping.meta ~= nil and next(ping.meta) ~= nil then error("expected empty meta for bare entry") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsOpenEntryDataTypeFidelity(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappBytesForHubModuleTest(t, []wapp.Entry{
		{
			ID:   wapp.NewID("wippy.dummy", "config"),
			Kind: "config",
			Data: map[string]any{
				"str":    "hello",
				"num":    42,
				"flag":   true,
				"arr":    []any{1, 2, 3},
				"nested": map[string]any{"inner": "value"},
			},
		},
	})

	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		local entries, eerr = pkg:entries()
		if eerr then error(eerr) end
		local d = entries[1].data
		if type(d.str) ~= "string" or d.str ~= "hello" then error("str fidelity") end
		if type(d.num) ~= "number" or d.num ~= 42 then error("num fidelity") end
		if type(d.flag) ~= "boolean" or d.flag ~= true then error("flag fidelity") end
		if type(d.arr) ~= "table" or #d.arr ~= 3 then error("arr fidelity") end
		if d.arr[1] ~= 1 or d.arr[2] ~= 2 or d.arr[3] ~= 3 then error("arr element fidelity") end
		if type(d.nested) ~= "table" or d.nested.inner ~= "value" then error("nested fidelity") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsOpenResourcesEmptyAndMultiFile(t *testing.T) {
	t.Chdir(t.TempDir())

	// No resource: resources() is empty and fs() errors.
	noRes := buildWappBytesForHubModuleTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}})
	l := newReadSurfaceState(t, artifactClientReturning(t, noRes, nil))
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		local res, rerr = pkg:resources()
		if rerr then error(rerr) end
		if #res ~= 0 then error("expected empty resources, got " .. tostring(#res)) end
		local vfs, verr = pkg:fs("wippy.dummy:assets")
		if verr == nil then error("expected error opening fs with no resource") end
		if vfs ~= nil then error("expected nil fs handle") end
	`); err != nil {
		t.Fatalf("lua no-resource error: %v", err)
	}

	// Multi-file resource: file_count, hash, size, and type all present.
	multi := buildWappWithResourceForHubTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}},
		wapp.NewID("wippy.dummy", "assets"),
		map[string]string{
			"one.txt":       "aaa",
			"two.txt":       "bbbb",
			"sub/three.txt": "cc",
		},
	)
	l2 := newReadSurfaceState(t, artifactClientReturning(t, multi, nil))
	if err := l2.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		local res, rerr = pkg:resources()
		if rerr then error(rerr) end
		if #res ~= 1 then error("resource count mismatch: " .. tostring(#res)) end
		local r = res[1]
		if r.id ~= "wippy.dummy:assets" then error("resource id mismatch: " .. tostring(r.id)) end
		if r.type ~= "tree" then error("resource type mismatch: " .. tostring(r.type)) end
		if r.file_count ~= 3 then error("file_count mismatch: " .. tostring(r.file_count)) end
		if type(r.hash) ~= "string" or #r.hash == 0 then error("hash missing") end
		if r.size <= 0 then error("size must be positive: " .. tostring(r.size)) end
	`); err != nil {
		t.Fatalf("lua multi-file error: %v", err)
	}
}

func TestVersionsOpenFSDeep(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappWithResourceForHubTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}},
		wapp.NewID("wippy.dummy", "assets"),
		map[string]string{
			"root.txt":  "top",
			"a/b/c.txt": "deep",
			"a/b/d.txt": "sibling",
		},
	)

	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end

		-- Unknown resource id errors.
		local _, verr = pkg:fs("wippy.dummy:missing")
		if verr == nil then error("expected error for unknown resource id") end

		local vfs, ferr = pkg:fs("wippy.dummy:assets")
		if ferr then error(ferr) end

		-- readdir at root collects entries via the iterator.
		local top = {}
		for e in vfs:readdir(".") do top[e.name] = e.type end
		if top["root.txt"] ~= "file" then error("root.txt not listed as file") end
		if top["a"] ~= "directory" then error("a not listed as directory") end

		-- readdir into a nested path collects both files.
		local nested = {}
		for e in vfs:readdir("a/b") do nested[e.name] = e.type end
		if nested["c.txt"] ~= "file" then error("c.txt missing in a/b") end
		if nested["d.txt"] ~= "file" then error("d.txt missing in a/b") end

		-- Nested read.
		local body, berr = vfs:read_file("a/b/c.txt")
		if berr then error(berr) end
		if body ~= "deep" then error("nested read mismatch: " .. tostring(body)) end

		-- stat on a file reports size and non-dir.
		local st, serr = vfs:stat("a/b/c.txt")
		if serr then error(serr) end
		if st.is_dir ~= false then error("expected file, not dir") end
		if st.size ~= 4 then error("stat size mismatch: " .. tostring(st.size)) end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsOpenHandleLifecycle(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappWithResourceForHubTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}},
		wapp.NewID("wippy.dummy", "assets"),
		map[string]string{"readme.txt": "hi"},
	)

	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))
	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end

		local ok, cerr = pkg:close()
		if cerr then error(cerr) end
		if ok ~= true then error("close did not return true") end

		-- Every accessor errors after close.
		local _, m = pkg:metadata()
		if m == nil then error("metadata must error after close") end
		local _, e = pkg:entries()
		if e == nil then error("entries must error after close") end
		local _, r = pkg:resources()
		if r == nil then error("resources must error after close") end
		local _, f = pkg:fs("wippy.dummy:assets")
		if f == nil then error("fs must error after close") end

		-- Double close is idempotent.
		local ok2, cerr2 = pkg:close()
		if cerr2 then error(cerr2) end
		if ok2 ~= true then error("second close did not return true") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestVersionsOpenTwoIndependentPackages(t *testing.T) {
	t.Chdir(t.TempDir())
	artifactA := buildWappBytesForHubModuleTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.alpha", "one"), Kind: "function.lua"}})
	artifactB := buildWappBytesForHubModuleTest(t,
		[]wapp.Entry{
			{ID: wapp.NewID("wippy.beta", "two"), Kind: "library.lua"},
			{ID: wapp.NewID("wippy.beta", "three"), Kind: "config"},
		})

	sumA := sha256.Sum256(artifactA)
	sumB := sha256.Sum256(artifactB)
	digestA := "sha256:" + hex.EncodeToString(sumA[:])
	digestB := "sha256:" + hex.EncodeToString(sumB[:])

	fake := &fakeArtifactClient{
		getDownloadFn: func(_ context.Context, params *boothub.DownloadParams) (*boothub.DownloadInfo, error) {
			if params.Module == "alpha" {
				return &boothub.DownloadInfo{URL: "memory://alpha", Version: "v1.0.0", Digest: digestA}, nil
			}
			return &boothub.DownloadInfo{URL: "memory://beta", Version: "v2.0.0", Digest: digestB}, nil
		},
		downloadFn: func(_ context.Context, url, destPath string) error {
			if url == "memory://alpha" {
				return os.WriteFile(destPath, artifactA, 0600)
			}
			return os.WriteFile(destPath, artifactB, 0600)
		},
	}

	l := newReadSurfaceState(t, fake)
	if err := l.DoString(`
		local a, aerr = hub.versions.open("wippy/alpha", "v1.0.0")
		if aerr then error(aerr) end
		local b, berr = hub.versions.open("wippy/beta", "v2.0.0")
		if berr then error(berr) end

		local ea = a:entries()
		local eb = b:entries()
		if #ea ~= 1 then error("alpha entry count mismatch: " .. tostring(#ea)) end
		if ea[1].id ~= "wippy.alpha:one" then error("alpha id mismatch: " .. tostring(ea[1].id)) end
		if #eb ~= 2 then error("beta entry count mismatch: " .. tostring(#eb)) end

		if a.version ~= "v1.0.0" then error("alpha version mismatch") end
		if b.version ~= "v2.0.0" then error("beta version mismatch") end
		if a.digest == b.digest then error("expected distinct digests") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// TestPackageHandleAutoClosedByStore verifies the frame-end contract that keeps
// Windows TempDir cleanup working: newPackageHandle registers an AddCleanup on
// the resource store, so closing the store (request end) releases the open .wapp
// even when the caller never calls pkg:close().
func TestPackageHandleAutoClosedByStore(t *testing.T) {
	t.Chdir(t.TempDir())
	artifact := buildWappBytesForHubModuleTest(t, []wapp.Entry{
		{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"},
	})
	require.NoError(t, os.WriteFile("pkg.wapp", artifact, 0600))

	ctx := setupContext()
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))

	file, reader, err := openArtifactReader("pkg.wapp")
	require.NoError(t, err)
	h := newPackageHandle(ctx, file, reader, "v0.1.2", "sha256:test")
	require.False(t, h.isClosed())

	require.NoError(t, store.Close())
	require.True(t, h.isClosed(), "closing the resource store must release the handle")

	require.NoError(t, h.Close()) // idempotent after store-driven close
}

func findCachedWapp(t *testing.T, root string) string {
	t.Helper()
	var found string
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".wapp") {
			found = p
		}
		return nil
	}))
	require.NotEmpty(t, found, "no cached .wapp found under %s", root)
	return found
}

// A cache entry that passes digest/size verification but is not a readable
// WAPP (e.g. the registry omitted a digest and the file is truncated) must be
// evicted and re-downloaded, not returned as-is.
func TestVersionsOpenReDownloadsCorruptCache(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	artifact := buildWappWithResourceForHubTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}},
		wapp.NewID("wippy.dummy", "assets"),
		map[string]string{"a.txt": "hi"})

	var downloads int
	// No digest/size, so VerifyDownloadedArtifact is a no-op and cannot catch
	// a corrupt cache entry on its own.
	client := &fakeArtifactClient{
		getDownloadFn: func(_ context.Context, _ *boothub.DownloadParams) (*boothub.DownloadInfo, error) {
			return &boothub.DownloadInfo{URL: "memory://dummy", Version: "v0.1.2"}, nil
		},
		downloadFn: func(_ context.Context, _, destPath string) error {
			downloads++
			return os.WriteFile(destPath, artifact, 0600)
		},
	}
	l := newReadSurfaceState(t, client)

	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		pkg:close()
	`); err != nil {
		t.Fatalf("first open: %v", err)
	}
	require.Equal(t, 1, downloads)

	cached := findCachedWapp(t, root)
	require.NoError(t, os.WriteFile(cached, []byte("not a wapp"), 0600))

	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		pkg:close()
	`); err != nil {
		t.Fatalf("second open after corruption: %v", err)
	}
	require.Equal(t, 2, downloads, "corrupt cache must trigger a re-download")
}

// A directories.modules override must apply to the read-surface cache too, so
// hub.versions.open caches where hub.cache.* looks.
func TestVersionsOpenCachesInLockResolvedVendorDir(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.lock"),
		[]byte("directories:\n  modules: custom-mods\n  src: .\n"), 0600))

	artifact := buildWappWithResourceForHubTest(t,
		[]wapp.Entry{{ID: wapp.NewID("wippy.dummy", "ping"), Kind: "function.lua"}},
		wapp.NewID("wippy.dummy", "assets"),
		map[string]string{"a.txt": "hi"})

	l := newReadSurfaceState(t, artifactClientReturning(t, artifact, nil))

	if err := l.DoString(`
		local pkg, err = hub.versions.open("wippy/dummy", "v0.1.2")
		if err then error(err) end
		pkg:close()

		local list, lerr = hub.cache.list()
		if lerr then error(lerr) end
		if #list < 1 then error("opened artifact not visible to cache.list under override") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	require.DirExists(t, filepath.Join(root, "custom-mods", "vendor"),
		"open must cache under the lock-resolved vendor dir")
	require.NoDirExists(t, filepath.Join(root, ".wippy", "vendor"),
		"open must not fall back to the hard-coded default vendor dir")
}
