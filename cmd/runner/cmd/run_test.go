package cmd

import (
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/deps"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPrepareExcludeDirs(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			got := prepareExcludeDirs(tt.folderPath, tt.modulesDirPath, tt.lockFileDir, tt.lockFile, logger)

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

func TestPrepareExcludeDirs_EmptyLockFile(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()

	lockFile := &deps.LockFile{
		Replacements: []deps.Replacement{},
	}

	got := prepareExcludeDirs("/app", "/app/.wippy/vendor", "/app", lockFile, logger)

	assert.Equal(t, 1, len(got))
}

func TestPrepareExcludeDirs_NilLogger(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("prepareExcludeDirs() panicked: %v", r)
		}
	}()

	_ = prepareExcludeDirs("/app", "/app/.wippy/vendor", "/app", nil, zap.NewNop())
}
