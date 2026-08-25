// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// TestDependencyHandlerColdBootInstallsLegacyPromotedRoot covers the complete
// onboarding boundary. A pre-provenance history update promotes a dependency
// declared by an installed module to an application root. Cold boot must both
// retain that promotion and materialize the newly required module from Hub.
func TestDependencyHandlerColdBootInstallsLegacyPromotedRoot(t *testing.T) {
	ctx := newTestContext()
	vendorDir := filepath.Join(t.TempDir(), "vendor")
	artifact := buildWappBytes(t, []wapp.Entry{{
		ID:   wapp.NewID("kickside.kb10", "service"),
		Kind: "service",
		Data: map[string]any{"ready": true},
	}})
	sum := sha256.Sum256(artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	downloads := 0
	client := &fakeHub{
		getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
			require.Equal(t, "kickside", org)
			require.Equal(t, "kb10", module)
			require.Equal(t, "1.0.0", constraint)
			return &ModuleManifest{
				Org: org, Name: module, Version: constraint, VersionID: constraint,
				Digest: digest, SizeBytes: uint64(len(artifact)), URL: "memory://kickside/kb10",
			}, nil
		},
		downloadFile: func(_ context.Context, _ string, destination string) error {
			downloads++
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destination, artifact, 0o600)
		},
	}

	dependencyID := regapi.NewID("kickside.knowledge.requirements", "kb10")
	dependency := regapi.Entry{
		ID:   dependencyID,
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{
			"component": "kickside/kb10",
			"version":   "1.0.0",
		}),
	}
	baseline := regapi.ProvenancedState{
		Entries: regapi.State{dependency},
		Provenance: regapi.ProvenanceMap{
			dependencyID: {Module: "kickside/knowledge", Version: "1.0.0", Digest: "sha256:knowledge"},
		},
	}
	legacyUpdate := dependency
	legacyUpdate.DependencyRoot = true
	v0 := version.New(regapi.RootVersion)
	v1 := version.FromParent(v0, 1)
	history := historymem.New()
	require.NoError(t, history.Save(v1, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: legacyUpdate}}, true))

	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: client, Logger: zap.NewNop(), Resolver: resolver,
		LockPath: filepath.Join(t.TempDir(), "wippy.lock"), VendorDir: vendorDir,
	})
	require.NoError(t, err)
	reg := registryimpl.NewRegistry(
		history,
		&bootRecordingRunner{},
		topology.NewStateBuilder(zap.NewNop(), resolver),
		resolver,
		zap.NewNop(),
		registryimpl.WithKindDirective(
			regapi.NamespaceDependency,
			regexp.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution),
		),
	)

	require.NoError(t, reg.LoadState(ctx, baseline, v1))
	require.Equal(t, []regapi.ID{dependencyID}, reg.DependencyRoots())
	require.Equal(t, 1, downloads, "cold onboarding must fetch the newly required KB10 artifact")
	installed, err := reg.GetEntry(regapi.NewID("kickside.kb10", "service"))
	require.NoError(t, err)
	require.Equal(t, true, installed.Data.Data().(map[string]any)["ready"])
	record, ok := reg.EntryProvenance(installed.ID)
	require.True(t, ok)
	require.Equal(t, "kickside/kb10", record.Module)
}
