// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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
	getManifest  func(ctx context.Context, org, module, constraint string) (*ModuleManifest, error)
	listVersions func(ctx context.Context, org, module string) ([]VersionInfo, error)
	getDownload  func(context.Context, *DownloadParams) (*DownloadInfo, error)
	downloadFile func(context.Context, string, string) error
}

func (f *fakeHub) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	if f.getManifest != nil {
		return f.getManifest(ctx, org, module, constraint)
	}
	return nil, fmt.Errorf("module not found")
}

func (f *fakeHub) ListAllVersions(ctx context.Context, org, module string) ([]VersionInfo, error) {
	if f.listVersions != nil {
		return f.listVersions(ctx, org, module)
	}
	return nil, nil
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
			getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
				return nil, fmt.Errorf("no matching version")
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
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
			getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org: org, Name: module, Version: "v1.0.0",
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
			getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org: org, Name: module, Version: "v1.0.0",
					URL:    "https://example.invalid/http-v1.0.0.wapp",
					Digest: "sha256:deadbeef",
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
			getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org: org, Name: module, Version: "v1.0.0",
					URL:    "https://example.invalid/http-v1.0.0.wapp",
					Digest: expectedDigest,
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

func TestDependencyHandler_ResolveTimeoutSetsDeadline(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	wappPath := filepath.Join(vendorDir, "acme", "http-v1.0.0.wapp")
	writeWapp(t, wappPath, []wapp.Entry{
		{
			ID:   wapp.NewID("mod", "svc"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})

	var resolveHadDeadline bool
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(callCtx context.Context, org, module, _ string) (*ModuleManifest, error) {
				deadline, ok := callCtx.Deadline()
				if ok && time.Until(deadline) > 0 {
					resolveHadDeadline = true
				}
				return &ModuleManifest{
					Org: org, Name: module, Version: "v1.0.0",
				}, nil
			},
		},
		Logger:         zap.NewNop(),
		VendorDir:      vendorDir,
		ResolveTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app", "dep"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"v1.0.0"}`, payload.JSON),
	}

	_, err = handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, nil)
	require.NoError(t, err)
	assert.True(t, resolveHadDeadline, "resolve call should have timeout deadline")
}

func TestDependencyHandler_DownloadTimeoutSetsDeadlinesForURLAndDownload(t *testing.T) {
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
	sum := sha256.Sum256(moduleData)
	expectedDigest := "sha256:" + hex.EncodeToString(sum[:])

	var urlHadDeadline bool
	var downloadHadDeadline bool

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org: org, Name: module, Version: "v1.0.0",
				}, nil
			},
			getDownload: func(callCtx context.Context, _ *DownloadParams) (*DownloadInfo, error) {
				deadline, ok := callCtx.Deadline()
				if ok && time.Until(deadline) > 0 {
					urlHadDeadline = true
				}
				return &DownloadInfo{
					URL:    "https://example.invalid/http-v1.0.0.wapp",
					Digest: expectedDigest,
					Size:   uint64(len(moduleData)),
				}, nil
			},
			downloadFile: func(callCtx context.Context, _ string, destPath string) error {
				deadline, ok := callCtx.Deadline()
				if ok && time.Until(deadline) > 0 {
					downloadHadDeadline = true
				}
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return err
				}
				return os.WriteFile(destPath, moduleData, 0600)
			},
		},
		Logger:          zap.NewNop(),
		VendorDir:       vendorDir,
		DownloadTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app", "dep"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"v1.0.0"}`, payload.JSON),
	}

	_, err = handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, nil)
	require.NoError(t, err)
	assert.True(t, urlHadDeadline, "download URL request should have timeout deadline")
	assert.True(t, downloadHadDeadline, "artifact download should have timeout deadline")
}

