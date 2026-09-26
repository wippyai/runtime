// SPDX-License-Identifier: MPL-2.0

// Package helperimage defines the trailer used to carry the separately built
// Linux confinement helper in a single installed runtime executable.
package helperimage

import (
	"encoding/binary"
	"errors"
	"io"
)

const Magic = "WIPPY-CONFINE-v1"
const TrailerSize = int64(len(Magic) + 8)
const MaxBytes = 64 << 20

func Locate(source io.ReaderAt, fileSize int64) (*io.SectionReader, error) {
	if fileSize < TrailerSize {
		return nil, errors.New("runtime has no embedded confinement helper")
	}
	trailer := make([]byte, TrailerSize)
	if _, err := source.ReadAt(trailer, fileSize-TrailerSize); err != nil {
		return nil, err
	}
	if string(trailer[:len(Magic)]) != Magic {
		return nil, errors.New("runtime has no embedded confinement helper")
	}
	size := binary.LittleEndian.Uint64(trailer[len(Magic):])
	if size == 0 || size > MaxBytes || int64(size) > fileSize-TrailerSize {
		return nil, errors.New("invalid embedded helper size")
	}
	start := fileSize - TrailerSize - int64(size)
	return io.NewSectionReader(source, start, int64(size)), nil
}
