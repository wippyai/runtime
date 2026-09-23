// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	publishv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/publish/v1"
	"github.com/wippyai/runtime/api/hub/wippy/api/hub/publish/v1/publishv1connect"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/wapp"
)

type sealedPublishStatus struct {
	publishv1connect.UnimplementedPublishServiceHandler
}

func (sealedPublishStatus) GetPublishStatus(
	context.Context,
	*connect.Request[publishv1.GetPublishStatusRequest],
) (*connect.Response[publishv1.GetPublishStatusResponse], error) {
	return connect.NewResponse(&publishv1.GetPublishStatusResponse{
		Status: publishv1.PublishStatus_PUBLISH_STATUS_COMPLETED,
	}), nil
}

// sealedHub records what a Hub stores for one uploaded version: the exact
// body and the digest it computes from that body.
type sealedHub struct {
	digest  string
	claimed string
	version string
	label   string
	body    []byte
	mu      sync.Mutex
}

func newSealedHub(t *testing.T, onRegister ...func() error) (*sealedHub, *httptest.Server) {
	t.Helper()
	recorded := &sealedHub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/account/modules", func(w http.ResponseWriter, _ *http.Request) {
		if len(onRegister) > 0 {
			if err := onRegister[0](); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusConflict) // already registered
	})
	mux.HandleFunc("/api/v1/publish/upload", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256(body)
		recorded.mu.Lock()
		recorded.body = body
		recorded.digest = "sha256:" + hex.EncodeToString(sum[:])
		recorded.claimed = r.Header.Get("X-Wippy-Digest")
		recorded.version = r.Header.Get("X-Wippy-Version")
		recorded.label = r.Header.Get("X-Wippy-Label")
		recorded.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"publish_id":"publish-sealed"}`))
	})
	path, handler := publishv1connect.NewPublishServiceHandler(sealedPublishStatus{})
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return recorded, server
}

type sealedManifests struct {
	manifest hub.ModuleManifest
}

func (p sealedManifests) GetManifest(_ context.Context, org, module, _ string) (*hub.ModuleManifest, error) {
	if org != p.manifest.Org || module != p.manifest.Name {
		return nil, hub.ErrModuleNotFound
	}
	manifest := p.manifest
	return &manifest, nil
}

func (p sealedManifests) ListAllVersions(context.Context, string, string) ([]hub.VersionInfo, error) {
	return []hub.VersionInfo{{Version: p.manifest.Version}}, nil
}

func writeSealedPack(t *testing.T, path string, metadata wapp.Metadata, entries ...wapp.Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	if entries == nil {
		entries = []wapp.Entry{{ID: wapp.NewID("bee.tools", "definition"), Kind: "ns.definition"}}
	}
	if err := wapp.NewWriter().PackEntries(metadata, entries, &buf); err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write pack: %v", err)
	}
	return buf.Bytes()
}

func sealedPackMetadata(version string) wapp.Metadata {
	return wapp.Metadata{
		"name":      "tools",
		"namespace": "bee.tools",
		"version":   version,
		"packed_at": "2026-09-23T02:51:07Z",
	}
}

type sealedPublishArgs struct {
	wappPath string
	version  string
	label    string
	registry string
	dryRun   bool
	create   bool
}

func runSealedPublish(t *testing.T, dir, manifest string, args sealedPublishArgs) (string, error) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("WIPPY_TOKEN", "test-token")
	t.Setenv("WIPPY_REGISTRY", args.registry)
	if err := os.WriteFile(filepath.Join(dir, "wippy.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.SetContext(context.Background())
	command.Flags().String("config", dir, "")
	command.Flags().Bool("dry-run", args.dryRun, "")
	command.Flags().String("version", args.version, "")
	command.Flags().String("label", args.label, "")
	command.Flags().String("registry", args.registry, "")
	command.Flags().String("wapp", args.wappPath, "")
	command.Flags().Bool("create", args.create, "")
	previousSilent := silentLogs
	t.Cleanup(func() { silentLogs = previousSilent })

	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	output := make(chan string)
	go func() {
		data, _ := io.ReadAll(reader)
		output <- string(data)
	}()
	runErr := runPublish(command, nil)
	os.Stdout = stdout
	_ = writer.Close()
	return <-output, runErr
}

// A Bee release embeds sealed packs and pins each one's hash in wippy.lock.
// Publishing that pack must record the same digest on the Hub, so the online
// resolver accepts the lock's pin.
func TestPublishSealedWappUploadsExactBytesThatResolveAgainstLock(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "tools-1.2.3.wapp")
	sealed := writeSealedPack(t, packPath, sealedPackMetadata("1.2.3"))
	sum := sha256.Sum256(sealed)
	lockHash := "sha256:" + hex.EncodeToString(sum[:])

	recorded, server := newSealedHub(t)
	_, err := runSealedPublish(t, dir, "organization: bee\nmodule: tools\ntype: library\n", sealedPublishArgs{
		wappPath: packPath,
		registry: server.URL,
	})
	if err != nil {
		t.Fatalf("publish sealed pack: %v", err)
	}

	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	if !bytes.Equal(recorded.body, sealed) {
		t.Fatalf("uploaded %d bytes differ from the sealed pack's %d bytes", len(recorded.body), len(sealed))
	}
	if "sha256:"+recorded.claimed != recorded.digest {
		t.Fatalf("X-Wippy-Digest %q does not describe the uploaded body %s", recorded.claimed, recorded.digest)
	}
	if recorded.version != "1.2.3" || recorded.label != "" {
		t.Fatalf("uploaded as version=%q label=%q, want version 1.2.3 without label", recorded.version, recorded.label)
	}

	result, err := hub.Resolve(context.Background(), sealedManifests{manifest: hub.ModuleManifest{
		Org: "bee", Name: "tools", Version: "1.2.3", Digest: recorded.digest, SizeBytes: uint64(len(recorded.body)),
	}}, []hub.DependencySpec{{Org: "bee", Name: "tools", Constraint: "1.2.3"}}, &hub.ResolveOptions{
		LockedVersions: map[string]string{"bee/tools": "1.2.3"},
		LockedDigests:  map[string]string{"bee/tools@1.2.3": lockHash},
	})
	if err != nil {
		t.Fatalf("resolve against lock: %v", err)
	}
	if len(result.Errors) > 0 {
		t.Fatalf("resolve against lock: %+v", result.Errors)
	}
	if err := hub.VerifyDownloadedArtifact(packPath, recorded.digest, uint64(len(recorded.body))); err != nil {
		t.Fatalf("sealed pack does not verify against the Hub digest: %v", err)
	}
}

func TestPublishSealedWappKeepsValidatedBytesWhenSourceChanges(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "tools.wapp")
	sealed := writeSealedPack(t, packPath, sealedPackMetadata("1.2.3"))
	replacementMetadata := sealedPackMetadata("1.2.3")
	replacementMetadata["packed_at"] = "2026-09-24T02:51:07Z"
	replacement := writeSealedPack(t, filepath.Join(dir, "replacement.wapp"), replacementMetadata)
	if bytes.Equal(sealed, replacement) {
		t.Fatal("replacement pack must have different bytes")
	}
	replaced := make(chan struct{}, 1)
	recorded, server := newSealedHub(t, func() error {
		if err := os.WriteFile(packPath, replacement, 0o600); err != nil {
			return err
		}
		replaced <- struct{}{}
		return nil
	})
	output, err := runSealedPublish(t, dir, "organization: bee\nmodule: tools\ntype: library\n", sealedPublishArgs{
		wappPath: packPath,
		registry: server.URL,
		create:   true,
	})
	if err != nil {
		t.Fatalf("publish after source replacement: %v", err)
	}
	select {
	case <-replaced:
	default:
		t.Fatal("source was not replaced during registration")
	}
	sum := sha256.Sum256(sealed)
	wantDigest := "sha256:" + hex.EncodeToString(sum[:])
	if !strings.Contains(output, wantDigest) {
		t.Fatalf("displayed digest differs from validated bytes: %s", output)
	}
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	if !bytes.Equal(recorded.body, sealed) || recorded.digest != wantDigest {
		t.Fatalf("uploaded replacement rather than validated pack: digest=%s want=%s", recorded.digest, wantDigest)
	}
}

func TestPublishSealedWappDryRunPrintsDigest(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "tools.wapp")
	sealed := writeSealedPack(t, packPath, sealedPackMetadata("1.2.3"))
	sum := sha256.Sum256(sealed)

	output, err := runSealedPublish(t, dir, "organization: bee\nmodule: tools\n", sealedPublishArgs{
		wappPath: packPath,
		registry: "http://127.0.0.1:1",
		dryRun:   true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if want := "sha256:" + hex.EncodeToString(sum[:]); !strings.Contains(output, want) {
		t.Fatalf("dry run output lacks digest %s:\n%s", want, output)
	}
	after, err := os.ReadFile(packPath)
	if err != nil || !bytes.Equal(after, sealed) {
		t.Fatalf("dry run changed or removed the sealed pack: %v", err)
	}
}

func TestPublishSealedWappRefusals(t *testing.T) {
	for _, tc := range []struct {
		metadata wapp.Metadata
		name     string
		manifest string
		version  string
		label    string
		want     string
		entries  []wapp.Entry
		raw      []byte
	}{
		{
			name:     "manifest version differs",
			manifest: "organization: bee\nmodule: tools\nversion: 1.2.4\n",
			metadata: sealedPackMetadata("1.2.3"),
			want:     `pack version "1.2.3" does not match publish version "1.2.4"`,
		},
		{
			name:     "flag version differs",
			manifest: "organization: bee\nmodule: tools\n",
			version:  "2.0.0",
			metadata: sealedPackMetadata("1.2.3"),
			want:     `pack version "1.2.3" does not match publish version "2.0.0"`,
		},
		{
			name:     "module name differs",
			manifest: "organization: bee\nmodule: shell\n",
			metadata: sealedPackMetadata("1.2.3"),
			want:     `does not match "shell"`,
		},
		{
			name:     "organization differs",
			manifest: "organization: hive\nmodule: tools\n",
			metadata: sealedPackMetadata("1.2.3"),
			want:     `namespace "bee.tools" does not match "hive.tools"`,
		},
		{
			name:     "version missing",
			manifest: "organization: bee\nmodule: tools\n",
			metadata: wapp.Metadata{"name": "tools", "namespace": "bee.tools"},
			want:     `pack metadata has no "version"`,
		},
		{
			name:     "version is not semver",
			manifest: "organization: bee\nmodule: tools\n",
			metadata: sealedPackMetadata("latest"),
			want:     "version must be valid semver",
		},
		{
			name:     "label",
			manifest: "organization: bee\nmodule: tools\n",
			label:    "latest",
			metadata: sealedPackMetadata("1.2.3"),
			want:     "--wapp publishes an exact version and cannot be combined with --label",
		},
		{
			name:     "no definition",
			manifest: "organization: bee\nmodule: tools\n",
			metadata: sealedPackMetadata("1.2.3"),
			entries:  []wapp.Entry{{ID: wapp.NewID("bee.tools", "lib"), Kind: "library.lua"}},
			want:     "module must have exactly one ns.definition entry",
		},
		{
			name:     "invalid structure",
			manifest: "organization: bee\nmodule: tools\n",
			raw:      []byte("not a wapp"),
			want:     "read pack",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			packPath := filepath.Join(dir, "tools.wapp")
			if tc.raw != nil {
				if err := os.WriteFile(packPath, tc.raw, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				writeSealedPack(t, packPath, tc.metadata, tc.entries...)
			}
			_, err := runSealedPublish(t, dir, tc.manifest, sealedPublishArgs{
				wappPath: packPath,
				version:  tc.version,
				label:    tc.label,
				registry: "http://127.0.0.1:1",
				dryRun:   true,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runPublish error = %v, want %q", err, tc.want)
			}
		})
	}
}
