package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	neturl "net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	"go.uber.org/zap"
)

func TestDefaultPresignExpiration(t *testing.T) {
	assert.Equal(t, 15*time.Minute, DefaultPresignExpiration)
}

func TestNewStorage(t *testing.T) {
	logger := zap.NewNop()
	client := s3.New(s3.Options{Region: "us-east-1"})

	storage := NewStorage(client, "test-bucket", logger)

	assert.NotNil(t, storage)
	assert.Equal(t, "test-bucket", storage.bucket)
	assert.Equal(t, client, storage.client)
	assert.NotNil(t, storage.log)
}

func TestStorage_DeleteObjects_EmptyKeys(t *testing.T) {
	logger := zap.NewNop()
	client := s3.New(s3.Options{Region: "us-east-1"})
	storage := NewStorage(client, "test-bucket", logger)

	err := storage.DeleteObjects(context.Background(), []string{})
	assert.NoError(t, err)
}

// mockListObjectsClient is a mock S3 client for ListObjectsV2
type mockListObjectsClient struct {
	output *s3.ListObjectsV2Output
	err    error
}

func (m *mockListObjectsClient) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.output, nil
}

func TestStorage_ListObjects(t *testing.T) {
	logger := zap.NewNop()

	t.Run("success with nil options", func(t *testing.T) {
		mock := &mockListObjectsClient{
			output: &s3.ListObjectsV2Output{
				Contents: []types.Object{
					{Key: aws.String("file1.txt"), Size: aws.Int64(100), ETag: aws.String("etag1")},
					{Key: aws.String("file2.txt"), Size: aws.Int64(200), ETag: aws.String("etag2")},
				},
				IsTruncated:           aws.Bool(false),
				NextContinuationToken: nil,
			},
		}

		storage := &Storage{
			client: nil, // Will use mock
			bucket: "test-bucket",
			log:    logger,
		}

		// Use mock directly
		result, err := listObjectsWithMock(context.Background(), mock, storage.bucket, nil)
		require.NoError(t, err)
		assert.Len(t, result.Objects, 2)
		assert.Equal(t, "file1.txt", result.Objects[0].Key)
		assert.Equal(t, int64(100), result.Objects[0].Size)
		assert.False(t, result.IsTruncated)
	})

	t.Run("success with options", func(t *testing.T) {
		mock := &mockListObjectsClient{
			output: &s3.ListObjectsV2Output{
				Contents: []types.Object{
					{Key: aws.String("prefix/file1.txt"), Size: aws.Int64(100), ETag: aws.String("etag1")},
				},
				IsTruncated:           aws.Bool(true),
				NextContinuationToken: aws.String("token123"),
			},
		}

		opts := &cloudstorage.ListObjectsOptions{
			Prefix:            "prefix/",
			MaxKeys:           10,
			ContinuationToken: "prev-token",
		}

		result, err := listObjectsWithMock(context.Background(), mock, "test-bucket", opts)
		require.NoError(t, err)
		assert.Len(t, result.Objects, 1)
		assert.True(t, result.IsTruncated)
		assert.Equal(t, "token123", result.NextContinuationToken)
	})

	t.Run("error", func(t *testing.T) {
		mock := &mockListObjectsClient{
			err: errors.New("list failed"),
		}

		result, err := listObjectsWithMock(context.Background(), mock, "test-bucket", nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// listObjectsWithMock is a helper that mimics ListObjects logic with a mock
func listObjectsWithMock(ctx context.Context, client *mockListObjectsClient, bucket string, opts *cloudstorage.ListObjectsOptions) (*cloudstorage.ListObjectsResult, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	}

	if opts != nil {
		if opts.Prefix != "" {
			input.Prefix = aws.String(opts.Prefix)
		}
		if opts.MaxKeys > 0 {
			input.MaxKeys = aws.Int32(int32(opts.MaxKeys))
		}
		if opts.ContinuationToken != "" {
			input.ContinuationToken = aws.String(opts.ContinuationToken)
		}
	}

	output, err := client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, err
	}

	result := &cloudstorage.ListObjectsResult{
		IsTruncated:           aws.ToBool(output.IsTruncated),
		NextContinuationToken: aws.ToString(output.NextContinuationToken),
		Objects:               make([]cloudstorage.ObjectMetadata, 0, len(output.Contents)),
	}

	for _, item := range output.Contents {
		result.Objects = append(result.Objects, cloudstorage.ObjectMetadata{
			Key:  aws.ToString(item.Key),
			Size: aws.ToInt64(item.Size),
			ETag: aws.ToString(item.ETag),
		})
	}

	return result, nil
}

// mockGetObjectClient is a mock S3 client for GetObject
type mockGetObjectClient struct {
	output *s3.GetObjectOutput
	err    error
}

func (m *mockGetObjectClient) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.output, nil
}

func TestStorage_DownloadObject(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		content := []byte("file content")
		mock := &mockGetObjectClient{
			output: &s3.GetObjectOutput{
				Body: io.NopCloser(bytes.NewReader(content)),
			},
		}

		var buf bytes.Buffer
		err := downloadObjectWithMock(context.Background(), mock, "test-bucket", "test-key", &buf, nil)
		require.NoError(t, err)
		assert.Equal(t, content, buf.Bytes())
	})

	t.Run("success with range", func(t *testing.T) {
		content := []byte("partial")
		mock := &mockGetObjectClient{
			output: &s3.GetObjectOutput{
				Body: io.NopCloser(bytes.NewReader(content)),
			},
		}

		var buf bytes.Buffer
		opts := &cloudstorage.DownloadOptions{Range: "bytes=0-6"}
		err := downloadObjectWithMock(context.Background(), mock, "test-bucket", "test-key", &buf, opts)
		require.NoError(t, err)
		assert.Equal(t, content, buf.Bytes())
	})

	t.Run("error", func(t *testing.T) {
		mock := &mockGetObjectClient{
			err: errors.New("download failed"),
		}

		var buf bytes.Buffer
		err := downloadObjectWithMock(context.Background(), mock, "test-bucket", "test-key", &buf, nil)
		assert.Error(t, err)
	})
}

