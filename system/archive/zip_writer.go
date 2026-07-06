// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"archive/zip"
	"io"
	"strings"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

func (zipCodec) OpenWriter(w io.Writer, _ archiveapi.Options) (archiveapi.Writer, error) {
	return &zipWriter{zw: zip.NewWriter(w)}, nil
}

type zipWriter struct {
	zw *zip.Writer
}

func (z *zipWriter) Create(e archiveapi.Entry) (io.Writer, error) {
	hdr := &zip.FileHeader{Name: e.Name, Method: zip.Deflate}
	if e.Method == "store" {
		hdr.Method = zip.Store
	}
	if e.Mode != 0 {
		hdr.SetMode(e.Mode)
	}
	if !e.Modified.IsZero() {
		hdr.Modified = e.Modified
	}
	if e.IsDir && !strings.HasSuffix(hdr.Name, "/") {
		hdr.Name += "/"
	}
	return z.zw.CreateHeader(hdr)
}

func (z *zipWriter) Close() error {
	return z.zw.Close()
}
