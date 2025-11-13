// Package cloudstorage provides interfaces and types for interacting with cloud storage services.
package cloudstorage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
