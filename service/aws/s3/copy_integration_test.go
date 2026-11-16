// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	apierror "github.com/wippyai/runtime/api/error"
)

func uploadForCopy(t *testing.T, storage *Storage, key string, body []byte, opts *cloudstorage.UploadOptions) {
	t.Helper()
	require.NoError(t, storage.UploadObject(context.Background(), key, bytes.NewReader(body), opts))
	t.Cleanup(func() { _ = storage.DeleteObjects(context.Background(), []string{key}) })
}

func downloadForCopy(t *testing.T, storage *Storage, key string) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, storage.DownloadObject(context.Background(), key, &buf, nil))
	return buf.Bytes()
}

func TestCopyObjectIntegration(t *testing.T) {
	storage := newIntegrationStorage(t)
	ctx := context.Background()
	prefix := "integration/copy/" + time.Now().Format("150405.000") + "/"

	t.Run("copies to a new key and keeps metadata", func(t *testing.T) {
		src, dst := prefix+"src.txt", prefix+"dst.txt"
		uploadForCopy(t, storage, src, []byte("payload"), &cloudstorage.UploadOptions{
			ContentType: "text/plain",
			Metadata:    map[string]string{"owner": "tests"},
		})
		t.Cleanup(func() { _ = storage.DeleteObjects(ctx, []string{dst}) })

		result, err := storage.CopyObject(ctx, src, dst, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, result.ETag)

		assert.Equal(t, []byte("payload"), downloadForCopy(t, storage, dst))
		head, err := storage.HeadObject(ctx, dst)
		require.NoError(t, err)
		assert.Equal(t, "text/plain", head.ContentType)
		assert.Equal(t, "tests", head.UserMetadata["owner"])
	})

	t.Run("replaces metadata", func(t *testing.T) {
		src, dst := prefix+"rsrc.txt", prefix+"rdst.txt"
		uploadForCopy(t, storage, src, []byte("payload"), &cloudstorage.UploadOptions{
			Metadata: map[string]string{"owner": "tests"},
		})
		t.Cleanup(func() { _ = storage.DeleteObjects(ctx, []string{dst}) })

		_, err := storage.CopyObject(ctx, src, dst, &cloudstorage.CopyOptions{
			MetadataDirective: cloudstorage.MetadataDirectiveReplace,
			Metadata:          map[string]string{"stage": "two"},
			ContentType:       "application/json",
		})
		require.NoError(t, err)

		head, err := storage.HeadObject(ctx, dst)
		require.NoError(t, err)
		assert.Equal(t, "application/json", head.ContentType)
		assert.Equal(t, "two", head.UserMetadata["stage"])
		assert.NotContains(t, head.UserMetadata, "owner")
	})

	t.Run("copy onto itself with REPLACE refreshes last modified", func(t *testing.T) {
		key := prefix + "self.txt"
		uploadForCopy(t, storage, key, []byte("immutable"), &cloudstorage.UploadOptions{ContentType: "text/plain"})
		before, err := storage.HeadObject(ctx, key)
		require.NoError(t, err)

		time.Sleep(2100 * time.Millisecond)
		result, err := storage.CopyObject(ctx, key, key, &cloudstorage.CopyOptions{
			MetadataDirective: cloudstorage.MetadataDirectiveReplace,
			ContentType:       "text/plain",
			Metadata:          map[string]string{"touched": "yes"},
		})
		require.NoError(t, err)

		after, err := storage.HeadObject(ctx, key)
		require.NoError(t, err)
		assert.True(t, after.LastModified.After(before.LastModified), "last modified %v is not after %v", after.LastModified, before.LastModified)
		assert.False(t, result.LastModified.IsZero())
		assert.Equal(t, "yes", after.UserMetadata["touched"])
		assert.Equal(t, []byte("immutable"), downloadForCopy(t, storage, key))
	})

	t.Run("if_match mismatch is a precondition failure", func(t *testing.T) {
		src, dst := prefix+"psrc.txt", prefix+"pdst.txt"
		uploadForCopy(t, storage, src, []byte("payload"), nil)
		t.Cleanup(func() { _ = storage.DeleteObjects(ctx, []string{dst}) })

		_, err := storage.CopyObject(ctx, src, dst, &cloudstorage.CopyOptions{IfMatch: `"deadbeef"`})
		requireClassified(t, err, apierror.Conflict, apierror.False)
		require.ErrorIs(t, err, cloudstorage.ErrPreconditionFailed)

		head, err := storage.HeadObject(ctx, src)
		require.NoError(t, err)
		_, err = storage.CopyObject(ctx, src, dst, &cloudstorage.CopyOptions{IfMatch: head.ETag})
		require.NoError(t, err)
	})

	t.Run("missing source is not found", func(t *testing.T) {
		_, err := storage.CopyObject(ctx, prefix+"absent.txt", prefix+"never.txt", nil)
		requireClassified(t, err, apierror.NotFound, apierror.False)
		require.ErrorIs(t, err, cloudstorage.ErrNotFound)
	})

	t.Run("read-only credentials are denied", func(t *testing.T) {
		src, dst := prefix+"dsrc.txt", prefix+"ddst.txt"
		uploadForCopy(t, storage, src, []byte("payload"), nil)

		if os.Getenv("S3_RO_ACCESS_KEY") == "" {
			t.Skip("set S3_RO_ACCESS_KEY and S3_RO_SECRET_KEY for an identity without write access")
		}
		readOnly := withCredentials(storage, os.Getenv("S3_RO_ACCESS_KEY"), os.Getenv("S3_RO_SECRET_KEY"))
		_, err := readOnly.CopyObject(ctx, src, dst, nil)
		requireClassified(t, err, apierror.PermissionDenied, apierror.False)
	})

	t.Run("multipart copy above the single request limit", func(t *testing.T) {
		src, dst := prefix+"big.bin", prefix+"big-copy.bin"
		body := make([]byte, 11<<20)
		for i := range body {
			body[i] = byte(i % 251)
		}
		uploadForCopy(t, storage, src, body, &cloudstorage.UploadOptions{
			ContentType: "application/octet-stream",
			Metadata:    map[string]string{"owner": "tests"},
		})
		t.Cleanup(func() { _ = storage.DeleteObjects(ctx, []string{dst}) })

		small := *storage
		small.copySingleLimit = 5 << 20
		small.copyPartSize = 5 << 20
		result, err := small.CopyObject(ctx, src, dst, &cloudstorage.CopyOptions{IfNoneMatch: "*"})
		require.NoError(t, err)
		assert.NotEmpty(t, result.ETag)
		assert.False(t, result.LastModified.IsZero())

		assert.True(t, bytes.Equal(body, downloadForCopy(t, storage, dst)))
		head, err := storage.HeadObject(ctx, dst)
		require.NoError(t, err)
		assert.Equal(t, "tests", head.UserMetadata["owner"])

		_, err = small.CopyObject(ctx, src, dst, &cloudstorage.CopyOptions{IfNoneMatch: "*"})
		requireClassified(t, err, apierror.Conflict, apierror.False)
	})

	t.Run("if_none_match star refuses an existing destination", func(t *testing.T) {
		src, dst := prefix+"nsrc.txt", prefix+"ndst.txt"
		uploadForCopy(t, storage, src, []byte("one"), nil)
		uploadForCopy(t, storage, dst, []byte("two"), nil)

		_, err := storage.CopyObject(ctx, src, dst, &cloudstorage.CopyOptions{IfNoneMatch: "*"})
		requireClassified(t, err, apierror.Conflict, apierror.False)
		assert.Equal(t, []byte("two"), downloadForCopy(t, storage, dst))
	})
}
