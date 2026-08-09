// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/hub"
)

// TestPublishModuleTypeFlagDefaultsEmpty guards the highest-blast-radius line in
// this change. The flag used to default to "application", which is why every
// module the CLI created was cataloged as an app. It must now default to empty
// so "not declared" is a state hub can see and reject, and so wippy.yaml's
// `type:` is not silently overridden by a flag the user never passed.
func TestPublishModuleTypeFlagDefaultsEmpty(t *testing.T) {
	t.Parallel()

	flag := publishCmd.Flags().Lookup("module-type")
	if flag == nil {
		t.Fatal("publish is missing the --module-type flag")
	}
	if flag.DefValue != "" {
		t.Errorf("--module-type default = %q, want \"\" — a non-empty default silently types every new module",
			flag.DefValue)
	}
}

// TestEnsureModuleRegistered_DefaultsAndWarnsWhenTypeUndeclared: during the
// grace period an undeclared type must not fail the publish — upgrading the CLI
// cannot break a pipeline that creates a new module. It defaults to
// "application" (what hub does too) and tells the user to declare it.
func TestEnsureModuleRegistered_DefaultsAndWarnsWhenTypeUndeclared(t *testing.T) {
	cfg := &config.ModuleConfig{Organization: "acme", ModuleName: "widgets"}

	var gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ModuleType string `json:"module_type"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotType = body.ModuleType
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"org_name": "acme", "name": "widgets", "visibility": "private", "module_type": body.ModuleType,
		})
	}))
	defer srv.Close()

	client, err := hub.NewClient(hub.Options{BaseURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatalf("NewClient(): %v", err)
	}

	if err := ensureModuleRegistered(t.Context(), client, srv.URL, cfg, "Widgets", "", "private"); err != nil {
		t.Fatalf("ensureModuleRegistered() must not fail on an undeclared type during grace: %v", err)
	}
	if gotType != "application" {
		t.Errorf("module_type sent = %q, want %q (grace-period default)", gotType, "application")
	}
}
