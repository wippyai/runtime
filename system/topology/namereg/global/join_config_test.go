// SPDX-License-Identifier: MPL-2.0

package global

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultJoinConfig(t *testing.T) {
	got := DefaultJoinConfig()
	want := JoinConfig{
		Timeout:       10 * time.Second,
		MaxEntries:    65536,
		MaxBytes:      16 << 20,
		MaxConcurrent: 4,
	}
	if got != want {
		t.Fatalf("DefaultJoinConfig() = %+v, want %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("default join config must validate: %v", err)
	}
}

func TestJoinConfigValidateRejectsNonpositive(t *testing.T) {
	base := DefaultJoinConfig()
	tests := []struct {
		name string
		edit func(*JoinConfig)
	}{
		{name: "timeout zero", edit: func(c *JoinConfig) { c.Timeout = 0 }},
		{name: "timeout negative", edit: func(c *JoinConfig) { c.Timeout = -time.Second }},
		{name: "entries zero", edit: func(c *JoinConfig) { c.MaxEntries = 0 }},
		{name: "entries negative", edit: func(c *JoinConfig) { c.MaxEntries = -1 }},
		{name: "bytes zero", edit: func(c *JoinConfig) { c.MaxBytes = 0 }},
		{name: "bytes negative", edit: func(c *JoinConfig) { c.MaxBytes = -1 }},
		{name: "concurrent zero", edit: func(c *JoinConfig) { c.MaxConcurrent = 0 }},
		{name: "concurrent negative", edit: func(c *JoinConfig) { c.MaxConcurrent = -1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() unexpectedly accepted nonpositive value")
			}
		})
	}
}

func TestJoinErrorsAreDistinct(t *testing.T) {
	if ErrJoinSnapshotTooLarge == nil || ErrJoinBusy == nil {
		t.Fatal("join runtime errors must be initialized")
	}
	if errors.Is(ErrJoinSnapshotTooLarge, ErrJoinBusy) || errors.Is(ErrJoinBusy, ErrJoinSnapshotTooLarge) {
		t.Fatal("join runtime errors must remain distinct")
	}
}
