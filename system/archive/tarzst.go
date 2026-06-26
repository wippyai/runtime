// SPDX-License-Identifier: MPL-2.0

package archive

import (
	"io"

	"github.com/klauspost/compress/zstd"
	archiveapi "github.com/wippyai/runtime/api/archive"
)

func init() {
	archiveapi.Register(tarZstCodec{})
}

type tarZstCodec struct{}

func (tarZstCodec) Name() string { return "tar.zst" }

func (tarZstCodec) Extensions() []string { return []string{".tar.zst", ".tzst"} }

func (tarZstCodec) Sniff(h []byte) bool {
	return len(h) >= 4 && h[0] == 0x28 && h[1] == 0xb5 && h[2] == 0x2f && h[3] == 0xfd
}

func (tarZstCodec) OpenStream(r io.Reader, o archiveapi.Options) (archiveapi.Walker, error) {
	zr, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	tw, err := tarCodec{}.OpenStream(zr.IOReadCloser(), o)
	if err != nil {
		zr.Close()
		return nil, err
	}
	tw.(*tarWalker).closer = zstdDecoderCloser{zr}
	return tw, nil
}

func (tarZstCodec) OpenWriter(w io.Writer, o archiveapi.Options) (archiveapi.Writer, error) {
	zw, err := zstd.NewWriter(w)
	if err != nil {
		return nil, err
	}
	tw, err := tarCodec{}.OpenWriter(zw, o)
	if err != nil {
		_ = zw.Close()
		return nil, err
	}
	tw.(*tarWriter).extra = zw
	return tw, nil
}

type zstdDecoderCloser struct {
	d *zstd.Decoder
}

func (c zstdDecoderCloser) Close() error {
	c.d.Close()
	return nil
}
