package entries

import (
	"path/filepath"
	"testing"
)

func TestCommonDotPrefix(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{name: "empty", in: nil, want: ""},
		{name: "single", in: []string{"app.test"}, want: "app.test"},
		{name: "shared prefix", in: []string{"app.test.one", "app.test.two"}, want: "app.test"},
		{name: "no shared prefix", in: []string{"app.one", "lib.two"}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commonDotPrefix(tt.in)
			if got != tt.want {
				t.Fatalf("commonDotPrefix(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveNamespaceDirs(t *testing.T) {
	root := filepath.Join("tmp", "extract")

	tests := []struct {
		name       string
		namespaces []string
		want       map[string]string
	}{
		{
			name:       "single namespace stays at root",
			namespaces: []string{"app.test"},
			want: map[string]string{
				"app.test": root,
			},
		},
		{
			name:       "shared prefix maps to suffix dirs",
			namespaces: []string{"app.test.one", "app.test.two"},
			want: map[string]string{
				"app.test.one": filepath.Join(root, "one"),
				"app.test.two": filepath.Join(root, "two"),
			},
		},
		{
			name:       "no prefix creates full namespace paths",
			namespaces: []string{"app.one", "lib.two"},
			want: map[string]string{
				"app.one": filepath.Join(root, "app", "one"),
				"lib.two": filepath.Join(root, "lib", "two"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveNamespaceDirs(root, tt.namespaces)
			if len(got) != len(tt.want) {
				t.Fatalf("resolveNamespaceDirs returned %d dirs, want %d", len(got), len(tt.want))
			}
			for ns, wantDir := range tt.want {
				if got[ns] != wantDir {
					t.Fatalf("resolveNamespaceDirs(%q) = %q, want %q", ns, got[ns], wantDir)
				}
			}
		})
	}
}
