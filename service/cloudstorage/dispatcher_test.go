// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/dispatcher"
)

type testReceiver struct {
	fn func(tag uint64, data any, err error)
}

func (r *testReceiver) CompleteYield(tag uint64, data any, err error) {
	r.fn(tag, data, err)
}

func newTestReceiver(fn func(tag uint64, data any, err error)) dispatcher.ResultReceiver {
	return &testReceiver{fn: fn}
}

type mockStorage struct {
	listErr         error
	headErr         error
	downloadErr     error
	uploadErr       error
	deleteErr       error
	presignGetErr   error
	presignPutErr   error
	listResult      *csapi.ListObjectsResult
	headResult      *csapi.HeadObjectResult
	uploadOpts      *csapi.UploadOptions
	presignGetURL   string
	presignPutURL   string
	downloadContent []byte
}

func (m *mockStorage) ListObjects(_ context.Context, _ *csapi.ListObjectsOptions) (*csapi.ListObjectsResult, error) {
	return m.listResult, m.listErr
}

func (m *mockStorage) HeadObject(_ context.Context, _ string) (*csapi.HeadObjectResult, error) {
	return m.headResult, m.headErr
}

func (m *mockStorage) DownloadObject(_ context.Context, _ string, w io.Writer, _ *csapi.DownloadOptions) error {
	if m.downloadErr != nil {
		return m.downloadErr
	}
	_, err := w.Write(m.downloadContent)
	return err
}

func (m *mockStorage) UploadObject(_ context.Context, _ string, _ io.Reader, opts *csapi.UploadOptions) error {
	m.uploadOpts = opts
	return m.uploadErr
}

func (m *mockStorage) DeleteObjects(_ context.Context, _ []string) error {
	return m.deleteErr
}

func (m *mockStorage) PresignedGetURL(_ context.Context, _ string, _ *csapi.PresignedGetOptions) (string, error) {
	return m.presignGetURL, m.presignGetErr
}

func (m *mockStorage) PresignedPutURL(_ context.Context, _ string, _ *csapi.PresignedPutOptions) (string, error) {
	return m.presignPutURL, m.presignPutErr
}

func TestDispatcher_StartStop(t *testing.T) {
	d := NewDispatcher()
	ctx := context.Background()

	err := d.Start(ctx)
	assert.NoError(t, err)

	err = d.Stop(ctx)
	assert.NoError(t, err)
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher()

	var registered []dispatcher.CommandID
	register := func(id dispatcher.CommandID, h dispatcher.Handler) {
		registered = append(registered, id)
		assert.NotNil(t, h)
	}

	d.RegisterAll(register)

	assert.Len(t, registered, 12)
	assert.Contains(t, registered, csapi.ListObjects)
	assert.Contains(t, registered, csapi.DownloadObject)
	assert.Contains(t, registered, csapi.UploadObject)
	assert.Contains(t, registered, csapi.DeleteObjects)
	assert.Contains(t, registered, csapi.PresignedGetURL)
	assert.Contains(t, registered, csapi.PresignedPutURL)
	assert.Contains(t, registered, csapi.HeadObject)
	assert.Contains(t, registered, csapi.CreateMultipartUpload)
	assert.Contains(t, registered, csapi.PresignedUploadPartURLs)
	assert.Contains(t, registered, csapi.CompleteMultipartUpload)
	assert.Contains(t, registered, csapi.AbortMultipartUpload)
	assert.Contains(t, registered, csapi.OpenReader)
}

type mockMultipartStorage struct {
	mockStorage
	createResult   *csapi.CreateMultipartUploadResult
	completeResult *csapi.CompleteMultipartUploadResult
	partURLs       []csapi.PresignedPartURL
	createErr      error
	partsErr       error
	completeErr    error
	abortErr       error
	lastParts      []csapi.CompletedPart
}

func (m *mockMultipartStorage) CreateMultipartUpload(_ context.Context, _ string, _ *csapi.CreateMultipartUploadOptions) (*csapi.CreateMultipartUploadResult, error) {
	return m.createResult, m.createErr
}

func (m *mockMultipartStorage) PresignedUploadPartURLs(_ context.Context, _, _ string, _ *csapi.PresignedUploadPartOptions) ([]csapi.PresignedPartURL, error) {
	return m.partURLs, m.partsErr
}

func (m *mockMultipartStorage) CompleteMultipartUpload(_ context.Context, _, _ string, parts []csapi.CompletedPart) (*csapi.CompleteMultipartUploadResult, error) {
	m.lastParts = parts
	return m.completeResult, m.completeErr
}

