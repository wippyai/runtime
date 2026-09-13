// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishArtifactConcurrentIdenticalArtifacts(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("wippy", "agent-0.1.0-dev.sha256-"+digestFor([]byte("same"))+".wapp")
	data := []byte("same")
	digest := "sha256:" + digestFor(data)
	handle, err := os.OpenRoot(root)
	require.NoError(t, err)
	require.NoError(t, handle.MkdirAll(filepath.Dir(relative), 0o700))
	handle.Close()

	const writers = 12
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wait sync.WaitGroup
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			handle, err := os.OpenRoot(root)
			if err == nil {
				err = publishArtifact(handle, relative, bytes.NewReader(data), digest, uint64(len(data)))
				handle.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, verifyCachedPath(root, relative, digest, uint64(len(data))))
}

func TestPublishArtifactReaderFailureLeavesNoFinalArtifact(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("wippy", "agent-0.1.0-dev.sha256-"+digestFor([]byte("expected"))+".wapp")
	handle, err := os.OpenRoot(root)
	require.NoError(t, err)
	require.NoError(t, handle.MkdirAll(filepath.Dir(relative), 0o700))
	err = publishArtifact(handle, relative, failingArtifactReader{}, "sha256:"+digestFor([]byte("expected")), uint64(len("expected")))
	handle.Close()
	require.Error(t, err)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	_, err = os.Stat(filepath.Join(root, relative))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestPublishArtifactDigestMismatchLeavesNoFinalArtifact(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("wippy", "agent-0.1.0-dev.sha256-"+digestFor([]byte("expected"))+".wapp")
	handle, err := os.OpenRoot(root)
	require.NoError(t, err)
	require.NoError(t, handle.MkdirAll(filepath.Dir(relative), 0o700))
	err = publishArtifact(handle, relative, bytes.NewReader([]byte("tampered")), "sha256:"+digestFor([]byte("expected")), uint64(len("tampered")))
	handle.Close()
	require.Error(t, err)
	_, err = os.Stat(filepath.Join(root, relative))
	require.ErrorIs(t, err, os.ErrNotExist)
}

type failingArtifactReader struct{}

func (failingArtifactReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = 'x'
	return 1, io.ErrUnexpectedEOF
}

func digestFor(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

var _ io.Reader = failingArtifactReader{}
