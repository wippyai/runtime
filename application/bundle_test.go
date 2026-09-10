// SPDX-License-Identifier: MPL-2.0

package application

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
)

func testBundle(t *testing.T) Bundle {
	t.Helper()
	var data bytes.Buffer
	err := wapp.NewWriter().PackEntries(wapp.Metadata{"namespace": "acme.app", "name": "app", "version": "1.0.0"}, nil, &data)
	require.NoError(t, err)
	digest := sha256.Sum256(data.Bytes())
	return Bundle{Root: "acme/app", Packs: []Pack{{Module: "acme/app", Version: "1.0.0", Digest: fmt.Sprintf("sha256:%x", digest), Data: data.Bytes()}}}
}

func TestBundleSeedsOfflineAndPreservesUpdatedVersion(t *testing.T) {
	bundle := testBundle(t)
	dir := filepath.Join(t.TempDir(), "deployment")
	path, err := bundle.Seed(dir)
	require.NoError(t, err)
	locked, err := lock.New(path)
	require.NoError(t, err)
	require.Equal(t, []string{"acme/app"}, locked.GetRootModules())
	paths := locked.GetModuleLoadPaths()
	require.Len(t, paths, 2)
	data, err := os.ReadFile(paths[1].Path)
	require.NoError(t, err)
	require.Equal(t, bundle.Packs[0].Data, data)
	locked.SetModule(lock.Module{Name: "acme/app", Version: "2.0.0", Root: true})
	require.NoError(t, locked.Write())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = bundle.Seed(dir)
	require.NoError(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestBundleRejectsCorruptionAndIdentityMismatch(t *testing.T) {
	for _, mutation := range []func(*Bundle){
		func(b *Bundle) { b.Packs[0].Data = append(b.Packs[0].Data, 0) },
		func(b *Bundle) { b.Packs[0].Version = "2.0.0" },
		func(b *Bundle) { b.Packs = append(b.Packs, b.Packs[0]) },
		func(b *Bundle) { b.Root = "acme/other" },
	} {
		bundle := testBundle(t)
		mutation(&bundle)
		dir := filepath.Join(t.TempDir(), "deployment")
		_, err := bundle.Seed(dir)
		require.Error(t, err)
		_, err = os.Stat(dir)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestBundleFailsClosedOnExistingInvalidDeployment(t *testing.T) {
	bundle := testBundle(t)
	dir := t.TempDir()
	_, err := bundle.Seed(dir)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wippy.lock"), []byte("not: [yaml"), 0o600))
	_, err = bundle.Seed(dir)
	require.Error(t, err)
}

func TestBundleConcurrentFirstLaunch(t *testing.T) {
	bundle := testBundle(t)
	dir := filepath.Join(t.TempDir(), "deployment")
	var wait sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wait.Go(func() { _, err := bundle.Seed(dir); failures <- err })
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
}
