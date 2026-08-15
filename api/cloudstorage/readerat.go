// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
)

var (
	ErrReaderClosed   = errors.New("cloudstorage: reader closed")
	ErrReaderUnpinned = errors.New("cloudstorage: object has no consistency token")
)

const (
	DefaultReaderBlockSize   = 8 * 1024 * 1024
	DefaultReaderCacheBlocks = 4
	MaxReaderBlockSize       = 128 * 1024 * 1024
	MaxReaderCacheBlocks     = 64
	MaxReaderCacheBytes      = 256 * 1024 * 1024
)

type RangeReaderAtOptions struct {
	ETag        string
	BlockSize   int64
	CacheBlocks int
}

type cachedBlock struct {
	data []byte
	idx  int64
	use  uint64
}

type RangeReaderAt struct {
	storage Storage
	ctx     context.Context
	cancel  context.CancelFunc
	key     string
	etag    string
	blocks  []cachedBlock

	blockSize int64
	size      int64
	useSeq    uint64

	mu     sync.Mutex
	closed bool
}

var _ io.ReaderAt = (*RangeReaderAt)(nil)

func NewRangeReaderAt(ctx context.Context, storage Storage, key string, size int64, opts *RangeReaderAtOptions) *RangeReaderAt {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	blockSize := int64(DefaultReaderBlockSize)
	cacheBlocks := DefaultReaderCacheBlocks
	etag := ""
	if opts != nil {
		if opts.BlockSize > 0 {
			blockSize = opts.BlockSize
		}
		if blockSize > MaxReaderBlockSize {
			blockSize = MaxReaderBlockSize
		}
		if opts.CacheBlocks > 0 {
			cacheBlocks = opts.CacheBlocks
		}
		if cacheBlocks > MaxReaderCacheBlocks {
			cacheBlocks = MaxReaderCacheBlocks
		}
		etag = opts.ETag
	}
	if maxBlocks := int64(MaxReaderCacheBytes) / blockSize; int64(cacheBlocks) > maxBlocks {
		cacheBlocks = int(maxBlocks)
	}

	return &RangeReaderAt{
		storage:   storage,
		ctx:       ctx,
		cancel:    cancel,
		key:       key,
		etag:      etag,
		blockSize: blockSize,
		size:      size,
		blocks:    make([]cachedBlock, 0, cacheBlocks),
	}
}

func (r *RangeReaderAt) Size() int64 { return r.size }

func (r *RangeReaderAt) Key() string { return r.key }

func (r *RangeReaderAt) Close() error {
	r.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.blocks = nil
	return nil
}

func (r *RangeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("cloudstorage: negative read offset")
	}
	if len(p) == 0 {
		if off >= r.size {
			return 0, io.EOF
		}
		return 0, nil
	}
	if off >= r.size {
		return 0, io.EOF
	}

	n := 0
	for n < len(p) {
		pos := off + int64(n)
		if pos >= r.size {
			return n, io.EOF
		}
		block, err := r.block(pos / r.blockSize)
		if err != nil {
			return n, err
		}
		lo := int(pos - (pos/r.blockSize)*r.blockSize)
		if lo >= len(block) {
			return n, io.ErrUnexpectedEOF
		}
		n += copy(p[n:], block[lo:])
	}
	return n, nil
}

func (r *RangeReaderAt) block(idx int64) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, ErrReaderClosed
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}

	r.useSeq++
	for i := range r.blocks {
		if r.blocks[i].idx == idx {
			r.blocks[i].use = r.useSeq
			return r.blocks[i].data, nil
		}
	}

	data, err := r.fetch(idx)
	if err != nil {
		return nil, err
	}

	entry := cachedBlock{data: data, idx: idx, use: r.useSeq}
	if len(r.blocks) < cap(r.blocks) {
		r.blocks = append(r.blocks, entry)
		return data, nil
	}

	lru := 0
	for i := 1; i < len(r.blocks); i++ {
		if r.blocks[i].use < r.blocks[lru].use {
			lru = i
		}
	}
	r.blocks[lru] = entry
	return data, nil
}

func (r *RangeReaderAt) fetch(idx int64) ([]byte, error) {
	start := idx * r.blockSize
	end := start + r.blockSize
	if end > r.size {
		end = r.size
	}
	want := end - start

	var buf bytes.Buffer
	buf.Grow(int(want))

	opts := &DownloadOptions{
		Range: fmt.Sprintf("bytes=%d-%d", start, end-1),
	}
	if r.etag != "" {
		opts.IfMatch = r.etag
	}

	if err := r.storage.DownloadObject(r.ctx, r.key, &buf, opts); err != nil {
		return nil, err
	}
	if int64(buf.Len()) != want {
		return nil, fmt.Errorf("cloudstorage: range read returned %d bytes, want %d (object changed?): %w",
			buf.Len(), want, io.ErrUnexpectedEOF)
	}
	return buf.Bytes(), nil
}

type OpenReaderCmd struct {
	Storage Storage
	Key     string
}

var openReaderCmdPool = sync.Pool{New: func() any { return &OpenReaderCmd{} }}

func AcquireOpenReaderCmd() *OpenReaderCmd           { return openReaderCmdPool.Get().(*OpenReaderCmd) }
func (c *OpenReaderCmd) CmdID() dispatcher.CommandID { return OpenReader }
func (c *OpenReaderCmd) Release() {
	c.Storage = nil
	c.Key = ""
	openReaderCmdPool.Put(c)
}

type OpenReaderResponse struct {
	Error error
	ETag  string
	Size  int64
}
