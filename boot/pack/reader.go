package pack

import (
	"bytes"
	"crypto/sha256"
	"io"
	"io/fs"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/attrs"
	apipack "github.com/wippyai/runtime/api/boot/pack"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

const smallBufferSize = 64 * 1024 // 64KB

// smallBufferPool pools 64KB buffers for small reads.
var smallBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, smallBufferSize)
		return &buf
	},
}

// Reader reads pack files with lazy loading.
type Reader struct {
	reader     io.ReaderAt
	header     *Header
	footer     *Footer
	toc        *TOC
	handle     *codec.MsgpackHandle
	transcoder payload.Transcoder

	// Lazy loaded data
	metadata       attrs.Bag
	metadataOnce   sync.Once
	metadataErr    error
	entries        []registry.Entry
	entriesOnce    sync.Once
	entriesErr     error
	resources      map[string]interface{} // resource ID -> TreeResource or BlobResource
	resourcesMutex sync.RWMutex
}

// NewReader creates a pack reader using footer-first reading.
func NewReader(r io.ReaderAt, transcoder payload.Transcoder) (*Reader, error) {
	pr := &Reader{
		reader:     r,
		handle:     newMsgpackHandle(),
		transcoder: transcoder,
		resources:  make(map[string]interface{}),
	}

	// Read header
	headerBuf := make([]byte, headerSize)
	if _, err := r.ReadAt(headerBuf, 0); err != nil {
		return nil, NewReadHeaderError(err)
	}

	header, err := ReadHeader(bytes.NewReader(headerBuf))
	if err != nil {
		return nil, err
	}
	pr.header = header

	// Validate data size to prevent memory exhaustion (max 1GB)
	const maxDataSize = 1 << 30
	if header.DataSize > maxDataSize {
		return nil, NewDataSizeExceedsMaxError(header.DataSize, maxDataSize)
	}

	// Validate data hash
	dataBuf := make([]byte, header.DataSize)
	if _, err := r.ReadAt(dataBuf, int64(header.DataOffset)); err != nil { //nolint:gosec // Offset is validated by header structure
		return nil, NewReadDataError(err)
	}

	dataHash := sha256.Sum256(dataBuf)
	if !bytes.Equal(dataHash[:], header.DataHash[:]) {
		return nil, ErrDataCorrupted
	}

	// Read footer (need io.ReadSeeker)
	var rs io.ReadSeeker
	if seeker, ok := r.(io.ReadSeeker); ok {
		rs = seeker
	} else {
		rs = io.NewSectionReader(r, 0, 1<<63-1) // Max int64
	}
	footer, err := ReadFooter(rs)
	if err != nil {
		return nil, NewReadFooterError(err)
	}
	pr.footer = footer

	// Read and decompress TOC from footer location
	tocBuf := make([]byte, footer.TOCSize)
	if _, err := r.ReadAt(tocBuf, int64(footer.TOCOffset)); err != nil { //nolint:gosec // Offset is validated by footer structure
		return nil, NewReadTOCError(err)
	}

	toc, err := pr.decompressFrame(tocBuf)
	if err != nil {
		return nil, NewDecompressTOCError(err)
	}

	pr.toc = &TOC{}
	decoder := codec.NewDecoder(bytes.NewReader(toc), pr.handle)
	if err := decoder.Decode(pr.toc); err != nil {
		return nil, NewDecodeTOCError(err)
	}

	return pr, nil
}

// GetMetadata returns pack metadata (lazy loaded).
func (pr *Reader) GetMetadata() (attrs.Bag, error) {
	pr.metadataOnce.Do(func() {
		data, err := pr.readFrame(pr.toc.Metadata)
		if err != nil {
			pr.metadataErr = NewReadMetadataFrameError(err)
			return
		}

		decoder := codec.NewDecoder(bytes.NewReader(data), pr.handle)
		if err := decoder.Decode(&pr.metadata); err != nil {
			pr.metadataErr = NewDecodeMetadataError(err)
			return
		}
	})

	return pr.metadata, pr.metadataErr
}

