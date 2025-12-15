// Package pack provides compressed binary archive format for Wippy resources.
//
// Format: Header(260) + DataFrames + CompressedTOC + Footer(16)
// - Per-file compression based on type (text compressed, images/binaries raw)
// - Data frames store files as-is (no additional compression)
// - Only metadata/entries/TOC frames use frame-level compression
// - Footer-first reading for streaming support
package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/klauspost/compress/zstd"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

const (
	// Magic header identifying Wippy pack files
	magic = "WIPPYPACK"

	// Max size for a data frame (10MB)
	maxFrameSize = 10 * 1024 * 1024
)

// DefaultCompressionFunc provides default logic for compression decision.
// Skips compression for already-compressed formats (images, media, archives).
var DefaultCompressionFunc = func(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))

	skipExts := map[string]bool{
		// Images
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".webp": true, ".ico": true,
		// Fonts
		".woff": true, ".woff2": true, ".ttf": true, ".otf": true,
		// Media
		".mp4": true, ".webm": true, ".mp3": true, ".ogg": true,
		".wav": true, ".avi": true, ".mov": true,
		// Already compressed
		".gz": true, ".zip": true, ".br": true, ".zst": true,
		".7z": true, ".rar": true, ".bz2": true, ".xz": true,
	}

	return !skipExts[ext]
}

// encodedPayload is an intermediate structure for msgpack serialization
type encodedPayload struct {
	Format payload.Format
	Data   any
}

// encodedEntry is an intermediate structure for msgpack serialization
type encodedEntry struct {
	ID   registry.ID
	Kind string
	Meta attrs.Bag
	Data *encodedPayload
}

// ProgressCallback is called during packing to report progress.
// Parameters: resourceID, currentFile, totalFiles
type ProgressCallback func(resourceID registry.ID, current, total int)

// Writer writes pack files with filesystem integration.
type Writer struct {
	transcoder       payload.Transcoder
	handle           *codec.MsgpackHandle
	metadataLevel    zstd.EncoderLevel
	entriesLevel     zstd.EncoderLevel
	tocLevel         zstd.EncoderLevel
	compressionFunc  func(string) bool
	progressCallback ProgressCallback
}

// WriterOption configures Writer.
type WriterOption func(*Writer)

// WithProgressCallback sets a callback for tracking packing progress.
func WithProgressCallback(fn ProgressCallback) WriterOption {
	return func(w *Writer) {
		w.progressCallback = fn
	}
}

// NewWriter creates a new pack writer.
func NewWriter(transcoder payload.Transcoder, opts ...WriterOption) *Writer {
	pw := &Writer{
		transcoder:      transcoder,
		handle:          newMsgpackHandle(),
		metadataLevel:   zstd.SpeedDefault,
		entriesLevel:    zstd.SpeedDefault,
		tocLevel:        zstd.SpeedDefault,
		compressionFunc: DefaultCompressionFunc,
	}

	for _, opt := range opts {
		opt(pw)
	}

	return pw
}

// PackEntries creates a pack file with only metadata and entries (no resources).
func (pw *Writer) PackEntries(
	metadata attrs.Bag,
	entries []registry.Entry,
	w io.Writer,
) error {
	// Convert entries to encoded format
	encodedEntries := make([]encodedEntry, len(entries))
	for i, entry := range entries {
		encoded, err := pw.normalizeEntry(entry)
		if err != nil {
			return NewNormalizeEntryError(i, err)
		}
		encodedEntries[i] = encoded
	}

	// Create metadata and entries frames (compressed)
	metaFrame, metaInfo, err := pw.createMetadataFrame(metadata)
	if err != nil {
		return NewCreateMetadataFrameError(err)
	}

	entriesFrame, entriesInfo, err := pw.createEntriesFrame(encodedEntries)
	if err != nil {
		return NewCreateEntriesFrameError(err)
	}

	// Build TOC with no resources
	toc := &TOC{
		Metadata:   metaInfo,
		Entries:    entriesInfo,
		Resources:  nil,
		DataFrames: nil,
	}

	// Write pack with just metadata and entries frames
	allFrames := []rawFrame{metaFrame, entriesFrame}
	return pw.writePack(w, toc, allFrames)
}

