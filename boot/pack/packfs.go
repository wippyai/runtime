package pack

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"time"

	apipack "github.com/wippyai/runtime/api/pack"
)

// packFS implements fs.ReadDirFS for tree resources.
type packFS struct {
	tree   *TreeResource
	reader *Reader
}

// newPackFS creates a filesystem from a tree resource.
func newPackFS(tree *TreeResource, reader *Reader) fs.ReadDirFS {
	return &packFS{
		tree:   tree,
		reader: reader,
	}
}

// Open implements fs.FS.
func (pfs *packFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Clean path
	name = path.Clean(name)
	if name == "." {
		name = ""
	}

	// Check if it's a directory
	if children, ok := pfs.tree.Dirs[name]; ok {
		return &packDir{
			name:     path.Base(name),
			children: children,
			pfs:      pfs,
		}, nil
	}

	// Check if it's a file
	if entry, ok := pfs.tree.Files[name]; ok {
		return &packFile{
			name:   path.Base(name),
			entry:  entry,
			reader: pfs.reader,
		}, nil
	}

	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadDir implements fs.ReadDirFS.
func (pfs *packFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	name = path.Clean(name)
	if name == "." {
		name = ""
	}

	children, ok := pfs.tree.Dirs[name]
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}

	entries := make([]fs.DirEntry, len(children))
	for i, child := range children {
		childPath := path.Join(name, child)

		// Check if directory or file
		if _, isDir := pfs.tree.Dirs[childPath]; isDir {
			entries[i] = &packDirEntry{
				name:  child,
				isDir: true,
			}
		} else if fileEntry, ok := pfs.tree.Files[childPath]; ok {
			entries[i] = &packDirEntry{
				name:  child,
				isDir: false,
				size:  int64(fileEntry.Size), //nolint:gosec // Size is bounded by pack format
				mode:  fs.FileMode(fileEntry.Mode),
				mtime: time.Unix(fileEntry.ModTime, 0),
			}
		}
	}

	return entries, nil
}

// packFile implements fs.File for files in the pack.
type packFile struct {
	name             string
	entry            FileEntry
	reader           *Reader
	offset           int64
	decompressedData []byte
}

// Stat implements fs.File.
func (pf *packFile) Stat() (fs.FileInfo, error) {
	return &packFileInfo{
		name:  pf.name,
		size:  int64(pf.entry.Size), //nolint:gosec // Size is bounded by pack format
		mode:  fs.FileMode(pf.entry.Mode),
		mtime: time.Unix(pf.entry.ModTime, 0),
	}, nil
}

// Read implements fs.File.
func (pf *packFile) Read(p []byte) (n int, err error) {
	if pf.offset >= int64(pf.entry.Size) { //nolint:gosec // Size is bounded by pack format
		return 0, io.EOF
	}

	// Determine how much to read
	toRead := uint64(len(p))
	remaining := pf.entry.Size - uint64(pf.offset) //nolint:gosec // Offset is bounded by file size
	if toRead > remaining {
		toRead = remaining
	}

	// Handle compressed files by decompressing once and caching
	if pf.entry.Compressed && pf.decompressedData == nil {
		// Decompress entire file on first read
		if len(pf.entry.Location.Chunks) == 0 {
			// Small file - no chunks
			frameIdx := pf.entry.Location.FrameIndex
			frameInfo := pf.getFrameInfo(frameIdx)
			if frameInfo == nil {
				return 0, fmt.Errorf("frame not found: %d", frameIdx)
			}

			compressedData, err := pf.reader.ReadFrameData(*frameInfo, pf.entry.Location.Offset, pf.entry.CompressedSize)
			if err != nil {
				return 0, err
			}

			pf.decompressedData, err = decompressFileData(compressedData)
			if err != nil {
				return 0, fmt.Errorf("decompress file data: %w", err)
			}
		} else {
			// Large file with chunks
			// Read all chunks and decompress
			compressedData, err := pf.readChunked(0, pf.entry.CompressedSize)
			if err != nil {
				return 0, err
			}

			pf.decompressedData, err = decompressFileData(compressedData)
			if err != nil {
				return 0, fmt.Errorf("decompress file data: %w", err)
			}
		}
	}

	// Read from appropriate location
	var data []byte
	switch {
	case pf.entry.Compressed:
		// Read from cached decompressed data
		start := uint64(pf.offset) //nolint:gosec // Offset is bounded by file size
		end := start + toRead
		if end > uint64(len(pf.decompressedData)) {
			end = uint64(len(pf.decompressedData))
		}
		data = pf.decompressedData[start:end]
	case len(pf.entry.Location.Chunks) == 0:
		// Small uncompressed file - no chunks
		frameIdx := pf.entry.Location.FrameIndex
		frameInfo := pf.getFrameInfo(frameIdx)
		if frameInfo == nil {
			return 0, fmt.Errorf("frame not found: %d", frameIdx)
		}

		data, err = pf.reader.ReadFrameData(*frameInfo, pf.entry.Location.Offset+uint64(pf.offset), toRead) //nolint:gosec // Offset is bounded by file size
		if err != nil {
			return 0, err
		}
	default:
		// Large uncompressed file with chunks
		data, err = pf.readChunked(uint64(pf.offset), toRead) //nolint:gosec // Offset is bounded by file size
		if err != nil {
			return 0, err
		}
	}

	n = copy(p, data)
	pf.offset += int64(n)

	if pf.offset >= int64(pf.entry.Size) { //nolint:gosec // Size is bounded by pack format
		err = io.EOF
	}

	return n, err
}

