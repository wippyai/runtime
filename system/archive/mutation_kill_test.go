// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

func codec(t *testing.T, name string) archiveapi.Codec {
	t.Helper()
	c, ok := archiveapi.Get(name)
	if !ok {
		t.Fatalf("codec %q not registered", name)
	}
	return c
}

func assertSniff(t *testing.T, c archiveapi.Codec, h []byte, want bool) {
	t.Helper()
	if got := c.Sniff(h); got != want {
		t.Fatalf("%s.Sniff(% x) = %v, want %v", c.Name(), h, got, want)
	}
}

func TestSniffExactness(t *testing.T) {
	zc, tc := codec(t, "zip"), codec(t, "tar")
	gc, zs := codec(t, "tar.gz"), codec(t, "tar.zst")

	assertSniff(t, zc, []byte("PK\x03\x04....."), true)
	assertSniff(t, zc, []byte("PK\x05\x06....."), true)
	assertSniff(t, zc, []byte("PK\x07\x08....."), true)
	assertSniff(t, zc, []byte("PK\x01\x02....."), false)
	assertSniff(t, zc, []byte("XK\x03\x04....."), false)
	assertSniff(t, zc, []byte("PX\x03\x04....."), false)
	assertSniff(t, zc, []byte("PK\x03"), false)

	ustar := make([]byte, 300)
	copy(ustar[257:], "ustar")
	assertSniff(t, tc, ustar, true)
	notustar := make([]byte, 300)
	copy(notustar[257:], "xstar")
	assertSniff(t, tc, notustar, false)
	assertSniff(t, tc, make([]byte, 100), false)

	assertSniff(t, gc, []byte{0x1f, 0x8b, 0x08, 0x00}, true)
	assertSniff(t, gc, []byte{0x1f, 0x00}, false)
	assertSniff(t, gc, []byte{0x1f}, false)

	assertSniff(t, zs, []byte{0x28, 0xb5, 0x2f, 0xfd}, true)
	assertSniff(t, zs, []byte{0x28, 0xb5, 0x2f, 0x00}, false)
	assertSniff(t, zs, []byte{0x28, 0xb5}, false)

	// exact minimum-length headers exercise the length boundary checks.
	assertSniff(t, zc, []byte("PK\x03\x04"), true)
	assertSniff(t, gc, []byte{0x1f, 0x8b}, true)
	assertSniff(t, zs, []byte{0x28, 0xb5, 0x2f, 0xfd}, true)
	exactTar := make([]byte, 263)
	copy(exactTar[257:], "ustar")
	assertSniff(t, tc, exactTar, true)
	assertSniff(t, tc, exactTar[:262], false)
}

func TestCapReaderCloseIdempotent(t *testing.T) {
	tc := &trackCloser{}
	rc := capReader(tc, 100)
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
	if tc.closed != 1 {
		t.Fatalf("underlying closed %d times, want exactly 1", tc.closed)
	}
}

type trackCloser struct{ closed int }

func (c *trackCloser) Read([]byte) (int, error) { return 0, io.EOF }
func (c *trackCloser) Close() error             { c.closed++; return nil }

func TestMaxEntriesBoundary(t *testing.T) {
	for _, format := range []string{"zip", "tar"} {
		data := buildArchive(t, format) // exactly len(samples) entries
		rc := codec(t, format).(archiveapi.RandomReadable)
		n := len(samples)
		if _, err := rc.OpenRandom(bytes.NewReader(data), int64(len(data)), archiveapi.Options{MaxEntries: n}); err != nil {
			t.Fatalf("%s MaxEntries=%d should pass: %v", format, n, err)
		}
		if _, err := rc.OpenRandom(bytes.NewReader(data), int64(len(data)), archiveapi.Options{MaxEntries: n - 1}); err == nil {
			t.Fatalf("%s MaxEntries=%d should fail", format, n-1)
		}
	}
}

func TestMaxFileBytesBoundary(t *testing.T) {
	for _, format := range []string{"zip", "tar"} {
		data := buildArchive(t, format)
		rc := codec(t, format).(archiveapi.RandomReadable)
		body := samples[0].body
		size := int64(len(body))

		r, err := rc.OpenRandom(bytes.NewReader(data), int64(len(data)), archiveapi.Options{MaxFileBytes: size})
		if err != nil {
			t.Fatal(err)
		}
		rd, _, err := r.Open(samples[0].name)
		if err != nil {
			t.Fatalf("%s exact-size open: %v", format, err)
		}
		got, err := io.ReadAll(rd)
		rd.Close()
		if err != nil || string(got) != body {
			t.Fatalf("%s exact-size read: %v %q", format, err, got)
		}

		r2, _ := rc.OpenRandom(bytes.NewReader(data), int64(len(data)), archiveapi.Options{MaxFileBytes: size - 1})
		if _, _, err := r2.Open(samples[0].name); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("%s size-1 open err = %v, want ErrTooLarge", format, err)
		}
	}
}

