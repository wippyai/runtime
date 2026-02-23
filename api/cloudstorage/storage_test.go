// SPDX-License-Identifier: MPL-2.0

// Package cloudstorage provides interfaces and types for interacting with cloud storage services.
package cloudstorage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
)

func TestObjectMetadata_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		metadata ObjectMetadata
		wantErr  bool
	}{
		{
			name: "complete metadata",
			metadata: ObjectMetadata{
				Key:         "documents/file.txt",
				Size:        1024,
				ContentType: "text/plain",
				ETag:        "abc123",
			},
			wantErr: false,
		},
		{
			name: "minimal metadata",
			metadata: ObjectMetadata{
				Key:  "file.txt",
				Size: 0,
			},
			wantErr: false,
		},
		{
			name: "large file",
			metadata: ObjectMetadata{
				Key:         "archive.zip",
				Size:        9223372036854775807,
				ContentType: "application/zip",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.metadata)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded ObjectMetadata
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.metadata, decoded)
		})
	}
}

func TestListObjectsOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		options ListObjectsOptions
		wantErr bool
	}{
		{
			name: "with all options",
			options: ListObjectsOptions{
				Prefix:            "documents/",
				MaxKeys:           100,
				ContinuationToken: "token123",
			},
			wantErr: false,
		},
		{
			name:    "empty options",
			options: ListObjectsOptions{},
			wantErr: false,
		},
		{
			name: "prefix only",
			options: ListObjectsOptions{
				Prefix: "images/",
			},
			wantErr: false,
		},
		{
			name: "max keys only",
			options: ListObjectsOptions{
				MaxKeys: 50,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.options)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded ListObjectsOptions
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.options, decoded)
		})
	}
}

func TestListObjectsResult_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		result  ListObjectsResult
		wantErr bool
	}{
		{
			name: "truncated result with objects",
			result: ListObjectsResult{
				Objects: []ObjectMetadata{
					{Key: "file1.txt", Size: 100},
					{Key: "file2.txt", Size: 200},
				},
				IsTruncated:           true,
				NextContinuationToken: "next_token",
			},
			wantErr: false,
		},
		{
			name: "complete result",
			result: ListObjectsResult{
				Objects: []ObjectMetadata{
					{Key: "file1.txt", Size: 100},
				},
				IsTruncated: false,
			},
			wantErr: false,
		},
		{
			name: "empty result",
			result: ListObjectsResult{
				Objects:     []ObjectMetadata{},
				IsTruncated: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.result)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded ListObjectsResult
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.result, decoded)
		})
	}
}

func TestPresignedGetOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		options PresignedGetOptions
		wantErr bool
	}{
		{
			name: "with expiration",
			options: PresignedGetOptions{
				Expiration: 15 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "1 hour expiration",
			options: PresignedGetOptions{
				Expiration: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name:    "zero expiration",
			options: PresignedGetOptions{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.options)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded PresignedGetOptions
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.options, decoded)
		})
	}
}

func TestPresignedPutOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		options PresignedPutOptions
		wantErr bool
	}{
		{
			name: "complete options",
			options: PresignedPutOptions{
				Expiration:    15 * time.Minute,
				ContentType:   "application/json",
				ContentLength: 1024,
			},
			wantErr: false,
		},
		{
			name: "with content type only",
			options: PresignedPutOptions{
				Expiration:  30 * time.Minute,
				ContentType: "image/png",
			},
			wantErr: false,
		},
		{
			name:    "minimal options",
			options: PresignedPutOptions{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.options)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded PresignedPutOptions
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.options, decoded)
		})
	}
}

func TestDownloadOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		options DownloadOptions
		wantErr bool
	}{
		{
			name: "with range",
			options: DownloadOptions{
				Range: "bytes=0-1023",
			},
			wantErr: false,
		},
		{
			name: "without range",
			options: DownloadOptions{
				Range: "",
			},
			wantErr: false,
		},
		{
			name: "partial range",
			options: DownloadOptions{
				Range: "bytes=500-",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.options)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded DownloadOptions
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.options, decoded)
		})
	}
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, ListObjects, dispatcher.CommandID(160))
	assert.Equal(t, DownloadObject, dispatcher.CommandID(161))
	assert.Equal(t, UploadObject, dispatcher.CommandID(162))
	assert.Equal(t, DeleteObjects, dispatcher.CommandID(163))
	assert.Equal(t, PresignedGetURL, dispatcher.CommandID(164))
	assert.Equal(t, PresignedPutURL, dispatcher.CommandID(165))
}

