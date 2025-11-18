package pack

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func BenchmarkPackEntries(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	benchmarks := []struct {
		name  string
		count int
	}{
		{"10_entries", 10},
		{"100_entries", 100},
		{"1000_entries", 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			entries := make([]registry.Entry, bm.count)
			for i := 0; i < bm.count; i++ {
				entries[i] = registry.Entry{
					ID:   registry.ParseID("test:entry"),
					Kind: "test.kind",
					Meta: registry.Metadata{"index": i},
					Data: payload.New(map[string]any{
						"value": i,
						"data":  "benchmark data content",
					}),
				}
			}

			metadata := registry.Metadata{"benchmark": "true"}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var buf bytes.Buffer
				err := pw.PackEntries(metadata, entries, &buf)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPackFilesystem(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	benchmarks := []struct {
		name      string
		fileCount int
		fileSize  int
	}{
		{"small_10files_1KB", 10, 1024},
		{"medium_50files_10KB", 50, 10 * 1024},
		{"large_100files_100KB", 100, 100 * 1024},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			tmpDir := b.TempDir()

			// Create test files
			content := make([]byte, bm.fileSize)
			for i := 0; i < bm.fileSize; i++ {
				content[i] = byte(i % 256)
			}

			for i := 0; i < bm.fileCount; i++ {
				filename := filepath.Join(tmpDir, "file"+string(rune('0'+(i%10)))+".png")
				if err := os.WriteFile(filename, content, 0600); err != nil {
					b.Fatal(err)
				}
			}

			fsys := os.DirFS(tmpDir)
			metadata := registry.Metadata{}
			entries := []registry.Entry{}
			resourceID := registry.ParseID("bench:fs")
			resourceMeta := registry.Metadata{}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var buf bytes.Buffer
				err := pw.Pack(metadata, entries, fsys, resourceID, resourceMeta, &buf)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReadEntries(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	benchmarks := []struct {
		name  string
		count int
	}{
		{"10_entries", 10},
		{"100_entries", 100},
		{"1000_entries", 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			entries := make([]registry.Entry, bm.count)
			for i := 0; i < bm.count; i++ {
				entries[i] = registry.Entry{
					ID:   registry.ParseID("test:entry"),
					Kind: "test.kind",
					Meta: registry.Metadata{"index": i},
					Data: payload.New(map[string]any{
						"value": i,
						"data":  "benchmark data content",
					}),
				}
			}

			var buf bytes.Buffer
			err := pw.PackEntries(registry.Metadata{}, entries, &buf)
			if err != nil {
				b.Fatal(err)
			}

			packData := buf.Bytes()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				reader := bytes.NewReader(packData)
				pr, err := NewReader(reader, transcoder)
				if err != nil {
					b.Fatal(err)
				}

				_, err = pr.GetEntries()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReadFiles(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	benchmarks := []struct {
		name     string
		fileSize int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			tmpDir := b.TempDir()

			content := make([]byte, bm.fileSize)
			for i := 0; i < bm.fileSize; i++ {
				content[i] = byte(i % 256)
			}

			filename := filepath.Join(tmpDir, "test.png")
			if err := os.WriteFile(filename, content, 0600); err != nil {
				b.Fatal(err)
			}

			fsys := os.DirFS(tmpDir)

			var buf bytes.Buffer
			err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
				registry.ParseID("bench:file"), registry.Metadata{}, &buf)
			if err != nil {
				b.Fatal(err)
			}

			packData := buf.Bytes()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				reader := bytes.NewReader(packData)
				pr, err := NewReader(reader, transcoder)
				if err != nil {
					b.Fatal(err)
				}

				packFS, err := pr.GetFS(registry.ParseID("bench:file"))
				if err != nil {
					b.Fatal(err)
				}

				file, err := packFS.Open("test.png")
				if err != nil {
					b.Fatal(err)
				}

				_, err = io.ReadAll(file)
				file.Close()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCompression(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	b.Run("text_files", func(b *testing.B) {
		tmpDir := b.TempDir()

		// Create compressible text file
		textContent := make([]byte, 100*1024)
		for i := 0; i < len(textContent); i++ {
			textContent[i] = byte('a' + (i % 26))
		}

		filename := filepath.Join(tmpDir, "text.png")
		if err := os.WriteFile(filename, textContent, 0600); err != nil {
			b.Fatal(err)
		}

		fsys := os.DirFS(tmpDir)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
				registry.ParseID("bench:text"), registry.Metadata{}, &buf)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("binary_files", func(b *testing.B) {
		tmpDir := b.TempDir()

		// Create non-compressible binary file (image)
		binaryContent := make([]byte, 100*1024)
		for i := 0; i < len(binaryContent); i++ {
			binaryContent[i] = byte(i % 256)
		}

		filename := filepath.Join(tmpDir, "binary.png")
		if err := os.WriteFile(filename, binaryContent, 0600); err != nil {
			b.Fatal(err)
		}

		fsys := os.DirFS(tmpDir)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
				registry.ParseID("bench:binary"), registry.Metadata{}, &buf)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("mixed_files", func(b *testing.B) {
		tmpDir := b.TempDir()

		// Create mix of text and binary files
		textContent := make([]byte, 50*1024)
		for i := 0; i < len(textContent); i++ {
			textContent[i] = byte('a' + (i % 26))
		}

		binaryContent := make([]byte, 50*1024)
		for i := 0; i < len(binaryContent); i++ {
			binaryContent[i] = byte(i % 256)
		}

		if err := os.WriteFile(filepath.Join(tmpDir, "text.png"), textContent, 0600); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "binary.png"), binaryContent, 0600); err != nil {
			b.Fatal(err)
		}

		fsys := os.DirFS(tmpDir)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var buf bytes.Buffer
			err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
				registry.ParseID("bench:mixed"), registry.Metadata{}, &buf)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkReaderInit(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	entries := make([]registry.Entry, 100)
	for i := 0; i < 100; i++ {
		entries[i] = registry.Entry{
			ID:   registry.ParseID("test:entry"),
			Kind: "test.kind",
			Data: payload.New(map[string]any{"value": i}),
		}
	}

	var buf bytes.Buffer
	err := pw.PackEntries(registry.Metadata{}, entries, &buf)
	if err != nil {
		b.Fatal(err)
	}

	packData := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(packData)
		_, err := NewReader(reader, transcoder)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetMetadata(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	metadata := registry.Metadata{
		"version": "1.0.0",
		"name":    "benchmark",
		"extra":   "metadata content",
	}

	var buf bytes.Buffer
	err := pw.PackEntries(metadata, []registry.Entry{}, &buf)
	if err != nil {
		b.Fatal(err)
	}

	packData := buf.Bytes()
	reader := bytes.NewReader(packData)
	pr, err := NewReader(reader, transcoder)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pr.GetMetadata()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadDir(b *testing.B) {
	transcoder := systempayload.NewTranscoder()
	pw := NewWriter(transcoder)

	tmpDir := b.TempDir()

	// Create directory with many files
	for i := 0; i < 100; i++ {
		filename := filepath.Join(tmpDir, "file"+string(rune('0'+(i%10)))+".png")
		if err := os.WriteFile(filename, []byte("content"), 0600); err != nil {
			b.Fatal(err)
		}
	}

	fsys := os.DirFS(tmpDir)

	var buf bytes.Buffer
	err := pw.Pack(registry.Metadata{}, []registry.Entry{}, fsys,
		registry.ParseID("bench:readdir"), registry.Metadata{}, &buf)
	if err != nil {
		b.Fatal(err)
	}

	packData := buf.Bytes()
	reader := bytes.NewReader(packData)
	pr, err := NewReader(reader, transcoder)
	if err != nil {
		b.Fatal(err)
	}

	packFS, err := pr.GetFS(registry.ParseID("bench:readdir"))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := packFS.ReadDir(".")
		if err != nil {
			b.Fatal(err)
		}
	}
}
