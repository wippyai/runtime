// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"archive/zip"
	"bufio"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

func init() {
	archiveapi.Register(zipCodec{})
}

type zipCodec struct{}

func (zipCodec) Name() string { return "zip" }

func (zipCodec) Extensions() []string { return []string{".zip"} }

func (zipCodec) Sniff(h []byte) bool {
	if len(h) < 4 || h[0] != 'P' || h[1] != 'K' {
		return false
	}
	return (h[2] == 0x03 && h[3] == 0x04) ||
		(h[2] == 0x05 && h[3] == 0x06) ||
		(h[2] == 0x07 && h[3] == 0x08)
}

func zipEntry(f *zip.File) archiveapi.Entry {
	name := f.Name
	isDir := f.FileInfo().IsDir()
	method := "deflate"
	if f.Method == zip.Store {
		method = "store"
	}
	return archiveapi.Entry{
		Name:           name,
		Size:           int64(f.UncompressedSize64),
		CompressedSize: int64(f.CompressedSize64),
		Mode:           f.Mode(),
		Modified:       f.Modified,
		IsDir:          isDir,
		Method:         method,
		CRC32:          f.CRC32,
	}
}

// --- random access ---

func (zipCodec) OpenRandom(r io.ReaderAt, size int64, o archiveapi.Options) (archiveapi.Reader, error) {
	o = withDefaults(o)
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	if len(zr.File) > o.MaxEntries {
		return nil, fmt.Errorf("%w: %d entries", ErrLimitExceeded, len(zr.File))
	}
	zrd := &zipReader{opts: o, byName: make(map[string]*zip.File, len(zr.File))}
	for _, f := range zr.File {
		zrd.entries = append(zrd.entries, zipEntry(f))
		if _, dup := zrd.byName[f.Name]; !dup {
			zrd.byName[f.Name] = f
		}
	}
	return zrd, nil
}

type zipReader struct {
	byName  map[string]*zip.File
	entries []archiveapi.Entry
	opts    archiveapi.Options
}

func (z *zipReader) Entries() []archiveapi.Entry { return z.entries }

func (z *zipReader) Stat(name string) (archiveapi.Entry, bool) {
	f, ok := z.byName[name]
	if !ok {
		return archiveapi.Entry{}, false
	}
	return zipEntry(f), true
}

