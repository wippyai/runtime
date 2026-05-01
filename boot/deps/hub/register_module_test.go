// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterModule_HappyPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/account/modules" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer wpy_test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var got RegisterModuleParams
		if err := json.Unmarshal(body, &got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got.Org != "tmp" || got.Name != "hello-world" || got.Visibility != "private" {
			http.Error(w, "unexpected fields", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"abc","org_name":"tmp","name":"hello-world","display_name":"Hello","module_type":"application","visibility":"private"}`))
	}))
	defer srv.Close()

	c, err := NewClient(Options{BaseURL: srv.URL, Token: "wpy_test"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	c.httpClient = srv.Client()

	got, err := c.RegisterModule(context.Background(), &RegisterModuleParams{
		Org: "tmp", Name: "hello-world", DisplayName: "Hello",
		ModuleType: "application", Visibility: "private",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if got.ID != "abc" || got.Visibility != "private" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestRegisterModule_AlreadyExists(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"module exists"}}`))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{BaseURL: srv.URL, Token: "wpy_test"})
	c.httpClient = srv.Client()

	_, err := c.RegisterModule(context.Background(), &RegisterModuleParams{
		Org: "tmp", Name: "hello-world", ModuleType: "application",
	})
	if !errors.Is(err, ErrModuleAlreadyExists) {
		t.Fatalf("expected ErrModuleAlreadyExists, got %v", err)
	}
}

func TestRegisterModule_BadStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"insufficient permissions"}}`))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{BaseURL: srv.URL, Token: "wpy_test"})
	c.httpClient = srv.Client()

	_, err := c.RegisterModule(context.Background(), &RegisterModuleParams{
		Org: "tmp", Name: "hello-world", ModuleType: "application",
	})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}