func (m *mockMultipartStorage) AbortMultipartUpload(_ context.Context, _, _ string) error {
	return m.abortErr
}

func dispatchHandlers(t *testing.T) map[dispatcher.CommandID]dispatcher.Handler {
	t.Helper()
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})
	return handlers
}

func TestDispatcher_MultipartUnsupported(t *testing.T) {
	handlers := dispatchHandlers(t)
	storage := &mockStorage{} // base Storage only, no multipart capability

	t.Run("create", func(t *testing.T) {
		done := make(chan csapi.CreateMultipartUploadResponse, 1)
		cmd := &csapi.CreateMultipartUploadCmd{Storage: storage, Key: "k"}
		require.NoError(t, handlers[csapi.CreateMultipartUpload].Handle(context.Background(), cmd, 1,
			newTestReceiver(func(_ uint64, data any, _ error) {
				done <- data.(csapi.CreateMultipartUploadResponse)
			})))
		select {
		case resp := <-done:
			assert.ErrorIs(t, resp.Error, csapi.ErrMultipartUnsupported)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("parts", func(t *testing.T) {
		done := make(chan csapi.PresignedPartURLsResponse, 1)
		cmd := &csapi.PresignedPartURLsCmd{Storage: storage, Key: "k", UploadID: "u"}
		require.NoError(t, handlers[csapi.PresignedUploadPartURLs].Handle(context.Background(), cmd, 1,
			newTestReceiver(func(_ uint64, data any, _ error) {
				done <- data.(csapi.PresignedPartURLsResponse)
			})))
		select {
		case resp := <-done:
			assert.ErrorIs(t, resp.Error, csapi.ErrMultipartUnsupported)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("complete", func(t *testing.T) {
		done := make(chan csapi.CompleteMultipartUploadResponse, 1)
		cmd := &csapi.CompleteMultipartUploadCmd{Storage: storage, Key: "k", UploadID: "u"}
		require.NoError(t, handlers[csapi.CompleteMultipartUpload].Handle(context.Background(), cmd, 1,
			newTestReceiver(func(_ uint64, data any, _ error) {
				done <- data.(csapi.CompleteMultipartUploadResponse)
			})))
		select {
		case resp := <-done:
			assert.ErrorIs(t, resp.Error, csapi.ErrMultipartUnsupported)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("abort", func(t *testing.T) {
		done := make(chan csapi.AbortMultipartUploadResponse, 1)
		cmd := &csapi.AbortMultipartUploadCmd{Storage: storage, Key: "k", UploadID: "u"}
		require.NoError(t, handlers[csapi.AbortMultipartUpload].Handle(context.Background(), cmd, 1,
			newTestReceiver(func(_ uint64, data any, _ error) {
				done <- data.(csapi.AbortMultipartUploadResponse)
			})))
		select {
		case resp := <-done:
			assert.ErrorIs(t, resp.Error, csapi.ErrMultipartUnsupported)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})
}

func TestDispatcher_MultipartSupported(t *testing.T) {
	handlers := dispatchHandlers(t)
	storage := &mockMultipartStorage{
		createResult:   &csapi.CreateMultipartUploadResult{UploadID: "upload-1"},
		partURLs:       []csapi.PresignedPartURL{{PartNumber: 1, URL: "https://p1"}, {PartNumber: 2, URL: "https://p2"}},
		completeResult: &csapi.CompleteMultipartUploadResult{ETag: `"final"`},
	}

	done := make(chan csapi.CreateMultipartUploadResponse, 1)
	cmd := &csapi.CreateMultipartUploadCmd{Storage: storage, Key: "k"}
	require.NoError(t, handlers[csapi.CreateMultipartUpload].Handle(context.Background(), cmd, 1,
		newTestReceiver(func(_ uint64, data any, _ error) {
			done <- data.(csapi.CreateMultipartUploadResponse)
		})))
	select {
	case resp := <-done:
		require.NoError(t, resp.Error)
		assert.Equal(t, "upload-1", resp.Result.UploadID)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	partsDone := make(chan csapi.PresignedPartURLsResponse, 1)
	partsCmd := &csapi.PresignedPartURLsCmd{
		Storage:  storage,
		Key:      "k",
		UploadID: "upload-1",
		Options:  &csapi.PresignedUploadPartOptions{PartNumbers: []int32{1, 2}},
	}
	require.NoError(t, handlers[csapi.PresignedUploadPartURLs].Handle(context.Background(), partsCmd, 1,
		newTestReceiver(func(_ uint64, data any, _ error) {
			partsDone <- data.(csapi.PresignedPartURLsResponse)
		})))
	select {
	case resp := <-partsDone:
		require.NoError(t, resp.Error)
		require.Len(t, resp.URLs, 2)
		assert.Equal(t, int32(1), resp.URLs[0].PartNumber)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	completeDone := make(chan csapi.CompleteMultipartUploadResponse, 1)
	completeCmd := &csapi.CompleteMultipartUploadCmd{
		Storage:  storage,
		Key:      "k",
		UploadID: "upload-1",
		Parts:    []csapi.CompletedPart{{PartNumber: 1, ETag: `"a"`}},
	}
	require.NoError(t, handlers[csapi.CompleteMultipartUpload].Handle(context.Background(), completeCmd, 1,
		newTestReceiver(func(_ uint64, data any, _ error) {
			completeDone <- data.(csapi.CompleteMultipartUploadResponse)
		})))
	select {
	case resp := <-completeDone:
		require.NoError(t, resp.Error)
		assert.Equal(t, `"final"`, resp.Result.ETag)
		assert.Len(t, storage.lastParts, 1)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_OpenReader(t *testing.T) {
	handlers := dispatchHandlers(t)

	t.Run("success", func(t *testing.T) {
		storage := &mockStorage{
			headResult: &csapi.HeadObjectResult{Size: 4096, ETag: `"v1"`},
		}
		done := make(chan csapi.OpenReaderResponse, 1)
		cmd := &csapi.OpenReaderCmd{Storage: storage, Key: "big.zip"}
		require.NoError(t, handlers[csapi.OpenReader].Handle(context.Background(), cmd, 1,
			newTestReceiver(func(_ uint64, data any, _ error) {
				done <- data.(csapi.OpenReaderResponse)
			})))
		select {
		case resp := <-done:
			require.NoError(t, resp.Error)
			assert.Equal(t, int64(4096), resp.Size)
			assert.Equal(t, `"v1"`, resp.ETag)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("not found", func(t *testing.T) {
		storage := &mockStorage{headErr: csapi.ErrNotFound}
		done := make(chan csapi.OpenReaderResponse, 1)
		cmd := &csapi.OpenReaderCmd{Storage: storage, Key: "missing.zip"}
		require.NoError(t, handlers[csapi.OpenReader].Handle(context.Background(), cmd, 1,
			newTestReceiver(func(_ uint64, data any, _ error) {
				done <- data.(csapi.OpenReaderResponse)
			})))
		select {
		case resp := <-done:
			assert.ErrorIs(t, resp.Error, csapi.ErrNotFound)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("requires consistency token", func(t *testing.T) {
		storage := &mockStorage{headResult: &csapi.HeadObjectResult{Size: 4096}}
		done := make(chan csapi.OpenReaderResponse, 1)
		cmd := &csapi.OpenReaderCmd{Storage: storage, Key: "mutable.zip"}
		require.NoError(t, handlers[csapi.OpenReader].Handle(context.Background(), cmd, 1,
			newTestReceiver(func(_ uint64, data any, _ error) {
				done <- data.(csapi.OpenReaderResponse)
			})))
		select {
		case resp := <-done:
			assert.ErrorIs(t, resp.Error, csapi.ErrReaderUnpinned)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	})
}

func TestDispatcher_ListObjects(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	storage := &mockStorage{
		listResult: &csapi.ListObjectsResult{
			Objects: []csapi.ObjectMetadata{
				{
					Key:          "file1.txt",
					Size:         100,
					ETag:         "etag1",
					StorageClass: "STANDARD",
					LastModified: now,
					Owner: &csapi.Owner{
						ID:          "owner-id",
						DisplayName: "Owner Name",
					},
				},
				{Key: "file2.txt", Size: 200},
			},
			IsTruncated: false,
		},
	}

	cmd := &csapi.ListObjectsCmd{
		Storage: storage,
		Options: &csapi.ListObjectsOptions{Prefix: "test/"},
	}

	done := make(chan csapi.ListObjectsResponse, 1)
	err := handlers[csapi.ListObjects].Handle(context.Background(), cmd, 1, newTestReceiver(func(_ uint64, data any, _ error) {
		done <- data.(csapi.ListObjectsResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		assert.Len(t, resp.Result.Objects, 2)
		first := resp.Result.Objects[0]
		assert.Equal(t, "file1.txt", first.Key)
		assert.Equal(t, "STANDARD", first.StorageClass)
		assert.Equal(t, now, first.LastModified)
		require.NotNil(t, first.Owner)
		assert.Equal(t, "owner-id", first.Owner.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_HeadObject(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	storage := &mockStorage{
		headResult: &csapi.HeadObjectResult{
			Size:         42,
			ETag:         "head-etag",
			ContentType:  "text/plain",
			CacheControl: "max-age=60",
			StorageClass: "STANDARD",
			LastModified: now,
			UserMetadata: map[string]string{"env": "staging"},
		},
	}

	cmd := &csapi.HeadObjectCmd{
		Storage: storage,
		Key:     "test.txt",
	}

	done := make(chan csapi.HeadObjectResponse, 1)
	err := handlers[csapi.HeadObject].Handle(context.Background(), cmd, 1, newTestReceiver(func(_ uint64, data any, _ error) {
		done <- data.(csapi.HeadObjectResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		require.NotNil(t, resp.Result)
		assert.Equal(t, int64(42), resp.Result.Size)
		assert.Equal(t, "head-etag", resp.Result.ETag)
		assert.Equal(t, "staging", resp.Result.UserMetadata["env"])
		assert.Equal(t, now, resp.Result.LastModified)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_DownloadObject(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	storage := &mockStorage{
		downloadContent: []byte("hello world"),
	}

	var buf bytes.Buffer
	cmd := &csapi.DownloadObjectCmd{
		Storage: storage,
		Key:     "test.txt",
		Writer:  &buf,
	}

	done := make(chan csapi.DownloadObjectResponse, 1)
	err := handlers[csapi.DownloadObject].Handle(context.Background(), cmd, 1, newTestReceiver(func(_ uint64, data any, _ error) {
		done <- data.(csapi.DownloadObjectResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		assert.Equal(t, "hello world", buf.String())
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_UploadObject(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	storage := &mockStorage{}

	cmd := &csapi.UploadObjectCmd{
		Storage: storage,
		Key:     "test.txt",
		Reader:  bytes.NewReader([]byte("test content")),
		Options: &csapi.UploadOptions{
			ContentType: "text/plain",
			Metadata:    map[string]string{"env": "staging"},
		},
	}

	done := make(chan csapi.UploadObjectResponse, 1)
	err := handlers[csapi.UploadObject].Handle(context.Background(), cmd, 1, newTestReceiver(func(_ uint64, data any, _ error) {
		done <- data.(csapi.UploadObjectResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		require.NotNil(t, storage.uploadOpts, "options should reach the storage backend")
		assert.Equal(t, "text/plain", storage.uploadOpts.ContentType)
		assert.Equal(t, "staging", storage.uploadOpts.Metadata["env"])
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_DeleteObjects(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	storage := &mockStorage{}

	cmd := &csapi.DeleteObjectsCmd{
		Storage: storage,
		Keys:    []string{"file1.txt", "file2.txt"},
	}

	done := make(chan csapi.DeleteObjectsResponse, 1)
	err := handlers[csapi.DeleteObjects].Handle(context.Background(), cmd, 1, newTestReceiver(func(_ uint64, data any, _ error) {
		done <- data.(csapi.DeleteObjectsResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_PresignedGetURL(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	storage := &mockStorage{
		presignGetURL: "https://example.com/presigned-get",
	}

	cmd := &csapi.PresignedGetURLCmd{
		Storage:    storage,
		Key:        "test.txt",
		Expiration: 15 * time.Minute,
	}

	done := make(chan csapi.PresignedGetURLResponse, 1)
	err := handlers[csapi.PresignedGetURL].Handle(context.Background(), cmd, 1, newTestReceiver(func(_ uint64, data any, _ error) {
		done <- data.(csapi.PresignedGetURLResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		assert.Equal(t, "https://example.com/presigned-get", resp.URL)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_PresignedPutURL(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	storage := &mockStorage{
		presignPutURL: "https://example.com/presigned-put",
	}

	cmd := &csapi.PresignedPutURLCmd{
		Storage:       storage,
		Key:           "upload.txt",
		Expiration:    30 * time.Minute,
		ContentType:   "text/plain",
		ContentLength: 1024,
	}

	done := make(chan csapi.PresignedPutURLResponse, 1)
	err := handlers[csapi.PresignedPutURL].Handle(context.Background(), cmd, 1, newTestReceiver(func(_ uint64, data any, _ error) {
		done <- data.(csapi.PresignedPutURLResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		assert.Equal(t, "https://example.com/presigned-put", resp.URL)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
