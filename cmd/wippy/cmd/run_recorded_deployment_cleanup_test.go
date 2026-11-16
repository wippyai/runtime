// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestLoadRecordedDeploymentDefaultsReleasesResources(t *testing.T) {
	for _, mode := range []string{"cached", "corrupt artifact", "invalid defaults"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
			t.Setenv("WIPPY_CACHE_DIR", filepath.Join(dir, "wippy-cache"))
			t.Setenv(auth.EnvToken, "test-token")
			metadata := wapp.Metadata{"namespace": "acme.app", "name": "app", "version": "1.0.0"}
			if mode == "invalid defaults" {
				metadata["runtime.workspace.replacements.acme/app"] = "."
			}
			var pack bytes.Buffer
			require.NoError(t, wapp.NewWriter().PackEntries(metadata, []wapp.Entry{{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition}}, &pack))
			digest := fmt.Sprintf("sha256:%x", sha256.Sum256(pack.Bytes()))
			module := regapi.ResolvedModule{Name: "acme/app", Version: "1.0.0", VersionID: "1.0.0", Source: "hub", Digest: digest, SizeBytes: uint64(pack.Len())}
			path := filepath.Join(".wippy", "vendor", "acme", "app-1.0.0."+strings.ReplaceAll(digest, ":", "-")+".wapp")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			data := pack.Bytes()
			if mode == "corrupt artifact" {
				data = []byte("corrupt")
			}
			require.NoError(t, os.WriteFile(path, data, 0o600))
			history, config := recordedRunHistory(t, "sqlite", dir)
			root, err := history.Head()
			require.NoError(t, err)
			resolution := (&regapi.DependencyResolution{Deployment: &regapi.Deployment{Root: "acme/app", Modules: []regapi.ResolvedModule{module}}}).Canonical()
			require.True(t, resolution.Valid())
			require.NoError(t, history.(regapi.ResolutionHistory).SaveWithDependencyResolution(version.FromParent(root, 1), nil, resolution, true))
			require.NoError(t, history.(interface{ Close() error }).Close())
			configPath := filepath.Join(dir, ".wippy.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
			cfg, err := bootconfig.Load(configPath)
			require.NoError(t, err)
			workers := func() int {
				var stack bytes.Buffer
				require.NoError(t, pprof.Lookup("goroutine").WriteTo(&stack, 2))
				return strings.Count(stack.String(), ".cleanupLoop(")
			}
			before := workers()
			for range 3 {
				_, deployment, err := loadRecordedDeploymentDefaults(context.Background(), cfg, "", "", zap.NewNop())
				if mode == "cached" {
					require.NoError(t, err)
					require.NotNil(t, deployment)
				} else {
					require.Error(t, err)
				}
			}
			require.Eventually(t, func() bool { return workers() <= before }, time.Second, time.Millisecond,
				"preflight must release its cache workers on success and failure")
			require.NoFileExists(t, filepath.Join(dir, "history.db-wal"))
			require.NoFileExists(t, filepath.Join(dir, "history.db-shm"))
		})
	}
}
