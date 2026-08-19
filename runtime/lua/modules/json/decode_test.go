// SPDX-License-Identifier: MPL-2.0

package json

import (
	"math"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestDecodeNumberRepresentation(t *testing.T) {
	tests := []struct {
		check func(t *testing.T, value lua.LValue)
		name  string
		input string
	}{
		{
			name:  "maximum integer",
			input: "9223372036854775807",
			check: func(t *testing.T, value lua.LValue) {
				t.Helper()
				if value != lua.LInteger(math.MaxInt64) {
					t.Fatalf("got %v (%T), want maximum LInteger", value, value)
				}
			},
		},
		{
			name:  "minimum integer",
			input: "-9223372036854775808",
			check: func(t *testing.T, value lua.LValue) {
				t.Helper()
				if value != lua.LInteger(math.MinInt64) {
					t.Fatalf("got %v (%T), want minimum LInteger", value, value)
				}
			},
		},
		{
			name:  "integer beyond int64",
			input: "9223372036854775808",
			check: func(t *testing.T, value lua.LValue) {
				t.Helper()
				if _, ok := value.(lua.LNumber); !ok {
					t.Fatalf("got %v (%T), want LNumber fallback", value, value)
				}
			},
		},
		{
			name:  "fraction",
			input: "1.25",
			check: func(t *testing.T, value lua.LValue) {
				t.Helper()
				if value != lua.LNumber(1.25) {
					t.Fatalf("got %v (%T), want LNumber(1.25)", value, value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := Decode([]byte(tt.input))
			if err != nil {
				t.Fatalf("Decode(%q): %v", tt.input, err)
			}
			tt.check(t, value)
		})
	}
}

func TestDecodeRejectsUnrepresentableNumber(t *testing.T) {
	if _, err := Decode([]byte("1e400")); err == nil {
		t.Fatal("expected an error for a number outside float64 range")
	}
}
