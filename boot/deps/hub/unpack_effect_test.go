// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func TestModuleFilesystemEffect_HistoryFailureRestoresDirectoryAndSourceRoots(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "mod")
	stage := filepath.Join(parent, ".mod.stage-test")
	writeTreeMarker(t, target, "old")
	writeTreeMarker(t, stage, "new")

	ctx := newTestContext()
	moduleapi.WithSourceRoots(ctx, moduleapi.SourceRoots{
		"org/mod":     "/previous/mod",
		"org/removed": "/previous/removed",
	})
	effect := &moduleFilesystemEffect{
		staged: []stagedModuleDirectory{{module: "org/mod", stagingDir: stage, targetDir: target}},
		roots: &sourceRootEffect{
			desired: moduleapi.SourceRoots{"org/mod": target},
			modules: []string{"org/mod", "org/removed"},
		},
	}

	require.NoError(t, effect.Prepare(ctx))
	assert.Equal(t, "new", readTreeMarker(t, target))
	root, ok := moduleapi.SourceRoot(ctx, "org/mod")
	require.True(t, ok)
	assert.Equal(t, target, root)
	require.NoError(t, effect.Commit(ctx))

	// Registry history/CAS failure calls Rollback after Commit.
	require.NoError(t, effect.Rollback(ctx))
	assert.Equal(t, "old", readTreeMarker(t, target))
	root, ok = moduleapi.SourceRoot(ctx, "org/mod")
	require.True(t, ok)
	assert.Equal(t, "/previous/mod", root)
	root, ok = moduleapi.SourceRoot(ctx, "org/removed")
	require.True(t, ok)
	assert.Equal(t, "/previous/removed", root)
}

func TestModuleFilesystemEffect_ActivationFailureRestoresEarlierModules(t *testing.T) {
	parent := t.TempDir()
	firstTarget := filepath.Join(parent, "first")
	secondTarget := filepath.Join(parent, "second")
	firstStage := filepath.Join(parent, ".first.stage-test")
	missingSecondStage := filepath.Join(parent, ".second.stage-missing")
	writeTreeMarker(t, firstTarget, "old-first")
	writeTreeMarker(t, secondTarget, "old-second")
	writeTreeMarker(t, firstStage, "new-first")

	effect := &moduleFilesystemEffect{staged: []stagedModuleDirectory{
		{module: "org/first", stagingDir: firstStage, targetDir: firstTarget},
		{module: "org/second", stagingDir: missingSecondStage, targetDir: secondTarget},
	}}

	require.Error(t, effect.Prepare(context.Background()))
	assert.Equal(t, "old-first", readTreeMarker(t, firstTarget))
	assert.Equal(t, "old-second", readTreeMarker(t, secondTarget))
	assertNoLifecycleDirectories(t, parent)
	require.NoError(t, effect.Rollback(context.Background()), "cleanup after a failed Prepare is idempotent")
}

func TestModuleFilesystemEffect_FinalizeKeepsAllNewModulesAndRemovesBackups(t *testing.T) {
	parent := t.TempDir()
	var staged []stagedModuleDirectory
	for _, name := range []string{"first", "second"} {
		target := filepath.Join(parent, name)
		stage := filepath.Join(parent, "."+name+".stage-test")
		writeTreeMarker(t, target, "old-"+name)
		writeTreeMarker(t, stage, "new-"+name)
		staged = append(staged, stagedModuleDirectory{module: "org/" + name, stagingDir: stage, targetDir: target})
	}
	effect := &moduleFilesystemEffect{staged: staged}

	require.NoError(t, effect.Prepare(context.Background()))
	require.NoError(t, effect.Commit(context.Background()))
	require.NoError(t, effect.Finalize(context.Background()))
	assert.Equal(t, "new-first", readTreeMarker(t, filepath.Join(parent, "first")))
	assert.Equal(t, "new-second", readTreeMarker(t, filepath.Join(parent, "second")))
	assertNoLifecycleDirectories(t, parent)
}

func TestModuleFilesystemEffect_ActivationDoubleFailureRestoresThroughBookkeeping(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "mod")
	stage := filepath.Join(parent, ".mod.stage-test")
	writeTreeMarker(t, target, "old")
	writeTreeMarker(t, stage, "new")

	renameCalls := 0
	effect := &moduleFilesystemEffect{
		staged: []stagedModuleDirectory{{module: "org/mod", stagingDir: stage, targetDir: target}},
		ops: moduleFilesystemOps{rename: func(oldPath, newPath string) error {
			renameCalls++
			if renameCalls == 2 {
				return errors.New("injected staged activation failure")
			}
			if renameCalls == 3 {
				return errors.New("injected immediate backup restore failure")
			}
			return os.Rename(oldPath, newPath)
		}},
	}

	require.Error(t, effect.Prepare(context.Background()))
	assert.Equal(t, "old", readTreeMarker(t, target), "general rollback must retain and restore the failed activation")
	assert.Equal(t, filesystemEffectRolledBack, effect.state)
	assert.Empty(t, effect.activated)
	assertNoLifecycleDirectories(t, parent)
}

func TestModuleFilesystemEffect_ActivationRestoreFailureCanRetryRollback(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "mod")
	stage := filepath.Join(parent, ".mod.stage-test")
	writeTreeMarker(t, target, "old")
	writeTreeMarker(t, stage, "new")

	renameCalls := 0
	effect := &moduleFilesystemEffect{
		staged: []stagedModuleDirectory{{module: "org/mod", stagingDir: stage, targetDir: target}},
		ops: moduleFilesystemOps{rename: func(oldPath, newPath string) error {
			renameCalls++
			if renameCalls >= 2 && renameCalls <= 4 {
				return errors.New("injected activation and restore failure")
			}
			return os.Rename(oldPath, newPath)
		}},
	}

	require.Error(t, effect.Prepare(context.Background()))
	assert.Equal(t, filesystemEffectRollbackPending, effect.state)
	require.Len(t, effect.activated, 1, "failed backup restore must remain recoverable")
	assert.NoDirExists(t, target)
	require.NoError(t, effect.Rollback(context.Background()))
	assert.Equal(t, filesystemEffectRolledBack, effect.state)
	assert.Equal(t, "old", readTreeMarker(t, target))
	assertNoLifecycleDirectories(t, parent)
}

