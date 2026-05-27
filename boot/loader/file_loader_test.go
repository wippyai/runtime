// SPDX-License-Identifier: MPL-2.0

package loader

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"go.uber.org/zap"
)

func TestFileLoader_SkipTemporalDirs(t *testing.T) {
	t.Setenv("SKIP_TEMPORAL_TESTS", "1")

	loader := NewFileLoader(zap.NewNop())
	fsys := fstest.MapFS{
		"test/temporal/case.yaml":  &fstest.MapFile{Data: []byte("k: v")},
		"test/_temporal/case.yaml": &fstest.MapFile{Data: []byte("k: v")},
		"test/other/case.yaml":     &fstest.MapFile{Data: []byte("k: v")},
	}

	results, err := loader.LoadFS(fsys)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "test/other/case.yaml", results[0].Source())
}

func TestFileLoader_SkipSQSDirs(t *testing.T) {
	t.Setenv("SKIP_SQS_TESTS", "1")

	loader := NewFileLoader(zap.NewNop())
	fsys := fstest.MapFS{
		"test/sqs/case.yaml":   &fstest.MapFile{Data: []byte("k: v")},
		"test/other/case.yaml": &fstest.MapFile{Data: []byte("k: v")},
	}

	results, err := loader.LoadFS(fsys)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "test/other/case.yaml", results[0].Source())
}

func TestFileLoader_SkipsNodeModules(t *testing.T) {
	loader := NewFileLoader(zap.NewNop())
	fsys := fstest.MapFS{
		"_index.yaml":                  &fstest.MapFile{Data: []byte("version: \"1.0\"\nnamespace: app\nentries: []\n")},
		"node_modules/pkg/config.json": &fstest.MapFile{Data: []byte(`{"name":"not-a-wippy-entry"}`)},
		"src/node_modules/pkg/x.yaml":  &fstest.MapFile{Data: []byte("not: loaded\n")},
	}

	results, err := loader.LoadFS(fsys)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "_index.yaml", results[0].Source())
}

func TestFileLoader(t *testing.T) {
	// Setup test dependencies
	logger := zap.NewNop()
	loader := NewFileLoader(logger)

	t.Run("LoadFile", func(t *testing.T) {
		testCases := []struct {
			name          string
			files         fstest.MapFS
			fileToLoad    string
			expectedError bool
		}{
			{
				name: "valid JSON file",
				files: fstest.MapFS{
					"config.json": &fstest.MapFile{Data: []byte(`{"key": "value"}`)},
				},
				fileToLoad:    "config.json",
				expectedError: false,
			},
			{
				name: "valid YAML file",
				files: fstest.MapFS{
					"config.yaml": &fstest.MapFile{Data: []byte("key: value")},
				},
				fileToLoad:    "config.yaml",
				expectedError: false,
			},
			{
				name: "valid YML file",
				files: fstest.MapFS{
					"config.yml": &fstest.MapFile{Data: []byte("key: value")},
				},
				fileToLoad:    "config.yml",
				expectedError: false,
			},
			{
				name: "unsupported file format",
				files: fstest.MapFS{
					"config.txt": &fstest.MapFile{Data: []byte("plain text")},
				},
				fileToLoad:    "config.txt",
				expectedError: true,
			},
			{
				name: "non-existent file",
				files: fstest.MapFS{
					"config.json": &fstest.MapFile{Data: []byte(`{"key": "value"}`)},
				},
				fileToLoad:    "nonexistent.json",
				expectedError: true,
			},
			{
				name: "invalid JSON content",
				files: fstest.MapFS{
					"invalid.json": &fstest.MapFile{Data: []byte(`{"key": invalid}`)},
				},
				fileToLoad:    "invalid.json",
				expectedError: false, // Raw content loading should succeed
			},
			{
				name: "invalid YAML content",
				files: fstest.MapFS{
					"invalid.yaml": &fstest.MapFile{Data: []byte("key: : invalid")},
				},
				fileToLoad:    "invalid.yaml",
				expectedError: false, // Raw content loading should succeed
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Use the in-memory filesystem
				fsys := tc.files

				// Load the test file
				result, err := loader.LoadFile(fsys, tc.fileToLoad)

				if tc.expectedError {
					require.Error(t, err)
					require.Nil(t, result)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, tc.fileToLoad, result.Source())
					assert.NotNil(t, result.Data())
				}
			})
		}
	})

	t.Run("LoadFS", func(t *testing.T) {
		testCases := []struct {
			files         fstest.MapFS
			name          string
			expectedCount int
			expectedError bool
		}{
			{
				name: "mixed valid files",
				files: fstest.MapFS{
					"config1.json": &fstest.MapFile{Data: []byte(`{"key": "value1"}`)},
					"config2.yaml": &fstest.MapFile{Data: []byte("key: value2")},
					"config3.yml":  &fstest.MapFile{Data: []byte("key: value3")},
					"readme.txt":   &fstest.MapFile{Data: []byte("ignored file")},
				},
				expectedCount: 3, // Only .json, .yaml, and .yml files
				expectedError: false,
			},
			{
				name: "nested directories",
				files: fstest.MapFS{
					"dir1/config1.json":    &fstest.MapFile{Data: []byte(`{"key": "value1"}`)},
					"dir2/config2.yaml":    &fstest.MapFile{Data: []byte("key: value2")},
					"dir3/sub/config3.yml": &fstest.MapFile{Data: []byte("key: value3")},
					"readme.txt":           &fstest.MapFile{Data: []byte("ignored file")},
				},
				expectedCount: 3,
				expectedError: false,
			},
			{
				name: "empty directory",
				files: fstest.MapFS{
					"readme.txt": &fstest.MapFile{Data: []byte("ignored file")},
				},
				expectedCount: 0,
				expectedError: false,
			},
			{
				name: "invalid file contents",
				files: fstest.MapFS{
					"valid.json":   &fstest.MapFile{Data: []byte(`{"key": "value"}`)},
					"invalid.json": &fstest.MapFile{Data: []byte(`{"key": invalid}`)},
					"valid.yaml":   &fstest.MapFile{Data: []byte("key: value")},
					"invalid.yaml": &fstest.MapFile{Data: []byte("key: : invalid")},
					"ignored.txt":  &fstest.MapFile{Data: []byte("text content")},
				},
				expectedCount: 4, // All .json and .yaml files should be loaded, even if invalid
				expectedError: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Use the in-memory filesystem
				fsys := tc.files

				// Load all files from the directory
				results, err := loader.LoadFS(fsys)

				if tc.expectedError {
					require.Error(t, err)
					require.Nil(t, results)
				} else {
					require.NoError(t, err)
					require.Len(t, results, tc.expectedCount)

					// Verify each loaded file
					for _, result := range results {
						require.NotNil(t, result)
						assert.Contains(t, []payload.Format{payload.JSON, payload.YAML}, result.Format())
						assert.NotEmpty(t, result.Source())
						assert.NotNil(t, result.Data())
					}
				}
			})
		}
	})
}
