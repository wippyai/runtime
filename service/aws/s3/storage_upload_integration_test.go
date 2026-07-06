// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// readerOnly hides Seek/Len/WriterTo so the S3 client sees an opaque stream of
// unknown length, matching the io.Reader the Lua runtime passes through.
type readerOnly struct{ r io.Reader }

func (r readerOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

// newIntegrationStorage builds a Storage bound to a live S3-compatible endpoint
// (MinIO) from the environment. It returns nil when the harness is not configured
// so callers can skip.
func newIntegrationStorage(t *testing.T) *Storage {
	t.Helper()

	endpoint := os.Getenv("S3_ENDPOINT")
	access := os.Getenv("S3_ACCESS_KEY")
	secret := os.Getenv("S3_SECRET_KEY")
	bucket := os.Getenv("S3_BUCKET")
	if endpoint == "" || access == "" || secret == "" || bucket == "" {
		t.Skip("set S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET to run S3 integration tests")
	}

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(access, secret, ""),
	})
	return NewStorage(client, bucket, zap.NewNop())
}

// TestUploadObjectLargeStream proves that a streaming body larger than MinIO's
// 16MiB aws-chunked chunk cap uploads successfully and reads back byte-identical.
// A single PutObject with a streaming body fails here with
// "chunk too big: choose chunk size <= 16MiB"; multipart upload does not.
func TestUploadObjectLargeStream(t *testing.T) {
	storage := newIntegrationStorage(t)
	ctx := context.Background()

	const size = 32 << 20 // 32MiB, well past the 16MiB single-chunk cap
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i * 31)
	}
	want := sha256.Sum256(payload)

	key := "integration/large-stream.bin"
	t.Cleanup(func() { _ = storage.DeleteObjects(context.Background(), []string{key}) })

	// A pure io.Reader with no Seek and no known length: this is what the Lua
	// runtime hands to UploadObject. The AWS SDK then signs the body as an
	// aws-chunked stream, which MinIO rejects once a chunk exceeds 16MiB.
	err := storage.UploadObject(ctx, key, readerOnly{bytes.NewReader(payload)}, nil)
	require.NoError(t, err, "large streaming upload must not fail on the 16MiB chunk cap")

	var buf bytes.Buffer
	err = storage.DownloadObject(ctx, key, &buf, nil)
	require.NoError(t, err)
	require.Len(t, buf.Bytes(), size)
	require.Equal(t, want, sha256.Sum256(buf.Bytes()), "read-back must be byte-identical")
}
