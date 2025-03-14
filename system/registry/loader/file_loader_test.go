package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	temp_files "github.com/ponyruntime/pony/tests/tempfiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFileLoader(t *testing.T) {
	// Setup test dependencies
	logger := zap.NewNop()
	loader := NewFileLoader(logger)

	t.Run("LoadFile", func(t *testing.T) {
		testCases := []struct {
			name          string
			files         map[string]string
			fileToLoad    string
			expectedError bool
		}{
			{
				name: "valid JSON file",
				files: map[string]string{
					"config.json": `{"key": "value"}`,
				},
				fileToLoad:    "config.json",
				expectedError: false,
			},
			{
				name: "valid YAML file",
				files: map[string]string{
					"config.yaml": "key: value",
				},
				fileToLoad:    "config.yaml",
				expectedError: false,
			},
			{
				name: "valid YML file",
				files: map[string]string{
					"config.yml": "key: value",
				},
				fileToLoad:    "config.yml",
				expectedError: false,
			},
			{
				name: "unsupported file format",
				files: map[string]string{
					"config.txt": "plain text",
				},
				fileToLoad:    "config.txt",
				expectedError: true,
			},
			{
				name: "non-existent file",
				files: map[string]string{
					"config.json": `{"key": "value"}`,
				},
				fileToLoad:    "nonexistent.json",
				expectedError: true,
			},
			{
				name: "invalid JSON content",
				files: map[string]string{
					"invalid.json": `{"key": invalid}`,
				},
				fileToLoad:    "invalid.json",
				expectedError: false, // Raw content loading should succeed
			},
			{
				name: "invalid YAML content",
				files: map[string]string{
					"invalid.yaml": "key: : invalid",
				},
				fileToLoad:    "invalid.yaml",
				expectedError: false, // Raw content loading should succeed
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Spawn temporary directory with test files using the helper
				tmpDir, cleanup := temp_files.TempDirWithFiles(t, "fileloader-test", tc.files)
				defer cleanup()

				// Load the test file
				filePath := filepath.Join(tmpDir, tc.fileToLoad)
				result, err := loader.LoadFile(filePath)

				if tc.expectedError {
					require.Error(t, err)
					require.Nil(t, result)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
					assert.Equal(t, filePath, result.Source())
					assert.NotNil(t, result.Data())
				}
			})
		}
	})

	t.Run("LoadFolder", func(t *testing.T) {
		testCases := []struct {
			name          string
			files         map[string]string
			expectedCount int
			expectedError bool
		}{
			{
				name: "mixed valid files",
				files: map[string]string{
					"config1.json": `{"key": "value1"}`,
					"config2.yaml": "key: value2",
					"config3.yml":  "key: value3",
					"readme.txt":   "ignored file",
				},
				expectedCount: 3, // Only .json, .yaml, and .yml files
				expectedError: false,
			},
			{
				name: "nested directories",
				files: map[string]string{
					"dir1/config1.json":    `{"key": "value1"}`,
					"dir2/config2.yaml":    "key: value2",
					"dir3/sub/config3.yml": "key: value3",
					"readme.txt":           "ignored file",
				},
				expectedCount: 3,
				expectedError: false,
			},
			{
				name: "empty directory",
				files: map[string]string{
					"readme.txt": "ignored file",
				},
				expectedCount: 0,
				expectedError: false,
			},
			{
				name:          "non-existent directory",
				files:         map[string]string{},
				expectedCount: 0,
				expectedError: true,
			},
			{
				name: "invalid file contents",
				files: map[string]string{
					"valid.json":   `{"key": "value"}`,
					"invalid.json": `{"key": invalid}`,
					"valid.yaml":   "key: value",
					"invalid.yaml": "key: : invalid",
					"ignored.txt":  "text content",
				},
				expectedCount: 4, // All .json and .yaml files should be loaded, even if invalid
				expectedError: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				var tmpDir string
				var cleanup func()

				if len(tc.files) > 0 {
					tmpDir, cleanup = temp_files.TempDirWithFiles(t, "fileloader-test", tc.files)
					defer cleanup()
				} else {
					// For non-existent directory test
					tmpDir = filepath.Join(os.TempDir(), "nonexistent-dir")
				}

				// Load all files from the directory
				results, err := loader.LoadFolder(tmpDir)

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
