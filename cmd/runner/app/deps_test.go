package app

import (
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/deps"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

const lockFileName = "wippy.lock"

func TestDependencyManager_prepareExcludeDirs(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			dm := &DependencyManager{
				folderPath: tt.folderPath,
				lockFile:   lockFileName,
				logger:     logger,
			}

			got := dm.prepareExcludeDirs(tt.srcDir, tt.modulesDir, tt.lockFile, tt.lockPath)

			gotMap := make(map[string]bool)
			for _, item := range got {
				gotMap[filepath.Clean(item)] = true
			}

			wantMap := make(map[string]bool)
			for _, item := range tt.want {
				wantMap[filepath.Clean(item)] = true
			}

			assert.Equal(t, wantMap, gotMap)
		})
	}
}

func TestDependencyManager_prepareExcludeDirs_WithTrailingSlashes(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	dm := &DependencyManager{
		folderPath: "/app/",
		lockFile:   lockFileName,
		logger:     logger,
	}

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{
			{From: "test/module", To: "./replacements/test/"},
		},
	}

	got := dm.prepareExcludeDirs(".", ".wippy/", lockFile, "/app/wippy.lock")

	assert.Equal(t, 2, len(got))
}

func TestDependencyManager_prepareExcludeDirs_EmptyReplacements(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	dm := &DependencyManager{
		folderPath: "/app",
		lockFile:   lockFileName,
		logger:     logger,
	}

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{},
	}

	got := dm.prepareExcludeDirs(".", ".wippy", lockFile, "/app/wippy.lock")

	assert.Equal(t, 1, len(got))
}

func TestDependencyManager_prepareExcludeDirs_AbsoluteReplacementPaths(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	dm := &DependencyManager{
		folderPath: "/app",
		lockFile:   lockFileName,
		logger:     logger,
	}

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{
			{From: "test/module1", To: "/absolute/path/module"},
			{From: "test/module2", To: "../outside/module"},
		},
	}

	got := dm.prepareExcludeDirs(".", ".wippy", lockFile, "/app/wippy.lock")

	foundOutsidePath := false
	for _, dir := range got {
		if dir == "absolute" || dir == "outside" || filepath.IsAbs(dir) {
			foundOutsidePath = true
		}
	}

	assert.False(t, foundOutsidePath, "prepareExcludeDirs() included paths outside srcDir: %v", got)
	assert.GreaterOrEqual(t, len(got), 1, "prepareExcludeDirs() should have at least 1 item (.wippy)")
}