func TestDependencyHandler_Expand_UsesModuleDependencyEntriesForRequirementLinking(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	writeWapp(t, filepath.Join(vendorDir, "acme", "app-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.app", "bootloader"),
			Kind: regapi.NamespaceDependency,
			Data: map[string]any{
				"component": "wippy/bootloader",
				"version":   "v0.1.0",
				"parameters": []any{
					map[string]any{"name": "wippy.bootloader:env_storage", "value": "app:file_env"},
					map[string]any{"name": "wippy.bootloader:application_host", "value": "app:processes"},
				},
			},
		},
	})

	writeWapp(t, filepath.Join(vendorDir, "wippy", "bootloader-v0.1.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("wippy.bootloader", "env_storage"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{
					map[string]any{"entry": "app:service", "path": ".env_storage"},
				},
			},
		},
		{
			ID:   wapp.NewID("wippy.bootloader", "application_host"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{
					map[string]any{"entry": "app:service", "path": ".application_host"},
				},
			},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				switch {
				case org == "acme" && module == "app" && version == "v1.0.0":
					return &ModuleManifest{
						Org:     org,
						Name:    module,
						Version: version,
						Dependencies: []ManifestDep{
							{Org: "wippy", Name: "bootloader", Version: "v0.1.0"},
						},
					}, nil
				case org == "wippy" && module == "bootloader" && version == "v0.1.0":
					return &ModuleManifest{
						Org:     org,
						Name:    module,
						Version: version,
					}, nil
				default:
					return nil, fmt.Errorf("unexpected manifest request: %s/%s@%s", org, module, version)
				}
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	snapshot := regapi.State{
		{
			ID:   regapi.NewID("app", "service"),
			Kind: "process.lua",
			Data: payload.NewPayload(`{}`, payload.JSON),
		},
	}

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app", "root"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/app","version":"v1.0.0"}`, payload.JSON),
	}

	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: rootDep}, snapshot)
	require.NoError(t, err)
	assert.True(t, result.Applied)

	serviceID := regapi.NewID("app", "service")
	serviceUpdated := false
	for _, scoped := range result.Additional {
		if scoped.Operation.Kind != regapi.EntryUpdate || scoped.Operation.Entry.ID != serviceID {
			continue
		}
		serviceUpdated = true
		data, ok := scoped.Operation.Entry.Data.Data().(map[string]any)
		require.True(t, ok, "updated service data must be a map")
		assert.Equal(t, "app:file_env", data["env_storage"])
		assert.Equal(t, "app:processes", data["application_host"])
	}

	assert.True(t, serviceUpdated, "expected linked requirement values to update app:service")
}

func TestDependencyHandler_Expand_AppliesCanonicalComponentParametersToAliasNamespaceRequirements(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	writeWapp(t, filepath.Join(vendorDir, "butschster", "telegram-0.3.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("telegram", "env_storage"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{
					map[string]any{"entry": "telegram:webhook_url", "path": ".storage"},
				},
			},
		},
		{
			ID:   wapp.NewID("telegram", "webhook_url"),
			Kind: "env.variable",
			Data: map[string]any{},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org:     org,
					Name:    module,
					Version: version,
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "telegram"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{
			"component":"butschster/telegram",
			"version":"0.3.0",
			"parameters":[{"name":"butschster.telegram:env_storage","value":"app.env:file"}]
		}`, payload.JSON),
	}

	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: rootDep}, nil)
	require.NoError(t, err)
	assert.True(t, result.Applied)

	var webhookURL *regapi.Entry
	for _, scoped := range result.Additional {
		op := scoped.Operation
		if op.Kind == regapi.EntryCreate && op.Entry.ID == regapi.NewID("telegram", "webhook_url") {
			entry := op.Entry
			webhookURL = &entry
			break
		}
	}
	require.NotNil(t, webhookURL, "expected module env.variable to be created")
	data, ok := webhookURL.Data.Data().(map[string]any)
	require.True(t, ok, "created env.variable data must be a map")
	assert.Equal(t, "app.env:file", data["storage"])
}

