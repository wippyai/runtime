// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// externalRA mimics a module-provided random-access source (the cloudstorage
// ranged reader): io.ReaderAt + Size + Name, lifecycle owned by the provider.
// bytes.Reader supplies ReadAt and Size.
type externalRA struct {
	*bytes.Reader
	name string
}

func (e *externalRA) Name() string { return e.name }

// nameless is the same source without the optional Name hint, forcing
// format detection down the magic-byte sniff path.
type namelessRA struct {
	*bytes.Reader
}

func TestLuaOpenFromExternalReaderAt(t *testing.T) {
	l, dir, _ := setupEngine(t)

	run(t, l, `
		local w = assert(archive.create(appfs, "ext.zip"))
		assert(w:add("notes.txt", "hello world"))
		assert(w:add("docs/data.bin", string.rep("z", 4096)))
		assert(w:close())
	`)
	zipBytes, err := os.ReadFile(filepath.Join(dir, "ext.zip"))
	require.NoError(t, err)

	ud := l.NewUserData()
	ud.Value = &externalRA{Reader: bytes.NewReader(zipBytes), name: "ext.zip"}
	l.SetGlobal("extsrc", ud)

	run(t, l, `
		local r, err = archive.open(extsrc)
		assert(r, tostring(err))

		local names = {}
		for e in r:entries() do names[e.name] = e end
		assert(names["notes.txt"], "missing notes.txt")
		assert(names["docs/data.bin"], "missing docs/data.bin")
		assert(names["docs/data.bin"].size == 4096, "bad entry size")

		assert(r:read("notes.txt") == "hello world", "read mismatch")

		local s = assert(r:stream("docs/data.bin"))
		assert(s.read, "stream has no read method")

		local n = assert(r:extract_all(appfs, { prefix = "ext-out/" }))
		assert(n == 2, "extracted "..tostring(n))
		assert(r:close())
	`)

	got, err := os.ReadFile(filepath.Join(dir, "ext-out", "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello world", string(got))
}

func TestLuaOpenFromExternalReaderAtSniffsWithoutName(t *testing.T) {
	l, dir, _ := setupEngine(t)

	run(t, l, `
		local w = assert(archive.create(appfs, "sniff.zip"))
		assert(w:add("a.txt", "alpha"))
		assert(w:close())
	`)
	zipBytes, err := os.ReadFile(filepath.Join(dir, "sniff.zip"))
	require.NoError(t, err)

	ud := l.NewUserData()
	ud.Value = &namelessRA{Reader: bytes.NewReader(zipBytes)}
	l.SetGlobal("blindsrc", ud)

	run(t, l, `
		local r, err = archive.open(blindsrc)
		assert(r, tostring(err))
		assert(r:read("a.txt") == "alpha")
		assert(r:close())
	`)
}

func TestLuaOpenFromExternalReaderAtWithOptions(t *testing.T) {
	l, dir, _ := setupEngine(t)

	run(t, l, `
		local w = assert(archive.create(appfs, "lim.zip"))
		assert(w:add("big.txt", string.rep("x", 2048)))
		assert(w:close())
	`)
	zipBytes, err := os.ReadFile(filepath.Join(dir, "lim.zip"))
	require.NoError(t, err)

	ud := l.NewUserData()
	ud.Value = &externalRA{Reader: bytes.NewReader(zipBytes), name: "lim.zip"}
	l.SetGlobal("limsrc", ud)

	// Options land at arg 2 for external sources (no path argument).
	run(t, l, `
		local r, err = archive.open(limsrc, { max_inline_bytes = 16 })
		assert(r, tostring(err))
		local data, e = r:read("big.txt")
		assert(data == nil and e ~= nil, "expected inline-size error")
		assert(r:stream("big.txt"))
		assert(r:close())
	`)
}

func TestLuaOpenRejectsUnknownUserdata(t *testing.T) {
	l, _, _ := setupEngine(t)

	ud := l.NewUserData()
	ud.Value = struct{ x int }{x: 1}
	l.SetGlobal("junksrc", ud)

	run(t, l, `
		local r, err = archive.open(junksrc)
		assert(r == nil and err ~= nil, "expected source-type error")
	`)
}

// Interface conformance guards: the archive hook must keep accepting any
// userdata that satisfies externalReaderAt.
var (
	_ externalReaderAt = (*externalRA)(nil)
	_ externalReaderAt = (*namelessRA)(nil)
)
