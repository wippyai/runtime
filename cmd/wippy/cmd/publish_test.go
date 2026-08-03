// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/hub"
)

func TestPublishOutputPath(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantPath    string
		dryRun      bool
		wantCleanup bool
		wantError   bool
	}{
		{name: "default", wantPath: "default.wapp", wantCleanup: true},
		{name: "dry-run output", dryRun: true, output: "release.wapp", wantPath: "release.wapp"},
		{name: "upload output", output: "release.wapp", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualPath, cleanup, err := publishOutputPath(test.dryRun, test.output, "default.wapp")
			if (err != nil) != test.wantError {
				t.Fatalf("publishOutputPath() error = %v", err)
			}
			if err == nil && (actualPath != test.wantPath || cleanup != test.wantCleanup) {
				t.Fatalf("publishOutputPath() = (%q, %t), want (%q, %t)", actualPath, cleanup, test.wantPath, test.wantCleanup)
			}
		})
	}
}

func TestPublishPackedAt_SourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1749513600")
	actual, err := publishPackedAt()
	if err != nil {
		t.Fatalf("publishPackedAt() error = %v", err)
	}
	if actual != "2025-06-10T00:00:00Z" {
		t.Fatalf("publishPackedAt() = %q", actual)
	}
}

func TestPublishPackedAt_InvalidSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "invalid")
	if _, err := publishPackedAt(); err == nil {
		t.Fatal("publishPackedAt() error = nil")
	}
}

func TestPublishViaHubOrLegacy_LabelUploadKeepsVersionHeader(t *testing.T) {
	tmpDir := t.TempDir()
	wappPath := filepath.Join(tmpDir, "module.wapp")
	if err := os.WriteFile(wappPath, []byte("packed module"), 0o600); err != nil {
		t.Fatalf("write wapp: %v", err)
	}

	var gotVersion, gotLabel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/publish/upload" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		gotVersion = r.Header.Get("X-Wippy-Version")
		gotLabel = r.Header.Get("X-Wippy-Label")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"publish_id":"publish-1"}`))
	}))
	defer server.Close()

	client, err := hub.NewClient(hub.Options{
		BaseURL: server.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("new hub client: %v", err)
	}

	cfg := &config.ModuleConfig{
		Organization: "acme",
		ModuleName:   "app",
		Version:      "1.2.3",
	}
	publishID, err := publishViaHubOrLegacy(
		context.Background(),
		client,
		server.URL,
		cfg,
		wappPath,
		"latest",
		"release notes",
		false,
	)
	if err != nil {
		t.Fatalf("publishViaHubOrLegacy failed: %v", err)
	}
	if publishID != "publish-1" {
		t.Fatalf("publish id = %q, want publish-1", publishID)
	}
	if gotVersion != "1.2.3" {
		t.Fatalf("X-Wippy-Version = %q, want 1.2.3", gotVersion)
	}
	if gotLabel != "latest" {
		t.Fatalf("X-Wippy-Label = %q, want latest", gotLabel)
	}
}