func TestCapReaderEnforcesAndPassesThrough(t *testing.T) {
	over := capReader(io.NopCloser(strings.NewReader("hello world")), 5)
	got, err := io.ReadAll(over)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over-limit read err = %v, want ErrTooLarge", err)
	}
	if string(got) != "hello" {
		t.Fatalf("over-limit read delivered %q (%d bytes), want exactly the 5-byte cap", got, len(got))
	}
	exact := capReader(io.NopCloser(strings.NewReader("hello")), 5)
	if b, err := io.ReadAll(exact); err != nil || string(b) != "hello" {
		t.Fatalf("exact read = %q err=%v", b, err)
	}
	base := io.NopCloser(strings.NewReader("x"))
	if capReader(base, 0) != base {
		t.Fatal("capReader with max<=0 must return the reader unchanged")
	}
}

func TestMsdosTimeDecode(t *testing.T) {
	d := uint16((51 << 9) | (2 << 5) | 3)        // 1980+51=2031, month 2, day 3
	tm := uint16((4 << 11) | (5 << 5) | (6 / 2)) // 04:05:06
	got := msdosTime(d, tm)
	want := time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("msdosTime = %v, want %v", got, want)
	}
}

func TestRandomEntryMetadata(t *testing.T) {
	var buf bytes.Buffer
	zc := codec(t, "zip")
	w, _ := zc.(archiveapi.Writable).OpenWriter(&buf, archiveapi.Options{})
	mustCreate(t, w, archiveapi.Entry{Name: "d/", IsDir: true}, "")
	mustCreate(t, w, archiveapi.Entry{Name: "s.bin", Method: "store", Size: 6}, "stored")
	mustCreate(t, w, archiveapi.Entry{Name: "f.txt"}, "deflated content")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, _ := zc.(archiveapi.RandomReadable).OpenRandom(bytes.NewReader(buf.Bytes()), int64(buf.Len()), archiveapi.Options{})
	defer r.Close()

	d, ok := r.Stat("d/")
	if !ok || !d.IsDir {
		t.Fatalf("d/ stat: ok=%v isDir=%v", ok, d.IsDir)
	}
	s, ok := r.Stat("s.bin")
	if !ok || s.Method != "store" || s.Size != 6 || s.IsDir {
		t.Fatalf("s.bin stat: %+v ok=%v", s, ok)
	}
	f, ok := r.Stat("f.txt")
	if !ok || f.Method != "deflate" {
		t.Fatalf("f.txt method = %q ok=%v", f.Method, ok)
	}
}

// TestStreamDirEntryNoDesync locks in the fix where a directory entry's data
// descriptor must be consumed so files after it still decode.
func TestStreamDirEntryNoDesync(t *testing.T) {
	var buf bytes.Buffer
	zc := codec(t, "zip")
	w, _ := zc.(archiveapi.Writable).OpenWriter(&buf, archiveapi.Options{})
	mustCreate(t, w, archiveapi.Entry{Name: "a.txt"}, "alpha")
	mustCreate(t, w, archiveapi.Entry{Name: "mid/", IsDir: true}, "")
	mustCreate(t, w, archiveapi.Entry{Name: "b.txt"}, "bravo")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	sw, _ := zc.(archiveapi.StreamReadable).OpenStream(bytes.NewReader(buf.Bytes()), archiveapi.Options{})
	defer sw.Close()
	got := map[string]string{}
	sawDir := false
	for {
		e, rd, err := sw.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if e.IsDir {
			sawDir = true
			continue
		}
		b, _ := io.ReadAll(rd)
		got[e.Name] = string(b)
	}
	if !sawDir {
		t.Fatal("directory entry not seen in stream")
	}
	if got["a.txt"] != "alpha" || got["b.txt"] != "bravo" {
		t.Fatalf("desync: a=%q b=%q", got["a.txt"], got["b.txt"])
	}
}

