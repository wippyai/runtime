// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/cloudstorage"
	apierror "github.com/wippyai/runtime/api/error"
	"go.uber.org/zap"
)

// storageOperation runs one Storage operation that reaches the endpoint.
type storageOperation struct {
	run  func(context.Context, *Storage) error
	name string
	// bodyless operations receive no error document (HEAD).
	bodyless bool
}

func storageOperations() []storageOperation {
	return []storageOperation{
		{name: "list_objects", run: func(ctx context.Context, s *Storage) error {
			_, err := s.ListObjects(ctx, nil)
			return err
		}},
		{name: "list_object_versions", run: func(ctx context.Context, s *Storage) error {
			_, err := s.ListObjects(ctx, &cloudstorage.ListObjectsOptions{IncludeVersions: true})
			return err
		}},
		{name: "head_object", bodyless: true, run: func(ctx context.Context, s *Storage) error {
			_, err := s.HeadObject(ctx, "key")
			return err
		}},
		{name: "download_object", run: func(ctx context.Context, s *Storage) error {
			return s.DownloadObject(ctx, "key", io.Discard, nil)
		}},
		{name: "upload_object", run: func(ctx context.Context, s *Storage) error {
			return s.UploadObject(ctx, "key", bytes.NewReader([]byte("payload")), nil)
		}},
		// copy_object first heads the source, so its failures carry no error document.
		{name: "copy_object", bodyless: true, run: func(ctx context.Context, s *Storage) error {
			_, err := s.CopyObject(ctx, "src", "dst", nil)
			return err
		}},
		{name: "delete_objects", run: func(ctx context.Context, s *Storage) error {
			return s.DeleteObjects(ctx, []string{"key"})
		}},
		{name: "create_multipart_upload", run: func(ctx context.Context, s *Storage) error {
			_, err := s.CreateMultipartUpload(ctx, "key", nil)
			return err
		}},
		{name: "complete_multipart_upload", run: func(ctx context.Context, s *Storage) error {
			_, err := s.CompleteMultipartUpload(ctx, "key", "upload", []cloudstorage.CompletedPart{{PartNumber: 1, ETag: "\"e\""}})
			return err
		}},
		{name: "abort_multipart_upload", run: func(ctx context.Context, s *Storage) error {
			return s.AbortMultipartUpload(ctx, "key", "upload")
		}},
	}
}

func newErrorTestStorage(t *testing.T, endpoint string, httpClient awss3.HTTPClient) *Storage {
	t.Helper()
	cfg := aws.Config{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(endpoint),
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
		}),
	}
	client := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.UsePathStyle = true
		o.RetryMaxAttempts = 1
		if httpClient != nil {
			o.HTTPClient = httpClient
		}
	})
	return NewStorage(client, "bucket", zap.NewNop())
}

