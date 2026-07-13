// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
)

// wappFixture writes a throwaway .wapp payload and returns its path.
func wappFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "demo.wapp")
	if err := os.WriteFile(path, []byte("wapp-bytes"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// publishViaHubCapturingHeaders runs PublishViaHub against a stub hub and
// returns the headers the client actually put on the wire.
func publishViaHubCapturingHeaders(t *testing.T, in UploadInput) http.Header {
	t.Helper()

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"publish_id":"wf-1"}`))
	}))
	defer srv.Close()

	client, err := NewClient(Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatalf("NewClient(): %v", err)
	}

	in.FilePath = wappFixture(t)
	if _, err := client.PublishViaHub(t.Context(), in); err != nil {
		t.Fatalf("PublishViaHub(): %v", err)
	}
	return got
}

// TestPublishViaHub_SendsModuleTypeHeader proves the declared type actually
// leaves the CLI. This is the hub-mediated path, which is the one `wippy
// publish` prefers — if the header is dropped here, hub can never learn the
// type and every new module silently falls back to the old behavior.
func TestPublishViaHub_SendsModuleTypeHeader(t *testing.T) {
	t.Parallel()

	got := publishViaHubCapturingHeaders(t, UploadInput{
		Org:        "acme",
		Module:     "widgets",
		Version:    "1.0.0",
		ModuleType: "plugin",
	})

	if h := got.Get("X-Wippy-Module-Type"); h != "plugin" {
		t.Errorf("X-Wippy-Module-Type = %q, want %q", h, "plugin")
	}
}

// TestPublishViaHub_OmitsModuleTypeHeaderWhenUndeclared: no declared type means
// no header at all, so hub keeps the module's existing type rather than being
// told to reclassify it to "".
func TestPublishViaHub_OmitsModuleTypeHeaderWhenUndeclared(t *testing.T) {
	t.Parallel()

	got := publishViaHubCapturingHeaders(t, UploadInput{
		Org:     "acme",
		Module:  "widgets",
		Version: "1.0.0",
	})

	if _, present := got["X-Wippy-Module-Type"]; present {
		t.Errorf("X-Wippy-Module-Type must be absent when no type is declared, got %q",
			got.Get("X-Wippy-Module-Type"))
	}
}

// TestModuleTypeToProto covers the legacy Connect path's mapping. The empty ->
// UNSPECIFIED case is the contract: hub distinguishes "not declared" from
// "library", and collapsing them here would reclassify every module published
// by a client that declares no type.
func TestModuleTypeToProto(t *testing.T) {
	t.Parallel()

	cases := map[string]modulev1.ModuleType{
		"":            modulev1.ModuleType_MODULE_TYPE_UNSPECIFIED,
		"library":     modulev1.ModuleType_MODULE_TYPE_LIBRARY,
		"application": modulev1.ModuleType_MODULE_TYPE_APPLICATION,
		"agent":       modulev1.ModuleType_MODULE_TYPE_AGENT,
		"plugin":      modulev1.ModuleType_MODULE_TYPE_PLUGIN,
		// "app" is the badge label, not a type — must not smuggle through.
		"app": modulev1.ModuleType_MODULE_TYPE_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := moduleTypeToProto(in); got != want {
			t.Errorf("moduleTypeToProto(%q) = %v, want %v", in, got, want)
		}
	}
}
