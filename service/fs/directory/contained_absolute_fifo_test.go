//go:build unix

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOwnerSafeContainedAbsoluteFIFO(t *testing.T) {
	for _, op := range []string{"stat", "open", "openfile", "readfile"} {
		t.Run(op, func(t *testing.T) {
			root := t.TempDir()
			fifo := filepath.Join(root, "fifo")
			if err := unix.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(fifo, filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			volume, err := NewFactory().CreateFS(CreateFSConfig{DirPath: root, Mode: 0700, LinkPolicy: "owner_safe"})
			if err != nil {
				t.Fatal(err)
			}
			defer volume.(interface{ Close() error }).Close()
			assertOwnerSafeFIFORefusedPromptly(t, fifo, func() error {
				switch op {
				case "stat":
					_, err := fs.Stat(volume, "link")
					return err
				case "open":
					file, err := volume.Open("link")
					if file != nil {
						_ = file.Close()
					}
					return err
				case "openfile":
					file, err := volume.(*FS).OpenFile("link", os.O_RDONLY, 0)
					if file != nil {
						_ = file.Close()
					}
					return err
				default:
					_, err := fs.ReadFile(volume, "link")
					return err
				}
			})
		})
	}
}

func assertOwnerSafeFIFORefusedPromptly(t *testing.T, fifo string, read func() error) {
	t.Helper()
	// No writer is present: a blocking open cannot reach the file type check.
	done := make(chan error, 1)
	go func() { done <- read() }()
	select {
	case err := <-done:
		if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("expected a non-regular target refusal, got %v", err)
		}
	case <-time.After(time.Second):
		// Release a regressed blocking reader before failing. Supplying data
		// and EOF also releases ReadFile if regular-file validation is lost.
		writer, err := os.OpenFile(fifo, os.O_RDWR|unix.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte("fixture"))
		_ = writer.Close()
		select {
		case err := <-done:
			t.Fatalf("read blocked until a FIFO writer appeared; eventual result: %v", err)
		case <-time.After(time.Second):
			t.Fatal("read stayed blocked even after opening a FIFO writer")
		}
	}
}
