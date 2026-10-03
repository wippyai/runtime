// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"testing"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

func TestEvalMaxSteps(t *testing.T) {
	tests := []struct {
		value   any
		name    string
		want    uint64
		wantErr bool
	}{
		{name: "omitted", want: evalhost.DefaultMaxSteps},
		{name: "explicit unlimited", value: 0, want: 0},
		{name: "positive", value: 25000, want: 25000},
		{name: "negative", value: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]any{}
			if tt.value != nil {
				values["eval.max_steps"] = tt.value
			}
			cfg := boot.NewConfig(boot.WithSection("lua", values)).Sub("lua")
			got, err := evalMaxSteps(cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("evalMaxSteps returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("evalMaxSteps = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResolveEvalSettings(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got, err := resolveEvalSettings(nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.programCacheSize != evalhost.DefaultEvalProgramCacheSize || got.detachedLifetime != evalhost.DefaultDetachedEvalLifetime {
			t.Fatalf("unexpected defaults: %+v", got)
		}
	})

	t.Run("configured", func(t *testing.T) {
		cfg := boot.NewConfig(boot.WithSection("lua", map[string]any{
			"eval.program_cache_size": 7,
			"eval.spawn_host":         "app:eval",
		})).Sub("lua")
		got, err := resolveEvalSettings(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got.programCacheSize != 7 || got.spawnHost != "app:eval" {
			t.Fatalf("unexpected settings: %+v", got)
		}
	})

	rejected := map[string][]any{
		"eval.program_cache_size": {0, -1},
		"eval.detached_lifetime":  {"0s", "-1s"},
	}
	for key, values := range rejected {
		for _, value := range values {
			cfg := boot.NewConfig(boot.WithSection("lua", map[string]any{key: value})).Sub("lua")
			if _, err := resolveEvalSettings(cfg); err == nil {
				t.Fatalf("%s = %v is accepted", key, value)
			}
		}
	}
}
