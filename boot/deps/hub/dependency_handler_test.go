package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

type fakeHub struct {
	resolve      func(context.Context, *ResolveDependenciesParams) (*ResolveDependenciesResult, error)
	getDownload  func(context.Context, *DownloadParams) (*DownloadInfo, error)
	downloadFile func(context.Context, string, string) error
}

func (f *fakeHub) ResolveDependencies(ctx context.Context, params *ResolveDependenciesParams) (*ResolveDependenciesResult, error) {
	if f.resolve != nil {
		return f.resolve(ctx, params)
	}
	return &ResolveDependenciesResult{}, nil
}

func (f *fakeHub) GetDownloadURL(ctx context.Context, params *DownloadParams) (*DownloadInfo, error) {
	if f.getDownload != nil {
		return f.getDownload(ctx, params)
	}
	return &DownloadInfo{}, nil
}

func (f *fakeHub) DownloadToFile(ctx context.Context, url, destPath string) error {
	if f.downloadFile != nil {
		return f.downloadFile(ctx, url, destPath)
	}
	return nil
}

func TestDependencyHandler_ResolveErrors(t *testing.T) {
	ctx := newTestContext()

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			resolve: func(context.Context, *ResolveDependenciesParams) (*ResolveDependenciesResult, error) {
				return &ResolveDependenciesResult{
					Errors: []ResolutionError{
						{Org: "acme", Name: "http", Constraint: "^1.0.0", Message: "no matching version"},
					},
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app", "dep"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"^1.0.0"}`, payload.JSON),
	}

	_, err = handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, nil)
	require.Error(t, err)

	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Conflict, apiErr.Kind())
	assert.Contains(t, apiErr.Details().GetString("summary", ""), "no matching version")

	if details, ok := apiErr.Details().(attrs.Bag); ok {
		errorsList, _ := details["errors"].([]map[string]any)
		require.Len(t, errorsList, 1)
		assert.Equal(t, "acme/http", errorsList[0]["module"])
	}
}

func TestDependencyHandler_EntryConflict(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	wappPath := filepath.Join(vendorDir, "acme", "http-v1.0.0.wapp")
	writeWapp(t, wappPath, []wapp.Entry{
		{
			ID:   wapp.NewID("app", "conflict"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			resolve: func(context.Context, *ResolveDependenciesParams) (*ResolveDependenciesResult, error) {
				return &ResolveDependenciesResult{
					Modules: []ResolvedModule{
						{Org: "acme", Name: "http", Version: "v1.0.0"},
					},
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app", "dep"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"v1.0.0"}`, payload.JSON),
	}

	snapshot := regapi.State{
		{
			ID:   regapi.NewID("app", "conflict"),
			Kind: "service",
			Data: payload.NewPayload(`{"existing":true}`, payload.JSON),
		},
	}

	_, err = handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, snapshot)
	require.Error(t, err)

	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Conflict, apiErr.Kind())
	assert.Equal(t, "acme/http", apiErr.Details().GetString("desired_module", ""))
}

func TestDependencyHandler_RejectsDownloadedArtifactWithDigestMismatch(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")
	moduleData := buildWappBytes(t, []wapp.Entry{
		{
			ID:   wapp.NewID("mod", "svc"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			resolve: func(context.Context, *ResolveDependenciesParams) (*ResolveDependenciesResult, error) {
				return &ResolveDependenciesResult{
					Modules: []ResolvedModule{
						{
							Org:     "acme",
							Name:    "http",
							Version: "v1.0.0",
							URL:     "https://example.invalid/http-v1.0.0.wapp",
							Digest:  "sha256:deadbeef",
						},
					},
				}, nil
			},
			downloadFile: func(_ context.Context, _ string, destPath string) error {
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return err
				}
				return os.WriteFile(destPath, moduleData, 0600)
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app", "dep"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"v1.0.0"}`, payload.JSON),
	}

	_, err = handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, nil)
	require.Error(t, err)

	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, apierror.Invalid, apiErr.Kind())
	assert.Equal(t, "acme/http@v1.0.0", apiErr.Details().GetString("module", ""))
}

func TestDependencyHandler_RedownloadsCorruptCachedArtifact(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	wappPath := filepath.Join(vendorDir, "acme", "http-v1.0.0.wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(wappPath), 0755))
	require.NoError(t, os.WriteFile(wappPath, []byte("corrupt"), 0600))

	moduleData := buildWappBytes(t, []wapp.Entry{
		{
			ID:   wapp.NewID("mod", "svc"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})
	sum := sha256.Sum256(moduleData)
	expectedDigest := "sha256:" + hex.EncodeToString(sum[:])

	var downloadCalls atomic.Int32
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			resolve: func(context.Context, *ResolveDependenciesParams) (*ResolveDependenciesResult, error) {
				return &ResolveDependenciesResult{
					Modules: []ResolvedModule{
						{
							Org:     "acme",
							Name:    "http",
							Version: "v1.0.0",
							URL:     "https://example.invalid/http-v1.0.0.wapp",
							Digest:  expectedDigest,
						},
					},
				}, nil
			},
			downloadFile: func(_ context.Context, _ string, destPath string) error {
				downloadCalls.Add(1)
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return err
				}
				return os.WriteFile(destPath, moduleData, 0600)
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app", "dep"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"v1.0.0"}`, payload.JSON),
	}

	_, err = handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), downloadCalls.Load())
}

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	transcoder := syspayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	return ctx
}

func writeWapp(t *testing.T, path string, entries []wapp.Entry) {
	t.Helper()
	buf := buildWappBytes(t, entries)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, buf, 0600))
}

func buildWappBytes(t *testing.T, entries []wapp.Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackEntries(wapp.Metadata{}, entries, &buf))
	return buf.Bytes()
}