func TestDependencyHandler_Expand_FailsBeforeRegistryApplyWhenRequirementTargetIsMissing(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	writeWapp(t, filepath.Join(vendorDir, "butschster", "telegram-0.3.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("telegram", "webhook_router"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{
					map[string]any{"entry": "telegram.handler:webhook_endpoint", "path": ".meta.router"},
				},
			},
		},
		{
			ID:   wapp.NewID("telegram.handler", "webhook.endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{"router": "telegram:router"},
			Data: map[string]any{"method": "POST", "path": "/webhook"},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org:     org,
					Name:    module,
					Version: version,
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "telegram"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{
			"component":"butschster/telegram",
			"version":"0.3.0",
			"parameters":[{"name":"butschster.telegram:webhook_router","value":"app:api"}]
		}`, payload.JSON),
	}

	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: rootDep}, nil)
	require.ErrorContains(t, err, "dependency pipeline failed")
	require.ErrorContains(t, err, "telegram.handler:webhook_endpoint")
	assert.False(t, result.Applied)
	assert.Empty(t, result.Additional)
}

func TestDependencyHandler_Expand_DoesNotFailOnUnrelatedSnapshotRequirement(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	writeWapp(t, filepath.Join(vendorDir, "acme", "tool-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.tool", "service"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org:     org,
					Name:    module,
					Version: version,
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	snapshot := regapi.State{
		{
			ID:   regapi.NewID("app", "unrelated_req"),
			Kind: regapi.NamespaceRequirement,
			Data: payload.NewPayload(`{"targets":[{"entry":"app:missing","path":".value"}]}`, payload.JSON),
		},
	}
	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "tool"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/tool","version":"v1.0.0"}`, payload.JSON),
	}

	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: rootDep}, snapshot)
	require.NoError(t, err)
	assert.True(t, result.Applied)

	createdService := false
	for _, scoped := range result.Additional {
		if scoped.Operation.Kind == regapi.EntryCreate && scoped.Operation.Entry.ID == regapi.NewID("acme.tool", "service") {
			createdService = true
		}
	}
	assert.True(t, createdService, "unrelated app requirements must not block dependency install")
}

// TestDependencyHandler_Expand_DoesNotReValidateUntouchedInstalledModule proves
// that installing one module does not strict-re-validate a different, already
// installed module that is unchanged. The installed module carries an
// unprovided, no-default requirement; touching an unrelated dependency must not
// turn that into a hard failure.
func TestDependencyHandler_Expand_DoesNotReValidateUntouchedInstalledModule(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	// Newly installed module (the op); no requirements.
	writeWapp(t, filepath.Join(vendorDir, "acme", "fresh-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.fresh", "service"),
			Kind: "process.lua",
			Data: map[string]any{"ok": true},
		},
	})
	// Already installed sibling module with an unprovided, no-default requirement.
	writeWapp(t, filepath.Join(vendorDir, "acme", "legacy-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.legacy", "needs_value"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{
					map[string]any{"entry": "acme.legacy:target", "path": ".value"},
				},
			},
		},
		{
			ID:   wapp.NewID("acme.legacy", "target"),
			Kind: "process.lua",
			Data: map[string]any{},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return &ModuleManifest{Org: org, Name: module, Version: version}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	snapshot := regapi.State{
		{
			ID:   regapi.NewID("app.deps", "legacy"),
			Kind: regapi.NamespaceDependency,
			Data: payload.NewPayload(`{"component":"acme/legacy","version":"v1.0.0"}`, payload.JSON),
		},
		// Module-owned entry stamping legacy's installed version into the snapshot.
		markModuleMeta(regapi.Entry{
			ID:   regapi.NewID("acme.legacy", "target"),
			Kind: "process.lua",
			Data: payload.NewPayload(`{}`, payload.JSON),
		}, "acme/legacy", "v1.0.0"),
	}

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "fresh"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/fresh","version":"v1.0.0"}`, payload.JSON),
	}

	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: rootDep}, snapshot)
	require.NoError(t, err, "installing an unrelated module must not re-validate an untouched installed module")
	assert.True(t, result.Applied)
}

// TestDependencyHandler_Expand_NewModuleMissingRequirementStillFails guards that
// scoping strict validation to touched modules does not relax validation of the
// module actually being installed: a new module with an unprovided, no-default
// requirement must still fail.
func TestDependencyHandler_Expand_NewModuleMissingRequirementStillFails(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	writeWapp(t, filepath.Join(vendorDir, "acme", "needy-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.needy", "needs_value"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{
					map[string]any{"entry": "acme.needy:target", "path": ".value"},
				},
			},
		},
		{
			ID:   wapp.NewID("acme.needy", "target"),
			Kind: "process.lua",
			Data: map[string]any{},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return &ModuleManifest{Org: org, Name: module, Version: version}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "needy"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/needy","version":"v1.0.0"}`, payload.JSON),
	}

	_, err = handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: rootDep}, regapi.State{})
	require.Error(t, err, "a newly installed module with an unprovided required value must fail")
}