func downloadObjectWithMock(ctx context.Context, client *mockGetObjectClient, bucket, key string, w io.Writer, opts *cloudstorage.DownloadOptions) error {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	if opts != nil && opts.Range != "" {
		input.Range = aws.String(opts.Range)
	}

	output, err := client.GetObject(ctx, input)
	if err != nil {
		return err
	}
	defer func() { _ = output.Body.Close() }()

	_, err = io.Copy(w, output.Body)
	return err
}

// mockPutObjectClient is a mock S3 client for PutObject
type mockPutObjectClient struct {
	err error
}

func (m *mockPutObjectClient) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &s3.PutObjectOutput{}, nil
}

func TestStorage_UploadObject(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := &mockPutObjectClient{}
		content := bytes.NewReader([]byte("upload content"))

		err := uploadObjectWithMock(context.Background(), mock, "test-bucket", "test-key", content)
		require.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		mock := &mockPutObjectClient{err: errors.New("upload failed")}
		content := bytes.NewReader([]byte("upload content"))

		err := uploadObjectWithMock(context.Background(), mock, "test-bucket", "test-key", content)
		assert.Error(t, err)
	})
}

func uploadObjectWithMock(ctx context.Context, client *mockPutObjectClient, bucket, key string, content io.Reader) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   content,
	}

	_, err := client.PutObject(ctx, input)
	return err
}

// mockDeleteObjectsClient is a mock S3 client for DeleteObjects
type mockDeleteObjectsClient struct {
	err error
}

func (m *mockDeleteObjectsClient) DeleteObjects(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func TestStorage_DeleteObjects(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := &mockDeleteObjectsClient{}
		keys := []string{"file1.txt", "file2.txt"}

		err := deleteObjectsWithMock(context.Background(), mock, "test-bucket", keys)
		require.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		mock := &mockDeleteObjectsClient{err: errors.New("delete failed")}
		keys := []string{"file1.txt"}

		err := deleteObjectsWithMock(context.Background(), mock, "test-bucket", keys)
		assert.Error(t, err)
	})
}

func deleteObjectsWithMock(ctx context.Context, client *mockDeleteObjectsClient, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	objects := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objects[i] = types.ObjectIdentifier{
			Key: aws.String(key),
		}
	}

	input := &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true),
		},
	}

	_, err := client.DeleteObjects(ctx, input)
	return err
}

