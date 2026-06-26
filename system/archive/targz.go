// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"compress/gzip"
	"io"

	archiveapi "github.com/wippyai/runtime/api/archive"
)

func init() {
	archiveapi.Register(tarGzCodec{})
}

type tarGzCodec struct{}

func (tarGzCodec) Name() string { return "tar.gz" }

func (tarGzCodec) Extensions() []string { return []string{".tar.gz", ".tgz"} }

func (tarGzCodec) Sniff(h []byte) bool {
	return len(h) >= 2 && h[0] == 0x1f && h[1] == 0x8b
}

func (tarGzCodec) OpenStream(r io.Reader, o archiveapi.Options) (archiveapi.Walker, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	tw, err := tarCodec{}.OpenStream(gz, o)
	if err != nil {
		_ = gz.Close()
		return nil, err
	}
	tw.(*tarWalker).closer = gz
	return tw, nil
}

func (tarGzCodec) OpenWriter(w io.Writer, o archiveapi.Options) (archiveapi.Writer, error) {
	gw := gzip.NewWriter(w)
	tw, err := tarCodec{}.OpenWriter(gw, o)
	if err != nil {
		_ = gw.Close()
		return nil, err
	}
	tw.(*tarWriter).extra = gw
	return tw, nil
}