func TestDependencyHandler_Expand_DeleteLastDependencyDoesNotFailOnUnrelatedSnapshotRequirement(t *testing.T) {
	ctx := newTestContext()

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "tool"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/tool","version":"v1.0.0"}`, payload.JSON),
	}
	moduleSvc := regapi.Entry{
		ID:   regapi.NewID("acme.tool", "service"),
		Kind: "service",
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        "acme/tool",
			metaModuleVersionKey: "v1.0.0",
		}),
		Data: payload.NewPayload(`{"ok":true}`, payload.JSON),
	}
	unrelatedReq := regapi.Entry{
		ID:   regapi.NewID("app", "unrelated_req"),
		Kind: regapi.NamespaceRequirement,
		Data: payload.NewPayload(`{"targets":[{"entry":"app:missing","path":".value"}]}`, payload.JSON),
	}

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: rootDep.ID}},
		regapi.State{rootDep, moduleSvc, unrelatedReq},
	)
	require.NoError(t, err)
	require.True(t, result.Applied)

	deletedService := false
	for _, scoped := range result.Additional {
		if scoped.Operation.Kind == regapi.EntryDelete && scoped.Operation.Entry.ID == moduleSvc.ID {
			deletedService = true
		}
	}
	assert.True(t, deletedService, "unrelated app requirements must not block dependency uninstall")
}

func TestDependencyHandler_Expand_DeleteRootDependencyIgnoresModuleOwnedDependencies(t *testing.T) {
	ctx := newTestContext()
	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "dummy"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"wippy/dummy","version":"v1.0.0"}`, payload.JSON),
	}
	moduleDep := regapi.Entry{
		ID:   regapi.NewID("wippy.dummy", "runtime_dependency"),
		Kind: regapi.NamespaceDependency,
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        "wippy/dummy",
			metaModuleVersionKey: "v1.0.0",
		}),
		Data: payload.NewPayload(`{"component":"missing/transitive","version":"v99.0.0"}`, payload.JSON),
	}
	moduleSvc := regapi.Entry{
		ID:   regapi.NewID("wippy.dummy", "service"),
		Kind: "service",
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        "wippy/dummy",
			metaModuleVersionKey: "v1.0.0",
		}),
		Data: payload.NewPayload(`{"ok":true}`, payload.JSON),
	}

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return nil, fmt.Errorf("module-owned dependency must not be resolved as a root: %s/%s@%s", org, module, version)
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: rootDep.ID}},
		regapi.State{rootDep, moduleDep, moduleSvc},
	)
	require.NoError(t, err)
	require.True(t, result.Applied)

	deleted := make(map[regapi.ID]bool)
	for _, scoped := range result.Additional {
		if scoped.Operation.Kind == regapi.EntryDelete {
			deleted[scoped.Operation.Entry.ID] = true
			assert.Equal(t, regapi.ScopeBaseline, scoped.Scope)
		}
	}
	assert.True(t, deleted[moduleDep.ID], "module-owned dependency entry should be removed with the module")
	assert.True(t, deleted[moduleSvc.ID], "module-owned service entry should be removed with the module")
}

func TestDependencyHandler_Expand_DeleteRootDependencyKeepsRemainingRoots(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	keepRoot := regapi.Entry{
		ID:   regapi.NewID("app.deps", "keep"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/keep","version":"v1.0.0"}`, payload.JSON),
	}
	deleteRoot := regapi.Entry{
		ID:   regapi.NewID("app.deps", "dummy"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"wippy/dummy","version":"v1.0.0"}`, payload.JSON),
	}
	keepSvc := regapi.Entry{
		ID:   regapi.NewID("acme.keep", "service"),
		Kind: "service",
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        "acme/keep",
			metaModuleVersionKey: "v1.0.0",
		}),
		Data: payload.NewPayload(`{"ok":true}`, payload.JSON),
	}
	removedDep := regapi.Entry{
		ID:   regapi.NewID("wippy.dummy", "runtime_dependency"),
		Kind: regapi.NamespaceDependency,
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        "wippy/dummy",
			metaModuleVersionKey: "v1.0.0",
		}),
		Data: payload.NewPayload(`{"component":"missing/transitive","version":"v99.0.0"}`, payload.JSON),
	}
	removedSvc := regapi.Entry{
		ID:   regapi.NewID("wippy.dummy", "service"),
		Kind: "service",
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        "wippy/dummy",
			metaModuleVersionKey: "v1.0.0",
		}),
		Data: payload.NewPayload(`{"ok":true}`, payload.JSON),
	}

	writeWapp(t, filepath.Join(vendorDir, "acme", "keep-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.keep", "service"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})

	var manifestRequests []string
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				manifestRequests = append(manifestRequests, org+"/"+module+"@"+version)
				if org == "acme" && module == "keep" && version == "v1.0.0" {
					return &ModuleManifest{Org: org, Name: module, Version: version}, nil
				}
				return nil, fmt.Errorf("unexpected manifest request: %s/%s@%s", org, module, version)
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: deleteRoot.ID}},
		regapi.State{keepRoot, deleteRoot, keepSvc, removedDep, removedSvc},
	)
	require.NoError(t, err)
	require.True(t, result.Applied)
	assert.Equal(t, []string{"acme/keep@v1.0.0"}, manifestRequests)

	deleted := make(map[regapi.ID]bool)
	for _, scoped := range result.Additional {
		if scoped.Operation.Kind == regapi.EntryDelete {
			deleted[scoped.Operation.Entry.ID] = true
		}
	}
	assert.True(t, deleted[removedDep.ID], "removed module dependency entry should be deleted")
	assert.True(t, deleted[removedSvc.ID], "removed module service entry should be deleted")
	assert.False(t, deleted[keepSvc.ID], "remaining module entries must not be deleted")
}

func TestDependencyHandler_CollectDesiredDependencies_IgnoresModuleOwnedDependencies(t *testing.T) {
	ctx := newTestContext()
	transcoder := payload.GetTranscoder(ctx)
	require.NotNil(t, transcoder)

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "root"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/root","version":"v1.0.0"}`, payload.JSON),
	}
	moduleDep := regapi.Entry{
		ID:   regapi.NewID("acme.root", "child"),
		Kind: regapi.NamespaceDependency,
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/root"}),
		Data: payload.NewPayload(`{"component":"acme/child","version":"v1.0.0"}`, payload.JSON),
	}

	deps, err := handler.collectDesiredDependencies(ctx,
		regapi.Operation{Kind: regapi.EntryUpdate, Entry: rootDep},
		regapi.State{rootDep, moduleDep},
		transcoder,
	)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, rootDep.ID, deps[0].entry.ID)
	assert.Equal(t, "acme/root", deps[0].definition.Component)
}

