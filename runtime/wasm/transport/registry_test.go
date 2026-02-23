// SPDX-License-Identifier: MPL-2.0

package transport

import "testing"

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("  WASI-HTTP  ", struct{}{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, ok := r.Get("wasi-http")
	if !ok {
		t.Fatal("Get() expected normalized key lookup to succeed")
	}
}

func TestRegistryRegisterRejectsInvalid(t *testing.T) {
	r := NewRegistry()

	if err := r.Register("", struct{}{}); err == nil {
		t.Fatal("Register() expected error for empty name")
	}
	if err := r.Register("payload", nil); err == nil {
		t.Fatal("Register() expected error for nil transport")
	}
}

func TestRegistryRegisterRejectsDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("payload", struct{}{}); err != nil {
		t.Fatalf("Register() first error = %v", err)
	}
	if err := r.Register("PAYLOAD", struct{}{}); err == nil {
		t.Fatal("Register() expected duplicate error")
	}
}
