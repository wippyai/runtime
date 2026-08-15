// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
)

func putPresigned(t *testing.T, url string, body []byte) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url, bytes.NewReader(body))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "part PUT failed: %s", payload)
	etag := strings.Trim(resp.Header.Get("ETag"), `"`)
	require.NotEmpty(t, etag, "part PUT returned no ETag")
	return etag
}

func TestMultipartUploadIntegration(t *testing.T) {
	storage := newIntegrationStorage(t)
	ctx := context.Background()

	key := "integration/multipart.bin"
	t.Cleanup(func() { _ = storage.DeleteObjects(context.Background(), []string{key}) })

	part1 := bytes.Repeat([]byte{'a'}, 5<<20)
	part2 := bytes.Repeat([]byte{'b'}, 1024)

	created, err := storage.CreateMultipartUpload(ctx, key, &cloudstorage.CreateMultipartUploadOptions{
		ContentType: "application/octet-stream",
		Metadata:    map[string]string{"source": "multipart-integration"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.UploadID)

	urls, err := storage.PresignedUploadPartURLs(ctx, key, created.UploadID, &cloudstorage.PresignedUploadPartOptions{
		PartNumbers: []int32{1, 2},
	})
	require.NoError(t, err)
	require.Len(t, urls, 2)

	etag1 := putPresigned(t, urls[0].URL, part1)
	etag2 := putPresigned(t, urls[1].URL, part2)

	done, err := storage.CompleteMultipartUpload(ctx, key, created.UploadID, []cloudstorage.CompletedPart{
		{PartNumber: 2, ETag: etag2},
		{PartNumber: 1, ETag: etag1},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, done.ETag)

	head, err := storage.HeadObject(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, int64(len(part1)+len(part2)), head.Size)
	assert.Equal(t, "multipart-integration", head.UserMetadata["source"])

	var buf bytes.Buffer
	require.NoError(t, storage.DownloadObject(ctx, key, &buf, &cloudstorage.DownloadOptions{
		Range: "bytes=5242876-5242883", // last 4 bytes of part1 + first 4 of part2
	}))
	assert.Equal(t, "aaaabbbb", buf.String())
}

func TestMultipartAbortIntegration(t *testing.T) {
	storage := newIntegrationStorage(t)
	ctx := context.Background()

	key := "integration/multipart-aborted.bin"
	created, err := storage.CreateMultipartUpload(ctx, key, nil)
	require.NoError(t, err)

	require.NoError(t, storage.AbortMultipartUpload(ctx, key, created.UploadID))

	_, err = storage.CompleteMultipartUpload(ctx, key, created.UploadID, []cloudstorage.CompletedPart{
		{PartNumber: 1, ETag: "deadbeef"},
	})
	assert.Error(t, err, "completing an aborted upload must fail")
}

func TestRangeReaderZipIntegration(t *testing.T) {
	storage := newIntegrationStorage(t)
	ctx := context.Background()

	big := bytes.Repeat([]byte("0123456789abcdef"), 20*1024) // 320 KiB
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w1, err := zw.Create("hello.txt")
	require.NoError(t, err)
	_, err = w1.Write([]byte("hello from s3"))
	require.NoError(t, err)
	w2, err := zw.Create("data/big.bin")
	require.NoError(t, err)
	_, err = w2.Write(big)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	key := "integration/reader.zip"
	t.Cleanup(func() { _ = storage.DeleteObjects(context.Background(), []string{key}) })
	require.NoError(t, storage.UploadObject(ctx, key, bytes.NewReader(zipBuf.Bytes()), &cloudstorage.UploadOptions{
		ContentType: "application/zip",
	}))

	head, err := storage.HeadObject(ctx, key)
	require.NoError(t, err)
	require.Equal(t, int64(zipBuf.Len()), head.Size)

	reader := cloudstorage.NewRangeReaderAt(ctx, storage, key, head.Size, &cloudstorage.RangeReaderAtOptions{
		BlockSize:   64 * 1024,
		CacheBlocks: 2,
		ETag:        head.ETag,
	})
	defer func() { _ = reader.Close() }()

	zr, err := zip.NewReader(reader, head.Size)
	require.NoError(t, err, "zip central directory must parse over ranged reads")
	require.Len(t, zr.File, 2)

	rc, err := zr.Open("hello.txt")
	require.NoError(t, err)
	small, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "hello from s3", string(small))

	rc, err = zr.Open("data/big.bin")
	require.NoError(t, err)
	gotBig, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.True(t, bytes.Equal(big, gotBig), "big entry must round-trip byte-identical")
}