func TestDependencyHandler_CollectDesiredDependencies_PinsExistingInstalledVersions(t *testing.T) {
	ctx := newTestContext()
	transcoder := payload.GetTranscoder(ctx)
	require.NotNil(t, transcoder)

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	existingDep := regapi.Entry{
		ID:   regapi.NewID("app", "facade"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"wippy/facade","version":">=v0.5.16"}`, payload.JSON),
	}
	newDep := regapi.Entry{
		ID:   regapi.NewID("app", "dummy"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"wippy/dummy","version":">=v0.0.0"}`, payload.JSON),
	}
	snapshot := regapi.State{
		existingDep,
		{
			ID:   regapi.NewID("wippy.facade", "public_files"),
			Kind: "fs.directory",
			Meta: attrs.NewBagFrom(map[string]any{
				metaModuleKey:        "wippy/facade",
				metaModuleVersionKey: "0.5.39",
			}),
			Data: payload.NewPayload(`{"path":"./static"}`, payload.JSON),
		},
	}

	deps, err := handler.collectDesiredDependencies(ctx,
		regapi.Operation{Kind: regapi.EntryCreate, Entry: newDep},
		snapshot,
		transcoder,
	)
	require.NoError(t, err)

	versions := make(map[string]string)
	for _, dep := range deps {
		versions[dep.definition.Component] = dep.definition.Version
	}
	assert.Equal(t, "0.5.39", versions["wippy/facade"])
	assert.Equal(t, ">=v0.0.0", versions["wippy/dummy"])
}