// Pack creates a pack file from filesystem and registry entries.
func (pw *Writer) Pack(
	metadata attrs.Bag,
	entries []registry.Entry,
	fsys fs.FS,
	resourceID registry.ID,
	resourceMeta attrs.Bag,
	w io.Writer,
) error {
	// Convert entries to encoded format
	encodedEntries := make([]encodedEntry, len(entries))
	for i, entry := range entries {
		encoded, err := pw.normalizeEntry(entry)
		if err != nil {
			return NewNormalizeEntryError(i, err)
		}
		encodedEntries[i] = encoded
	}

	// Create metadata and entries frames (compressed)
	metaFrame, metaInfo, err := pw.createMetadataFrame(metadata)
	if err != nil {
		return NewCreateMetadataFrameError(err)
	}

	entriesFrame, entriesInfo, err := pw.createEntriesFrame(encodedEntries)
	if err != nil {
		return NewCreateEntriesFrameError(err)
	}

	// Process filesystem and create tree resource with data frames
	tree, dataFrames, err := pw.processFilesystem(fsys, resourceID, resourceMeta)
	if err != nil {
		return NewProcessFilesystemError(err)
	}

	// Create resource frame (compressed TOC of tree structure)
	resourceFrame, resourceInfo, err := pw.createResourceFrame(tree)
	if err != nil {
		return NewCreateResourceFrameError("", err)
	}

	// Build TOC
	toc := &TOC{
		Metadata:  metaInfo,
		Entries:   entriesInfo,
		Resources: []ResourceInfo{resourceInfo},
	}

	// Combine all frames
	allFrames := []rawFrame{metaFrame, entriesFrame, resourceFrame}
	allFrames = append(allFrames, dataFrames...)

	// Write pack file
	return pw.writePack(w, toc, allFrames)
}

// PackWithResources creates a pack file with multiple filesystem resources.
// This is used by the pack command to embed multiple directories.
func (pw *Writer) PackWithResources(
	metadata attrs.Bag,
	entries []registry.Entry,
	resources []ResourceSpec,
	w io.Writer,
) error {
	// Convert entries to encoded format
	encodedEntries := make([]encodedEntry, len(entries))
	for i, entry := range entries {
		encoded, err := pw.normalizeEntry(entry)
		if err != nil {
			return NewNormalizeEntryError(i, err)
		}
		encodedEntries[i] = encoded
	}

	// Create metadata and entries frames (compressed)
	metaFrame, metaInfo, err := pw.createMetadataFrame(metadata)
	if err != nil {
		return NewCreateMetadataFrameError(err)
	}

	entriesFrame, entriesInfo, err := pw.createEntriesFrame(encodedEntries)
	if err != nil {
		return NewCreateEntriesFrameError(err)
	}

	// Process all filesystem resources
	resourceInfos := make([]ResourceInfo, 0, len(resources))
	resourceFrames := make([]rawFrame, 0, len(resources))
	allDataFrames := make([]rawFrame, 0)

	// Data frames start after: metadata(1) + entries(1) + resource trees(N)
	if len(resources) < 0 || len(resources) > int(^uint32(0)-2) {
		return NewInvalidResourceCountError(len(resources))
	}
	dataFrameStartIndex := 2 + uint32(len(resources)) //nolint:gosec // bounds checked above

	for _, spec := range resources {
		if len(allDataFrames) < 0 || len(allDataFrames) > int(^uint32(0)) {
			return NewInvalidResourceCountError(len(allDataFrames))
		}
		tree, dataFrames, err := pw.processFilesystemWithOffset(spec.FS, spec.ID, spec.Meta, dataFrameStartIndex+uint32(len(allDataFrames))) //nolint:gosec // bounds checked above
		if err != nil {
			return NewProcessResourceFilesystemError(spec.ID.String(), err)
		}

		// Create resource frame for this tree
		resourceFrame, resourceInfo, err := pw.createResourceFrame(tree)
		if err != nil {
			return NewCreateResourceFrameError(spec.ID.String(), err)
		}

		// Store resource info and frames separately
		resourceInfos = append(resourceInfos, resourceInfo)
		resourceFrames = append(resourceFrames, resourceFrame)
		allDataFrames = append(allDataFrames, dataFrames...)
	}

	// Build TOC
	toc := &TOC{
		Metadata:  metaInfo,
		Entries:   entriesInfo,
		Resources: resourceInfos,
	}

	// Combine all frames: metadata, entries, all resource frames, then all data frames
	allFrames := []rawFrame{metaFrame, entriesFrame}
	allFrames = append(allFrames, resourceFrames...)
	allFrames = append(allFrames, allDataFrames...)

	// Write pack file
	return pw.writePack(w, toc, allFrames)
}

// rawFrame represents a frame with its data
type rawFrame struct {
	data             []byte
	compressed       bool // whether this frame itself is compressed
	uncompressedSize uint64
}

