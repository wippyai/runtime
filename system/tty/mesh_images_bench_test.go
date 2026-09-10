// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"testing"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func BenchmarkMeshImageCaptureWarm(b *testing.B) {
	f := imageMesh(b, true, true)
	capture, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
	require.NoError(b, err)
	require.NoError(b, capture.Close())
	chunks := f.tb.imageChunks.Load()
	before := f.ta.wireBytes.Load() + f.tb.wireBytes.Load()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		capture, err := f.remote.(ttyapi.CaptureViewport).Capture(f.agent)
		if err != nil {
			b.Fatal(err)
		}
		_ = capture.Close()
	}
	b.StopTimer()
	require.Equal(b, chunks, f.tb.imageChunks.Load())
	b.ReportMetric(float64(f.ta.wireBytes.Load()+f.tb.wireBytes.Load()-before)/float64(b.N), "wire_bytes/capture")
	b.ReportMetric(0, "image_bytes/capture")
}
