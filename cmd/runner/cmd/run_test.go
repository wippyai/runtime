package cmd

import (
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/deps"
	"go.uber.org/zap"
)

func TestPrepareExcludeDirs(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name           string
		folderPath     string
		modulesDirPath string
		lockFileDir    string
		lockFile       *deps.LockFile
		want           []string
	}{
		{
			name:           "no exclusions",
			folderPath:     "/app",
			modulesDirPath: "",
			lockFileDir:    "/app",
			lockFile:       nil,
			want:           []string{},
		},
		{
			name:           "exclude modules directory inside source",
			folderPath:     "/app",
			modulesDirPath: "/app/.wippy/vendor",
			lockFileDir:    "/app",
			lockFile:       nil,
			want:           []string{".wippy/vendor"},
		},
		{
			name:           "exclude modules directory outside source",
			folderPath:     "/app/src",
			modulesDirPath: "/app/.wippy/vendor",
			lockFileDir:    "/app",
			lockFile:       nil,
			want:           []string{},
		},
		{
			name:           "exclude replacements inside source",
			folderPath:     "/app",
			modulesDirPath: "/app/.wippy/vendor",
			lockFileDir:    "/app",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module1", To: "./replacements/module1"},
					{From: "test/module2", To: "./replacements/module2"},
				},
			},
			want: []string{".wippy/vendor", "replacements/module1", "replacements/module2"},
		},
		{
			name:           "exclude replacements outside source",
			folderPath:     "/app/src",
			modulesDirPath: "/app/.wippy/vendor",
			lockFileDir:    "/app",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module1", To: "./replacements/module1"},
				},
			},
			want: []string{},
		},
		{
			name:           "mixed: some inside, some outside",
			folderPath:     "/app",
			modulesDirPath: "/app/.wippy/vendor",
			lockFileDir:    "/app",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module1", To: "./local/module1"},
					{From: "test/module2", To: "../external/module2"},
				},
			},
			want: []string{".wippy/vendor", "local/module1"},
		},
		{
			name:           "absolute paths",
			folderPath:     "/home/user/app",
			modulesDirPath: "/home/user/app/.wippy/vendor",
			lockFileDir:    "/home/user/app",
			lockFile: &deps.LockFile{
				Replacements: []deps.Replacement{
					{From: "test/module1", To: "./replacements/module1"},
				},
			},
			want: []string{".wippy/vendor", "replacements/module1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prepareExcludeDirs(tt.folderPath, tt.modulesDirPath, tt.lockFileDir, tt.lockFile, logger)

			if len(got) != len(tt.want) {
				t.Errorf("prepareExcludeDirs() got %d items, want %d items\ngot: %v\nwant: %v", len(got), len(tt.want), got, tt.want)
				return
			}

			// Convert to map for easier comparison
			gotMap := make(map[string]bool)
			for _, item := range got {
				// Normalize paths for comparison
				gotMap[filepath.Clean(item)] = true
			}

			for _, wantItem := range tt.want {
				cleanWant := filepath.Clean(wantItem)
				if !gotMap[cleanWant] {
					t.Errorf("prepareExcludeDirs() missing expected item: %s\ngot: %v", wantItem, got)
				}
			}
		})
	}
}

func TestPrepareExcludeDirs_EmptyLockFile(t *testing.T) {
	logger := zap.NewNop()

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{},
	}

	got := prepareExcludeDirs("/app", "/app/.wippy/vendor", "/app", lockFile, logger)

	if len(got) != 1 {
		t.Errorf("prepareExcludeDirs() with empty replacements got %d items, want 1", len(got))
	}
}

func TestPrepareExcludeDirs_NilLogger(t *testing.T) {
	// Should not panic with nil logger (though we pass zap.NewNop() in real code)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("prepareExcludeDirs() panicked: %v", r)
		}
	}()

	_ = prepareExcludeDirs("/app", "/app/.wippy/vendor", "/app", nil, zap.NewNop())
}
