// SPDX-License-Identifier: MPL-2.0

//go:build darwin

package darwin

import (
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const (
	inheritedFDEnv   = "WIPPY_TEST_INHERITED_BOUND_PATH_FD"
	inheritedPathEnv = "WIPPY_TEST_INHERITED_BOUND_PATH"
)

func TestBoundPathDescriptorsCloseOnExec(t *testing.T) {
	root, err := BindDeclaredDirectory(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	child := root.declared + string(os.PathSeparator) + "child"
	require.NoError(t, os.Mkdir(child, 0o700))

	rootPath, err := root.OpenDescendant(root.declared)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rootPath.Close()) })
	descendantPath, err := root.OpenDescendant(child)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, descendantPath.Close()) })

	for _, file := range []*os.File{root.file, rootPath.file, descendantPath.file} {
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
		require.NoError(t, err)
		require.NotZero(t, flags&unix.FD_CLOEXEC)
	}

	for _, path := range []*BoundPath{rootPath, descendantPath} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBoundPathDescriptorIsNotInherited$")
		cmd.Env = append(os.Environ(),
			inheritedFDEnv+"="+strconv.Itoa(int(path.file.Fd())),
			inheritedPathEnv+"="+path.Path,
		)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
}

func TestBoundPathDescriptorIsNotInherited(t *testing.T) {
	raw := os.Getenv(inheritedFDEnv)
	if raw == "" {
		t.Skip("helper process")
	}
	fd, err := strconv.Atoi(raw)
	require.NoError(t, err)
	actual, err := pathFromFD(fd)
	if err != nil {
		return
	}
	require.NotEqual(t, os.Getenv(inheritedPathEnv), actual, "bound directory descriptor survived exec")
}