func (z *zipReader) Open(name string) (io.ReadCloser, archiveapi.Entry, error) {
	f, ok := z.byName[name]
	if !ok {
		return nil, archiveapi.Entry{}, fs.ErrNotExist
	}
	e := zipEntry(f)
	if e.IsDir {
		return nil, e, errors.New("entry is a directory")
	}
	if int64(f.UncompressedSize64) > z.opts.MaxFileBytes {
		return nil, e, fmt.Errorf("%w: %s", ErrTooLarge, name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, e, err
	}
	return capReader(rc, z.opts.MaxFileBytes), e, nil
}

func (z *zipReader) Close() error { return nil }

// --- streaming (forward-only) over local file headers ---

const (
	sigLocalFile  = 0x04034b50
	sigDataDesc   = 0x08074b50
	flagDataDesc  = 0x0008
	zip64Sentinel = 0xffffffff
)

func (zipCodec) OpenStream(r io.Reader, o archiveapi.Options) (archiveapi.Walker, error) {
	return &zipWalker{br: bufio.NewReaderSize(r, 64<<10), opts: withDefaults(o)}, nil
}

type zipWalker struct {
	br        *bufio.Reader
	body      io.ReadCloser
	opts      archiveapi.Options
	count     int
	stopped   bool
	pendDesc  bool
	pendZip64 bool
}

// finalizeCurrent drains the current entry body to the end of its deflate/stored
// stream and consumes any trailing data descriptor, so the reader is positioned
// exactly at the next local file header.
func (w *zipWalker) finalizeCurrent() error {
	if w.body != nil {
		if _, err := io.Copy(io.Discard, w.body); err != nil {
			return err
		}
		_ = w.body.Close()
		w.body = nil
	}
	if w.pendDesc {
		if err := w.skipDataDescriptor(w.pendZip64); err != nil {
			return err
		}
		w.pendDesc = false
		w.pendZip64 = false
	}
	return nil
}

func (w *zipWalker) skipDataDescriptor(zip64 bool) error {
	var first uint32
	if err := binary.Read(w.br, binary.LittleEndian, &first); err != nil {
		return err
	}
	sizeBytes := 4
	if zip64 {
		sizeBytes = 8
	}
	// With the optional signature: [sig][crc][comp][uncomp]; without it the
	// first word already was the CRC.
	if first == sigDataDesc {
		if _, err := w.br.Discard(4); err != nil {
			return err
		}
	}
	_, err := w.br.Discard(2 * sizeBytes)
	return err
}

func (w *zipWalker) Next() (archiveapi.Entry, io.Reader, error) {
	if err := w.finalizeCurrent(); err != nil {
		return archiveapi.Entry{}, nil, err
	}
	if w.stopped {
		return archiveapi.Entry{}, nil, io.EOF
	}

	var sig uint32
	if err := binary.Read(w.br, binary.LittleEndian, &sig); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return archiveapi.Entry{}, nil, io.EOF
		}
		return archiveapi.Entry{}, nil, err
	}
	if sig != sigLocalFile {
		w.stopped = true
		return archiveapi.Entry{}, nil, io.EOF
	}

	var h struct {
		Version    uint16
		Flags      uint16
		Method     uint16
		ModTime    uint16
		ModDate    uint16
		CRC32      uint32
		CompSize   uint32
		UncompSize uint32
		NameLen    uint16
		ExtraLen   uint16
	}
	if err := binary.Read(w.br, binary.LittleEndian, &h); err != nil {
		return archiveapi.Entry{}, nil, err
	}
	name := make([]byte, h.NameLen)
	if _, err := io.ReadFull(w.br, name); err != nil {
		return archiveapi.Entry{}, nil, err
	}
	if h.ExtraLen > 0 {
		if _, err := w.br.Discard(int(h.ExtraLen)); err != nil {
			return archiveapi.Entry{}, nil, err
		}
	}

	w.count++
	if w.count > w.opts.MaxEntries {
		return archiveapi.Entry{}, nil, fmt.Errorf("%w: too many entries", ErrLimitExceeded)
	}

	method := "deflate"
	if h.Method == zip.Store {
		method = "store"
	}
	e := archiveapi.Entry{
		Name:     string(name),
		Size:     int64(h.UncompSize),
		Mode:     fs.FileMode(0o644),
		Modified: msdosTime(h.ModDate, h.ModTime),
		IsDir:    len(name) > 0 && name[len(name)-1] == '/',
		Method:   method,
		CRC32:    h.CRC32,
	}

	// Directory entries are handled like any other: their (empty) body is still
	// drained and any trailing data descriptor consumed, so the stream stays in
	// sync. The caller distinguishes them via e.IsDir.
	hasDesc := h.Flags&flagDataDesc != 0
	w.pendDesc = hasDesc
	// A real zip64 entry sets BOTH local-header sizes to the sentinel; requiring
	// both avoids misreading the data descriptor width for a non-zip64 entry
	// whose size happens to be exactly 0xffffffff.
	w.pendZip64 = h.CompSize == zip64Sentinel && h.UncompSize == zip64Sentinel

	switch h.Method {
	case zip.Store:
		if hasDesc {
			w.pendDesc = false
			w.stopped = true
			return e, nil, fmt.Errorf("stored entry %q uses a streaming data descriptor and cannot be read from a non-seekable source", e.Name)
		}
		w.body = capReader(io.NopCloser(io.LimitReader(w.br, int64(h.CompSize))), w.opts.MaxFileBytes)
		return e, w.body, nil
	case zip.Deflate:
		var raw io.Reader
		if hasDesc {
			raw = w.br
		} else {
			raw = io.LimitReader(w.br, int64(h.CompSize))
		}
		w.body = capReader(flate.NewReader(raw), w.opts.MaxFileBytes)
		return e, w.body, nil
	default:
		return e, nil, fmt.Errorf("unsupported zip method %d for %q", h.Method, e.Name)
	}
}

func (w *zipWalker) Close() error {
	if w.body != nil {
		err := w.body.Close()
		w.body = nil
		return err
	}
	return nil
}

func msdosTime(d, t uint16) time.Time {
	return time.Date(
		int(d>>9)+1980,
		time.Month(clamp(int((d>>5)&0xf), 1, 12)),
		clamp(int(d&0x1f), 1, 31),
		clamp(int(t>>11), 0, 23),
		clamp(int((t>>5)&0x3f), 0, 59),
		clamp(int((t&0x1f)*2), 0, 59),
		0, time.UTC)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
