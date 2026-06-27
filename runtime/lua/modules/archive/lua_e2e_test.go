// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	secapi "github.com/wippyai/runtime/api/security"
	fsmod "github.com/wippyai/runtime/runtime/lua/modules/fs"
	"github.com/wippyai/runtime/service/fs/directory"
)

func setupEngine(t *testing.T) (*lua.LState, string, *resource.Store) {
	t.Helper()
	dir := t.TempDir()
	fsys, err := directory.NewFS(dir, 0o755, false)
	require.NoError(t, err)

	ctx := secapi.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))

	l := lua.NewState()
	l.SetContext(ctx)
	t.Cleanup(func() {
		l.Close()
		_ = store.Close()
		_ = fsys.Close()
	})

	atbl, _ := Module.Build()
	l.SetGlobal("archive", atbl)

	ud := l.NewUserData()
	ud.Value = fsmod.NewFS(fsys, ".")
	l.SetGlobal("appfs", ud)

	return l, dir, store
}

func run(t *testing.T, l *lua.LState, script string) {
	t.Helper()
	if err := l.DoString(script); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

func TestLuaZipRoundTripAllApis(t *testing.T) {
	l, dir, _ := setupEngine(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "source.bin"), []byte("streamed-from-fs"), 0o644))

	run(t, l, `
		-- create with add / add_file / add_dir
		local w, err = archive.create(appfs, "out.zip")
		if not w then error("create: "..tostring(err)) end
		assert(w:add("notes.txt", "hello world"))
		assert(w:add_dir("docs"))
		assert(w:add_file("docs/source.bin", appfs, "source.bin"))
		assert(w:close())

		-- open random reader and exercise entries/stat/read/stream
		local r, err = archive.open(appfs, "out.zip")
		assert(r, err)
		local names = {}
		for e in r:entries() do names[e.name] = e end
		assert(names["notes.txt"], "missing notes.txt")
		assert(names["docs/source.bin"], "missing source.bin")

		local info, err = r:stat("notes.txt")
		assert(info and info.size == 11, "bad stat size")

		local data, err = r:read("notes.txt")
		assert(data == "hello world", "read mismatch: "..tostring(data))

		local s, err = r:stream("docs/source.bin")
		assert(s, err)            -- a stream.Stream userdata
		assert(s.read, "stream has no read method")

		-- extract everything into out/
		local n, err = r:extract_all(appfs, { prefix = "extracted/" })
		assert(n and n >= 2, "extract_all count: "..tostring(n))
		assert(r:close())
	`)

	got, err := os.ReadFile(filepath.Join(dir, "extracted", "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello world", string(got))
	got, err = os.ReadFile(filepath.Join(dir, "extracted", "docs", "source.bin"))
	require.NoError(t, err)
	require.Equal(t, "streamed-from-fs", string(got))
}