// processFilesystem walks filesystem, compresses files per-policy, creates data frames
func (pw *Writer) processFilesystem(
	fsys fs.FS,
	id registry.ID,
	meta attrs.Bag,
) (*TreeResource, []rawFrame, error) {
	return pw.processFilesystemWithOffset(fsys, id, meta, 3)
}

// processFilesystemWithOffset walks filesystem with explicit starting frame index
func (pw *Writer) processFilesystemWithOffset(
	fsys fs.FS,
	id registry.ID,
	meta attrs.Bag,
	startFrameIndex uint32,
) (*TreeResource, []rawFrame, error) {
	tree := &TreeResource{
		ID:    id,
		Meta:  meta,
		Files: make(map[string]FileEntry),
		Dirs:  make(map[string][]string),
	}

	// Count total files first for progress reporting
	totalFiles := 0
	if pw.progressCallback != nil {
		_ = fs.WalkDir(fsys, ".", func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			totalFiles++
			return nil
		})
	}

	var dataFrames []rawFrame
	currentFrame := &bytes.Buffer{}
	frameIndex := startFrameIndex
	filesProcessed := 0

	err := fs.WalkDir(fsys, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Clean path
		filePath = path.Clean(filePath)
		if filePath == "." {
			filePath = ""
		}

		if d.IsDir() {
			if filePath != "" {
				tree.Dirs[filePath] = []string(nil)
			}
			return nil
		}

		// Read file
		fileInfo, err := d.Info()
		if err != nil {
			return NewStatFileError(filePath, err)
		}

		file, err := fsys.Open(filePath)
		if err != nil {
			return NewOpenFileError(filePath, err)
		}

		fileData, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			return NewReadFileError(filePath, err)
		}

		// Compute hash of original data
		hashBytes := sha256.Sum256(fileData)
		hash := hex.EncodeToString(hashBytes[:])

		// Decide if we should compress this file
		shouldCompress := pw.shouldCompressFile(filePath)

		var finalData []byte
		var compressed bool

		if shouldCompress && len(fileData) > 0 {
			// Compress individual file
			var buf bytes.Buffer
			zw, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
			if err != nil {
				return NewCreateZstdWriterError(filePath, err)
			}
			defer func() { _ = zw.Close() }()

			if _, err := zw.Write(fileData); err != nil {
				return NewCompressFileError(filePath, err)
			}

			if err := zw.Close(); err != nil {
				return NewCloseZstdWriterError(filePath, err)
			}

			finalData = buf.Bytes()
			compressed = true
		} else {
			finalData = fileData
			compressed = false
		}

		// Handle chunking for large files (> ChunkSize)
		var chunks []ChunkInfo
		var location FileLocation

		if uint64(len(finalData)) > ChunkSize {
			// Large file - split into chunks
			chunks = make([]ChunkInfo, 0)
			dataOffset := uint64(0)

			for dataOffset < uint64(len(finalData)) {
				chunkSize := ChunkSize
				if dataOffset+chunkSize > uint64(len(finalData)) {
					chunkSize = uint64(len(finalData)) - dataOffset
				}

				chunkData := finalData[dataOffset : dataOffset+chunkSize]

				// Check if chunk fits in current frame
				if currentFrame.Len()+len(chunkData) > maxFrameSize && currentFrame.Len() > 0 {
					// Close current frame
					dataFrames = append(dataFrames, rawFrame{
						data:             currentFrame.Bytes(),
						compressed:       false,
						uncompressedSize: uint64(currentFrame.Len()), //nolint:gosec // Frame size is bounded by maxFrameSize
					})
					currentFrame = &bytes.Buffer{}
					frameIndex++
				}

				// Record chunk info
				chunks = append(chunks, ChunkInfo{
					Offset:      dataOffset,
					Size:        uint32(chunkSize), //nolint:gosec // Chunk size is bounded by chunkSizeThreshold
					FrameIndex:  frameIndex,
					FrameOffset: uint64(currentFrame.Len()), //nolint:gosec // Frame size is bounded by maxFrameSize
				})

				// Write chunk to current frame
				currentFrame.Write(chunkData)
				dataOffset += chunkSize
			}

			// Set location for first chunk (for compatibility)
			location = FileLocation{
				FrameIndex: chunks[0].FrameIndex,
				Offset:     chunks[0].FrameOffset,
				Chunks:     chunks,
			}
		} else {
			// Small file - no chunking
			// Check if we need a new frame
			if currentFrame.Len()+len(finalData) > maxFrameSize && currentFrame.Len() > 0 {
				// Close current frame
				dataFrames = append(dataFrames, rawFrame{
					data:             currentFrame.Bytes(),
					compressed:       false,
					uncompressedSize: uint64(currentFrame.Len()), //nolint:gosec // Frame size is bounded by maxFrameSize
				})
				currentFrame = &bytes.Buffer{}
				frameIndex++
			}

			location = FileLocation{
				FrameIndex: frameIndex,
				Offset:     uint64(currentFrame.Len()), //nolint:gosec // Frame size is bounded by maxFrameSize
				Chunks:     nil,
			}

			// Write file to current frame
			currentFrame.Write(finalData)
		}

		// Store file entry
		entry := FileEntry{
			Size:       uint64(len(fileData)),
			Mode:       uint32(fileInfo.Mode()),
			ModTime:    fileInfo.ModTime().Unix(),
			Hash:       hash,
			Compressed: compressed,
			Meta:       nil,
			Location:   location,
		}

		// Store compressed size if file was compressed
		if compressed {
			entry.CompressedSize = uint64(len(finalData))
		}

		tree.Files[filePath] = entry

		// Update parent directory
		dir := path.Dir(filePath)
		if dir == "." {
			dir = ""
		}
		if children, ok := tree.Dirs[dir]; ok {
			tree.Dirs[dir] = append(children, path.Base(filePath))
		} else {
			tree.Dirs[dir] = []string{path.Base(filePath)}
		}

		// Report progress
		filesProcessed++
		if pw.progressCallback != nil && totalFiles > 0 {
			pw.progressCallback(id, filesProcessed, totalFiles)
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	// Close final frame if it has data
	if currentFrame.Len() > 0 {
		dataFrames = append(dataFrames, rawFrame{
			data:             currentFrame.Bytes(),
			compressed:       false,
			uncompressedSize: uint64(currentFrame.Len()), //nolint:gosec // Frame size is bounded by maxFrameSize
		})
	}

	return tree, dataFrames, nil
}

