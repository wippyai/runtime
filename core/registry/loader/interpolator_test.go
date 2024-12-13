package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/core/payload"

	"github.com/ponyruntime/pony/core/payload/json"
	"github.com/ponyruntime/pony/core/payload/yaml"
)

// createTestTranscoder registers JSON and YAML transcoders for testing.
func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()

	// Register JSON
	tr.RegisterTranscoder(payload.Json, payload.Golang, 1, &json.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.Json, 1, &json.FromGolang{})
	tr.RegisterUnmarshaler(payload.Json, &json.ToGolang{})

	// Register YAML
	tr.RegisterTranscoder(payload.Yaml, payload.Golang, 1, &yaml.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.Yaml, 1, &yaml.FromGolang{})
	tr.RegisterUnmarshaler(payload.Yaml, &yaml.ToGolang{})

	return tr
}

func TestInterpolator_Interpolate_NoReplacements(t *testing.T) {
	i := NewInterpolator(nil, nil)
	p := payload.NewPayload(map[string]string{"key": "value"}, payload.Json)
	dtt := createTestTranscoder()
	result, err := i.Interpolate(p, dtt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Compare formats and unmarshaled data
	if result.Format() != p.Format() {
		t.Errorf("expected format: %s, got: %s", p.Format(), result.Format())
	}
	var resultData, pData interface{}
	dtt.Unmarshal(result, &resultData)
	dtt.Unmarshal(p, &pData)
	if !reflect.DeepEqual(resultData, pData) {
		t.Errorf("expected data: %v, got: %v", pData, resultData)
	}
}

func TestInterpolator_Interpolate_Variables(t *testing.T) {
	vars := Variables{
		"NAME": "John",
	}
	i := NewInterpolator(vars, nil)
	p := payload.NewPayload(map[string]interface{}{"name": "${NAME}", "city": "London"}, payload.Golang)
	dtt := createTestTranscoder()
	result, err := i.Interpolate(p, dtt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expected payload after interpolation
	expected := payload.NewPayload(map[string]interface{}{"name": "John", "city": "London"}, payload.Golang)

	// Compare formats
	if result.Format() != expected.Format() {
		t.Errorf("expected format: %s, got: %s", expected.Format(), result.Format())
	}

	// Compare unmarshaled data
	var resultData, expectedData interface{}
	dtt.Unmarshal(result, &resultData)
	dtt.Unmarshal(expected, &expectedData)
	if !reflect.DeepEqual(resultData, expectedData) {
		t.Errorf("expected data: %v, got: %v", expectedData, resultData)
	}
}

func TestInterpolator_Interpolate_File(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "interpolator_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFilePath := filepath.Join(tempDir, "test.txt")
	testFileContent := "This is the file content."
	err = os.WriteFile(testFilePath, []byte(testFileContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	fileReadFunc := func(path string) (string, error) {
		// Prepend tempDir to path for testing
		fullPath := filepath.Join(tempDir, path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return string(data), nil
	}

	i := NewInterpolator(nil, fileReadFunc)
	p := payload.NewPayload(map[string]interface{}{"content": "file://test.txt"}, payload.Golang)
	dtt := createTestTranscoder()

	result, err := i.Interpolate(p, dtt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected payload after interpolation
	expected := payload.NewPayload(map[string]interface{}{"content": testFileContent}, payload.Golang)

	// Compare formats
	if result.Format() != expected.Format() {
		t.Errorf("expected format: %s, got: %s", expected.Format(), result.Format())
	}

	// Compare unmarshaled data
	var resultData, expectedData interface{}
	dtt.Unmarshal(result, &resultData)
	dtt.Unmarshal(expected, &expectedData)
	if !reflect.DeepEqual(resultData, expectedData) {
		t.Errorf("expected data: %v, got: %v", expectedData, resultData)
	}
}

func TestInterpolator_Interpolate_File_Missing(t *testing.T) {
	fileReadFunc := func(path string) (string, error) {
		return "", fmt.Errorf("file not found: %s", path)
	}
	i := NewInterpolator(nil, fileReadFunc)
	p := payload.NewPayload(map[string]interface{}{"content": "file://missing.txt"}, payload.Golang)
	dtt := createTestTranscoder()

	_, err := i.Interpolate(p, dtt)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "file not found: missing.txt") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInterpolator_Interpolate_FileInterpolationDisabled(t *testing.T) {
	i := NewInterpolator(nil, nil)
	p := payload.NewPayload(`{"content": "file://test.txt"}`, payload.Json)

	dtt := createTestTranscoder()
	result, err := i.Interpolate(p, dtt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Compare formats and unmarshaled data
	if result.Format() != p.Format() {
		t.Errorf("expected format: %s, got: %s", p.Format(), result.Format())
	}
	var resultData, pData interface{}
	dtt.Unmarshal(result, &resultData)
	dtt.Unmarshal(p, &pData)
	if !reflect.DeepEqual(resultData, pData) {
		t.Errorf("expected data: %v, got: %v", pData, resultData)
	}
}
