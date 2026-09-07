//go:build linux

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	fsapi "github.com/wippyai/runtime/api/fs"
)

func TestDescriptorAppendAcrossIndependentHandles(t *testing.T) {
	root := t.TempDir()
	filesystem, err := NewFS(root, 0700, false)
	require.NoError(t, err)
	defer filesystem.Close()
	directory, err := os.Open(root)
	require.NoError(t, err)
	defer directory.Close()
	require.NoError(t, os.WriteFile(filepath.Join(root, "log"), []byte("prefix\n"), 0600))
	const writers = 4
	const records = 200
	files := make([]*descriptorFileLinux, writers)
	for i := range files {
		file, err := filesystem.OpenDescriptorAt(directory, "log", fsapi.DescriptorOpenRequest{Write: true, NoFollow: true})
		require.NoError(t, err)
		files[i] = file.(*descriptorFileLinux)
		defer file.Close()
	}
	start := make(chan struct{})
	results := make(chan error, writers)
	var workers sync.WaitGroup
	for i, file := range files {
		workers.Go(func() {
			<-start
			record := []byte{byte('a' + i), '\n'}
			for range records {
				n, err := file.Append(record)
				if err != nil {
					results <- err
					return
				}
				if n != len(record) {
					t.Errorf("short append: %d", n)
					return
				}
			}
		})
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	// A positional write must remain positional after atomic append calls.
	n, err := files[0].WriteAt([]byte("PREFIX"), 0)
	require.NoError(t, err)
	require.Equal(t, 6, n)
	data, err := os.ReadFile(filepath.Join(root, "log"))
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(data, []byte("PREFIX\n")))
	require.Len(t, data, len("prefix\n")+writers*records*2)
	for i := range writers {
		require.Equal(t, records, bytes.Count(data, []byte{byte('a' + i), '\n'}))
	}
}
