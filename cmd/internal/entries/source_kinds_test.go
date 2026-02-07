package entries

import "testing"

func TestSourceExtForKind(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want string
	}{
		{name: "exact function lua", kind: "function.lua", want: ".lua"},
		{name: "exact template jet", kind: "template.jet", want: ".jet"},
		{name: "suffix fallback lua", kind: "custom.lua", want: ".lua"},
		{name: "suffix fallback jet", kind: "custom.jet", want: ".jet"},
		{name: "unsupported kind", kind: "config.yaml", want: ""},
		{name: "no extension", kind: "config", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sourceExtForKind(tt.kind)
			if got != tt.want {
				t.Fatalf("sourceExtForKind(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