// shouldCompressFile decides if a file should be compressed.
func (pw *Writer) shouldCompressFile(filename string) bool {
	return pw.compressionFunc(filename)
}

// createMetadataFrame creates compressed metadata frame
func (pw *Writer) createMetadataFrame(metadata attrs.Bag) (rawFrame, FrameInfo, error) {
	return pw.createCompressedFrame(metadata, pw.metadataLevel)
}

// createEntriesFrame creates compressed entries frame
func (pw *Writer) createEntriesFrame(entries []encodedEntry) (rawFrame, FrameInfo, error) {
	return pw.createCompressedFrame(entries, pw.entriesLevel)
}

// createResourceFrame creates compressed resource frame
func (pw *Writer) createResourceFrame(tree *TreeResource) (rawFrame, ResourceInfo, error) {
	frame, frameInfo, err := pw.createCompressedFrame(tree, zstd.SpeedDefault)
	if err != nil {
		return rawFrame{}, ResourceInfo{}, err
	}

	var totalSize uint64
	for _, f := range tree.Files {
		totalSize += f.Size
	}

	return frame, ResourceInfo{
		ID:        tree.ID,
		Type:      "tree",
		Meta:      tree.Meta,
		Frame:     frameInfo,
		FileCount: uint32(len(tree.Files)), //nolint:gosec // File count is bounded by filesystem constraints
		TotalSize: totalSize,
	}, nil
}

// createCompressedFrame creates a msgpack-encoded and zstd-compressed frame
func (pw *Writer) createCompressedFrame(data interface{}, level zstd.EncoderLevel) (rawFrame, FrameInfo, error) {
	// Encode with msgpack
	var buf bytes.Buffer
	encoder := codec.NewEncoder(&buf, pw.handle)
	if err := encoder.Encode(data); err != nil {
		return rawFrame{}, FrameInfo{}, NewMsgpackEncodeError(err)
	}

	uncompData := buf.Bytes()
	uncompSize := uint64(len(uncompData))

	// Compress with zstd
	var compBuf bytes.Buffer
	zw, err := zstd.NewWriter(&compBuf, zstd.WithEncoderLevel(level))
	if err != nil {
		return rawFrame{}, FrameInfo{}, NewCreateZstdWriterError("", err)
	}
	defer func() { _ = zw.Close() }()

	if _, err := zw.Write(uncompData); err != nil {
		return rawFrame{}, FrameInfo{}, NewZstdCompressError(err)
	}

	if err := zw.Close(); err != nil {
		return rawFrame{}, FrameInfo{}, NewCloseZstdWriterError("", err)
	}

	compData := compBuf.Bytes()

	// Compute hash of compressed data
	hashBytes := sha256.Sum256(compData)
	hash := hex.EncodeToString(hashBytes[:])

	return rawFrame{
			data:             compData,
			compressed:       true,
			uncompressedSize: uncompSize,
		}, FrameInfo{
			Size:             uint64(len(compData)),
			UncompressedSize: uncompSize,
			Hash:             hash,
		}, nil
}

