// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"go.uber.org/zap"
)

func BenchmarkLoadReplacementEntries(b *testing.B) {
	for _, layout := range []string{"src", "flat"} {
		b.Run(layout, func(b *testing.B) {
			root := b.TempDir()
			source := root
			if layout == "src" {
				source = filepath.Join(root, "src")
			}
			if err := os.MkdirAll(source, 0755); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 32; i++ {
				data := fmt.Sprintf(`{"namespace":"app","entries":[{"name":"entry%d","kind":"registry.entry","value":"live"}]}`, i)
				if err := os.WriteFile(filepath.Join(source, fmt.Sprintf("entry%d.json", i)), []byte(data), 0600); err != nil {
					b.Fatal(err)
				}
			}
			scratch := filepath.Join(root, "scratch")
			if err := os.MkdirAll(scratch, 0755); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 256; i++ {
				if err := os.WriteFile(filepath.Join(scratch, fmt.Sprintf("file%d.txt", i)), []byte("unrelated build artifact"), 0600); err != nil {
					b.Fatal(err)
				}
			}
			ctx := newTestContext()
			transcoder, logger := payload.GetTranscoder(ctx), zap.NewNop()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				entries, err := loadReplacementEntries(ctx, root, logger, transcoder)
				if err != nil || len(entries) != 32 {
					b.Fatalf("load: %d entries, %v", len(entries), err)
				}
			}
		})
	}
}
