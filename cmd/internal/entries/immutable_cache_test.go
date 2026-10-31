// SPDX-License-Identifier: MPL-2.0

package entries

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestEnsureModulesInstalledReusesImmutableArtifact(t *testing.T) {
	for _, unpack := range []bool{false, true} {
		for _, corrupt := range []bool{false, true} {
			t.Run(fmt.Sprintf("unpack=%t/corrupt=%t", unpack, corrupt), func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", dir)
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					http.Error(w, "verified local artifacts must not be downloaded", http.StatusNotFound)
				}))
				defer server.Close()
				t.Setenv(auth.EnvRegistry, server.URL)
				var pack bytes.Buffer
				require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{"namespace": "acme.feature", "name": "feature", "version": "1.0.0"}, []wapp.Entry{{ID: wapp.NewID("acme.feature", "definition"), Kind: "ns.definition"}}, &pack))
				digest := fmt.Sprintf("sha256:%x", sha256.Sum256(pack.Bytes()))
				name := graph.Name{Organization: "acme", Module: "feature"}
				locked, err := lock.New(filepath.Join(dir, "wippy.lock"))
				require.NoError(t, err)
				locked.SetModule(lock.Module{Name: "acme/feature", Version: "1.0.0", Hash: digest})
				locked.SetUnpackModulesOverride(&unpack)
				vendor := filepath.Join(dir, locked.GetVendorPath())
				relative, err := hub.ImmutableWappRelativePath(name, "1.0.0", digest)
				require.NoError(t, err)
				cached := filepath.Join(vendor, relative)
				require.NoError(t, os.MkdirAll(filepath.Dir(cached), 0o700))
				data := pack.Bytes()
				if corrupt {
					data = []byte("corrupt cache content")
				}
				require.NoError(t, os.WriteFile(cached, data, 0o600))
				deployment := filepath.Join(vendor, lock.WappPath(name, "1.0.0"))
				err = EnsureModulesInstalledFromLock(context.Background(), locked, zap.NewNop(), nil)
				if corrupt {
					require.ErrorContains(t, err, "digest mismatch")
					require.NoFileExists(t, deployment)
				} else {
					require.NoError(t, err)
					installed, err := os.ReadFile(deployment)
					require.NoError(t, err)
					require.Equal(t, pack.Bytes(), installed)
					if unpack {
						require.DirExists(t, filepath.Join(vendor, lock.ModulePath(name)))
					}
					// A version-addressed deployment remains independent of its cache.
					require.NoError(t, os.WriteFile(deployment, []byte("changed deployment"), 0o600))
				}
				stored, err := os.ReadFile(cached)
				require.NoError(t, err)
				require.Equal(t, data, stored)
				require.Zero(t, calls.Load())
				staged, err := filepath.Glob(filepath.Join(filepath.Dir(cached), ".artifact-*"))
				require.NoError(t, err)
				require.Empty(t, staged, "materialization must release private candidates on success and failure")
			})
		}
	}
}
