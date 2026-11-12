// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	apierror "github.com/wippyai/runtime/api/error"
	"go.uber.org/zap"
)

// TestListObjectsProviderErrorKinds proves against a live S3-compatible
// endpoint that a listing distinguishes a missing bucket from rejected
// credentials.
func TestListObjectsProviderErrorKinds(t *testing.T) {
	storage := newIntegrationStorage(t)
	ctx := context.Background()

	t.Run("missing bucket", func(t *testing.T) {
		missing := NewStorage(storage.client, storage.bucket+"-missing", zap.NewNop())
		_, err := missing.ListObjects(ctx, nil)
		details := requireClassified(t, err, apierror.NotFound, apierror.False)
		require.ErrorIs(t, err, cloudstorage.ErrNotFound)
		require.Equal(t, "NoSuchBucket", details["code"])
	})

	t.Run("unknown access key", func(t *testing.T) {
		_, err := withCredentials(storage, "unknown-access-key", os.Getenv("S3_SECRET_KEY")).ListObjects(ctx, nil)
		details := requireClassified(t, err, apierror.PermissionDenied, apierror.False)
		require.Equal(t, "InvalidAccessKeyId", details["code"])
	})

	t.Run("wrong secret", func(t *testing.T) {
		_, err := withCredentials(storage, os.Getenv("S3_ACCESS_KEY"), "wrong-secret").ListObjects(ctx, nil)
		details := requireClassified(t, err, apierror.PermissionDenied, apierror.False)
		require.Equal(t, "SignatureDoesNotMatch", details["code"])
	})
}

func withCredentials(storage *Storage, access, secret string) *Storage {
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(os.Getenv("S3_ENDPOINT")),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(access, secret, ""),
	})
	return NewStorage(client, storage.bucket, zap.NewNop())
}
