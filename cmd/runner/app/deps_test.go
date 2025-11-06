package app

import (
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/deps"
	"go.uber.org/zap"
)

func TestDependencyManager_prepareExcludeDirs(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name       string
		folderPath string
		srcDir     string
		modulesDir string
		lockPath   string
		lockFile   *deps.LockFile
		want       []string
	}{
		{
			name:       "no exclusions",
			folderPath: "/app",
			srcDir:     ".",
			modulesDir: "",
			lockPath:   "/app/wippy.lock",
			lockFile:   nil,
			want:       []string{},
		},
		{
			name:       "exclude modules directory inside src",
			folderPath: "/app",
			srcDir:     ".",
			modulesDir: ".wippy",
			lockPath:   "/app/wippy.lock",
			lockFile:   nil,
			want:       []string{".wippy"},
		},
		{
			name:       "modules outside src directory",
			folderPath: "/app",
			srcDir:     "src",
			modulesDir: ".wippy",
			lockPath:   "/app/wippy.lock",
			lockFile:   nil,
			want:       []string{},
		},
		{
			name:       "exclude replacements inside src",
			folderPath: "/app",
			srcDir:     ".",
			modulesDir: ".wippy",
			lockPath:   "/app/wippy.lock",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module1", To: "./local/module1"},
					{From: "test/module2", To: "./local/module2"},
				},
			},
			want: []string{".wippy", "local/module1", "local/module2"},
		},
		{
			name:       "replacements outside src directory",
			folderPath: "/app",
			srcDir:     "src",
			modulesDir: "src/.wippy",
			lockPath:   "/app/wippy.lock",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module1", To: "./local/module1"},
				},
			},
			want: []string{".wippy"},
		},
		{
			name:       "complex nested structure",
			folderPath: "/home/user/project",
			srcDir:     "app/src",
			modulesDir: "app/src/.wippy",
			lockPath:   "/home/user/project/app/wippy.lock",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module1", To: "./src/local/module1"},
					{From: "test/module2", To: "./replacements/module2"},
				},
			},
			want: []string{".wippy", "local/module1"},
		},
		{
			name:       "src dir with dot",
			folderPath: "/app",
			srcDir:     ".",
			modulesDir: ".wippy",
			lockPath:   "/app/wippy.lock",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module", To: "./replacements/test"},
				},
			},
			want: []string{".wippy", "replacements/test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := &DependencyManager{
				folderPath: tt.folderPath,
				lockFile:   "wippy.lock",
				logger:     logger,
			}

			got := dm.prepareExcludeDirs(tt.srcDir, tt.modulesDir, tt.lockFile, tt.lockPath)

			if len(got) != len(tt.want) {
				t.Errorf("prepareExcludeDirs() got %d items, want %d items\ngot: %v\nwant: %v",
					len(got), len(tt.want), got, tt.want)
				return
			}

			// Convert to map for easier comparison
			gotMap := make(map[string]bool)
			for _, item := range got {
				gotMap[filepath.Clean(item)] = true
			}

			for _, wantItem := range tt.want {
				cleanWant := filepath.Clean(wantItem)
				if !gotMap[cleanWant] {
					t.Errorf("prepareExcludeDirs() missing expected item: %s\ngot: %v\nwant: %v",
						wantItem, got, tt.want)
				}
			}
		})
	}
}

func TestDependencyManager_prepareExcludeDirs_WithTrailingSlashes(t *testing.T) {
	logger := zap.NewNop()
	dm := &DependencyManager{
		folderPath: "/app/",
		lockFile:   "wippy.lock",
		logger:     logger,
	}

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{
			{From: "test/module", To: "./replacements/test/"},
		},
	}

	got := dm.prepareExcludeDirs(".", ".wippy/", lockFile, "/app/wippy.lock")

	// Should still work correctly with trailing slashes
	if len(got) != 2 {
		t.Errorf("prepareExcludeDirs() with trailing slashes got %d items, want 2", len(got))
	}
}

func TestDependencyManager_prepareExcludeDirs_EmptyReplacements(t *testing.T) {
	logger := zap.NewNop()
	dm := &DependencyManager{
		folderPath: "/app",
		lockFile:   "wippy.lock",
		logger:     logger,
	}

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{},
	}

	got := dm.prepareExcludeDirs(".", ".wippy", lockFile, "/app/wippy.lock")

	if len(got) != 1 {
		t.Errorf("prepareExcludeDirs() with empty replacements got %d items, want 1", len(got))
	}
}

func TestDependencyManager_prepareExcludeDirs_AbsoluteReplacementPaths(t *testing.T) {
	logger := zap.NewNop()
	dm := &DependencyManager{
		folderPath: "/app",
		lockFile:   "wippy.lock",
		logger:     logger,
	}

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{
			{From: "test/module1", To: "/absolute/path/module"},
			{From: "test/module2", To: "../outside/module"},
		},
	}

	got := dm.prepareExcludeDirs(".", ".wippy", lockFile, "/app/wippy.lock")

	// Paths that resolve outside the srcDir should not be included
	// We expect only .wippy to be included
	// Note: absolute paths and paths starting with .. should be excluded
	foundOutsidePath := false
	for _, dir := range got {
		if dir == "absolute" || dir == "outside" || filepath.IsAbs(dir) {
			foundOutsidePath = true
		}
	}

	if foundOutsidePath {
		t.Errorf("prepareExcludeDirs() included paths outside srcDir: %v", got)
	}

	// At minimum, we should have .wippy
	if len(got) < 1 {
		t.Errorf("prepareExcludeDirs() got %d items, want at least 1 (.wippy)", len(got))
	}
}
