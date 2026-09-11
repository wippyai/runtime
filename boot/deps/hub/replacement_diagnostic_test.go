// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/boot/deps/lock"
)

func TestMissingReplacementDiagnosticIdentifiesLocalModuleAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	h := &DependencyHandler{replacements: map[string]lock.Replacement{
		"example/local": {From: "example/local", To: path},
	}}
	err := h.refreshReplacementModuleIdentities([]ResolvedModule{{Org: "example", Name: "local", Version: "1.0.0"}})
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.Contains(t, err.Error(), "local replacement")
	require.Contains(t, err.Error(), "example/local")
	require.Contains(t, filepath.ToSlash(err.Error()), filepath.ToSlash(path))
	require.False(t, strings.Contains(err.Error(), "downloaded"))
	var detailed apierror.Error
	require.ErrorAs(t, err, &detailed)
	require.Equal(t, apierror.Invalid, detailed.Kind())
	require.Contains(t, detailed.Details().GetString("module", ""), "example/local")
	require.Equal(t, path, detailed.Details().GetString("path", ""))
}
