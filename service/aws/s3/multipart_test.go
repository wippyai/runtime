// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	neturl "net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	"go.uber.org/zap"
)

func presignTestStorage(t *testing.T) *Storage {
	t.Helper()
	creds := aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"}, nil
	})
	client := s3.New(s3.Options{Region: "us-east-1", Credentials: creds})
	return NewStorage(client, "test-bucket", zap.NewNop())
}

func TestStorage_ImplementsMultipartStorage(t *testing.T) {
	var s any = presignTestStorage(t)
	_, ok := s.(cloudstorage.MultipartStorage)
	assert.True(t, ok, "Storage must implement cloudstorage.MultipartStorage")
}

func TestStorage_PresignedUploadPartURLs(t *testing.T) {
	storage := presignTestStorage(t)
	ctx := context.Background()

	t.Run("generates signed URLs per part", func(t *testing.T) {
		urls, err := storage.PresignedUploadPartURLs(ctx, "big/archive.zip", "upload-123", &cloudstorage.PresignedUploadPartOptions{
			PartNumbers: []int32{1, 2, 7},
			Expiration:  5 * time.Minute,
		})
		require.NoError(t, err)
		require.Len(t, urls, 3)

		for i, want := range []int32{1, 2, 7} {
			assert.Equal(t, want, urls[i].PartNumber)

			u, err := neturl.Parse(urls[i].URL)
			require.NoError(t, err)
			q := u.Query()
			assert.Equal(t, "upload-123", q.Get("uploadId"))
			assert.Equal(t, "300", q.Get("X-Amz-Expires"))
			assert.NotEmpty(t, q.Get("X-Amz-Signature"))
			assert.Equal(t, int64(want), mustParseInt(t, q.Get("partNumber")))
		}
	})

	t.Run("default expiration", func(t *testing.T) {
		urls, err := storage.PresignedUploadPartURLs(ctx, "k", "u", &cloudstorage.PresignedUploadPartOptions{
			PartNumbers: []int32{1},
		})
		require.NoError(t, err)
		u, err := neturl.Parse(urls[0].URL)
		require.NoError(t, err)
		assert.Equal(t, "900", u.Query().Get("X-Amz-Expires"))
	})

	t.Run("validation", func(t *testing.T) {
		_, err := storage.PresignedUploadPartURLs(ctx, "k", "", &cloudstorage.PresignedUploadPartOptions{PartNumbers: []int32{1}})
		assert.Error(t, err, "empty upload ID")

		_, err = storage.PresignedUploadPartURLs(ctx, "k", "u", nil)
		assert.Error(t, err, "nil options")

		_, err = storage.PresignedUploadPartURLs(ctx, "k", "u", &cloudstorage.PresignedUploadPartOptions{})
		assert.Error(t, err, "no part numbers")

		_, err = storage.PresignedUploadPartURLs(ctx, "k", "u", &cloudstorage.PresignedUploadPartOptions{PartNumbers: []int32{0}})
		assert.Error(t, err, "part number below range")

		_, err = storage.PresignedUploadPartURLs(ctx, "k", "u", &cloudstorage.PresignedUploadPartOptions{PartNumbers: []int32{cloudstorage.MaxPartNumber + 1}})
		assert.Error(t, err, "part number above range")

		tooMany := make([]int32, cloudstorage.MaxPresignPartBatch+1)
		for i := range tooMany {
			tooMany[i] = int32(i + 1)
		}
		_, err = storage.PresignedUploadPartURLs(ctx, "k", "u", &cloudstorage.PresignedUploadPartOptions{PartNumbers: tooMany})
		assert.Error(t, err, "batch over limit")
	})
}

func TestStorage_CompleteMultipartUpload_Validation(t *testing.T) {
	storage := presignTestStorage(t)
	ctx := context.Background()

	_, err := storage.CompleteMultipartUpload(ctx, "k", "", []cloudstorage.CompletedPart{{PartNumber: 1, ETag: "e"}})
	assert.Error(t, err, "empty upload ID")

	_, err = storage.CompleteMultipartUpload(ctx, "k", "u", nil)
	assert.Error(t, err, "no parts")

	_, err = storage.CompleteMultipartUpload(ctx, "k", "u", []cloudstorage.CompletedPart{{PartNumber: 1}})
	assert.Error(t, err, "missing etag")

	_, err = storage.CompleteMultipartUpload(ctx, "k", "u", []cloudstorage.CompletedPart{{PartNumber: 0, ETag: "e"}})
	assert.Error(t, err, "part number out of range")
}

func TestStorage_AbortMultipartUpload_Validation(t *testing.T) {
	storage := presignTestStorage(t)
	assert.Error(t, storage.AbortMultipartUpload(context.Background(), "k", ""), "empty upload ID")
}

func mustParseInt(t *testing.T, s string) int64 {
	t.Helper()
	var v int64
	for _, c := range s {
		require.True(t, c >= '0' && c <= '9', "non-digit in %q", s)
		v = v*10 + int64(c-'0')
	}
	return v
}
