package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func Test_resolveModulePath(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	projectRoot, err := os.Getwd()
	require.NoError(t, err)

	t.Run("absolute path within current directory", func(t *testing.T) {
		t.Parallel()

		// Create a test path within current directory
		testPath := filepath.Join(projectRoot, "test", "module")
		expected := "test/module"

		result, err := resolveModulePath(testPath, projectRoot, logger)
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("absolute path escaping current directory", func(t *testing.T) {
		t.Parallel()

		// Create a path that goes outside current directory
		parentDir := filepath.Dir(projectRoot)
		escapePath := filepath.Join(parentDir, "outside", "module")

		result, err := resolveModulePath(escapePath, projectRoot, logger)
		assert.Error(t, err)
		assert.Empty(t, result)
		assert.Contains(t, err.Error(), "is outside the project root")
	})

	t.Run("relative path with ./ prefix", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			modulePath string
			want       string
		}{
			{
				name:       "simple path",
				modulePath: "./test/module",
				want:       "test/module",
			},
			{
				name:       "nested path",
				modulePath: "./src/components/button",
				want:       "src/components/button",
			},
			{
				name:       "single level",
				modulePath: "./module",
				want:       "module",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			})
		}
	})

	t.Run("relative path with ../ prefix", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			modulePath string
		}{
			{
				name:       "single level up",
				modulePath: "../test/module",
			},
			{
				name:       "multiple levels up",
				modulePath: "../../../test/module",
			},
			{
				name:       "mixed with ./",
				modulePath: ".././test/module",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
				assert.Error(t, err)
				assert.Empty(t, result)
				assert.Contains(t, err.Error(), "is outside the project root")
			})
		}
	})

	t.Run("plain relative path", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			modulePath string
			want       string
		}{
			{
				name:       "simple path",
				modulePath: "test/module",
				want:       "test/module",
			},
			{
				name:       "nested path",
				modulePath: "src/components/button",
				want:       "src/components/button",
			},
			{
				name:       "single level",
				modulePath: "module",
				want:       "module",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			})
		}
	})

	t.Run("cross-platform path separators", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "Windows backslashes",
					modulePath: "test\\module\\path",
					want:       "test/module/path",
				},
				{
					name:       "Windows absolute path",
					modulePath: filepath.Join("C:\\", "test", "module"),
					want:       "",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					// For Windows absolute path test
					if filepath.IsAbs(tt.modulePath) && (strings.HasPrefix(tt.modulePath, "C:") || strings.HasPrefix(tt.modulePath, "c:")) {
						result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
						assert.Error(t, err)
						assert.Empty(t, result)
						return
					}

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		} else {
			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "Unix forward slashes",
					modulePath: "test/module/path",
					want:       "test/module/path",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		}
	})

	t.Run("absolute path same as project root", func(t *testing.T) {
		t.Parallel()

		result, err := resolveModulePath(projectRoot, projectRoot, logger)
		require.NoError(t, err)
		assert.Equal(t, ".", result)
	})

	t.Run("empty path", func(t *testing.T) {
		t.Parallel()

		result, err := resolveModulePath("", projectRoot, logger)
		require.NoError(t, err)
		assert.Equal(t, "", result)
	})

	t.Run("path with multiple ./ prefixes", func(t *testing.T) {
		t.Parallel()

		result, err := resolveModulePath("./././test/module", projectRoot, logger)
		require.NoError(t, err)
		// Path is normalized by filepath.Clean, so multiple ./ are removed
		assert.Equal(t, "test/module", result)
	})

	t.Run("absolute path in subdirectory", func(t *testing.T) {
		t.Parallel()

		// Create temp directory structure
		tmpDir := filepath.Join(projectRoot, "test_resolve")
		err := os.MkdirAll(tmpDir, 0755)
		require.NoError(t, err)
		defer func() {
			_ = os.RemoveAll(tmpDir)
		}()

		// Test absolute path to subdirectory
		subPath := filepath.Join(tmpDir, "sub", "module")
		expected := "test_resolve/sub/module"

		result, err := resolveModulePath(subPath, projectRoot, logger)
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("error cases", func(t *testing.T) {
		t.Parallel()

		t.Run("absolute path escaping with single ..", func(t *testing.T) {
			t.Parallel()

			parentDir := filepath.Dir(projectRoot)
			escapePath := filepath.Join(parentDir, "module")

			result, err := resolveModulePath(escapePath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})

		t.Run("absolute path escaping with multiple ..", func(t *testing.T) {
			t.Parallel()

			// Go up multiple levels
			parentDir := filepath.Dir(projectRoot)
			grandParentDir := filepath.Dir(parentDir)
			escapePath := filepath.Join(grandParentDir, "module")

			result, err := resolveModulePath(escapePath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})

		t.Run("absolute path escaping to root", func(t *testing.T) {
			t.Parallel()

			if runtime.GOOS == "windows" {
				t.Skip("Skipping root path test on Windows")
			}

			// Test path that escapes to root
			rootPath := filepath.Join(string(filepath.Separator), "etc", "module")

			result, err := resolveModulePath(rootPath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})

		t.Run("Windows different drive error", func(t *testing.T) {
			t.Parallel()

			if runtime.GOOS != "windows" {
				t.Skip("Skipping Windows-specific test on non-Windows platform")
			}

			currentDir, err := os.Getwd()
			require.NoError(t, err)

			// On Windows, filepath.Rel can fail if paths are on different drives
			// Try to find a different drive (if available)
			if len(currentDir) > 0 && currentDir[0] >= 'A' && currentDir[0] <= 'Z' {
				// Current drive is a letter, try opposite drive
				var otherDrive string
				if currentDir[0] == 'C' {
					otherDrive = "D:"
				} else {
					otherDrive = "C:"
				}

				otherDrivePath := filepath.Join(otherDrive+string(filepath.Separator), "test", "module")

				// filepath.Rel will fail for paths on different drives
				result, err := resolveModulePath(otherDrivePath, projectRoot, logger)
				// This should either error from filepath.Rel or from security check
				assert.Error(t, err)
				assert.Empty(t, result)
				// Error message should indicate the problem
				assert.True(t,
					strings.Contains(err.Error(), "failed to resolve relative path") ||
						strings.Contains(err.Error(), "is outside the project root"),
					"Error message should indicate path resolution problem, got: %s", err.Error())
			}
		})

		t.Run("absolute path with .. in middle", func(t *testing.T) {
			t.Parallel()

			// Create a path that uses .. in the middle (should be normalized but still escape)
			// This tests the security check after filepath.Rel
			// Use a path that would normalize to outside current dir
			testSubDir := filepath.Join(projectRoot, "test")
			err := os.MkdirAll(testSubDir, 0755)
			require.NoError(t, err)
			defer func() {
				_ = os.RemoveAll(testSubDir)
			}()

			// Path that goes up and then back down (but still outside)
			escapePath := filepath.Join(testSubDir, "..", "..", "outside", "module")

			result, err := resolveModulePath(escapePath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})

		t.Run("absolute path exactly at parent boundary", func(t *testing.T) {
			t.Parallel()

			parentDir := filepath.Dir(projectRoot)
			// Path exactly at parent (should still be considered outside)
			escapePath := parentDir

			result, err := resolveModulePath(escapePath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})

		t.Run("absolute path with .. at start", func(t *testing.T) {
			t.Parallel()

			// Create absolute path that starts with .. (normalized by filepath.Join)
			parentDir := filepath.Dir(projectRoot)
			escapePath := filepath.Join(parentDir, "..", "outside", "module")

			result, err := resolveModulePath(escapePath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})

		t.Run("absolute path with multiple .. sequences", func(t *testing.T) {
			t.Parallel()

			// Create path with multiple .. sequences
			testSubDir := filepath.Join(projectRoot, "test", "sub")
			err := os.MkdirAll(testSubDir, 0755)
			require.NoError(t, err)
			defer func() {
				_ = os.RemoveAll(filepath.Join(projectRoot, "test"))
			}()

			// Path with multiple .. that escapes
			escapePath := filepath.Join(testSubDir, "..", "..", "..", "outside", "module")

			result, err := resolveModulePath(escapePath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})

		t.Run("absolute path with .. and . mixed", func(t *testing.T) {
			t.Parallel()

			// Create path with .. and . mixed
			testSubDir := filepath.Join(projectRoot, "test")
			err := os.MkdirAll(testSubDir, 0755)
			require.NoError(t, err)
			defer func() {
				_ = os.RemoveAll(testSubDir)
			}()

			// Path with .. and . that escapes
			escapePath := filepath.Join(testSubDir, ".", "..", "..", "outside", "module")

			result, err := resolveModulePath(escapePath, projectRoot, logger)
			assert.Error(t, err)
			assert.Empty(t, result)
			assert.Contains(t, err.Error(), "is outside the project root")
		})
	})

	t.Run("edge cases with unusual paths", func(t *testing.T) {
		t.Parallel()

		t.Run("path with multiple slashes", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "double slashes",
					modulePath: "test//module",
					want:       "test//module",
				},
				{
					name:       "triple slashes",
					modulePath: "test///module",
					want:       "test///module",
				},
				{
					name:       "multiple slashes in middle",
					modulePath: "test//sub//module",
					want:       "test//sub//module",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		})

		t.Run("path with trailing slashes", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "single trailing slash",
					modulePath: "test/module/",
					want:       "test/module/",
				},
				{
					name:       "multiple trailing slashes",
					modulePath: "test/module///",
					want:       "test/module///",
				},
				{
					name:       "trailing slash with ./",
					modulePath: "./test/module/",
					want:       "test/module",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		})

		t.Run("path with dots in different places", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "dot in filename",
					modulePath: "test.module",
					want:       "test.module",
				},
				{
					name:       "multiple dots in filename",
					modulePath: "test.module.file",
					want:       "test.module.file",
				},
				{
					name:       "dot at start of filename",
					modulePath: "test/.module",
					want:       "test/.module",
				},
				{
					name:       "dot at end of filename",
					modulePath: "test/module.",
					want:       "test/module.",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		})

		t.Run("path with spaces", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "space in filename",
					modulePath: "test/module name",
					want:       "test/module name",
				},
				{
					name:       "spaces in directory name",
					modulePath: "test dir/module",
					want:       "test dir/module",
				},
				{
					name:       "multiple spaces",
					modulePath: "test  module",
					want:       "test  module",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		})

		t.Run("path with special characters", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "hyphen in path",
					modulePath: "test-module/file",
					want:       "test-module/file",
				},
				{
					name:       "underscore in path",
					modulePath: "test_module/file",
					want:       "test_module/file",
				},
				{
					name:       "numbers in path",
					modulePath: "test123/module456",
					want:       "test123/module456",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		})

		t.Run("path with complex combinations", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "mixed dots and slashes",
					modulePath: "./test/./module/./file",
					want:       "test/module/file",
				},
				{
					name:       "path with .. in middle",
					modulePath: "test/../outside/module",
					want:       "test/../outside/module",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					if tt.want == "" {
						// This should be an error case
						assert.Error(t, err)
						assert.Empty(t, result)
					} else {
						require.NoError(t, err)
						assert.Equal(t, tt.want, result)
					}
				})
			}
		})

		t.Run("path with only dots", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "single dot",
					modulePath: ".",
					want:       ".",
				},
				{
					name:       "double dots",
					modulePath: "..",
					want:       "..",
				},
				{
					name:       "triple dots",
					modulePath: "...",
					want:       "...",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					if tt.want == "" {
						// This should be an error case
						assert.Error(t, err)
						assert.Empty(t, result)
					} else {
						require.NoError(t, err)
						assert.Equal(t, tt.want, result)
					}
				})
			}
		})

		t.Run("path with leading and trailing elements", func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "leading slash removed",
					modulePath: "/test/module",
					want:       "",
				},
				{
					name:       "just slash",
					modulePath: "/",
					want:       "",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					if runtime.GOOS == "windows" {
						// On Windows, leading slash might be handled differently
						t.Skip("Skipping leading slash test on Windows")
					}

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					if tt.want == "" {
						// This should be an error case (absolute path outside)
						assert.Error(t, err)
						assert.Empty(t, result)
					} else {
						require.NoError(t, err)
						assert.Equal(t, tt.want, result)
					}
				})
			}
		})

		t.Run("path with normalized elements", func(t *testing.T) {
			t.Parallel()

			currentDir, err := os.Getwd()
			require.NoError(t, err)

			// Create test directory structure
			testDir := filepath.Join(currentDir, "test_edge")
			err = os.MkdirAll(testDir, 0755)
			require.NoError(t, err)
			defer func() {
				_ = os.RemoveAll(testDir)
			}()

			tests := []struct {
				name       string
				modulePath string
				want       string
			}{
				{
					name:       "path with . in middle",
					modulePath: filepath.Join(testDir, "sub", ".", "module"),
					want:       "test_edge/sub/module",
				},
				{
					name:       "path that normalizes to same directory",
					modulePath: filepath.Join(testDir, "sub", "..", "module"),
					want:       "test_edge/module",
				},
			}

			for _, tt := range tests {

				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					result, err := resolveModulePath(tt.modulePath, projectRoot, logger)
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				})
			}
		})
	})
}
