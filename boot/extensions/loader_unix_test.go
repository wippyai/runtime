//go:build !windows

package extensions

import (
	"testing"

	"github.com/wippyai/runtime/api/boot"
	extensionapi "github.com/wippyai/runtime/api/extension"
)

func TestParsePaths_Normalize(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection("extensions", map[string]any{
		"paths": []any{"  ./a.so  ", "", " ./b.so "},
	}))

	paths, err := parsePaths(cfg.Sub("extensions"))
	if err != nil {
		t.Fatalf("parsePaths error: %v", err)
	}
	if len(paths) != 2 || paths[0] != "./a.so" || paths[1] != "./b.so" {
		t.Fatalf("unexpected paths: %#v", paths)
	}

	cfg = boot.NewConfig(boot.WithSection("extensions", map[string]any{
		"paths": "  ./single.so ",
	}))
	paths, err = parsePaths(cfg.Sub("extensions"))
	if err != nil {
		t.Fatalf("parsePaths error: %v", err)
	}
	if len(paths) != 1 || paths[0] != "./single.so" {
		t.Fatalf("unexpected paths: %#v", paths)
	}
}

func TestAppendComponents_Duplicate(t *testing.T) {
	seen := make(map[string]struct{})
	result := Result{}

	compA := boot.New(boot.P{Name: "dup"})
	compB := boot.New(boot.P{Name: "dup"})
	manifest := &extensionapi.Manifest{Components: []boot.Component{compA, compB}}

	if err := appendComponents(&result, manifest, "demo.so", seen, nil); err == nil {
		t.Fatalf("expected duplicate component error")
	}
}

func TestAppendComponents_DuplicateAcrossExtensions(t *testing.T) {
	seen := make(map[string]struct{})
	result := Result{}

	compA := boot.New(boot.P{Name: "a"})
	if err := appendComponents(&result, &extensionapi.Manifest{Components: []boot.Component{compA}}, "one.so", seen, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	compB := boot.New(boot.P{Name: "a"})
	if err := appendComponents(&result, &extensionapi.Manifest{Components: []boot.Component{compB}}, "two.so", seen, nil); err == nil {
		t.Fatalf("expected duplicate component error")
	}
}

func TestAppendComponents_SkipsNil(t *testing.T) {
	seen := make(map[string]struct{})
	result := Result{}

	comp := boot.New(boot.P{Name: "ok"})
	if err := appendComponents(&result, &extensionapi.Manifest{Components: []boot.Component{nil, comp}}, "demo.so", seen, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(result.Components))
	}
}

func TestAppendComponents_ReservedCollision(t *testing.T) {
	seen := make(map[string]struct{})
	reserved := map[string]struct{}{"reserved": {}}
	result := Result{}

	comp := boot.New(boot.P{Name: "reserved"})
	if err := appendComponents(&result, &extensionapi.Manifest{Components: []boot.Component{comp}}, "demo.so", seen, reserved); err == nil {
		t.Fatalf("expected reserved name collision error")
	}
}
