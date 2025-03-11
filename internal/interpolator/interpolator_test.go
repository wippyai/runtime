package interpolator

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInterpolator_Interpolate_NoReplacements(t *testing.T) {
	i := NewInterpolator() // No replacers
	data := map[string]interface{}{"key": "value", "num": 123}

	result, err := i.Interpolate(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(result, data) {
		t.Errorf("expected data: %v, got: %v", data, result)
	}
}

func TestInterpolator_Interpolate_StringReplacement_Simple(t *testing.T) {
	replaceFunc := func(s string, _ any) (string, error) {
		if s == "test" {
			return "replaced", nil
		}
		return s, nil
	}

	i := NewInterpolator(replaceFunc)
	data := "test"

	result, err := i.Interpolate(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "replaced" {
		t.Errorf("expected string to be replaced, got: %v", result)
	}
}

func TestInterpolator_Interpolate_StringReplacement_Nested(t *testing.T) {
	replaceFunc := func(s string, _ any) (string, error) {
		if s == "test" {
			return "replaced", nil
		}
		return s, nil
	}

	i := NewInterpolator(replaceFunc)

	data := map[string]interface{}{
		"a": "not replaced",
		"b": map[string]interface{}{
			"c": []string{"test", "not replaced"},
			"d": "test",
		},
		"e": []interface{}{
			map[string]string{"f": "test"},
			"test",
		},
		"g": struct {
			H string
		}{
			H: "test",
		},
	}

	result, err := i.Interpolate(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]interface{}{
		"a": "not replaced",
		"b": map[string]interface{}{
			"c": []string{"replaced", "not replaced"},
			"d": "replaced",
		},
		"e": []interface{}{
			map[string]string{"f": "replaced"},
			"replaced",
		},
		"g": struct {
			H string
		}{
			H: "replaced",
		},
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected data: %v, got: %v", expected, result)
	}
}

func TestInterpolator_Interpolate_MultipleReplacers(t *testing.T) {
	replaceFunc1 := func(s string, _ any) (string, error) {
		if s == "test" {
			return "replaced1", nil
		}
		return s, nil
	}
	replaceFunc2 := func(s string, _ any) (string, error) {
		if s == "replaced1" {
			return "replaced2", nil
		}
		return s, nil
	}
	i := NewInterpolator(replaceFunc1, replaceFunc2)
	data := []string{"test", "other"}

	result, err := i.Interpolate(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []string{"replaced2", "other"}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected data: %v, got: %v", expected, result)
	}
}

func TestInterpolator_Interpolate_ReplacerWithContext(t *testing.T) {
	type MyContext struct {
		Prefix string
	}
	replaceFunc := func(s string, ctx interface{}) (string, error) {
		if c, ok := ctx.(MyContext); ok && s == "test" {
			return c.Prefix + s, nil
		}
		return s, nil
	}
	i := NewInterpolator(replaceFunc)
	data := "test"
	ctx := MyContext{Prefix: "prefix-"}

	result, err := i.Interpolate(data, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "prefix-test" {
		t.Errorf("expected result: 'prefix-test', got: '%v'", result)
	}
}

func TestInterpolator_Interpolate_NestedStructuresWithContext(t *testing.T) {
	type MyContext struct {
		Prefix string
	}
	replaceFunc := func(s string, ctx interface{}) (string, error) {
		if c, ok := ctx.(MyContext); ok {
			if s == "test" {
				return c.Prefix + s, nil
			}
		}
		return s, nil
	}

	i := NewInterpolator(replaceFunc)
	data := map[string]interface{}{
		"a": "not replaced",
		"b": map[string]interface{}{
			"c": []string{"test", "not replaced"},
			"d": "test",
		},
		"e": []interface{}{
			map[string]string{"f": "test"},
			"test",
		},
		"g": struct {
			H string
		}{
			H: "test",
		},
	}
	ctx := MyContext{Prefix: "prefix-"}

	result, err := i.Interpolate(data, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]interface{}{
		"a": "not replaced",
		"b": map[string]interface{}{
			"c": []string{"prefix-test", "not replaced"},
			"d": "prefix-test",
		},
		"e": []interface{}{
			map[string]string{"f": "prefix-test"},
			"prefix-test",
		},
		"g": struct {
			H string
		}{
			H: "prefix-test",
		},
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected data: %v, got: %v", expected, result)
	}
}

func TestInterpolator_Interpolate_NilData(t *testing.T) {
	i := NewInterpolator() // No replacers
	result, err := i.Interpolate(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected result to be nil, got: %v", result)
	}
}

func TestInterpolator_Interpolate_NilPointer(t *testing.T) {
	i := NewInterpolator()
	var data *map[string]string
	result, err := i.Interpolate(data, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.ValueOf(result).IsNil() {
		t.Errorf("expected a nil pointer back but got: %v", result)
	}
}

func TestInterpolator_Interpolate_EmptyStructures(t *testing.T) {
	i := NewInterpolator()
	data := map[string]interface{}{
		"map":   map[string]string{},
		"slice": []string{},
		"struct": struct {
			Field string
		}{},
	}

	result, err := i.Interpolate(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(result, data) {
		t.Errorf("expected result same as input, got: %v", result)
	}
}

func TestInterpolator_Interpolate_ReplacerErrorHandling(t *testing.T) {
	replaceFunc := func(s string, _ any) (string, error) {
		if s == "test" {
			return "", fmt.Errorf("test error")
		}
		return s, nil
	}
	i := NewInterpolator(replaceFunc)
	data := "test"
	_, err := i.Interpolate(data, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "test error") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInterpolator_Interpolate_File_Replacement(t *testing.T) {
	// Spawn a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "interpolator_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Spawn a test file
	testFilePath := filepath.Join(tempDir, "test.txt")
	testFileContent := "This is the file content."
	err = os.WriteFile(testFilePath, []byte(testFileContent), 0600)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	fileReadFunc := func(path string, _ any) (string, error) {
		// Prepend tempDir to path for testing
		fullPath := filepath.Join(tempDir, path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return string(data), nil
	}

	fileReplacer := func(s string, ctx interface{}) (string, error) {
		if s == "test.txt" {
			return fileReadFunc(s, ctx)
		}
		return s, nil
	}

	i := NewInterpolator(fileReplacer)
	p := map[string]interface{}{"content": "test.txt"}

	result, err := i.Interpolate(p, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected payload after interpolation
	expected := map[string]interface{}{"content": testFileContent}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected data: %v, got: %v", expected, result)
	}
}

func TestInterpolator_Interpolate_File_Missing(t *testing.T) {
	fileReadFunc := func(path string, _ any) (string, error) {
		return "", fmt.Errorf("file not found: %s", path)
	}
	fileReplacer := func(s string, ctx interface{}) (string, error) {
		if s == "missing.txt" {
			return fileReadFunc(s, ctx)
		}
		return s, nil
	}
	i := NewInterpolator(fileReplacer)
	p := map[string]interface{}{"content": "missing.txt"}

	_, err := i.Interpolate(p, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "file not found: missing.txt") {
		t.Errorf("unexpected error message: %v", err)
	}
}