// GetEntries returns registry entries (lazy loaded).
func (pr *Reader) GetEntries() ([]registry.Entry, error) {
	pr.entriesOnce.Do(func() {
		data, err := pr.readFrame(pr.toc.Entries)
		if err != nil {
			pr.entriesErr = NewReadEntriesFrameError(err)
			return
		}

		// Decode as []any to manually reconstruct entries
		var encodedEntries []any
		decoder := codec.NewDecoder(bytes.NewReader(data), pr.handle)
		if err := decoder.Decode(&encodedEntries); err != nil {
			pr.entriesErr = NewDecodeEntriesError(err)
			return
		}

		// Convert encoded entries back to registry entries
		entries := make([]registry.Entry, len(encodedEntries))
		for i, encEntryData := range encodedEntries {
			encEntryMap, ok := encEntryData.(map[string]any)
			if !ok {
				pr.entriesErr = NewInvalidEntryFormatError(i)
				return
			}

			var entry registry.Entry

			// Extract ID
			if idData, ok := encEntryMap["ID"]; ok {
				if idMap, ok := idData.(map[string]any); ok {
					ns, _ := idMap["ns"].(string)
					name, _ := idMap["name"].(string)
					entry.ID = registry.NewID(ns, name)
				}
			}

			// Extract Kind
			if kind, ok := encEntryMap["Kind"].(string); ok {
				entry.Kind = kind
			}

			// Extract Meta
			if metaData, ok := encEntryMap["Meta"].(map[string]any); ok {
				entry.Meta = attrs.Bag(metaData)
			}

			// Extract Data
			if dataMap, ok := encEntryMap["Data"].(map[string]any); ok {
				format := payload.Golang
				if f, ok := dataMap["Format"].(string); ok {
					format = payload.Format(f)
				}
				data := dataMap["Data"]
				entry.Data = payload.NewPayload(data, format)
			}

			entries[i] = entry
		}

		pr.entries = entries
	})

	return pr.entries, pr.entriesErr
}

// ListResources returns resource metadata from TOC (no lazy loading needed).
func (pr *Reader) ListResources() ([]apipack.ResourceInfo, error) {
	result := make([]apipack.ResourceInfo, len(pr.toc.Resources))
	for i, r := range pr.toc.Resources {
		result[i] = apipack.ResourceInfo{
			ID:        r.ID,
			Type:      r.Type,
			Meta:      r.Meta,
			Hash:      r.Frame.Hash,
			Size:      r.TotalSize,
			FileCount: r.FileCount,
		}
	}
	return result, nil
}

// GetFS returns filesystem for a tree resource (lazy loaded).
func (pr *Reader) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	res, err := pr.loadResource(id)
	if err != nil {
		return nil, err
	}

	tree, ok := res.(*TreeResource)
	if !ok {
		return nil, NewResourceNotTreeError(id.String())
	}

	return newPackFS(tree, pr), nil
}

// GetBlob returns blob reader (lazy loaded).
func (pr *Reader) GetBlob(id registry.ID) (apipack.BlobReader, error) {
	res, err := pr.loadResource(id)
	if err != nil {
		return nil, err
	}

	blob, ok := res.(*BlobResource)
	if !ok {
		return nil, NewResourceNotBlobError(id.String())
	}

	return newBlobReader(blob, pr), nil
}

// loadResource lazy loads a resource.
func (pr *Reader) loadResource(id registry.ID) (interface{}, error) {
	key := id.String()

	pr.resourcesMutex.RLock()
	if res, ok := pr.resources[key]; ok {
		pr.resourcesMutex.RUnlock()
		return res, nil
	}
	pr.resourcesMutex.RUnlock()

	// Find resource in TOC
	var resInfo *ResourceInfo
	for i := range pr.toc.Resources {
		if pr.toc.Resources[i].ID.Equal(id) {
			resInfo = &pr.toc.Resources[i]
			break
		}
	}

	if resInfo == nil {
		return nil, NewResourceNotFoundError(id.String())
	}

	// Read and decode resource
	data, err := pr.readFrame(resInfo.Frame)
	if err != nil {
		return nil, NewReadResourceFrameError(err)
	}

	var res interface{}
	decoder := codec.NewDecoder(bytes.NewReader(data), pr.handle)

	switch resInfo.Type {
	case "tree":
		tree := &TreeResource{}
		if err := decoder.Decode(tree); err != nil {
			return nil, NewDecodeTreeResourceError(err)
		}
		res = tree
	case "blob":
		blob := &BlobResource{}
		if err := decoder.Decode(blob); err != nil {
			return nil, NewDecodeBlobResourceError(err)
		}
		res = blob
	default:
		return nil, NewUnknownResourceTypeError(resInfo.Type)
	}

	// Cache it
	pr.resourcesMutex.Lock()
	pr.resources[key] = res
	pr.resourcesMutex.Unlock()

	return res, nil
}