// writePack writes the complete pack file
func (pw *Writer) writePack(w io.Writer, toc *TOC, frames []rawFrame) error {
	dataOffset := uint64(headerSize)
	currentOffset := dataOffset

	// Calculate offsets for all frames
	if len(frames) >= 1 {
		toc.Metadata.Offset = currentOffset
		currentOffset += uint64(len(frames[0].data))
	}

	if len(frames) >= 2 {
		toc.Entries.Offset = currentOffset
		currentOffset += uint64(len(frames[1].data))
	}

	// Update offsets for resource frames
	// Frame structure: metadata, entries, all resource frames, then all data frames
	numResources := len(toc.Resources)
	frameIdx := 2

	// Validate frame count
	expectedMinFrames := 2 + numResources // metadata + entries + resources
	if len(frames) < expectedMinFrames {
		return NewInsufficientFramesError(len(frames), expectedMinFrames)
	}

	// Process all resource frames
	for i := range toc.Resources {
		toc.Resources[i].Frame.Offset = currentOffset
		toc.Resources[i].Frame.Size = uint64(len(frames[frameIdx].data))
		toc.Resources[i].Frame.UncompressedSize = frames[frameIdx].uncompressedSize
		currentOffset += uint64(len(frames[frameIdx].data))
		frameIdx++
	}

	// Process all data frames (after resource frames)
	dataFrameStart := 2 + numResources
	numDataFrames := len(frames) - dataFrameStart
	toc.DataFrames = make([]FrameInfo, numDataFrames)

	for i := 0; i < numDataFrames; i++ {
		toc.DataFrames[i] = FrameInfo{
			Offset:           currentOffset,
			Size:             uint64(len(frames[frameIdx].data)),
			UncompressedSize: frames[frameIdx].uncompressedSize,
			Hash:             "",
		}
		currentOffset += uint64(len(frames[frameIdx].data))
		frameIdx++
	}

	// Calculate total data size
	dataSize := uint64(0)
	for _, frame := range frames {
		dataSize += uint64(len(frame.data))
	}

	// Prepare header
	header := &Header{}
	header.DataOffset = dataOffset
	header.DataSize = dataSize

	// Compute data hash
	hasher := sha256.New()
	for _, frame := range frames {
		hasher.Write(frame.data)
	}
	dataHashBytes := hasher.Sum(nil)
	copy(header.DataHash[:], dataHashBytes)

	// Write header
	if err := WriteHeader(w, header); err != nil {
		return NewWriteHeaderError(err)
	}

	// Write all data frames
	for i, frame := range frames {
		if _, err := w.Write(frame.data); err != nil {
			return NewWriteFrameError(i, err)
		}
	}

	// TOC starts after all data frames
	tocOffset := dataOffset + dataSize

	// Serialize and compress TOC
	tocData, tocInfo, err := pw.createCompressedFrame(toc, pw.tocLevel)
	if err != nil {
		return NewCreateTOCFrameError(err)
	}

	// Write TOC frame
	if _, err := w.Write(tocData.data); err != nil {
		return NewWriteTOCError(err)
	}

	// Write footer
	footer := &Footer{
		TOCOffset: tocOffset,
		TOCSize:   tocInfo.Size,
	}

	if err := WriteFooter(w, footer); err != nil {
		return NewWriteFooterError(err)
	}

	return nil
}

// normalizeEntry normalizes entry payload to encoded format for msgpack.
func (pw *Writer) normalizeEntry(entry registry.Entry) (encodedEntry, error) {
	var encPayload *encodedPayload

	if entry.Data != nil {
		normalized := entry.Data
		var err error

		if entry.Data.Format() != payload.String &&
			entry.Data.Format() != payload.Bytes &&
			entry.Data.Format() != payload.GoError &&
			entry.Data.Format() != payload.Golang {
			normalized, err = pw.transcoder.Transcode(entry.Data, payload.Golang)
			if err != nil {
				return encodedEntry{}, NewTranscodePayloadError(err)
			}
		}

		encPayload = &encodedPayload{
			Format: normalized.Format(),
			Data:   normalized.Data(),
		}
	}

	return encodedEntry{
		ID:   entry.ID,
		Kind: entry.Kind,
		Meta: entry.Meta,
		Data: encPayload,
	}, nil
}
