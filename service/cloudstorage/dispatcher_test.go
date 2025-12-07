package cloudstorage

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/dispatcher"
	csapi "github.com/wippyai/runtime/api/dispatcher/cloudstorage"
	"github.com/wippyai/runtime/api/process"
)

type testReceiver struct {
	fn func(tag any, data any, err error)
}

func (r *testReceiver) CompleteYield(tag any, data any, err error) {
	r.fn(tag, data, err)
}

func newTestReceiver(fn func(tag any, data any, err error)) process.ResultReceiver {
	return &testReceiver{fn: fn}
}

type mockStorage struct {
	listResult      *cloudstorage.ListObjectsResult
	listErr         error
	downloadContent []byte
	downloadErr     error
	uploadErr       error
	deleteErr       error
	presignGetURL   string
	presignGetErr   error
	presignPutURL   string
	presignPutErr   error
}

func (m *mockStorage) ListObjects(_ context.Context, _ *cloudstorage.ListObjectsOptions) (*cloudstorage.ListObjectsResult, error) {
	return m.listResult, m.listErr
}

func (m *mockStorage) DownloadObject(_ context.Context, _ string, w io.Writer, _ *cloudstorage.DownloadOptions) error {
	if m.downloadErr != nil {
		return m.downloadErr
	}
	_, err := w.Write(m.downloadContent)
	return err
}

func (m *mockStorage) UploadObject(_ context.Context, _ string, _ io.Reader) error {
	return m.uploadErr
}

func (m *mockStorage) DeleteObjects(_ context.Context, _ []string) error {
	return m.deleteErr
}

func (m *mockStorage) PresignedGetURL(_ context.Context, _ string, _ *cloudstorage.PresignedGetOptions) (string, error) {
	return m.presignGetURL, m.presignGetErr
}

func (m *mockStorage) PresignedPutURL(_ context.Context, _ string, _ *cloudstorage.PresignedPutOptions) (string, error) {
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

	assert.Len(t, registered, 6)
	assert.Contains(t, registered, csapi.CmdListObjects)
	assert.Contains(t, registered, csapi.CmdDownloadObject)
	assert.Contains(t, registered, csapi.CmdUploadObject)
	assert.Contains(t, registered, csapi.CmdDeleteObjects)
	assert.Contains(t, registered, csapi.CmdPresignedGetURL)
	assert.Contains(t, registered, csapi.CmdPresignedPutURL)
}

func TestDispatcher_ListObjects(t *testing.T) {
	d := NewDispatcher()
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	storage := &mockStorage{
		listResult: &cloudstorage.ListObjectsResult{
			Objects: []cloudstorage.ObjectMetadata{
				{Key: "file1.txt", Size: 100},
				{Key: "file2.txt", Size: 200},
			},
			IsTruncated: false,
		},
	}

	cmd := &csapi.ListObjectsCmd{
		Storage: storage,
		Options: &cloudstorage.ListObjectsOptions{Prefix: "test/"},
	}

	done := make(chan csapi.ListObjectsResponse, 1)
	err := handlers[csapi.CmdListObjects].Handle(context.Background(), cmd, "tag1", newTestReceiver(func(_ any, data any, _ error) {
		done <- data.(csapi.ListObjectsResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
		assert.Len(t, resp.Result.Objects, 2)
		assert.Equal(t, "file1.txt", resp.Result.Objects[0].Key)
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
	err := handlers[csapi.CmdDownloadObject].Handle(context.Background(), cmd, "tag1", newTestReceiver(func(_ any, data any, _ error) {
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
	}

	done := make(chan csapi.UploadObjectResponse, 1)
	err := handlers[csapi.CmdUploadObject].Handle(context.Background(), cmd, "tag1", newTestReceiver(func(_ any, data any, _ error) {
		done <- data.(csapi.UploadObjectResponse)
	}))
	require.NoError(t, err)

	select {
	case resp := <-done:
		assert.NoError(t, resp.Error)
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
	err := handlers[csapi.CmdDeleteObjects].Handle(context.Background(), cmd, "tag1", newTestReceiver(func(_ any, data any, _ error) {
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
	err := handlers[csapi.CmdPresignedGetURL].Handle(context.Background(), cmd, "tag1", newTestReceiver(func(_ any, data any, _ error) {
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
	err := handlers[csapi.CmdPresignedPutURL].Handle(context.Background(), cmd, "tag1", newTestReceiver(func(_ any, data any, _ error) {
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