func TestModuleFilesystemEffect_RollbackRetainsFailedDiscardCleanup(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "mod")
	stage := filepath.Join(parent, ".mod.stage-test")
	writeTreeMarker(t, target, "old")
	writeTreeMarker(t, stage, "new")

	removeCalls := 0
	effect := &moduleFilesystemEffect{
		staged: []stagedModuleDirectory{{module: "org/mod", stagingDir: stage, targetDir: target}},
		ops: moduleFilesystemOps{removeAll: func(path string) error {
			removeCalls++
			if removeCalls == 1 {
				return errors.New("injected discard cleanup failure")
			}
			return os.RemoveAll(path)
		}},
	}

	require.NoError(t, effect.Prepare(context.Background()))
	require.Error(t, effect.Rollback(context.Background()))
	assert.Equal(t, filesystemEffectRollbackPending, effect.state)
	require.Len(t, effect.activated, 1)
	assert.NotEmpty(t, effect.activated[0].discardDir)
	assert.Equal(t, "old", readTreeMarker(t, target))
	require.NoError(t, effect.Rollback(context.Background()))
	assert.Equal(t, filesystemEffectRolledBack, effect.state)
	assertNoLifecycleDirectories(t, parent)
}

func TestModuleFilesystemEffect_FinalizeRetainsFailedBackupCleanup(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "mod")
	stage := filepath.Join(parent, ".mod.stage-test")
	writeTreeMarker(t, target, "old")
	writeTreeMarker(t, stage, "new")

	removeCalls := 0
	effect := &moduleFilesystemEffect{
		staged: []stagedModuleDirectory{{module: "org/mod", stagingDir: stage, targetDir: target}},
		ops: moduleFilesystemOps{removeAll: func(path string) error {
			removeCalls++
			if removeCalls == 1 {
				return errors.New("injected backup cleanup failure")
			}
			return os.RemoveAll(path)
		}},
	}

	require.NoError(t, effect.Prepare(context.Background()))
	require.NoError(t, effect.Commit(context.Background()))
	require.Error(t, effect.Finalize(context.Background()))
	assert.Equal(t, filesystemEffectCommitted, effect.state)
	require.Len(t, effect.activated, 1, "failed backup cleanup must remain retryable")
	assert.DirExists(t, effect.activated[0].backupDir)
	require.NoError(t, effect.Finalize(context.Background()))
	assert.Equal(t, filesystemEffectFinalized, effect.state)
	assert.Equal(t, "new", readTreeMarker(t, target))
	assertNoLifecycleDirectories(t, parent)
}

func TestModuleFilesystemEffect_PlannedRollbackRetainsFailedStageCleanup(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, ".mod.stage-test")
	writeTreeMarker(t, stage, "new")

	removeCalls := 0
	effect := &moduleFilesystemEffect{
		staged: []stagedModuleDirectory{{module: "org/mod", stagingDir: stage, targetDir: filepath.Join(parent, "mod")}},
		ops: moduleFilesystemOps{removeAll: func(path string) error {
			removeCalls++
			if removeCalls == 1 {
				return errors.New("injected stage cleanup failure")
			}
			return os.RemoveAll(path)
		}},
	}

	require.Error(t, effect.Rollback(context.Background()))
	assert.Equal(t, filesystemEffectRollbackPending, effect.state)
	require.Len(t, effect.staged, 1)
	assert.DirExists(t, stage)
	require.NoError(t, effect.Rollback(context.Background()))
	assert.Equal(t, filesystemEffectRolledBack, effect.state)
	assert.NoDirExists(t, stage)
}

func TestMaterializeModuleForLoad_RestartReusesVerifiedDirectory(t *testing.T) {
	projectDir := t.TempDir()
	vendorDir := filepath.Join(projectDir, ".wippy", "vendor")
	lockPath := filepath.Join(projectDir, lock.DefaultFilename)
	target := filepath.Join(vendorDir, "org", "mod")
	writeTreeMarker(t, target, "installed")
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, writeExtractedModuleMeta(target, digest, 17))

	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "."})
	lockObj.SetOptions(lock.Options{UnpackModules: true})
	lockObj.SetModule(lock.Module{Name: "org/mod", Version: "1.0.0"})
	require.NoError(t, lockObj.Write())

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("a verified unpacked module must be reused on restart")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	path, staged, err := handler.materializeModuleForLoad(context.Background(), ResolvedModule{
		Org: "org", Name: "mod", Version: "1.0.0", Digest: digest, SizeBytes: 17,
	})
	require.NoError(t, err)
	assert.Equal(t, target, path)
	assert.Nil(t, staged)
	assert.Equal(t, "installed", readTreeMarker(t, target))
}

func writeTreeMarker(t *testing.T, dir, value string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte(value), 0644))
}

func readTreeMarker(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "marker"))
	require.NoError(t, err)
	return string(data)
}

func assertNoLifecycleDirectories(t *testing.T, parent string) {
	t.Helper()
	for _, pattern := range []string{".*.backup-*", ".*.discard-*", ".*.stage-*"} {
		matches, err := filepath.Glob(filepath.Join(parent, pattern))
		require.NoError(t, err)
		assert.Empty(t, matches, pattern)
	}
}