func TestLuaScanSequential(t *testing.T) {
	l, dir, _ := setupEngine(t)

	run(t, l, `
		local w = assert(archive.create(appfs, "s.zip"))
		assert(w:add("a.txt", "alpha"))
		assert(w:add("b.txt", "bravo"))
		assert(w:close())
	`)
	zipBytes, err := os.ReadFile(filepath.Join(dir, "s.zip"))
	require.NoError(t, err)
	l.SetGlobal("zipbytes", lua.LString(zipBytes))

	run(t, l, `
		-- forward-only scan over an in-memory byte source
		local s, err = archive.scan(zipbytes, { format = "zip" })
		assert(s, err)
		local count = 0
		for e in s:walk() do count = count + 1 end
		assert(count == 2, "walked "..count.." entries")
		assert(s:close())

		-- scan + extract_all (streaming to fs, no dispatcher needed)
		local s2 = assert(archive.scan(zipbytes, { format = "zip" }))
		local n = assert(s2:extract_all(appfs, { prefix = "unz/" }))
		assert(n == 2, "extracted "..tostring(n))
		assert(s2:close())
	`)
	got, err := os.ReadFile(filepath.Join(dir, "unz", "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "alpha", string(got))
}

func TestLuaTarRoundTrip(t *testing.T) {
	l, dir, _ := setupEngine(t)
	run(t, l, `
		local w = assert(archive.create(appfs, "out.tar", { format = "tar" }))
		assert(w:add("x.txt", "tar-content"))
		assert(w:close())
		local r = assert(archive.open(appfs, "out.tar"))
		assert(r:read("x.txt") == "tar-content")
		assert(r:extract_all(appfs, { prefix = "t/" }))
		assert(r:close())
	`)
	got, err := os.ReadFile(filepath.Join(dir, "t", "x.txt"))
	require.NoError(t, err)
	require.Equal(t, "tar-content", string(got))
}

func TestLuaErrorPaths(t *testing.T) {
	l, _, _ := setupEngine(t)
	run(t, l, `
		-- unknown format
		local r, err = archive.open("not an archive at all")
		assert(r == nil and err ~= nil, "expected unknown-format error")

		-- read() guard above max_inline_bytes
		local w = assert(archive.create(appfs, "big.zip"))
		assert(w:add("big.txt", string.rep("x", 4096)))
		assert(w:close())
		local rr = assert(archive.open(appfs, "big.zip", { max_inline_bytes = 16 }))
		local data, e = rr:read("big.txt")
		assert(data == nil and e ~= nil, "expected inline-size error")
		-- but stream() still works for the large entry
		assert(rr:stream("big.txt"))
		assert(rr:close())

		-- formats() lists the built-ins
		local fmts = archive.formats()
		local set = {}
		for _, f in ipairs(fmts) do set[f] = true end
		assert(set["zip"] and set["tar"] and set["tar.gz"] and set["tar.zst"], "missing formats")
	`)
}

// TestLuaWalkDropsStreams proves walk() does not accumulate one resource-table
// entry per archive member: each yielded stream is dropped as the walk advances.
func TestLuaWalkDropsStreams(t *testing.T) {
	l, dir, store := setupEngine(t)
	run(t, l, `
		local w = assert(archive.create(appfs, "multi.zip"))
		for i = 1, 8 do assert(w:add("f"..i..".txt", "data"..i)) end
		assert(w:close())
	`)
	zipBytes, err := os.ReadFile(filepath.Join(dir, "multi.zip"))
	require.NoError(t, err)
	l.SetGlobal("zipbytes", lua.LString(zipBytes))

	run(t, l, `
		local s = assert(archive.scan(zipbytes, { format = "zip" }))
		local n = 0
		for e, entry in s:walk() do n = n + 1 end
		assert(n == 8, "walked "..n.." entries")
		assert(s:close())
	`)

	if got := store.Table().Len(); got > 1 {
		t.Fatalf("resource table retained %d entries after walking 8; streams not dropped", got)
	}
}

// TestLuaMaxTotalBytes proves the cumulative uncompressed cap is enforced on
// extract_all (decompression-bomb defense).
func TestLuaMaxTotalBytes(t *testing.T) {
	l, dir, _ := setupEngine(t)
	run(t, l, `
		local w = assert(archive.create(appfs, "tot.zip"))
		for i = 1, 3 do assert(w:add("f"..i..".txt", string.rep("x", 1000))) end
		assert(w:close())

		local r = assert(archive.open(appfs, "tot.zip", { max_total_bytes = 1500 }))
		local n, err = r:extract_all(appfs, { prefix = "out/" })
		assert(n == nil and err ~= nil, "expected max_total_bytes error, got n="..tostring(n))
		assert(r:close())

		-- a generous cap lets the same archive through
		local r2 = assert(archive.open(appfs, "tot.zip", { max_total_bytes = 1 << 20 }))
		assert(r2:extract_all(appfs, { prefix = "ok/" }) == 3)
		assert(r2:close())
	`)
	if _, err := os.Stat(filepath.Join(dir, "out", "f1.txt")); err != nil {
		t.Fatalf("first in-budget file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out", "f2.txt")); !os.IsNotExist(err) {
		t.Fatalf("over-budget file exists or stat failed unexpectedly: %v", err)
	}
}

func TestLuaScanMaxTotalBytesDoesNotLeaveOverBudgetFile(t *testing.T) {
	l, dir, _ := setupEngine(t)
	run(t, l, `
		local w = assert(archive.create(appfs, "scan-total.tar", { format = "tar" }))
		for i = 1, 3 do assert(w:add("f"..i..".txt", string.rep("x", 1000))) end
		assert(w:close())
	`)
	tarBytes, err := os.ReadFile(filepath.Join(dir, "scan-total.tar"))
	require.NoError(t, err)
	l.SetGlobal("tarbytes", lua.LString(tarBytes))

	run(t, l, `
		local s = assert(archive.scan(tarbytes, { format = "tar", max_total_bytes = 1500 }))
		local n, err = s:extract_all(appfs, { prefix = "scan-out/" })
		assert(n == nil and err ~= nil, "expected max_total_bytes error")
		assert(s:close())
	`)
	if _, err := os.Stat(filepath.Join(dir, "scan-out", "f1.txt")); err != nil {
		t.Fatalf("first in-budget scanned file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scan-out", "f2.txt")); !os.IsNotExist(err) {
		t.Fatalf("over-budget scanned file exists or stat failed unexpectedly: %v", err)
	}
}