// readFrame reads and decompresses a frame.
func (pr *Reader) readFrame(info FrameInfo) ([]byte, error) {
	buf := make([]byte, info.Size)
	if _, err := pr.reader.ReadAt(buf, int64(info.Offset)); err != nil { //nolint:gosec // Offset is validated by frame structure
		return nil, NewReadFrameError(err)
	}

	return pr.decompressFrame(buf)
}

// decompressFrame decompresses zstd compressed data.
func (pr *Reader) decompressFrame(compressed []byte) ([]byte, error) {
	return decompressZstd(compressed)
}

// FrameReader interface abstracts frame data reading for decoupling.
type FrameReader interface {
	ReadFrameData(frameInfo FrameInfo, offset, size uint64) ([]byte, error)
}

// ReadFrameData reads data from a specific frame at offset (for file content).
// Data frames are NOT decompressed (they contain raw file bytes).
func (pr *Reader) ReadFrameData(frameInfo FrameInfo, offset, size uint64) ([]byte, error) {
	return pr.readFrameData(frameInfo, offset, size)
}

// readFrameData reads data from a specific frame at offset (for file content).
// Data frames are NOT decompressed (they contain raw file bytes).
func (pr *Reader) readFrameData(frameInfo FrameInfo, offset, size uint64) ([]byte, error) {
	fileOffset := int64(frameInfo.Offset + offset) //nolint:gosec // Offset is validated by frame structure

	// Use pooled buffer for small reads
	if size <= smallBufferSize {
		bufPtr := smallBufferPool.Get().(*[]byte)
		defer smallBufferPool.Put(bufPtr)

		poolBuf := (*bufPtr)[:size]
		if _, err := pr.reader.ReadAt(poolBuf, fileOffset); err != nil {
			return nil, NewReadFrameDataError(err)
		}

		// Copy to result buffer
		result := make([]byte, size)
		copy(result, poolBuf)
		return result, nil
	}

	// Large reads allocate directly
	buf := make([]byte, size)
	if _, err := pr.reader.ReadAt(buf, fileOffset); err != nil {
		return nil, NewReadFrameDataError(err)
	}

	return buf, nil
}

// Packer provides high-level pack file operations.
type Packer struct {
	transcoder payload.Transcoder
}

// New creates a new Packer for reading pack files.
func New(transcoder payload.Transcoder) *Packer {
	return &Packer{
		transcoder: transcoder,
	}
}

// Unpack reads a pack file and returns its entries and metadata.
func (p *Packer) Unpack(r io.ReadSeeker) ([]registry.Entry, attrs.Bag, error) {
	var readerAt io.ReaderAt

	if ra, ok := r.(io.ReaderAt); ok {
		readerAt = ra
	} else {
		// Use io.NewSectionReader when r also implements ReaderAt
		if rs, ok := r.(interface {
			io.ReadSeeker
			io.ReaderAt
		}); ok {
			readerAt = io.NewSectionReader(rs, 0, 1<<63-1)
		} else {
			return nil, nil, ErrReaderMustImplementInterfaces
		}
	}

	pr, err := NewReader(readerAt, p.transcoder)
	if err != nil {
		return nil, nil, err
	}

	metadata, err := pr.GetMetadata()
	if err != nil {
		return nil, nil, NewGetMetadataError(err)
	}

	entries, err := pr.GetEntries()
	if err != nil {
		return nil, nil, NewGetEntriesError(err)
	}

	return entries, metadata, nil
}