// Seek implements io.Seeker.
func (pf *packFile) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = pf.offset + offset
	case io.SeekEnd:
		newOffset = int64(pf.entry.Size) + offset //nolint:gosec // Size is bounded by pack format
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}

	if newOffset < 0 {
		return 0, fmt.Errorf("negative position")
	}

	pf.offset = newOffset
	return newOffset, nil
}

// readChunked reads from chunked file.
func (pf *packFile) readChunked(offset, size uint64) ([]byte, error) {
	return readChunkedData(pf.entry.Location.Chunks, offset, size, pf.reader, pf.getFrameInfo)
}

// getFrameInfo gets frame info for a data frame index.
func (pf *packFile) getFrameInfo(frameIdx uint32) *FrameInfo {
	return getDataFrameInfo(frameIdx, pf.reader.toc.DataFrames)
}

// Close implements fs.File.
func (pf *packFile) Close() error {
	return nil
}

// packDir implements fs.File for directories.
type packDir struct {
	name     string
	children []string
	pfs      *packFS
	offset   int
}

// Stat implements fs.File.
func (pd *packDir) Stat() (fs.FileInfo, error) {
	return &packFileInfo{
		name:  pd.name,
		mode:  fs.ModeDir | 0755,
		isDir: true,
	}, nil
}

// Read implements fs.File (directories return error).
func (pd *packDir) Read(_ []byte) (n int, err error) {
	return 0, &fs.PathError{Op: "read", Path: pd.name, Err: fmt.Errorf("is a directory")}
}

// Close implements fs.File.
func (pd *packDir) Close() error {
	return nil
}

// ReadDir implements fs.ReadDirFile.
func (pd *packDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if pd.offset >= len(pd.children) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}

	end := len(pd.children)
	if n > 0 && pd.offset+n < end {
		end = pd.offset + n
	}

	entries := make([]fs.DirEntry, 0, end-pd.offset)
	for i := pd.offset; i < end; i++ {
		childName := pd.children[i]
		var dirPath string
		if pd.name == "" || pd.name == "." {
			dirPath = childName
		} else {
			dirPath = path.Join(pd.name, childName)
		}

		if _, isDir := pd.pfs.tree.Dirs[dirPath]; isDir {
			entries = append(entries, &packDirEntry{
				name:  childName,
				isDir: true,
			})
		} else if fileEntry, ok := pd.pfs.tree.Files[dirPath]; ok {
			entries = append(entries, &packDirEntry{
				name:  childName,
				size:  int64(fileEntry.Size), //nolint:gosec // Size is bounded by pack format
				mode:  fs.FileMode(fileEntry.Mode),
				mtime: time.Unix(fileEntry.ModTime, 0),
			})
		}
	}

	pd.offset = end
	return entries, nil
}

// packFileInfo implements fs.FileInfo.
type packFileInfo struct {
	name  string
	size  int64
	mode  fs.FileMode
	mtime time.Time
	isDir bool
}

func (pfi *packFileInfo) Name() string       { return pfi.name }
func (pfi *packFileInfo) Size() int64        { return pfi.size }
func (pfi *packFileInfo) Mode() fs.FileMode  { return pfi.mode }
func (pfi *packFileInfo) ModTime() time.Time { return pfi.mtime }
func (pfi *packFileInfo) IsDir() bool        { return pfi.isDir }
func (pfi *packFileInfo) Sys() interface{}   { return nil }

// packDirEntry implements fs.DirEntry.
type packDirEntry struct {
	name  string
	isDir bool
	size  int64
	mode  fs.FileMode
	mtime time.Time
}

func (pde *packDirEntry) Name() string      { return pde.name }
func (pde *packDirEntry) IsDir() bool       { return pde.isDir }
func (pde *packDirEntry) Type() fs.FileMode { return pde.mode.Type() }

func (pde *packDirEntry) Info() (fs.FileInfo, error) {
	return &packFileInfo{
		name:  pde.name,
		size:  pde.size,
		mode:  pde.mode,
		mtime: pde.mtime,
		isDir: pde.isDir,
	}, nil
}

// blobReader implements apipack.BlobReader for blob resources.
type blobReader struct {
	blob   *BlobResource
	reader *Reader
}

// newBlobReader creates a blob reader.
func newBlobReader(blob *BlobResource, reader *Reader) apipack.BlobReader {
	return &blobReader{
		blob:   blob,
		reader: reader,
	}
}

