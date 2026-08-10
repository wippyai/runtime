// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rangeFakeStorage struct {
	etag     string
	data     []byte
	mu       sync.Mutex
	fetches  int
	failEtag bool
}

func (f *rangeFakeStorage) fetchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches
}

func (f *rangeFakeStorage) DownloadObject(_ context.Context, _ string, w io.Writer, opts *DownloadOptions) error {
	f.mu.Lock()
	f.fetches++
	f.mu.Unlock()

	if f.failEtag && opts != nil && opts.IfMatch != "" && opts.IfMatch != f.etag {
		return ErrPreconditionFailed
	}

	start, end := int64(0), int64(len(f.data))-1
	if opts != nil && opts.Range != "" {
		spec, ok := strings.CutPrefix(opts.Range, "bytes=")
		if !ok {
			return fmt.Errorf("bad range %q", opts.Range)
		}
		lo, hi, ok := strings.Cut(spec, "-")
		if !ok {
			return fmt.Errorf("bad range %q", opts.Range)
		}
		var err error
		if start, err = strconv.ParseInt(lo, 10, 64); err != nil {
			return err
		}
		if end, err = strconv.ParseInt(hi, 10, 64); err != nil {
			return err
		}
	}
	if start < 0 || start >= int64(len(f.data)) {
		return errors.New("range start out of bounds")
	}
	if end >= int64(len(f.data)) {
		end = int64(len(f.data)) - 1
	}
	_, err := w.Write(f.data[start : end+1])
	return err
}

func (f *rangeFakeStorage) ListObjects(context.Context, *ListObjectsOptions) (*ListObjectsResult, error) {
	panic("unexpected ListObjects")
}
func (f *rangeFakeStorage) HeadObject(context.Context, string) (*HeadObjectResult, error) {
	panic("unexpected HeadObject")
}
func (f *rangeFakeStorage) UploadObject(context.Context, string, io.Reader, *UploadOptions) error {
	panic("unexpected UploadObject")
}
func (f *rangeFakeStorage) DeleteObjects(context.Context, []string) error {
	panic("unexpected DeleteObjects")
}
func (f *rangeFakeStorage) PresignedGetURL(context.Context, string, *PresignedGetOptions) (string, error) {
	panic("unexpected PresignedGetURL")
}
func (f *rangeFakeStorage) PresignedPutURL(context.Context, string, *PresignedPutOptions) (string, error) {
	panic("unexpected PresignedPutURL")
}

func testPattern(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i % 251)
	}
	return data
}

func newTestReader(t *testing.T, size int64, blockSize int64, cacheBlocks int) (*RangeReaderAt, *rangeFakeStorage) {
	t.Helper()
	fake := &rangeFakeStorage{data: testPattern(int(size))}
	r := NewRangeReaderAt(context.Background(), fake, "obj.zip", size, &RangeReaderAtOptions{
		BlockSize:   blockSize,
		CacheBlocks: cacheBlocks,
	})
	return r, fake
}

func TestRangeReaderAt_FullSequentialRead(t *testing.T) {
	const size = 1000
	r, fake := newTestReader(t, size, 64, 2)

	got := make([]byte, size)
	n, err := r.ReadAt(got, 0)
	require.NoError(t, err)
	assert.Equal(t, size, n)
	assert.Equal(t, testPattern(size), got)
	// ceil(1000/64) = 16 block fetches, each exactly once.
	assert.Equal(t, 16, fake.fetchCount())
}

func TestRangeReaderAt_ReadViaIoSectionReader(t *testing.T) {
	// io.SectionReader is how archive/zip consumes an io.ReaderAt.
	const size = 4096
	r, _ := newTestReader(t, size, 512, 4)

	sec := io.NewSectionReader(r, 100, 300)
	got, err := io.ReadAll(sec)
	require.NoError(t, err)
	assert.Equal(t, testPattern(size)[100:400], got)
}

func TestRangeReaderAt_CrossBlockRead(t *testing.T) {
	const size = 256
	r, fake := newTestReader(t, size, 100, 4)

	got := make([]byte, 150)
	n, err := r.ReadAt(got, 75)
	require.NoError(t, err)
	assert.Equal(t, 150, n)
	assert.Equal(t, testPattern(size)[75:225], got)
	// Offsets 75..224 span blocks 0, 1, 2.
	assert.Equal(t, 3, fake.fetchCount())
}

func TestRangeReaderAt_CacheHits(t *testing.T) {
	const size = 300
	r, fake := newTestReader(t, size, 100, 4)

	buf := make([]byte, 50)
	_, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	_, err = r.ReadAt(buf, 25)
	require.NoError(t, err)
	_, err = r.ReadAt(buf, 50)
	require.NoError(t, err)
	assert.Equal(t, 1, fake.fetchCount(), "same block must be fetched once")
}

