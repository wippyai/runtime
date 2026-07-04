// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	boothub "github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/wapp"
)

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
	l.SetContext(setupStrictContext())
	tbl, _ := mod.Build()
	l.SetGlobal(mod.Name, tbl)
	return l
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

func TestCacheListEmptyVendor(t *testing.T) {
	t.Chdir(t.TempDir())
	l := newReadSurfaceState(t, &fakeArtifactClient{})
	if err := l.DoString(`
		local list, err = hub.cache.list()
		if err then error(err) end
		if #list ~= 0 then error("expected empty list, got " .. tostring(#list)) end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestCacheListFlagsAndSizes(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")
	pinnedPath := writeCacheArtifact(t, vendorDir, "wippy", "pinned", "v1.0.0")
	orphanPath := writeCacheArtifact(t, vendorDir, "wippy", "orphan", "v2.0.0")
	writeLockFile(t, root, [2]string{"wippy/pinned", "v1.0.0"})

	l := newReadSurfaceState(t, &fakeArtifactClient{})
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
		if orphan.pinned ~= false then error("orphan flag wrong") end
		if pinned.version ~= "v1.0.0" then error("pinned version mismatch") end
		if orphan.version ~= "v2.0.0" then error("orphan version mismatch") end
		if pinned.size <= 0 then error("pinned size must be positive") end
		if orphan.size <= 0 then error("orphan size must be positive") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
	require.FileExists(t, pinnedPath)
	require.FileExists(t, orphanPath)
}

func TestCacheRemoveArgumentErrors(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	l := newReadSurfaceState(t, &fakeArtifactClient{})
	if err := l.DoString(`
		local ok, err = hub.cache.remove("", "v1.0.0")
		if err == nil then error("expected error for missing module") end
		if ok ~= nil then error("expected nil result for missing module") end

		local ok2, err2 = hub.cache.remove("wippy/mod", "")
		if err2 == nil then error("expected error for missing version") end
		if ok2 ~= nil then error("expected nil result for missing version") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestCacheRemovePinnedAndOrphan(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")
	pinnedPath := writeCacheArtifact(t, vendorDir, "wippy", "pinned", "v1.0.0")
	orphanPath := writeCacheArtifact(t, vendorDir, "wippy", "orphan", "v2.0.0")
	writeLockFile(t, root, [2]string{"wippy/pinned", "v1.0.0"})

	l := newReadSurfaceState(t, &fakeArtifactClient{})

	// Refusal without force; file survives.
	if err := l.DoString(`
		local ok, err = hub.cache.remove("wippy/pinned", "v1.0.0")
		if err == nil then error("expected refusal removing pinned artifact") end
		if ok ~= nil then error("expected nil result on refusal") end
	`); err != nil {
		t.Fatalf("lua refusal error: %v", err)
	}
	require.FileExists(t, pinnedPath)

	// Orphan removed without force.
	if err := l.DoString(`
		local ok, err = hub.cache.remove("wippy/orphan", "v2.0.0")
		if err then error(err) end
		if ok ~= true then error("orphan remove did not return true") end
	`); err != nil {
		t.Fatalf("lua orphan error: %v", err)
	}
	require.NoFileExists(t, orphanPath)

	// Pinned removed with force.
	if err := l.DoString(`
		local ok, err = hub.cache.remove("wippy/pinned", "v1.0.0", { force = true })
		if err then error(err) end
		if ok ~= true then error("forced remove did not return true") end
	`); err != nil {
		t.Fatalf("lua force error: %v", err)
	}
	require.NoFileExists(t, pinnedPath)
}

func TestCachePruneAllPinnedNoop(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")
	a := writeCacheArtifact(t, vendorDir, "wippy", "one", "v1.0.0")
	b := writeCacheArtifact(t, vendorDir, "wippy", "two", "v2.0.0")
	writeLockFile(t, root,
		[2]string{"wippy/one", "v1.0.0"},
		[2]string{"wippy/two", "v2.0.0"})

	l := newReadSurfaceState(t, &fakeArtifactClient{})
	if err := l.DoString(`
		local pruned, err = hub.cache.prune()
		if err then error(err) end
		if #pruned ~= 0 then error("expected nothing pruned, got " .. tostring(#pruned)) end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
	require.FileExists(t, a)
	require.FileExists(t, b)
}

func TestCachePruneDryRunAndReal(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")
	pinnedPath := writeCacheArtifact(t, vendorDir, "wippy", "pinned", "v1.0.0")
	orphanPath := writeCacheArtifact(t, vendorDir, "wippy", "orphan", "v2.0.0")
	writeLockFile(t, root, [2]string{"wippy/pinned", "v1.0.0"})

	l := newReadSurfaceState(t, &fakeArtifactClient{})

	if err := l.DoString(`
		local dry, err = hub.cache.prune({ dry_run = true })
		if err then error(err) end
		if #dry ~= 1 then error("dry run count mismatch: " .. tostring(#dry)) end
		if dry[1].module ~= "wippy/orphan" then error("dry run module mismatch") end
		if dry[1].pinned ~= false then error("dry run entry must be unpinned") end
	`); err != nil {
		t.Fatalf("lua dry-run error: %v", err)
	}
	require.FileExists(t, orphanPath, "dry run must not delete")
	require.FileExists(t, pinnedPath)

	if err := l.DoString(`
		local pruned, err = hub.cache.prune()
		if err then error(err) end
		if #pruned ~= 1 then error("prune count mismatch: " .. tostring(#pruned)) end
		if pruned[1].module ~= "wippy/orphan" then error("prune module mismatch") end
	`); err != nil {
		t.Fatalf("lua prune error: %v", err)
	}
	require.NoFileExists(t, orphanPath)
	require.FileExists(t, pinnedPath, "pinned artifact must survive prune")
}

func TestCachePermissionDenied(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")
	pinnedPath := writeCacheArtifact(t, vendorDir, "wippy", "pinned", "v1.0.0")
	writeLockFile(t, root, [2]string{"wippy/pinned", "v1.0.0"})

	l := newStrictReadSurfaceState(t, &fakeArtifactClient{})
	if err := l.DoString(`
		local _, lerr = hub.cache.list()
		if lerr == nil then error("expected permission denied for list") end

		local _, rerr = hub.cache.remove("wippy/pinned", "v1.0.0")
		if rerr == nil then error("expected permission denied for remove") end

		local _, perr = hub.cache.prune()
		if perr == nil then error("expected permission denied for prune") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}
	// Nothing was touched under strict denial.
	require.FileExists(t, pinnedPath)
}