func TestStreamStoreDataDescriptorRejected(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hw, _ := zw.CreateHeader(&zip.FileHeader{Name: "s.bin", Method: zip.Store})
	_, _ = hw.Write([]byte("stored-with-descriptor"))
	_ = zw.Close()

	sw, _ := codec(t, "zip").(archiveapi.StreamReadable).OpenStream(bytes.NewReader(buf.Bytes()), archiveapi.Options{})
	defer sw.Close()
	_, _, err := sw.Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("expected data-descriptor rejection, got %v", err)
	}
	if !strings.Contains(err.Error(), "data descriptor") {
		t.Fatalf("error = %v, want data-descriptor message", err)
	}
}

func TestTarStreamMaxEntries(t *testing.T) {
	data := buildArchive(t, "tar")
	sw, _ := codec(t, "tar").(archiveapi.StreamReadable).OpenStream(bytes.NewReader(data), archiveapi.Options{MaxEntries: 2})
	returned := 0
	for {
		_, _, err := sw.Next()
		if errors.Is(err, io.EOF) {
			t.Fatal("expected MaxEntries error, got EOF")
		}
		if err != nil {
			break
		}
		returned++
	}
	if returned != 2 {
		t.Fatalf("returned %d entries before limit, want 2", returned)
	}
}

func TestTarModePreserved(t *testing.T) {
	var buf bytes.Buffer
	tc := codec(t, "tar")
	w, _ := tc.(archiveapi.Writable).OpenWriter(&buf, archiveapi.Options{})
	mustCreate(t, w, archiveapi.Entry{Name: "m.txt", Mode: 0o600}, "hi")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, _ := tc.(archiveapi.RandomReadable).OpenRandom(bytes.NewReader(buf.Bytes()), int64(buf.Len()), archiveapi.Options{})
	defer r.Close()
	e, _ := r.Stat("m.txt")
	if e.Mode.Perm() != 0o600 {
		t.Fatalf("tar mode = %o, want 600", e.Mode.Perm())
	}
}

func TestSanitizeMoreCases(t *testing.T) {
	ok := map[string]string{
		"a/b/../c": "a/c",
		"./y":      "y",
		"..ab/x":   "..ab/x",
		"deep/dir": "deep/dir",
	}
	for in, want := range ok {
		got, valid := SanitizeEntryName(in)
		if !valid || got != want {
			t.Fatalf("SanitizeEntryName(%q) = %q valid=%v, want %q", in, got, valid, want)
		}
	}
	bad := []string{"a/../../b", "../../x", "/abs", "D:\\w", "x/..", "C:", "C:x"}
	for _, in := range bad {
		if _, valid := SanitizeEntryName(in); valid {
			t.Fatalf("SanitizeEntryName(%q) accepted, want rejected", in)
		}
	}
}

// TestTarRandomExtendedHeaders confirms the offset index points at real entry
// data even when tar emits multi-block GNU/PAX extended headers for long names,
// and across empty entries — i.e. cr.n stays exact through read-based skipping.
func TestTarRandomExtendedHeaders(t *testing.T) {
	longName := "deeply/nested/" + strings.Repeat("x", 150) + ".txt"
	var buf bytes.Buffer
	tc := codec(t, "tar")
	w, _ := tc.(archiveapi.Writable).OpenWriter(&buf, archiveapi.Options{})
	mustCreate(t, w, archiveapi.Entry{Name: "first.txt"}, "first")
	mustCreate(t, w, archiveapi.Entry{Name: "empty/", IsDir: true}, "")
	mustCreate(t, w, archiveapi.Entry{Name: longName}, "long-name-content")
	mustCreate(t, w, archiveapi.Entry{Name: "after.txt"}, "after")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := tc.(archiveapi.RandomReadable).OpenRandom(bytes.NewReader(buf.Bytes()), int64(buf.Len()), archiveapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for name, want := range map[string]string{
		"first.txt": "first",
		longName:    "long-name-content",
		"after.txt": "after",
	} {
		rd, _, err := r.Open(name)
		if err != nil {
			t.Fatalf("open %q: %v", name, err)
		}
		got, _ := io.ReadAll(rd)
		rd.Close()
		if string(got) != want {
			t.Fatalf("entry %q = %q, want %q (offset index misaligned)", name, got, want)
		}
	}
}

func mustCreate(t *testing.T, w archiveapi.Writer, e archiveapi.Entry, body string) {
	t.Helper()
	if e.Size == 0 && body != "" {
		e.Size = int64(len(body))
	}
	ew, err := w.Create(e)
	if err != nil {
		t.Fatalf("create %s: %v", e.Name, err)
	}
	if body != "" {
		if _, err := io.WriteString(ew, body); err != nil {
			t.Fatalf("write %s: %v", e.Name, err)
		}
	}
}
