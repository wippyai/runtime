// SPDX-License-Identifier: MPL-2.0

package cachedir

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDir_EnvOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-cache")
	t.Setenv("WIPPY_CACHE_DIR", custom)
	assert.Equal(t, custom, Dir())
}

func TestDir_Default(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("WIPPY_CACHE_DIR", "")
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)
	assert.Equal(t, filepath.Join(fakeHome, ".wippy", "cache"), Dir())
}
