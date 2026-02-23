// SPDX-License-Identifier: MPL-2.0

package stream

import "errors"

// Sentinel errors for stream operations.
var (
	ErrNotFound        = errors.New("stream not found")
	ErrClosed          = errors.New("stream closed")
	ErrNotReadable     = errors.New("stream is not readable")
	ErrNotWritable     = errors.New("stream is not writable")
	ErrNotSeekable     = errors.New("stream is not seekable")
	ErrNoTable         = errors.New("resource table not available")
	ErrScannerNotFound = errors.New("scanner not found")
)
