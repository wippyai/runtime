package loader

//
//import (
//	"log"
//	"os"
//	"path/filepath"
//	"testing"
//
//	"github.com/ponyruntime/pony/api/payload"
//	"github.com/ponyruntime/pony/api/registry"
//	"go.uber.org/zap"
//)
//
//func TestFolderLoader_Boot_MultipleFiles_Sorting_Interpolation(t *testing.T) {
//	// Create a temporary directory structure for testing
//	tempDir, err := os.MkdirTemp("", "folderloader_test")
//	if err != nil {
//		t.Fatalf("failed to create temp dir: %v", err)
//	}
//	defer os.RemoveAll(tempDir)
//
//	// Create files with content (including interpolation)
//	files := map[string]string{
//		"a.yaml": `
//name: setting_a
//kind: config
//data: value_a
//`,
//		"b/b.yaml": `
//name: setting_b
//kind: config
//data: file://b_data.txt
//`,
//		"b/b_data.txt": "value_b",
//		"c/nested/c.yaml": `
//name: setting_c
//kind: config
//data: "interpolated_${from_a}"
//`,
//	}
//	for path, content := range files {
//		fullPath := filepath.Join(tempDir, path)
//		err = os.MkdirAll(filepath.Dir(fullPath), 0755) // Create directories if they don't exist
//		if err != nil {
//			t.Fatalf("failed to create dir: %v", err)
//		}
//		err = os.WriteFile(fullPath, []byte(content), 0644)
//		if err != nil {
//			t.Fatalf("failed to write file %s: %v", path, err)
//		}
//	}
//
//	// Create a transcoder (replace with your actual transcoder setup)
//	dtt := createTestTranscoder()
//
//	// Create FolderLoader with variables
//	vars := Vars{
//		"from_a": "value_from_a",
//	}
//	logger, _ := zap.NewProduction()
//	defer logger.Sync()
//
//	folderLoader := NewFolderLoader(
//		dtt,
//		logger,
//	)
//
//	// Boot the FolderLoader
//	changeSet, err := folderLoader.Boot(tempDir, vars)
//	if err != nil {
//		t.Fatalf("failed to boot folder loader: %v", err)
//	}
//
//	log.Printf("ChangeSet: %v", changeSet)
//
//	// Expected ChangeSet (sorted by path) - CORRECTED PATHS
//	expectedChangeSet := registry.ChangeSet{
//		{
//			Kind: registry.Create,
//			Entry: registry.Entry{
//				Path: "setting_a", // File in root directory
//				Kind: "config",
//				Data: payload.NewPayload(map[string]interface{}{
//					"name": "setting_a",
//					"kind": "config",
//					"data": "value_a",
//				}, payload.Yaml),
//			},
//		},
//		{
//			Kind: registry.Create,
//			Entry: registry.Entry{
//				Path: "b.setting_b", // File in 'b' subdirectory
//				Kind: "config",
//				Data: payload.NewPayload(map[string]interface{}{
//					"name": "setting_b",
//					"kind": "config",
//					"data": "value_b", // Interpolated from file
//				}, payload.Yaml),
//			},
//		},
//		{
//			Kind: registry.Create,
//			Entry: registry.Entry{
//				Path: "c.nested.setting_c", // File in 'c/nested' subdirectory
//				Kind: "config",
//				Data: payload.NewPayload(map[string]interface{}{
//					"name": "setting_c",
//					"kind": "config",
//					"data": "interpolated_value_from_a", // Variable interpolation
//				}, payload.Yaml),
//			},
//		},
//	}
//
//	// Compare ChangeSets
//	if len(changeSet) != len(expectedChangeSet) {
//		t.Fatalf("expected %d ChangeSet entries, got %d", len(expectedChangeSet), len(changeSet))
//	}
//
//	for i, op := range changeSet {
//		expectedOp := expectedChangeSet[i]
//
//		if op.Kind != expectedOp.Kind {
//			t.Errorf("expected operation Kind: %s, got: %s for path: %s", expectedOp.Kind, op.Kind, op.Entry.Path)
//		}
//
//		if op.Entry.Path != expectedOp.Entry.Path {
//			t.Errorf("expected operation Path: %s, got: %s", expectedOp.Entry.Path, op.Entry.Path)
//		}
//
//		if op.Entry.Kind != expectedOp.Entry.Kind {
//			t.Errorf("expected operation Kind: %s, got: %s for path: %s", expectedOp.Entry.Kind, op.Entry.Kind, op.Entry.Path)
//		}
//
//		// Data comparison (you might need a more robust way to compare payloads)
//		// Here, we'll just check if the interpolated values are present
//		if expectedOp.Entry.Path == "b.setting_b" { // File interpolation
//			var data map[string]interface{}
//			err = dtt.Unmarshal(op.Entry.Data, &data)
//			if err != nil {
//				t.Fatalf("failed to unmarshal payload data for path %s: %v", op.Entry.Path, err)
//			}
//
//			if data["data"] != "value_b" {
//				t.Errorf("expected data to contain 'value_b' for path %s, got: %v", op.Entry.Path, data["data"])
//			}
//		} else if expectedOp.Entry.Path == "c.nested.setting_c" { // Variable interpolation
//			var data map[string]interface{}
//			err = dtt.Unmarshal(op.Entry.Data, &data)
//			if err != nil {
//				t.Fatalf("failed to unmarshal payload data for path %s: %v", op.Entry.Path, err)
//			}
//			if data["data"] != "interpolated_value_from_a" {
//				t.Errorf("expected data to contain 'interpolated_value_from_a' for path %s, got: %v", op.Entry.Path, data["data"])
//			}
//		}
//	}
//}
