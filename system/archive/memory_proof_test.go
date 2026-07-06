//go:build unix

// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

// patternReader yields deterministic, position-dependent bytes so the zip CRC
// is non-trivial and verified on read, without allocating the data up front.
type patternReader struct {
	remaining int64
	seed      byte
}

func (p *patternReader) Read(b []byte) (int, error) {
	if p.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(b))
	if n > p.remaining {
		n = p.remaining
	}
	for i := int64(0); i < n; i++ {
		p.seed = p.seed*31 + 7
		b[i] = p.seed
	}
	p.remaining -= n
	return int(n), nil
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func maxRSSBytes() int64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	if runtime.GOOS == "linux" {
		return ru.Maxrss * 1024 // Linux reports kilobytes
	}
	return ru.Maxrss // Darwin/BSD report bytes
}

// TestGiantZipBoundedMemory creates a multi-GB zip on disk and streams every
// entry through the random reader, proving peak RSS stays bounded regardless of
// archive size. Heavy; gated behind ARCHIVE_MEM_PROOF=1.
func TestGiantZipBoundedMemory(t *testing.T) {
	if os.Getenv("ARCHIVE_MEM_PROOF") == "" {
		t.Skip("set ARCHIVE_MEM_PROOF=1 to run the giant-zip memory proof (heavy)")
	}

	entryBytes := envInt64("ARCHIVE_ENTRY_BYTES", 512<<20) // 512 MiB per entry
	entryCount := envInt64("ARCHIVE_ENTRIES", 16)          // default total: 8 GiB
	rssBudget := envInt64("ARCHIVE_RSS_BUDGET", 350<<20)   // 350 MiB ceiling

	dir := t.TempDir()
	path := filepath.Join(dir, "giant.zip")

	zc, _ := archiveapi.Get("zip")
	wc := zc.(archiveapi.Writable)
	rc := zc.(archiveapi.RandomReadable)

	// --- create the giant zip, streaming (bounded memory) ---
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := wc.OpenWriter(f, archiveapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var written int64
	for i := int64(0); i < entryCount; i++ {
		ew, err := w.Create(archiveapi.Entry{
			Name:   "entry-" + strconv.FormatInt(i, 10) + ".bin",
			Method: "store",
			Size:   entryBytes,
		})
		if err != nil {
			t.Fatal(err)
		}
		n, err := io.Copy(ew, &patternReader{remaining: entryBytes, seed: byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		written += n
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	fi, _ := os.Stat(path)
	t.Logf("archive on disk: %.2f GiB (%d entries x %d bytes)",
		float64(fi.Size())/(1<<30), entryCount, entryBytes)

	rssAfterCreate := maxRSSBytes()

	// --- stream every entry through the random reader to io.Discard ---
	rf, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	reader, err := rc.OpenRandom(rf, fi.Size(), archiveapi.Options{MaxFileBytes: entryBytes})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	buf := make([]byte, 64<<10)
	var read int64
	for _, e := range reader.Entries() {
		erc, _, err := reader.Open(e.Name)
		if err != nil {
			t.Fatalf("open %s: %v", e.Name, err)
		}
		n, err := io.CopyBuffer(io.Discard, erc, buf)
		_ = erc.Close()
		if err != nil {
			t.Fatalf("read %s (CRC verified by zip.Reader): %v", e.Name, err)
		}
		read += n
	}

	peakRSS := maxRSSBytes()

	t.Logf("bytes written: %d", written)
	t.Logf("bytes read (CRC-verified): %d", read)
	t.Logf("peak RSS after create: %.1f MiB", float64(rssAfterCreate)/(1<<20))
	t.Logf("peak RSS overall:      %.1f MiB", float64(peakRSS)/(1<<20))
	t.Logf("archive/RSS ratio:     %.0fx", float64(fi.Size())/float64(peakRSS))

	if read != written {
		t.Fatalf("read %d bytes, wrote %d", read, written)
	}
	if peakRSS > rssBudget {
		t.Fatalf("peak RSS %.1f MiB exceeded budget %.1f MiB",
			float64(peakRSS)/(1<<20), float64(rssBudget)/(1<<20))
	}
}

type zeroReader struct{ remaining int64 }

func (z *zeroReader) Read(b []byte) (int, error) {
	if z.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(b))
	if n > z.remaining {
		n = z.remaining
	}
	for i := range b[:n] {
		b[i] = 0
	}
	z.remaining -= n
	return int(n), nil
}

// TestGiantZipStreamBoundedMemory proves the forward-only walker (deflate +
// data descriptors) decompresses a multi-GB archive with bounded memory.
// Gated behind ARCHIVE_MEM_PROOF=1.
func TestGiantZipStreamBoundedMemory(t *testing.T) {
	if os.Getenv("ARCHIVE_MEM_PROOF") == "" {
		t.Skip("set ARCHIVE_MEM_PROOF=1 to run the streaming memory proof (heavy)")
	}
	uncompressed := envInt64("ARCHIVE_STREAM_BYTES", 4<<30) // 4 GiB decompressed
	entryCount := envInt64("ARCHIVE_STREAM_ENTRIES", 8)
	rssBudget := envInt64("ARCHIVE_RSS_BUDGET", 350<<20)
	per := uncompressed / entryCount

	dir := t.TempDir()
	path := filepath.Join(dir, "giant-stream.zip")

	zc, _ := archiveapi.Get("zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := zc.(archiveapi.Writable).OpenWriter(f, archiveapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < entryCount; i++ {
		ew, err := w.Create(archiveapi.Entry{Name: "z-" + strconv.FormatInt(i, 10) + ".bin", Method: "deflate", Size: per})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(ew, &zeroReader{remaining: per}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	fi, _ := os.Stat(path)
	rf, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	walker, err := zc.(archiveapi.StreamReadable).OpenStream(rf, archiveapi.Options{MaxFileBytes: per})
	if err != nil {
		t.Fatal(err)
	}
	defer walker.Close()

	buf := make([]byte, 64<<10)
	var read int64
	for {
		e, r, err := walker.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if e.IsDir {
			continue
		}
		n, err := io.CopyBuffer(io.Discard, r, buf)
		if err != nil {
			t.Fatalf("read %s: %v", e.Name, err)
		}
		read += n
	}
	peakRSS := maxRSSBytes()

	t.Logf("compressed on disk: %.2f MiB; decompressed streamed: %.2f GiB",
		float64(fi.Size())/(1<<20), float64(read)/(1<<30))
	t.Logf("peak RSS overall: %.1f MiB", float64(peakRSS)/(1<<20))

	if read != uncompressed {
		t.Fatalf("streamed %d bytes, expected %d", read, uncompressed)
	}
	if peakRSS > rssBudget {
		t.Fatalf("peak RSS %.1f MiB exceeded budget %.1f MiB",
			float64(peakRSS)/(1<<20), float64(rssBudget)/(1<<20))
	}
}