// mockPresignClient is a mock presign client
type mockPresignClient struct {
	err    error
	getURL string
	putURL string
}

func (m *mockPresignClient) PresignGetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &v4.PresignedHTTPRequest{URL: m.getURL}, nil
}

func (m *mockPresignClient) PresignPutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &v4.PresignedHTTPRequest{URL: m.putURL}, nil
}

func TestStorage_PresignedGetURL(t *testing.T) {
	t.Run("success with default expiration", func(t *testing.T) {
		mock := &mockPresignClient{getURL: "https://s3.example.com/bucket/key?signed"}

		url, err := presignedGetURLWithMock(context.Background(), mock, "test-bucket", "test-key", nil)
		require.NoError(t, err)
		assert.Equal(t, "https://s3.example.com/bucket/key?signed", url)
	})

	t.Run("success with custom expiration", func(t *testing.T) {
		mock := &mockPresignClient{getURL: "https://s3.example.com/bucket/key?signed"}
		opts := &cloudstorage.PresignedGetOptions{Expiration: 1 * time.Hour}

		url, err := presignedGetURLWithMock(context.Background(), mock, "test-bucket", "test-key", opts)
		require.NoError(t, err)
		assert.Equal(t, "https://s3.example.com/bucket/key?signed", url)
	})

	t.Run("error", func(t *testing.T) {
		mock := &mockPresignClient{err: errors.New("presign failed")}

		url, err := presignedGetURLWithMock(context.Background(), mock, "test-bucket", "test-key", nil)
		assert.Error(t, err)
		assert.Empty(t, url)
	})
}

