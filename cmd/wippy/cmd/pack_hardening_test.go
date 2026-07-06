// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/boot/deps/hub"
)

type fakeHubPackDownloader struct {
	err     error
	payload []byte
	calls   int
}

func (f *fakeHubPackDownloader) DownloadToFile(_ context.Context, _ string, destPath string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, f.payload, 0o644)
}

func TestEnsureHubPackCachedUsesVerifiedCache(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "ui-1.2.3.wapp")
	payload := []byte("cached pack")
	if err := os.WriteFile(packPath, payload, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	downloader := &fakeHubPackDownloader{payload: []byte("downloaded pack")}
	err := ensureHubPackCached(context.Background(), downloader, hub.ResolvedModule{
		Version:   "1.2.3",
		URL:       "https://hub.example/download",
		Digest:    sha256Digest(payload),
		SizeBytes: uint64(len(payload)),
	}, packPath, "acme/ui", "https://hub.example")
	if err != nil {
		t.Fatalf("ensureHubPackCached: %v", err)
	}
	if downloader.calls != 0 {
		t.Fatalf("download calls = %d, want 0", downloader.calls)
	}
}

func TestEnsureHubPackCachedRedownloadsInvalidCache(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "ui-1.2.3.wapp")
	if err := os.WriteFile(packPath, []byte("corrupt cache"), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	payload := []byte("downloaded pack")
	downloader := &fakeHubPackDownloader{payload: payload}
	err := ensureHubPackCached(context.Background(), downloader, hub.ResolvedModule{
		Version:   "1.2.3",
		URL:       "https://hub.example/download",
		Digest:    sha256Digest(payload),
		SizeBytes: uint64(len(payload)),
	}, packPath, "acme/ui", "https://hub.example")
	if err != nil {
		t.Fatalf("ensureHubPackCached: %v", err)
	}
	if downloader.calls != 1 {
		t.Fatalf("download calls = %d, want 1", downloader.calls)
	}
	got, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("cache payload = %q, want %q", got, payload)
	}
}

func TestEnsureHubPackCachedRejectsInvalidDownload(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "ui-1.2.3.wapp")

	downloader := &fakeHubPackDownloader{payload: []byte("wrong pack")}
	err := ensureHubPackCached(context.Background(), downloader, hub.ResolvedModule{
		Version:   "1.2.3",
		URL:       "https://hub.example/download",
		Digest:    sha256Digest([]byte("expected pack")),
		SizeBytes: uint64(len("expected pack")),
	}, packPath, "acme/ui", "https://hub.example")
	if err == nil {
		t.Fatal("expected integrity error")
	}
	if !strings.Contains(err.Error(), "failed to verify downloaded acme/ui@1.2.3") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(packPath); !os.IsNotExist(statErr) {
		t.Fatalf("invalid download should be removed, stat err = %v", statErr)
	}
}

func TestResolvePackEmbedConfigAll(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		cmd := newTestPackCommand(t)
		if err := cmd.Flags().Set("embed-all", "true"); err != nil {
			t.Fatalf("set flag: %v", err)
		}

		cfg, err := resolvePackEmbedConfig(cmd, t.TempDir())
		if err != nil {
			t.Fatalf("resolvePackEmbedConfig: %v", err)
		}
		if !cfg.enabled() || !cfg.all || cfg.stagePatterns() != nil {
			t.Fatalf("embed config = %+v, want all with nil stage patterns", cfg)
		}
	})

	t.Run("config wildcard", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "wippy.yaml"), []byte("embed:\n  - \"**\"\n"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		cfg, err := resolvePackEmbedConfig(newTestPackCommand(t), tmpDir)
		if err != nil {
			t.Fatalf("resolvePackEmbedConfig: %v", err)
		}
		if !cfg.enabled() || !cfg.all || cfg.stagePatterns() != nil {
			t.Fatalf("embed config = %+v, want config wildcard all", cfg)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		cmd := newTestPackCommand(t)
		if err := cmd.Flags().Set("embed-all", "true"); err != nil {
			t.Fatalf("set embed-all: %v", err)
		}
		if err := cmd.Flags().Set("embed", "app:assets"); err != nil {
			t.Fatalf("set embed: %v", err)
		}

		_, err := resolvePackEmbedConfig(cmd, t.TempDir())
		if err == nil {
			t.Fatal("expected conflict error")
		}
	})
}

func newTestPackCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("embed", nil, "")
	cmd.Flags().Bool("embed-all", false, "")
	return cmd
}

func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