func TestDependencyHandler_ResolveModules_PinsInstalledTransitiveVersions(t *testing.T) {
	ctx := newTestContext()

	var manifestRequests []string
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			listVersions: func(_ context.Context, org, module string) ([]VersionInfo, error) {
				switch org + "/" + module {
				case "wippy/facade":
					return []VersionInfo{
						{Version: "0.5.39"},
						{Version: "0.6.0"},
					}, nil
				default:
					return nil, fmt.Errorf("unexpected version list request: %s/%s", org, module)
				}
			},
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				manifestRequests = append(manifestRequests, org+"/"+module+"@"+version)
				switch org + "/" + module + "@" + version {
				case "acme/app@1.0.0":
					return &ModuleManifest{
						Org:     org,
						Name:    module,
						Version: version,
						Dependencies: []ManifestDep{
							{Org: "wippy", Name: "facade", Version: ">=0.5.0"},
						},
					}, nil
				case "wippy/facade@0.5.39":
					return &ModuleManifest{Org: org, Name: module, Version: version}, nil
				case "wippy/facade@0.6.0":
					return &ModuleManifest{Org: org, Name: module, Version: version}, nil
				default:
					return nil, fmt.Errorf("unexpected manifest request: %s/%s@%s", org, module, version)
				}
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	modules, err := handler.resolveModules(ctx,
		[]DependencyDefinition{{Component: "acme/app", Version: "1.0.0"}},
		map[string]string{"wippy/facade": "0.5.39"},
	)

	require.NoError(t, err)
	require.Len(t, modules, 2)
	assert.Equal(t, "acme", modules[0].Org)
	assert.Equal(t, "app", modules[0].Name)
	assert.Equal(t, "1.0.0", modules[0].Version)
	assert.Equal(t, "wippy", modules[1].Org)
	assert.Equal(t, "facade", modules[1].Name)
	assert.Equal(t, "0.5.39", modules[1].Version)
	assert.Contains(t, manifestRequests, "wippy/facade@0.5.39")
	assert.NotContains(t, manifestRequests, "wippy/facade@0.6.0")
}

func TestDependencyHandler_CollectDesiredDependencies_DoesNotPinUpdatedDependency(t *testing.T) {
	ctx := newTestContext()
	transcoder := payload.GetTranscoder(ctx)
	require.NotNil(t, transcoder)

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	oldDep := regapi.Entry{
		ID:   regapi.NewID("app", "facade"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"wippy/facade","version":">=v0.5.16"}`, payload.JSON),
	}
	updatedDep := regapi.Entry{
		ID:   oldDep.ID,
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"wippy/facade","version":">=v0.6.0"}`, payload.JSON),
	}
	snapshot := regapi.State{
		oldDep,
		{
			ID:   regapi.NewID("wippy.facade", "public_files"),
			Kind: "fs.directory",
			Meta: attrs.NewBagFrom(map[string]any{
				metaModuleKey:        "wippy/facade",
				metaModuleVersionKey: "0.5.39",
			}),
			Data: payload.NewPayload(`{"path":"./static"}`, payload.JSON),
		},
	}

	deps, err := handler.collectDesiredDependencies(ctx,
		regapi.Operation{Kind: regapi.EntryUpdate, Entry: updatedDep},
		snapshot,
		transcoder,
	)
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "wippy/facade", deps[0].definition.Component)
	assert.Equal(t, ">=v0.6.0", deps[0].definition.Version)
}

func TestDependencyHandler_CollectControlledModules_FollowsInstalledDependencyLinks(t *testing.T) {
	ctx := newTestContext()
	transcoder := payload.GetTranscoder(ctx)
	require.NotNil(t, transcoder)

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	rootDep := regapi.Entry{
		ID:   regapi.NewID("app.deps", "root"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/app","version":"1.0.0"}`, payload.JSON),
	}
	appDep := regapi.Entry{
		ID:   regapi.NewID("acme.app", "lib_dep"),
		Kind: regapi.NamespaceDependency,
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/app"}),
		Data: payload.NewPayload(`{"component":"acme/lib","version":"1.0.0"}`, payload.JSON),
	}
	libDep := regapi.Entry{
		ID:   regapi.NewID("acme.lib", "core_dep"),
		Kind: regapi.NamespaceDependency,
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/lib"}),
		Data: payload.NewPayload(`{"component":"acme/core","version":"1.0.0"}`, payload.JSON),
	}
	lockLoadedDep := regapi.Entry{
		ID:   regapi.NewID("keeper.internal", "helper_dep"),
		Kind: regapi.NamespaceDependency,
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "keeper/keeper"}),
		Data: payload.NewPayload(`{"component":"keeper/helper","version":"1.0.0"}`, payload.JSON),
	}

	controlled, err := handler.collectControlledModules(ctx, regapi.State{rootDep, appDep, libDep, lockLoadedDep}, transcoder)
	require.NoError(t, err)
	assert.Contains(t, controlled, "acme/app")
	assert.Contains(t, controlled, "acme/lib")
	assert.Contains(t, controlled, "acme/core")
	assert.NotContains(t, controlled, "keeper/keeper")
	assert.NotContains(t, controlled, "keeper/helper")
}