// ReadAt implements apipack.BlobReader.
func (br *blobReader) ReadAt(p []byte, offset int64) (n int, err error) {
	if offset < 0 {
		return 0, fmt.Errorf("negative offset")
	}

	if offset >= int64(br.blob.Size) { //nolint:gosec // Size is bounded by pack format
		return 0, io.EOF
	}

	toRead := uint64(len(p))
	remaining := br.blob.Size - uint64(offset)
	if toRead > remaining {
		toRead = remaining
		err = io.EOF
	}

	// Read from chunks
	var data []byte
	if len(br.blob.Chunks) == 0 {
		// Small blob without chunks
		// For now, non-chunked blobs are not created by writer, but implement reading logic anyway
		// Non-chunked blobs would need to store data inline in BlobResource or similar
		// Since current writer doesn't create blobs, return meaningful error
		return 0, fmt.Errorf("blob has no chunks: non-chunked blob format not supported by current writer")
	}

	data, readErr := br.readChunked(uint64(offset), toRead)
	if readErr != nil {
		return 0, readErr
	}

	n = copy(p, data)
	return n, err
}

// readChunked reads from chunked blob.
func (br *blobReader) readChunked(offset, size uint64) ([]byte, error) {
	return readChunkedData(br.blob.Chunks, offset, size, br.reader, br.getFrameInfo)
}

// getFrameInfo gets frame info for a data frame index.
func (br *blobReader) getFrameInfo(frameIdx uint32) *FrameInfo {
	return getDataFrameInfo(frameIdx, br.reader.toc.DataFrames)
}

// Size implements apipack.BlobReader.
func (br *blobReader) Size() int64 {
	return int64(br.blob.Size) //nolint:gosec // Size is bounded by pack format
}

// Hash implements apipack.BlobReader.
func (br *blobReader) Hash() string {
	return br.blob.Hash
}

// Close implements apipack.BlobReader.
func (br *blobReader) Close() error {
	return nil
}

// decompressFileData decompresses per-file compressed data.
func decompressFileData(compressed []byte) ([]byte, error) {
	return decompressZstd(compressed)
}

// chunkInfo abstracts chunk information for reading.
type chunkInfo interface {
	getOffset() uint64
	getSize() uint32
	getFrameIndex() uint32
	getFrameOffset() uint64
}

// Implement chunkInfo for ChunkInfo
func (c ChunkInfo) getOffset() uint64      { return c.Offset }
func (c ChunkInfo) getSize() uint32        { return c.Size }
func (c ChunkInfo) getFrameIndex() uint32  { return c.FrameIndex }
func (c ChunkInfo) getFrameOffset() uint64 { return c.FrameOffset }

// Implement chunkInfo for BlobChunk
func (c BlobChunk) getOffset() uint64      { return c.Offset }
func (c BlobChunk) getSize() uint32        { return c.Size }
func (c BlobChunk) getFrameIndex() uint32  { return c.FrameIndex }
func (c BlobChunk) getFrameOffset() uint64 { return c.FrameOffset }

// readChunkedData reads data from chunks using frame reader.
func readChunkedData[C chunkInfo](chunks []C, offset, size uint64, reader FrameReader, getFrame func(uint32) *FrameInfo) ([]byte, error) {
	result := make([]byte, size)
	resultOffset := uint64(0)
	remaining := size

	for _, chunk := range chunks {
		chunkEnd := chunk.getOffset() + uint64(chunk.getSize())
		if offset >= chunkEnd {
			continue
		}
		if chunk.getOffset() > offset+size {
			break
		}

		chunkReadOffset := uint64(0)
		if offset > chunk.getOffset() {
			chunkReadOffset = offset - chunk.getOffset()
		}

		chunkReadSize := uint64(chunk.getSize()) - chunkReadOffset
		if chunkReadSize > remaining {
			chunkReadSize = remaining
		}

		frameInfo := getFrame(chunk.getFrameIndex())
		if frameInfo == nil {
			return nil, fmt.Errorf("chunk frame not found: %d", chunk.getFrameIndex())
		}

		data, err := reader.ReadFrameData(*frameInfo, chunk.getFrameOffset()+chunkReadOffset, chunkReadSize)
		if err != nil {
			return nil, err
		}

		copy(result[resultOffset:], data)
		resultOffset += chunkReadSize
		remaining -= chunkReadSize
		offset += chunkReadSize

		if remaining == 0 {
			break
		}
	}

	return result, nil
}

// getDataFrameInfo gets frame info for a data frame index from TOC.
func getDataFrameInfo(frameIdx uint32, dataFrames []FrameInfo) *FrameInfo {
	if frameIdx < FirstDataFrameIndex {
		return nil
	}

	dataFrameIdx := int(frameIdx - FirstDataFrameIndex)
	if dataFrameIdx >= len(dataFrames) {
		return nil
	}

	return &dataFrames[dataFrameIdx]
}
