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
