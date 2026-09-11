// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidDataEnvironmentDoesNotApplyOtherBindings(t *testing.T) {
	const name = "WIPPY_APP_TEST_VALID_BINDING"
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"../outside", "", "data\x00file"} {
		err := configureDataEnvironment(t.TempDir(), map[string]string{
			name: "data", "WIPPY_APP_TEST_Z_INVALID_BINDING": invalid,
		})
		if err == nil {
			t.Fatalf("accepted invalid path %q", invalid)
		}
		if _, found := os.LookupEnv(name); found {
			t.Fatal("invalid configuration partially changed the environment")
		}
	}
}

func TestDataEnvironmentPreservesExplicitEmptyOverride(t *testing.T) {
	const name = "WIPPY_APP_TEST_EMPTY_OVERRIDE"
	t.Setenv(name, "")
	if err := configureDataEnvironment(t.TempDir(), map[string]string{name: "data"}); err != nil {
		t.Fatal(err)
	}
	if value, found := os.LookupEnv(name); !found || value != "" {
		t.Fatalf("explicit override changed: %q, %v", value, found)
	}
}

func TestCanceledOwnerDoesNotCreateStateDirectory(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	state := filepath.Join(t.TempDir(), "not-created")
	err := runApplication(ctx, Options{}, state, false, "", nil, OwnerOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled invocation created state: %v", err)
	}
}