func TestRangeReaderAt_LRUEviction(t *testing.T) {
	const size = 400
	r, fake := newTestReader(t, size, 100, 2)

	buf := make([]byte, 10)
	for _, off := range []int64{0, 100, 200} { // fills blocks 0,1 then evicts 0
		_, err := r.ReadAt(buf, off)
		require.NoError(t, err)
	}
	assert.Equal(t, 3, fake.fetchCount())

	_, err := r.ReadAt(buf, 0) // block 0 was evicted → refetch
	require.NoError(t, err)
	assert.Equal(t, 4, fake.fetchCount())

	_, err = r.ReadAt(buf, 200) // block 2 still resident
	require.NoError(t, err)
	assert.Equal(t, 4, fake.fetchCount())
}

func TestRangeReaderAt_EOF(t *testing.T) {
	const size = 100
	r, _ := newTestReader(t, size, 64, 2)

	// Read ending exactly at EOF: full read, no error.
	got := make([]byte, 40)
	n, err := r.ReadAt(got, 60)
	require.NoError(t, err)
	assert.Equal(t, 40, n)

	// Read crossing EOF: partial read + io.EOF.
	n, err = r.ReadAt(make([]byte, 50), 80)
	assert.Equal(t, 20, n)
	assert.ErrorIs(t, err, io.EOF)

	// Read at EOF.
	n, err = r.ReadAt(got, 100)
	assert.Zero(t, n)
	assert.ErrorIs(t, err, io.EOF)

	// Read past EOF.
	n, err = r.ReadAt(got, 1000)
	assert.Zero(t, n)
	assert.ErrorIs(t, err, io.EOF)
}

func TestRangeReaderAt_NegativeOffset(t *testing.T) {
	r, _ := newTestReader(t, 10, 64, 2)
	_, err := r.ReadAt(make([]byte, 1), -1)
	assert.Error(t, err)
}

func TestRangeReaderAt_EmptyBuffer(t *testing.T) {
	r, _ := newTestReader(t, 10, 64, 2)
	n, err := r.ReadAt(nil, 0)
	assert.Zero(t, n)
	assert.NoError(t, err)
}

func TestRangeReaderAt_Close(t *testing.T) {
	r, _ := newTestReader(t, 10, 64, 2)
	require.NoError(t, r.Close())
	require.NoError(t, r.Close(), "close must be idempotent")

	_, err := r.ReadAt(make([]byte, 1), 0)
	assert.ErrorIs(t, err, ErrReaderClosed)
}

func TestRangeReaderAt_SizeAndKey(t *testing.T) {
	r, _ := newTestReader(t, 123, 64, 2)
	assert.Equal(t, int64(123), r.Size())
	assert.Equal(t, "obj.zip", r.Key())
}

func TestRangeReaderAt_ETagMismatchSurfaces(t *testing.T) {
	fake := &rangeFakeStorage{data: testPattern(100), etag: `"current"`, failEtag: true}
	r := NewRangeReaderAt(context.Background(), fake, "k", 100, &RangeReaderAtOptions{
		BlockSize: 64,
		ETag:      `"stale"`,
	})

	_, err := r.ReadAt(make([]byte, 10), 0)
	assert.ErrorIs(t, err, ErrPreconditionFailed)
}

func TestRangeReaderAt_ShortRangeResponse(t *testing.T) {
	// A backend returning fewer bytes than the requested range (object
	// shrank mid-read) must surface io.ErrUnexpectedEOF, not silence.
	fake := &rangeFakeStorage{data: testPattern(50)}
	r := NewRangeReaderAt(context.Background(), fake, "k", 100, &RangeReaderAtOptions{BlockSize: 64})

	_, err := r.ReadAt(make([]byte, 10), 0)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestRangeReaderAt_ConcurrentReads(t *testing.T) {
	const size = 4096
	r, _ := newTestReader(t, size, 256, 4)
	want := testPattern(size)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			buf := make([]byte, 128)
			for i := 0; i < 32; i++ {
				off := int64((seed*131 + i*97) % (size - len(buf)))
				n, err := r.ReadAt(buf, off)
				if err != nil {
					errs <- err
					return
				}
				if string(buf[:n]) != string(want[off:off+int64(n)]) {
					errs <- fmt.Errorf("mismatch at %d", off)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestRangeReaderAt_Defaults(t *testing.T) {
	fake := &rangeFakeStorage{data: testPattern(10)}
	r := NewRangeReaderAt(nil, fake, "k", 10, nil) // nil ctx must be tolerated
	assert.Equal(t, int64(DefaultReaderBlockSize), r.blockSize)
	assert.Equal(t, DefaultReaderCacheBlocks, cap(r.blocks))

	r2 := NewRangeReaderAt(context.Background(), fake, "k", 10, &RangeReaderAtOptions{CacheBlocks: 1000})
	assert.Equal(t, MaxReaderCacheBlocks, cap(r2.blocks))
}
