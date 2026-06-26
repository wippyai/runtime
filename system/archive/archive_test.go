// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

type sample struct {
	name string
	body string
}

var samples = []sample{
	{"readme.md", "# hello\nworld\n"},
	{"data/big.csv", "a,b,c\n1,2,3\n4,5,6\n"},
	{"nested/deep/file.txt", "deep content"},
}

func buildArchive(t *testing.T, format string) []byte {
	t.Helper()
	c, ok := archiveapi.Get(format)
	if !ok {
		t.Fatalf("codec %q not registered", format)
	}
	wc, ok := c.(archiveapi.Writable)
	if !ok {
		t.Fatalf("codec %q not writable", format)
	}
	var buf bytes.Buffer
	w, err := wc.OpenWriter(&buf, archiveapi.Options{})
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	for _, s := range samples {
		ew, err := w.Create(archiveapi.Entry{Name: s.name, Size: int64(len(s.body))})
		if err != nil {
			t.Fatalf("Create %s: %v", s.name, err)
		}
		if _, err := io.Copy(ew, bytes.NewReader([]byte(s.body))); err != nil {
			t.Fatalf("write %s: %v", s.name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("writer close: %v", err)
	}
	return buf.Bytes()
}

func TestRandomRoundTrip(t *testing.T) {
	for _, format := range []string{"zip", "tar"} {
		t.Run(format, func(t *testing.T) {
			data := buildArchive(t, format)
			c, _ := archiveapi.Get(format)
			rc := c.(archiveapi.RandomReadable)
			r, err := rc.OpenRandom(bytes.NewReader(data), int64(len(data)), archiveapi.Options{})
			if err != nil {
				t.Fatalf("OpenRandom: %v", err)
			}
			defer r.Close()

			if len(r.Entries()) != len(samples) {
				t.Fatalf("entries = %d, want %d", len(r.Entries()), len(samples))
			}
			for _, s := range samples {
				e, ok := r.Stat(s.name)
				if !ok {
					t.Fatalf("Stat %s missing", s.name)
				}
				if e.Size != int64(len(s.body)) {
					t.Fatalf("%s size = %d, want %d", s.name, e.Size, len(s.body))
				}
				rd, _, err := r.Open(s.name)
				if err != nil {
					t.Fatalf("Open %s: %v", s.name, err)
				}
				got, _ := io.ReadAll(rd)
				rd.Close()
				if string(got) != s.body {
					t.Fatalf("%s body = %q, want %q", s.name, got, s.body)
				}
			}
		})
	}
}

func TestStreamRoundTrip(t *testing.T) {
	for _, format := range []string{"zip", "tar", "tar.gz", "tar.zst"} {
		t.Run(format, func(t *testing.T) {
			data := buildArchive(t, format)
			c, _ := archiveapi.Get(format)
			sc := c.(archiveapi.StreamReadable)
			w, err := sc.OpenStream(bytes.NewReader(data), archiveapi.Options{})
			if err != nil {
				t.Fatalf("OpenStream: %v", err)
			}
			defer w.Close()

			got := map[string]string{}
			for {
				e, rd, err := w.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("Next: %v", err)
				}
				if e.IsDir {
					continue
				}
				b, err := io.ReadAll(rd)
				if err != nil {
					t.Fatalf("read %s: %v", e.Name, err)
				}
				got[e.Name] = string(b)
			}
			for _, s := range samples {
				if got[s.name] != s.body {
					t.Fatalf("%s body = %q, want %q", s.name, got[s.name], s.body)
				}
			}
		})
	}
}

func TestSniffResolve(t *testing.T) {
	cases := map[string]string{"zip": "zip", "tar": "tar", "tar.gz": "tar.gz", "tar.zst": "tar.zst"}
	for format, want := range cases {
		data := buildArchive(t, format)
		c, ok := archiveapi.Resolve("", "x."+format, data[:min(512, len(data))])
		if !ok || c.Name() != want {
			t.Fatalf("Resolve(%s) = %v ok=%v, want %s", format, c, ok, want)
		}
	}
}

func TestSanitizeEntryName(t *testing.T) {
	bad := []string{"/etc/passwd", "../escape", "a/../../b", "C:/win", "..\\x", ""}
	for _, n := range bad {
		if _, ok := SanitizeEntryName(n); ok {
			t.Fatalf("SanitizeEntryName(%q) accepted, want rejected", n)
		}
	}
	good := map[string]string{"a/b.txt": "a/b.txt", "x/./y": "x/y", "dir/": "dir/"}
	for in, want := range good {
		got, ok := SanitizeEntryName(in)
		if !ok || got != want {
			t.Fatalf("SanitizeEntryName(%q) = %q ok=%v, want %q", in, got, ok, want)
		}
	}
}

func TestFileSizeLimit(t *testing.T) {
	data := buildArchive(t, "zip")
	c, _ := archiveapi.Get("zip")
	rc := c.(archiveapi.RandomReadable)
	r, err := rc.OpenRandom(bytes.NewReader(data), int64(len(data)), archiveapi.Options{MaxFileBytes: 4})
	if err != nil {
		t.Fatalf("OpenRandom: %v", err)
	}
	defer r.Close()
	_, _, err = r.Open("readme.md")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Open with tiny MaxFileBytes err = %v, want ErrTooLarge", err)
	}
}

func TestStreamDeflateDataDescriptor(t *testing.T) {
	// Standard library zip.Writer streaming to a non-seekable writer emits data
	// descriptors; the stream walker must still decode deflate entries.
	data := buildArchive(t, "zip")
	if !bytes.Contains(data, []byte("PK\x03\x04")) {
		t.Fatal("missing local file header signature")
	}
	c, _ := archiveapi.Get("zip")
	w, err := c.(archiveapi.StreamReadable).OpenStream(bytes.NewReader(data), archiveapi.Options{})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer w.Close()
	count := 0
	for {
		e, rd, err := w.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		b, _ := io.ReadAll(rd)
		for _, s := range samples {
			if s.name == e.Name && string(b) != s.body {
				t.Fatalf("%s = %q want %q", e.Name, b, s.body)
			}
		}
		count++
	}
	if count != len(samples) {
		t.Fatalf("streamed %d entries, want %d", count, len(samples))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = fmt.Sprintf
