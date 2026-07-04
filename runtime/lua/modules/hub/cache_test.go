// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestCacheRemoveRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".wippy", "vendor"), 0755))
	writeLockFile(t, root)

	// A file outside the vendor directory that an unchecked traversal would hit:
	// filepath.Join(vendorDir, "wippy/x-../../../../outside.wapp") resolves here.
	outside := filepath.Join(root, "outside.wapp")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0600))

	l := newReadSurfaceState(t, artifactClientReturning(t, nil, nil))

	if err := l.DoString(`
		local ok, err = hub.cache.remove("wippy/x", "../../../../outside")
		if err == nil then error("expected error removing a traversing version") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	require.FileExists(t, outside, "path traversal must not delete files outside the cache")
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

func TestParseWappRelPath(t *testing.T) {
	cases := []struct {
		rel     string
		module  string
		version string
	}{
		{"wippy/pinned-v1.0.0.wapp", "wippy/pinned", "v1.0.0"},
		{"wippy/orphan-v1.2.3-beta.1.wapp", "wippy/orphan", "v1.2.3-beta.1"},
		{"wippy/my-module-v2.0.0.wapp", "wippy/my-module", "v2.0.0"},
		{"wippy/tool-v1.0.0-rc.2.wapp", "wippy/tool", "v1.0.0-rc.2"},
		{"org/api-v2-v1.0.0.wapp", "org/api-v2", "v1.0.0"},
		{"org/mod.wapp", "org/mod", ""},
	}
	for _, tc := range cases {
		t.Run(tc.rel, func(t *testing.T) {
			module, version := parseWappRelPath(tc.rel)
			require.Equal(t, tc.module, module)
			require.Equal(t, tc.version, version)
		})
	}
}

func TestCacheRemoveRejectsModuleSwap(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	vendorDir := filepath.Join(root, ".wippy", "vendor")

	// A pinned artifact that a crafted version could normalize onto.
	victim := writeCacheArtifact(t, vendorDir, "wippy", "other", "v1.0.0")
	writeLockFile(t, root, [2]string{"wippy/other", "v1.0.0"})

	l := newReadSurfaceState(t, artifactClientReturning(t, nil, nil))

	if err := l.DoString(`
		local ok, err = hub.cache.remove("wippy/mod", "x/../other-v1.0.0")
		if err == nil then error("expected error for version with separators") end
	`); err != nil {
		t.Fatalf("lua error: %v", err)
	}

	require.FileExists(t, victim, "a crafted version must not delete another module's artifact")
}
