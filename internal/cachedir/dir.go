// SPDX-License-Identifier: MPL-2.0

package cachedir

import (
	"os"
	"path/filepath"
)

// Dir resolves the machine cache directory.
// Precedence: $WIPPY_CACHE_DIR, else ~/.wippy/cache, else os.TempDir()/wippy-cache.
func Dir() string {
	if cacheDir := os.Getenv("WIPPY_CACHE_DIR"); cacheDir != "" {
		return cacheDir
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(homeDir, ".wippy", "cache")
	}

	return filepath.Join(os.TempDir(), "wippy-cache")
}
