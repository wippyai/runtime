// SPDX-License-Identifier: MPL-2.0

package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/common/v1"
	downloadv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1"
	"github.com/wippyai/runtime/api/hub/wippy/api/hub/download/v1/downloadv1connect"
	manifestv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1"
	"github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1/manifestv1connect"
	"github.com/wippyai/wapp"
)

// TestHubBinaryUpdate exercises the actual assembled hello executable against
// the canonical Hub wire API. Supply the builder fixture binary and its pack.
func TestHubBinaryUpdate(t *testing.T) {
	binary, packPath := os.Getenv("WIPPY_TEST_APPLICATION_BINARY"), os.Getenv("WIPPY_TEST_APPLICATION_PACK")
	if binary == "" || packPath == "" {
		t.Skip("requires assembled builder hello fixture")
	}
	binary, err := filepath.Abs(binary)
	require.NoError(t, err)
	data, err := os.ReadFile(packPath)
	require.NoError(t, err)
	reader, err := wapp.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	entries, err := reader.GetEntries()
	require.NoError(t, err)
	for i := range entries {
		if entries[i].ID.Name == "main" {
			entryData := entries[i].Data.(map[string]any)
			entryData["source"] = strings.ReplaceAll(entryData["source"].(string), "Hello,", "Updated,")
		}
	}
	entries = append(entries, wapp.Entry{ID: wapp.NewID("example.hello", "definition"), Kind: "ns.definition"}, wapp.Entry{ID: wapp.NewID("example.hello", "dependency"), Kind: "ns.dependency", Data: map[string]any{"component": "example/helper", "version": "1.0.0"}})
	makePack := func(name, version string, entries []wapp.Entry) []byte {
		var output bytes.Buffer
		require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{"namespace": "example." + name, "name": name, "version": version}, entries, &output))
		return output.Bytes()
	}
	artifacts := map[string][]byte{"hello": makePack("hello", "2.0.0", entries), "helper": makePack("helper", "1.0.0", []wapp.Entry{{ID: wapp.NewID("example.helper", "definition"), Kind: "ns.definition"}})}
	var reject atomic.Bool
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/packs/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(artifacts[strings.TrimPrefix(r.URL.Path, "/packs/")])
	})
	mux.Handle(manifestv1connect.ManifestServiceGetManifestProcedure, connect.NewUnaryHandler(manifestv1connect.ManifestServiceGetManifestProcedure, func(_ context.Context, req *connect.Request[manifestv1.GetManifestRequest]) (*connect.Response[manifestv1.GetManifestResponse], error) {
		if reject.Load() {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("fixture version unavailable"))
		}
		name := req.Msg.GetModule().GetName().GetName()
		artifact, ok := artifacts[name]
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("unknown module"))
		}
		version := "1.0.0"
		if name == "hello" {
			version = "2.0.0"
		}
		manifest := &manifestv1.ModuleManifest{Org: "example", Name: name, Version: version, VersionId: version, Digest: fmt.Sprintf("%x", sha256.Sum256(artifact)), SizeBytes: uint64(len(artifact)), Download: &commonv1.DownloadInfo{Url: server.URL + "/packs/" + name}}
		if name == "hello" {
			manifest.Dependencies = []*manifestv1.ResolvedDependency{{Org: "example", Name: "helper", Version: "1.0.0", Constraint: "1.0.0"}}
		}
		return connect.NewResponse(&manifestv1.GetManifestResponse{Manifest: manifest}), nil
	}))
	mux.Handle(downloadv1connect.DownloadServiceGetDownloadURLProcedure, connect.NewUnaryHandler(downloadv1connect.DownloadServiceGetDownloadURLProcedure, func(_ context.Context, req *connect.Request[downloadv1.GetDownloadURLRequest]) (*connect.Response[downloadv1.GetDownloadURLResponse], error) {
		name := req.Msg.GetModule().GetName().GetName()
		artifact, ok := artifacts[name]
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("unknown module"))
		}
		version := "1.0.0"
		if name == "hello" {
			version = "2.0.0"
		}
		return connect.NewResponse(&downloadv1.GetDownloadURLResponse{Version: version, Download: &commonv1.DownloadInfo{Url: server.URL + "/packs/" + name, Digest: fmt.Sprintf("%x", sha256.Sum256(artifact)), SizeBytes: uint64(len(artifact))}}), nil
	}))

	root := t.TempDir()
	state := filepath.Join(root, "state")
	cwd := filepath.Join(root, "empty")
	require.NoError(t, os.Mkdir(cwd, 0700))
	invoke := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, append([]string{"--state-dir", state}, args...)...)
		command.Dir = cwd
		command.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "PATH=/nonexistent")
		output, err := command.CombinedOutput()
		return string(output), err
	}
	output, err := invoke("run", "Before")
	require.NoError(t, err, output)
	require.Contains(t, output, "Hello, Before!")
	output, err = invoke("update", "example/hello", "--registry", server.URL)
	require.NoError(t, err, output)
	t.Log(output)
	output, err = invoke("run", "After")
	require.NoError(t, err, output)
	require.Contains(t, output, "Updated, After!")
	active, err := os.ReadFile(filepath.Join(state, "active.json"))
	require.NoError(t, err)
	output, err = invoke("--base", "run", "Recovery")
	require.NoError(t, err, output)
	require.Contains(t, output, "Hello, Recovery!")
	// A failed Hub resolution must preserve the installed selection on restart.
	reject.Store(true)
	output, err = invoke("update", "example/hello", "--registry", server.URL)
	require.Error(t, err, output)
	after, err := os.ReadFile(filepath.Join(state, "active.json"))
	require.NoError(t, err)
	require.Equal(t, active, after)
	output, err = invoke("run", "Restart")
	require.NoError(t, err, output)
	require.Contains(t, output, "Updated, Restart!")
	files, err := os.ReadDir(cwd)
	require.NoError(t, err)
	require.Empty(t, files)
}
