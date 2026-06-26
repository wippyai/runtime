// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

func init() {
	archiveapi.Register(tarCodec{})
}

type tarCodec struct{}

func (tarCodec) Name() string { return "tar" }

func (tarCodec) Extensions() []string { return []string{".tar"} }

// Sniff checks the "ustar" magic at offset 257 of the first header block.
func (tarCodec) Sniff(h []byte) bool {
	return len(h) >= 263 && string(h[257:262]) == "ustar"
}

func tarEntry(h *tar.Header) archiveapi.Entry {
	return archiveapi.Entry{
		Name:     h.Name,
		Size:     h.Size,
		Mode:     fs.FileMode(h.Mode),
		Modified: h.ModTime,
		IsDir:    h.Typeflag == tar.TypeDir || strings.HasSuffix(h.Name, "/"),
		Method:   "store",
	}
}

// --- random access via an in-memory offset index (offsets only) ---

func (tarCodec) OpenRandom(r io.ReaderAt, size int64, o archiveapi.Options) (archiveapi.Reader, error) {
	o = withDefaults(o)
	section := io.NewSectionReader(r, 0, size)
	cr := &countingReader{r: section}
	tr := tar.NewReader(cr)

	trd := &tarReader{ra: r, opts: o, byName: map[string]tarIndexEntry{}}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(trd.entries) >= o.MaxEntries {
			return nil, fmt.Errorf("%w: too many entries", ErrLimitExceeded)
		}
		e := tarEntry(h)
		trd.entries = append(trd.entries, e)
		if _, dup := trd.byName[h.Name]; !dup {
			trd.byName[h.Name] = tarIndexEntry{offset: cr.n, entry: e}
		}
	}
	return trd, nil
}

type tarIndexEntry struct {
	entry  archiveapi.Entry
	offset int64
}

type tarReader struct {
	ra      io.ReaderAt
	byName  map[string]tarIndexEntry
	closer  io.Closer
	entries []archiveapi.Entry
	opts    archiveapi.Options
}

func (t *tarReader) Entries() []archiveapi.Entry { return t.entries }

func (t *tarReader) Stat(name string) (archiveapi.Entry, bool) {
	ix, ok := t.byName[name]
	if !ok {
		return archiveapi.Entry{}, false
	}
	return ix.entry, true
}

func (t *tarReader) Open(name string) (io.ReadCloser, archiveapi.Entry, error) {
	ix, ok := t.byName[name]
	if !ok {
		return nil, archiveapi.Entry{}, fs.ErrNotExist
	}
	if ix.entry.IsDir {
		return nil, ix.entry, errors.New("entry is a directory")
	}
	if ix.entry.Size > t.opts.MaxFileBytes {
		return nil, ix.entry, fmt.Errorf("%w: %s", ErrTooLarge, name)
	}
	sr := io.NewSectionReader(t.ra, ix.offset, ix.entry.Size)
	return capReader(io.NopCloser(sr), t.opts.MaxFileBytes), ix.entry, nil
}

func (t *tarReader) Close() error {
	if t.closer != nil {
		return t.closer.Close()
	}
	return nil
}

// countingReader tracks the absolute byte offset consumed from the source so
// the tar index can record each entry's data start without buffering data.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// --- streaming (forward-only) ---

func (tarCodec) OpenStream(r io.Reader, o archiveapi.Options) (archiveapi.Walker, error) {
	return &tarWalker{tr: tar.NewReader(r), opts: withDefaults(o)}, nil
}

type tarWalker struct {
	tr      *tar.Reader
	closer  io.Closer
	opts    archiveapi.Options
	count   int
}

func (w *tarWalker) Next() (archiveapi.Entry, io.Reader, error) {
	h, err := w.tr.Next()
	if err != nil {
		return archiveapi.Entry{}, nil, err
	}
	w.count++
	if w.count > w.opts.MaxEntries {
		return archiveapi.Entry{}, nil, fmt.Errorf("%w: too many entries", ErrLimitExceeded)
	}
	e := tarEntry(h)
	return e, w.tr, nil
}

func (w *tarWalker) Close() error {
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

// --- streaming write ---

func (tarCodec) OpenWriter(w io.Writer, o archiveapi.Options) (archiveapi.Writer, error) {
	return newTarWriter(tar.NewWriter(w), nil, withDefaults(o)), nil
}

func newTarWriter(tw *tar.Writer, extra io.Closer, _ archiveapi.Options) *tarWriter {
	return &tarWriter{tw: tw, extra: extra}
}

type tarWriter struct {
	tw    *tar.Writer
	extra io.Closer
}

func (t *tarWriter) Create(e archiveapi.Entry) (io.Writer, error) {
	hdr := &tar.Header{
		Name:    e.Name,
		Size:    e.Size,
		Mode:    int64(0o644),
		ModTime: e.Modified,
	}
	if e.Mode != 0 {
		hdr.Mode = int64(e.Mode.Perm())
	}
	if e.IsDir {
		hdr.Typeflag = tar.TypeDir
		hdr.Size = 0
		if !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
	} else {
		hdr.Typeflag = tar.TypeReg
	}
	if err := t.tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	return t.tw, nil
}

func (t *tarWriter) Close() error {
	if err := t.tw.Close(); err != nil {
		return err
	}
	if t.extra != nil {
		return t.extra.Close()
	}
	return nil
}