func TestDependencyHandler_Expand_PreservesLockLoadedModuleEntries(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	writeWapp(t, filepath.Join(vendorDir, "acme", "http-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.http", "svc"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org:     org,
					Name:    module,
					Version: version,
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	lockLoadedID := regapi.NewID("keeper.hub.tools", "dependencies")
	snapshot := regapi.State{
		{
			ID:   lockLoadedID,
			Kind: "function.lua",
			Meta: attrs.NewBagFrom(map[string]any{
				metaModuleKey:        "keeper/keeper",
				metaModuleVersionKey: "0.5.2",
			}),
			Data: payload.New("return {}"),
		},
	}

	depEntry := regapi.Entry{
		ID:   regapi.NewID("app.deps", "http"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"v1.0.0"}`, payload.JSON),
	}

	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, snapshot)
	require.NoError(t, err)
	assert.True(t, result.Applied)

	createdModuleEntry := false
	for _, scoped := range result.Additional {
		op := scoped.Operation
		require.NotEqual(t, lockLoadedID, op.Entry.ID, "lock-loaded Keeper entry must not be touched")
		if op.Kind == regapi.EntryCreate && op.Entry.ID == regapi.NewID("acme.http", "svc") {
			createdModuleEntry = true
		}
	}
	assert.True(t, createdModuleEntry, "new dependency module entry should still be created")
}

func TestDependencyHandler_Expand_UsesLockReplacementForExistingRootDependency(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, ".wippy", "vendor")
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	localKeeper := filepath.Join(tmpDir, "local-keeper")
	uiStaticID := regapi.NewID("keeper.components", "ui_static_fs")

	require.NoError(t, os.MkdirAll(localKeeper, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(localKeeper, "_index.json"), []byte(`{
  "namespace": "keeper.components",
  "entries": [
    {
      "name": "ui_static_fs",
      "kind": "fs.directory",
      "meta": {
        "module": "keeper/keeper",
        "module_version": "0.5.4"
      },
      "path": "./static/keeper"
    }
  ]
}`), 0600))
	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
  modules: .wippy
  src: ./src
modules:
  - name: keeper/keeper
    version: 0.5.4
  - name: acme/http
    version: v1.0.0
replacements:
  - from: keeper/keeper
    to: ./local-keeper
`), 0600))

	writeWapp(t, filepath.Join(vendorDir, "acme", "http-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("acme.http", "svc"),
			Kind: "service",
			Data: map[string]any{"ok": true},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				return &ModuleManifest{
					Org:     org,
					Name:    module,
					Version: version,
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	transcoder := payload.GetTranscoder(ctx)
	require.NotNil(t, transcoder)
	keeperEntries, err := handler.loadEntriesForModule(ctx, transcoder, ResolvedModule{
		Org:     "keeper",
		Name:    "keeper",
		Version: "0.5.4",
	})
	require.NoError(t, err)
	require.NotEmpty(t, keeperEntries)
	loadedUIStatic := false
	for _, entry := range keeperEntries {
		if entry.ID == uiStaticID {
			loadedUIStatic = true
			require.Equal(t, regapi.Kind("fs.directory"), entry.Kind)
		}
	}
	require.True(t, loadedUIStatic)

	snapshot := regapi.State{
		{
			ID:   regapi.NewID("app.deps", "keeper"),
			Kind: regapi.NamespaceDependency,
			Data: payload.NewPayload(`{"component":"keeper/keeper","version":"0.5.4"}`, payload.JSON),
		},
		{
			ID:   uiStaticID,
			Kind: "fs.directory",
			Meta: attrs.NewBagFrom(map[string]any{
				metaModuleKey:        "keeper/keeper",
				metaModuleVersionKey: "0.5.4",
			}),
			Data: payload.NewPayload(`{"path":"./static/keeper"}`, payload.JSON),
		},
	}
	depEntry := regapi.Entry{
		ID:   regapi.NewID("app.deps", "http"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/http","version":"v1.0.0"}`, payload.JSON),
	}

	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryCreate, Entry: depEntry}, snapshot)
	require.NoError(t, err)
	assert.True(t, result.Applied)

	createdModuleEntry := false
	for _, scoped := range result.Additional {
		op := scoped.Operation
		require.NotEqual(t, uiStaticID, op.Entry.ID, "unrelated install must not rewrite existing local replacement entries")
		if op.Kind == regapi.EntryCreate && op.Entry.ID == regapi.NewID("acme.http", "svc") {
			createdModuleEntry = true
		}
	}
	assert.True(t, createdModuleEntry, "new dependency module entry should still be created")
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
