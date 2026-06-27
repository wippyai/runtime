// SPDX-License-Identifier: MPL-2.0

// Package archive provides the built-in codec implementations (zip, tar,
// tar.gz, tar.zst) registered into api/archive. All readers stream with
// bounded memory: nothing materializes a whole archive or a whole entry.
package archive

import (
	"errors"
	"io"
	"path"
	"strings"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

// Defaults for bounded-memory and safety limits.
const (
	DefaultMaxEntries     = 100_000
	DefaultMaxTotalBytes  = 2 << 30 // 2 GiB
	DefaultMaxFileBytes   = 1 << 30 // 1 GiB
	DefaultMaxInlineBytes = 16 << 20
	DefaultBufferBytes    = 64 << 10
)

// ErrLimitExceeded is returned when an archive violates a configured bound.
var ErrLimitExceeded = errors.New("archive limit exceeded")

// ErrTooLarge is returned when a single entry exceeds the per-file cap.
var ErrTooLarge = errors.New("archive entry exceeds size limit")

// withDefaults fills unset options with safe defaults.
func withDefaults(o archiveapi.Options) archiveapi.Options {
	if o.MaxEntries == 0 {
		o.MaxEntries = DefaultMaxEntries
	}
	if o.MaxTotalBytes == 0 {
		o.MaxTotalBytes = DefaultMaxTotalBytes
	}
	if o.MaxFileBytes == 0 {
		o.MaxFileBytes = DefaultMaxFileBytes
	}
	if o.MaxInlineBytes == 0 {
		o.MaxInlineBytes = DefaultMaxInlineBytes
	}
	if o.BufferBytes == 0 {
		o.BufferBytes = DefaultBufferBytes
	}
	return o
}

// limitedReadCloser caps how many bytes can be read from an entry, defending
// against a header that understates the real (decompressed) size.
type limitedReadCloser struct {
	r      io.Reader
	c      io.Closer
	n      int64
	max    int64
	closed bool
}

func capReader(rc io.ReadCloser, max int64) io.ReadCloser {
	if max <= 0 {
		return rc
	}
	return &limitedReadCloser{r: rc, c: rc, max: max}
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.n += int64(n)
	if l.n > l.max {
		// Hand back only the bytes up to the cap, never beyond it.
		over := int(l.n - l.max)
		if over > n {
			over = n
		}
		return n - over, ErrTooLarge
	}
	return n, err
}

func (l *limitedReadCloser) Close() error {
	if l.closed {
		return nil
	}
	l.closed = true
	if l.c != nil {
		return l.c.Close()
	}
	return nil
}

// SanitizeEntryName cleans an archive entry path for safe extraction, rejecting
// absolute paths, drive/UNC prefixes, and any traversal that escapes the root.
// It returns the cleaned forward-slash relative path and ok=false when the
// entry must be skipped.
func SanitizeEntryName(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" {
		return "", false
	}
	if strings.HasPrefix(name, "/") {
		return "", false
	}
	// Windows drive (C:) or UNC-style prefixes.
	if len(name) >= 2 && name[1] == ':' {
		return "", false
	}
	isDir := strings.HasSuffix(name, "/")
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return "", false
		}
	}
	if isDir {
		clean += "/"
	}
	return clean, true
}