func s3ErrorServer(t *testing.T, status int, code string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-amz-request-id", "req-1")
		if r.Method == http.MethodHead {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>` + code + `</Code><Message>rejected</Message><RequestId>req-1</RequestId></Error>`))
	}))
	t.Cleanup(server.Close)
	return server
}

func requireClassified(t *testing.T, err error, kind apierror.Kind, retryable apierror.Ternary) attrs.Bag {
	t.Helper()
	require.Error(t, err)
	var classified apierror.Error
	require.True(t, errors.As(err, &classified), "error %q carries no kind", err)
	require.Equal(t, kind, classified.Kind(), "error %q", err)
	require.Equal(t, retryable, classified.Retryable(), "error %q", err)
	details, _ := classified.Details().(attrs.Bag)
	return details
}

func TestStorageErrors_ResponseStatusClassifiesEveryOperation(t *testing.T) {
	cases := []struct {
		name      string
		code      string
		headCode  string
		kind      apierror.Kind
		status    int
		retryable apierror.Ternary
		notFound  bool
	}{
		{name: "invalid access key", status: http.StatusForbidden, code: "InvalidAccessKeyId", headCode: "Forbidden", kind: apierror.PermissionDenied, retryable: apierror.False},
		{name: "access denied", status: http.StatusForbidden, code: "AccessDenied", headCode: "Forbidden", kind: apierror.PermissionDenied, retryable: apierror.False},
		{name: "expired token", status: http.StatusBadRequest, code: "ExpiredToken", headCode: "BadRequest", kind: apierror.PermissionDenied, retryable: apierror.False},
		{name: "missing bucket", status: http.StatusNotFound, code: "NoSuchBucket", headCode: "NotFound", kind: apierror.NotFound, retryable: apierror.False, notFound: true},
		{name: "server error", status: http.StatusInternalServerError, code: "InternalError", headCode: "InternalServerError", kind: apierror.Unavailable, retryable: apierror.True},
		{name: "throttled", status: http.StatusServiceUnavailable, code: "SlowDown", headCode: "ServiceUnavailable", kind: apierror.RateLimited, retryable: apierror.True},
		{name: "request timeout", status: http.StatusBadRequest, code: "RequestTimeout", headCode: "BadRequest", kind: apierror.Timeout, retryable: apierror.True},
		{name: "conflict", status: http.StatusConflict, code: "OperationAborted", headCode: "Conflict", kind: apierror.Conflict, retryable: apierror.False},
		{name: "malformed request", status: http.StatusBadRequest, code: "MalformedXML", headCode: "BadRequest", kind: apierror.Invalid, retryable: apierror.False},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := s3ErrorServer(t, tc.status, tc.code)
			storage := newErrorTestStorage(t, server.URL, nil)
			for _, op := range storageOperations() {
				t.Run(op.name, func(t *testing.T) {
					err := op.run(context.Background(), storage)
					kind, retryable, code := tc.kind, tc.retryable, tc.code
					if op.bodyless {
						code = tc.headCode
						switch tc.status {
						case http.StatusBadRequest:
							kind, retryable = apierror.Invalid, apierror.False
						case http.StatusServiceUnavailable:
							kind = apierror.Unavailable
						}
					}
					details := requireClassified(t, err, kind, retryable)
					require.Equal(t, code, details["code"])
					require.Equal(t, tc.status, details["status_code"])
					require.Equal(t, "req-1", details["request_id"])
					require.NotEmpty(t, details["operation"])
					require.Equal(t, tc.notFound, errors.Is(err, cloudstorage.ErrNotFound))
				})
			}
		})
	}
}

func TestStorageErrors_PreconditionFailedKeepsSentinel(t *testing.T) {
	server := s3ErrorServer(t, http.StatusPreconditionFailed, "PreconditionFailed")
	storage := newErrorTestStorage(t, server.URL, nil)

	err := storage.DownloadObject(context.Background(), "key", io.Discard, &cloudstorage.DownloadOptions{IfMatch: "\"e\""})
	requireClassified(t, err, apierror.Conflict, apierror.False)
	require.ErrorIs(t, err, cloudstorage.ErrPreconditionFailed)

	err = storage.UploadObject(context.Background(), "key", strings.NewReader("x"), &cloudstorage.UploadOptions{IfNoneMatch: "*"})
	requireClassified(t, err, apierror.Conflict, apierror.False)
	require.ErrorIs(t, err, cloudstorage.ErrPreconditionFailed)
}

func TestStorageErrors_UnreachableEndpointIsUnavailable(t *testing.T) {
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())

	storage := newErrorTestStorage(t, endpoint, nil)
	for _, op := range storageOperations() {
		t.Run(op.name, func(t *testing.T) {
			details := requireClassified(t, op.run(context.Background(), storage), apierror.Unavailable, apierror.True)
			require.NotEmpty(t, details["operation"])
		})
	}
}

func TestStorageErrors_TransportTimeoutIsTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	storage := newErrorTestStorage(t, server.URL, &http.Client{Timeout: 50 * time.Millisecond})
	for _, op := range storageOperations() {
		t.Run(op.name, func(t *testing.T) {
			requireClassified(t, op.run(context.Background(), storage), apierror.Timeout, apierror.True)
		})
	}
}

func TestStorageErrors_CallerContext(t *testing.T) {
	server := s3ErrorServer(t, http.StatusOK, "")
	storage := newErrorTestStorage(t, server.URL, nil)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	for _, op := range storageOperations() {
		t.Run(op.name, func(t *testing.T) {
			requireClassified(t, op.run(canceled, storage), apierror.Canceled, apierror.False)
			requireClassified(t, op.run(expired, storage), apierror.Timeout, apierror.False)
		})
	}
}

func TestStorageErrors_CredentialFailureKeepsCauseKind(t *testing.T) {
	cause := apierror.New(apierror.PermissionDenied, "credentials rejected").WithRetryable(apierror.False)
	client := awss3.New(awss3.Options{
		Region: "us-east-1",
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{}, cause
		}),
	})
	storage := NewStorage(client, "bucket", zap.NewNop())

	_, err := storage.PresignedGetURL(context.Background(), "key", nil)
	requireClassified(t, err, apierror.PermissionDenied, apierror.False)
	require.ErrorIs(t, err, cause)

	_, err = storage.PresignedPutURL(context.Background(), "key", nil)
	requireClassified(t, err, apierror.PermissionDenied, apierror.False)

	_, err = storage.PresignedUploadPartURLs(context.Background(), "key", "upload", &cloudstorage.PresignedUploadPartOptions{PartNumbers: []int32{1}})
	requireClassified(t, err, apierror.PermissionDenied, apierror.False)
}

func TestStorageErrors_DeleteObjectsPartialFailure(t *testing.T) {
	respond := func(t *testing.T, body string) *Storage {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` + body + `</DeleteResult>`))
		}))
		t.Cleanup(server.Close)
		return newErrorTestStorage(t, server.URL, nil)
	}

	t.Run("uniform failures carry their kind", func(t *testing.T) {
		storage := respond(t, `<Error><Key>a</Key><Code>AccessDenied</Code><Message>denied</Message></Error>
<Error><Key>b</Key><Code>AccessDenied</Code><Message>denied</Message></Error>`)
		details := requireClassified(t, storage.DeleteObjects(context.Background(), []string{"a", "b"}), apierror.PermissionDenied, apierror.False)
		require.Equal(t, 2, details["failed"])
		require.Equal(t, "AccessDenied", details["code"])
	})

	t.Run("transient failures are retryable", func(t *testing.T) {
		storage := respond(t, `<Error><Key>a</Key><Code>InternalError</Code><Message>failed</Message></Error>`)
		requireClassified(t, storage.DeleteObjects(context.Background(), []string{"a"}), apierror.Unavailable, apierror.True)
	})

	t.Run("mixed failures are internal", func(t *testing.T) {
		storage := respond(t, `<Error><Key>a</Key><Code>AccessDenied</Code><Message>denied</Message></Error>
<Error><Key>b</Key><Code>InternalError</Code><Message>failed</Message></Error>`)
		details := requireClassified(t, storage.DeleteObjects(context.Background(), []string{"a", "b"}), apierror.Internal, apierror.False)
		require.Equal(t, 2, details["failed"])
	})
}

func TestStorageErrors_LocalValidationIsInvalid(t *testing.T) {
	storage := presignTestStorage(t)
	ctx := context.Background()

	_, err := storage.PresignedUploadPartURLs(ctx, "key", "", &cloudstorage.PresignedUploadPartOptions{PartNumbers: []int32{1}})
	requireClassified(t, err, apierror.Invalid, apierror.False)
	_, err = storage.PresignedUploadPartURLs(ctx, "key", "upload", &cloudstorage.PresignedUploadPartOptions{PartNumbers: []int32{0}})
	requireClassified(t, err, apierror.Invalid, apierror.False)
	_, err = storage.CompleteMultipartUpload(ctx, "key", "upload", nil)
	requireClassified(t, err, apierror.Invalid, apierror.False)
	_, err = storage.CompleteMultipartUpload(ctx, "key", "upload", []cloudstorage.CompletedPart{{PartNumber: 1}})
	requireClassified(t, err, apierror.Invalid, apierror.False)
	err = storage.AbortMultipartUpload(ctx, "key", "")
	requireClassified(t, err, apierror.Invalid, apierror.False)
}