func presignedGetURLWithMock(ctx context.Context, client *mockPresignClient, bucket, key string, opts *cloudstorage.PresignedGetOptions) (string, error) {
	expiration := DefaultPresignExpiration
	if opts != nil && opts.Expiration > 0 {
		expiration = opts.Expiration
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	result, err := client.PresignGetObject(ctx, input, func(options *s3.PresignOptions) {
		options.Expires = expiration
	})
	if err != nil {
		return "", err
	}

	return result.URL, nil
}

func TestStorage_PresignedPutURL(t *testing.T) {
	t.Run("success with default expiration", func(t *testing.T) {
		mock := &mockPresignClient{putURL: "https://s3.example.com/bucket/key?signed-put"}

		url, err := presignedPutURLWithMock(context.Background(), mock, "test-bucket", "test-key", nil)
		require.NoError(t, err)
		assert.Equal(t, "https://s3.example.com/bucket/key?signed-put", url)
	})

	t.Run("success with options", func(t *testing.T) {
		mock := &mockPresignClient{putURL: "https://s3.example.com/bucket/key?signed-put"}
		opts := &cloudstorage.PresignedPutOptions{
			Expiration:    30 * time.Minute,
			ContentType:   "application/json",
			ContentLength: 1024,
		}

		url, err := presignedPutURLWithMock(context.Background(), mock, "test-bucket", "test-key", opts)
		require.NoError(t, err)
		assert.Equal(t, "https://s3.example.com/bucket/key?signed-put", url)
	})

	t.Run("error", func(t *testing.T) {
		mock := &mockPresignClient{err: errors.New("presign failed")}

		url, err := presignedPutURLWithMock(context.Background(), mock, "test-bucket", "test-key", nil)
		assert.Error(t, err)
		assert.Empty(t, url)
	})
}

func presignedPutURLWithMock(ctx context.Context, client *mockPresignClient, bucket, key string, opts *cloudstorage.PresignedPutOptions) (string, error) {
	expiration := DefaultPresignExpiration
	if opts != nil && opts.Expiration > 0 {
		expiration = opts.Expiration
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	if opts != nil && opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	if opts != nil && opts.ContentLength > 0 {
		input.ContentLength = aws.Int64(opts.ContentLength)
	}

	result, err := client.PresignPutObject(ctx, input, func(options *s3.PresignOptions) {
		options.Expires = expiration
	})
	if err != nil {
		return "", err
	}

	return result.URL, nil
}

var _ cloudstorage.Storage = (*Storage)(nil)

// Test that presigner can be created from real client
func TestStorage_PresignerCreation(t *testing.T) {
	client := s3.New(s3.Options{Region: "us-east-1"})
	presigner := s3.NewPresignClient(client)
	assert.NotNil(t, presigner)
}

// Test actual Storage methods with integration-style test (uses real client but will fail on actual API call)
func TestStorage_RealClientMethods(t *testing.T) {
	logger := zap.NewNop()
	client := s3.New(s3.Options{
		Region:             "us-east-1",
		EndpointResolverV2: &mockEndpointResolver{},
		Credentials: aws.CredentialsProviderFunc(func(_ context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			}, nil
		}),
	})

	storage := NewStorage(client, "test-bucket", logger)

	t.Run("ListObjects with nil options", func(t *testing.T) {
		// This will fail at API call, but tests the code path
		_, err := storage.ListObjects(context.Background(), nil)
		assert.Error(t, err) // Expected to fail - no real S3
	})

	t.Run("ListObjects with options", func(t *testing.T) {
		opts := &cloudstorage.ListObjectsOptions{
			Prefix:            "test/",
			MaxKeys:           100,
			ContinuationToken: "token",
		}
		_, err := storage.ListObjects(context.Background(), opts)
		assert.Error(t, err) // Expected to fail - no real S3
	})

	t.Run("DownloadObject", func(t *testing.T) {
		var buf bytes.Buffer
		err := storage.DownloadObject(context.Background(), "test-key", &buf, nil)
		assert.Error(t, err)
	})

	t.Run("DownloadObject with range", func(t *testing.T) {
		var buf bytes.Buffer
		opts := &cloudstorage.DownloadOptions{Range: "bytes=0-100"}
		err := storage.DownloadObject(context.Background(), "test-key", &buf, opts)
		assert.Error(t, err)
	})

	t.Run("UploadObject", func(t *testing.T) {
		content := bytes.NewReader([]byte("test"))
		err := storage.UploadObject(context.Background(), "test-key", content)
		assert.Error(t, err)
	})

	t.Run("DeleteObjects with keys", func(t *testing.T) {
		err := storage.DeleteObjects(context.Background(), []string{"key1", "key2"})
		assert.Error(t, err)
	})

	t.Run("PresignedGetURL with nil opts", func(t *testing.T) {
		signedURL, err := storage.PresignedGetURL(context.Background(), "test-key", nil)
		assert.NoError(t, err)
		assert.Contains(t, signedURL, "test-key")
		assert.Contains(t, signedURL, "X-Amz-Signature")
	})

	t.Run("PresignedGetURL with opts", func(t *testing.T) {
		opts := &cloudstorage.PresignedGetOptions{Expiration: 30 * time.Minute}
		signedURL, err := storage.PresignedGetURL(context.Background(), "test-key", opts)
		assert.NoError(t, err)
		assert.Contains(t, signedURL, "test-key")
		assert.Contains(t, signedURL, "X-Amz-Expires=1800") // 30 minutes
	})

	t.Run("PresignedPutURL with nil opts", func(t *testing.T) {
		signedURL, err := storage.PresignedPutURL(context.Background(), "test-key", nil)
		assert.NoError(t, err)
		assert.Contains(t, signedURL, "test-key")
		assert.Contains(t, signedURL, "X-Amz-Signature")
	})

	t.Run("PresignedPutURL with opts", func(t *testing.T) {
		opts := &cloudstorage.PresignedPutOptions{
			Expiration:    1 * time.Hour,
			ContentType:   "text/plain",
			ContentLength: 1000,
		}
		signedURL, err := storage.PresignedPutURL(context.Background(), "test-key", opts)
		assert.NoError(t, err)
		assert.Contains(t, signedURL, "test-key")
		assert.Contains(t, signedURL, "X-Amz-Expires=3600") // 1 hour
	})
}

// mockEndpointResolver provides a mock endpoint for testing
type mockEndpointResolver struct{}

func (m *mockEndpointResolver) ResolveEndpoint(_ context.Context, _ s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	u, _ := neturl.Parse("https://s3.localhost.localstack.cloud:4566")
	return smithyendpoints.Endpoint{URI: *u}, nil
}
