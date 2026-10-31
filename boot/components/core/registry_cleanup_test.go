// SPDX-License-Identifier: MPL-2.0

package core

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	bootpkg "github.com/wippyai/runtime/boot"
	"go.uber.org/zap"
)

func TestRegistryComponentReleasesDependencyWorkers(t *testing.T) {
	for _, mode := range []string{"shutdown", "failed restore"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			cfg := boot.NewConfig()
			if mode == "failed restore" {
				digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("recorded artifact")))
				path := filepath.Join(dir, "vendor", "wippy", "llm-0.4.46."+strings.ReplaceAll(digest, ":", "-")+".wapp")
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, []byte("corrupt"), 0o600))
				dbPath := filepath.Join(dir, "registry.db")
				seedDependencyResolution(t, dbPath, "wippy/llm", "0.4.46", digest)
				cfg = boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
					RegistryHistoryType: "sqlite", RegistryHistoryPath: dbPath,
					RegistryDependencyVendorDir: filepath.Join(dir, "vendor"),
				}))
			}
			workers := func() int {
				var stack bytes.Buffer
				require.NoError(t, pprof.Lookup("goroutine").WriteTo(&stack, 2))
				return strings.Count(stack.String(), ".cleanupLoop(")
			}
			before := workers()
			for range 3 {
				ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
				require.NoError(t, err)
				loader, err := bootpkg.NewLoader(Artifacts(), Registry())
				require.NoError(t, err)
				ctx, err = loader.Load(ctx)
				if mode == "failed restore" {
					require.ErrorContains(t, err, "digest mismatch")
				} else {
					require.NoError(t, err)
					require.NoError(t, loader.Shutdown(ctx))
				}
			}
			require.Eventually(t, func() bool { return workers() <= before }, time.Second, time.Millisecond,
				"registry must release dependency cache workers after shutdown and failed startup")
		})
	}
}