func TestListObjectsCmd(t *testing.T) {
	cmd := AcquireListObjectsCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, ListObjects, cmd.CmdID())

	cmd.Options = &ListObjectsOptions{Prefix: "test/"}
	cmd.Release()

	cmd2 := AcquireListObjectsCmd()
	assert.Nil(t, cmd2.Storage)
	assert.Nil(t, cmd2.Options)
	cmd2.Release()
}

func TestDownloadObjectCmd(t *testing.T) {
	cmd := AcquireDownloadObjectCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, DownloadObject, cmd.CmdID())

	cmd.Key = "test.txt"
	cmd.Options = &DownloadOptions{Range: "bytes=0-100"}
	cmd.Release()

	cmd2 := AcquireDownloadObjectCmd()
	assert.Nil(t, cmd2.Storage)
	assert.Empty(t, cmd2.Key)
	assert.Nil(t, cmd2.Writer)
	assert.Nil(t, cmd2.Options)
	cmd2.Release()
}

func TestUploadObjectCmd(t *testing.T) {
	cmd := AcquireUploadObjectCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, UploadObject, cmd.CmdID())

	cmd.Key = "upload.txt"
	cmd.Release()

	cmd2 := AcquireUploadObjectCmd()
	assert.Nil(t, cmd2.Storage)
	assert.Empty(t, cmd2.Key)
	assert.Nil(t, cmd2.Reader)
	cmd2.Release()
}

func TestDeleteObjectsCmd(t *testing.T) {
	cmd := AcquireDeleteObjectsCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, DeleteObjects, cmd.CmdID())

	cmd.Keys = []string{"file1.txt", "file2.txt"}
	cmd.Release()

	cmd2 := AcquireDeleteObjectsCmd()
	assert.Nil(t, cmd2.Storage)
	assert.Nil(t, cmd2.Keys)
	cmd2.Release()
}

func TestPresignedGetURLCmd(t *testing.T) {
	cmd := AcquirePresignedGetURLCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, PresignedGetURL, cmd.CmdID())

	cmd.Key = "download.txt"
	cmd.Expiration = 15 * time.Minute
	cmd.Release()

	cmd2 := AcquirePresignedGetURLCmd()
	assert.Nil(t, cmd2.Storage)
	assert.Empty(t, cmd2.Key)
	assert.Zero(t, cmd2.Expiration)
	cmd2.Release()
}

func TestPresignedPutURLCmd(t *testing.T) {
	cmd := AcquirePresignedPutURLCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, PresignedPutURL, cmd.CmdID())

	cmd.Key = "upload.txt"
	cmd.Expiration = 30 * time.Minute
	cmd.ContentType = "text/plain"
	cmd.ContentLength = 1024
	cmd.Release()

	cmd2 := AcquirePresignedPutURLCmd()
	assert.Nil(t, cmd2.Storage)
	assert.Empty(t, cmd2.Key)
	assert.Zero(t, cmd2.Expiration)
	assert.Empty(t, cmd2.ContentType)
	assert.Zero(t, cmd2.ContentLength)
	cmd2.Release()
}

func TestResponseTypes(t *testing.T) {
	t.Run("ListObjectsResponse", func(t *testing.T) {
		resp := ListObjectsResponse{
			Result: &ListObjectsResult{Objects: []ObjectMetadata{{Key: "test.txt"}}},
		}
		assert.NotNil(t, resp.Result)
		assert.Nil(t, resp.Error)
	})

	t.Run("DownloadObjectResponse", func(t *testing.T) {
		resp := DownloadObjectResponse{Error: nil}
		assert.Nil(t, resp.Error)
	})

	t.Run("UploadObjectResponse", func(t *testing.T) {
		resp := UploadObjectResponse{Error: nil}
		assert.Nil(t, resp.Error)
	})

	t.Run("DeleteObjectsResponse", func(t *testing.T) {
		resp := DeleteObjectsResponse{Error: nil}
		assert.Nil(t, resp.Error)
	})

	t.Run("PresignedGetURLResponse", func(t *testing.T) {
		resp := PresignedGetURLResponse{URL: "https://example.com/file"}
		assert.Equal(t, "https://example.com/file", resp.URL)
		assert.Nil(t, resp.Error)
	})

	t.Run("PresignedPutURLResponse", func(t *testing.T) {
		resp := PresignedPutURLResponse{URL: "https://example.com/upload"}
		assert.Equal(t, "https://example.com/upload", resp.URL)
		assert.Nil(t, resp.Error)
	})
}
