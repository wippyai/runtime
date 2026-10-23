// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type atomicFileWriter struct {
	rename func(string, string) error
}

func (w atomicFileWriter) write(dir, name string, data []byte) error {
	file, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return err
	}
	path := file.Name()
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	if err := w.renameReplacing(path, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// renameRetryDelays bound how long publication waits for a reader on Windows.
// Go opens files there without delete sharing, so replacing a file that a Get
// or a prune scan has open fails with "Access is denied" until the reader
// closes it, which takes milliseconds.
var renameRetryDelays = []time.Duration{
	2 * time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 200 * time.Millisecond,
}

func (w atomicFileWriter) renameReplacing(from, to string) error {
	err := w.rename(from, to)
	if runtime.GOOS != "windows" {
		return err
	}
	for _, delay := range renameRetryDelays {
		if err == nil || !errors.Is(err, os.ErrPermission) {
			return err
		}
		time.Sleep(delay)
		err = w.rename(from, to)
	}
	return err
}
