package fs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolvePath only strips one leading slash. Double-slash input bypasses
// the traversal check and produces an absolute path.

func TestResolvePath_DoubleSlashStrippedToRelative(t *testing.T) {
	f := NewFS(nil, "app/src")

	resolved, err := f.resolvePath("//etc/passwd")
	require.NoError(t, err)
	assert.Equal(t, "etc/passwd", resolved,
		"all leading slashes stripped, resolves as relative path")
}

func TestResolvePath_TripleSlashStrippedToRelative(t *testing.T) {
	f := NewFS(nil, "app/src")

	resolved, err := f.resolvePath("///etc/passwd")
	require.NoError(t, err)
	assert.Equal(t, "etc/passwd", resolved,
		"all leading slashes stripped, resolves as relative path")
}

func TestResolvePath_AllSlashesStrippedToRoot(t *testing.T) {
	f := NewFS(nil, "app/src")

	resolved, err := f.resolvePath("///")
	require.NoError(t, err)
	assert.Equal(t, ".", resolved, "all-slash path resolves to root")
}

// Traversal that escapes the sandbox is blocked when cwd is shallow.

func TestResolvePath_TraversalBlocked(t *testing.T) {
	f := NewFS(nil, ".")

	tests := []struct {
		name string
		path string
	}{
		{"parent directory", "../etc/passwd"},
		{"deep traversal", "../../../etc/passwd"},
		{"bare dotdot", ".."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.resolvePath(tt.path)
			assert.Error(t, err, "path traversal should be blocked: %s", tt.path)
		})
	}
}

// With deeper cwd, relative ".." that stays within the sandbox is allowed.

func TestResolvePath_RelativeTraversalWithinSandbox(t *testing.T) {
	f := NewFS(nil, "app/src")

	tests := []struct {
		name string
		path string
		want string
	}{
		{"parent stays in sandbox", "../etc/passwd", "app/etc/passwd"},
		{"mid-path stays in sandbox", "foo/../../../etc/passwd", "etc/passwd"},
		{"dotdot to parent dir", "..", "app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := f.resolvePath(tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, resolved)
		})
	}
}

// Traversal that escapes root with deeper cwd.

func TestResolvePath_DeepCwdTraversalBlocked(t *testing.T) {
	f := NewFS(nil, "app/src")

	_, err := f.resolvePath("../../../etc/passwd")
	assert.Error(t, err, "traversal escaping root should be blocked")
}

// Traversal from absolute path that gets stripped.

func TestResolvePath_AbsoluteTraversalBlocked(t *testing.T) {
	f := NewFS(nil, "app/src")

	_, err := f.resolvePath("/../../../etc/passwd")
	assert.Error(t, err, "traversal via absolute path should be blocked")
}

// Null byte injection is correctly blocked.

func TestResolvePath_NullByteBlocked(t *testing.T) {
	f := NewFS(nil, "app/src")

	_, err := f.resolvePath("file\x00.lua")
	assert.ErrorIs(t, err, ErrNullBytePath)
}

// Single leading slash is stripped and becomes relative path.

func TestResolvePath_SingleSlashStripped(t *testing.T) {
	f := NewFS(nil, "app/src")

	resolved, err := f.resolvePath("/etc/passwd")
	require.NoError(t, err)
	assert.Equal(t, "etc/passwd", resolved, "single slash stripped to relative path")
}

// Paths that resolve within the sandbox are allowed.

func TestResolvePath_SafePathsAllowed(t *testing.T) {
	f := NewFS(nil, "app/src")

	tests := []struct {
		name string
		path string
		want string
	}{
		{"simple file", "main.lua", "app/src/main.lua"},
		{"subdirectory", "lib/utils.lua", "app/src/lib/utils.lua"},
		{"dot current", ".", "app/src"},
		{"resolve within root", "foo/../bar", "app/src/bar"},
		{"empty path", "", "app/src"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := f.resolvePath(tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, resolved)
		})
	}
}

// Short cwd doesn't allow mid-path traversal to escape.

func TestResolvePath_ShortCwdTraversalBlocked(t *testing.T) {
	f := NewFS(nil, "a")

	_, err := f.resolvePath("../../etc/passwd")
	assert.Error(t, err, "traversal should be blocked even with short cwd")
}

// Root cwd with traversal attempt.

func TestResolvePath_DotCwdTraversal(t *testing.T) {
	f := NewFS(nil, ".")

	_, err := f.resolvePath("../etc/passwd")
	assert.Error(t, err, "traversal from root cwd should be blocked")
}
