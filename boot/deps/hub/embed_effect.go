// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	regapi "github.com/wippyai/runtime/api/registry"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// embedPackRegistry is the subset of the embed registry used to manage the
// lifecycle of module-owned .wapp packs. It is satisfied by
// *service/fs/embed.Registry and stubbed in tests.
type embedPackRegistry interface {
	RegisterPack(packPath, module, version string, reader *wapp.Reader, file *os.File) error
	UnregisterPack(packPath string) error
	UnregisterModule(module, version string) error
	RetargetModule(module, fromVersion, toVersion string) error
}

// stagedPack describes a new module pack to register during Prepare and to
// roll back (unregister + close) if the apply fails.
type stagedPack struct {
	packPath string
	module   string
	version  string
}

// obsoletePack identifies a module pack to unregister (and close) on Finalize
// because the module was removed or replaced by a newer version.
type obsoletePack struct {
	module  string
	version string
}

// embedPackEffect ties embedded-pack lifecycle to a module operation. Embedded
// packs are module resources owned by dependency expansion, not by fs.embed
// entries, so this effect runs alongside the registry changeset:
//
//   - Prepare registers newly resolved packs so the fs.embed entries created in
//     the same changeset resolve during apply. New packs are keyed by their own
//     .wapp path, so a version update stages the new pack without disturbing the
//     pack still serving the old version.
//   - Commit activates the staged set but remains reversible.
//   - Finalize unregisters and closes obsolete packs only after the registry
//     history/head is durable. The new packs stay registered.
//   - Rollback unregisters and closes the packs staged in Prepare, restoring the
//     pre-operation set. Obsolete packs are left untouched because they are only
//     dropped during Finalize after a successful durable commit.
// packRetarget repoints filesystems served for a module from the superseded
// pack generation to the adopted one before the superseded pack closes.
type packRetarget struct {
	module string
	from   string
	to     string
}

type embedPackEffect struct {
	reg      embedPackRegistry
	staged   []stagedPack
	obsolete []obsoletePack
	retarget []packRetarget
	logger   *zap.Logger

	prepared []string // pack paths registered by Prepare, for rollback
}

func (e *embedPackEffect) Prepare(_ context.Context) error {
	for _, sp := range e.staged {
		f, err := os.Open(sp.packPath)
		if err != nil {
			e.rollbackPrepared()
			return newOpenPackError(sp.packPath, err)
		}

		reader, err := wapp.NewReader(f)
		if err != nil {
			f.Close()
			e.rollbackPrepared()
			return newReadPackError(sp.packPath, err)
		}

		if err := e.reg.RegisterPack(sp.packPath, sp.module, sp.version, reader, f); err != nil {
			f.Close()
			e.rollbackPrepared()
			return newRegisterPackError(sp.packPath, err)
		}
		e.prepared = append(e.prepared, sp.packPath)

		e.logger.Debug("staged embedded pack",
			zap.String("path", sp.packPath),
			zap.String("module", sp.module),
			zap.String("version", sp.version))
	}
	return nil
}

func (e *embedPackEffect) Commit(_ context.Context) error {
	return nil
}

func (e *embedPackEffect) Finalize(_ context.Context) error {
	var errs []error
	// Entries whose content did not change receive no event during the
	// transition, so filesystems cached for them still serve the superseded
	// pack. Repointing them precedes closing that pack.
	for _, rt := range e.retarget {
		if err := e.reg.RetargetModule(rt.module, rt.from, rt.to); err != nil {
			e.logger.Warn("failed to retarget embedded pack consumers",
				zap.String("module", rt.module),
				zap.String("from", rt.from),
				zap.String("to", rt.to),
				zap.Error(err))
			errs = append(errs, fmt.Errorf("retarget embedded pack %s %s->%s: %w", rt.module, rt.from, rt.to, err))
		}
	}
	for _, op := range e.obsolete {
		if err := e.reg.UnregisterModule(op.module, op.version); err != nil {
			// The changeset is already durable, so callers only report this as a
			// cleanup warning. Return it for observability instead of pretending
			// the obsolete handle was released.
			e.logger.Warn("failed to unregister obsolete embedded pack",
				zap.String("module", op.module),
				zap.String("version", op.version),
				zap.Error(err))
			errs = append(errs, fmt.Errorf("unregister embedded pack %s@%s: %w", op.module, op.version, err))
			continue
		}
		e.logger.Debug("released obsolete embedded pack",
			zap.String("module", op.module),
			zap.String("version", op.version))
	}
	return errors.Join(errs...)
}

func (e *embedPackEffect) Rollback(_ context.Context) error {
	e.rollbackPrepared()
	return nil
}

func (e *embedPackEffect) rollbackPrepared() {
	for _, packPath := range e.prepared {
		if err := e.reg.UnregisterPack(packPath); err != nil {
			e.logger.Warn("failed to unregister staged embedded pack during rollback",
				zap.String("path", packPath),
				zap.Error(err))
		}
	}
	e.prepared = nil
}

// buildEmbedPackEffect computes the pack-lifecycle effect for a module
// operation. resolved are the desired modules after the operation; snapshot is
// the registry state before it. controlled limits removals to modules owned by
// dependency roots participating in this operation. staged packs are the
// resolved modules backed by a .wapp on disk; obsolete packs are controlled
// modules whose version is no longer desired (removed) or changed (updated).
//
// Returns nil when there is no embed registry in the context, or when neither
// staged nor obsolete packs exist, so callers can append it unconditionally.
func (h *DependencyHandler) buildEmbedPackEffect(
	ctx context.Context,
	resolved []ResolvedModule,
	snapshot regapi.ProvenancedState,
	controlled map[string]struct{},
) (*embedPackEffect, error) {
	reg := embedpkg.GetRegistryFromContext(ctx)
	if reg == nil {
		return nil, nil
	}

	desired := make(map[string]string, len(resolved))
	installed := residentModuleVersions(snapshot.Prov)
	installedDigests := residentModuleDigests(snapshot.Prov)
	staged := make([]stagedPack, 0, len(resolved))
	for _, mod := range resolved {
		name := mod.Org + "/" + mod.Name
		if h.moduleUsesDirectoryMode(name) {
			continue
		}

		if installed[name] == mod.Version && reg.HasModulePack(name, mod.Version) {
			// The embed registry addresses packs by module and version. Replacing a
			// pack with different bytes at the same identity would close the old
			// handle during Prepare, so Rollback could not restore it. Fail closed
			// when both generations have durable identities; legacy snapshots that
			// predate module digests retain the existing behavior.
			if installedDigest := installedDigests[name]; installedDigest != "" && mod.Digest != "" &&
				!artifactDigestsEqual(installedDigest, mod.Digest) {
				return nil, NewDependencyIntegrityError(
					modKey(mod),
					errors.New("cannot replace an active embedded pack at the same module version"),
					mod.Digest,
					mod.SizeBytes,
				)
			}
			desired[name] = mod.Version
			continue
		}

		path, isWapp, err := h.modulePackPath(ctx, mod)
		if err != nil {
			return nil, err
		}
		if !isWapp {
			continue
		}
		desired[name] = mod.Version
		staged = append(staged, stagedPack{
			packPath: path,
			module:   name,
			version:  mod.Version,
		})
	}

	obsolete := obsoletePacksFor(installed, desired, controlled)

	retargets := make([]packRetarget, 0, len(obsolete))
	for _, op := range obsolete {
		if to := desired[op.module]; to != "" && to != op.version {
			retargets = append(retargets, packRetarget{module: op.module, from: op.version, to: to})
		}
	}

	if len(staged) == 0 && len(obsolete) == 0 {
		return nil, nil
	}

	logger := h.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	return &embedPackEffect{
		reg:      reg,
		staged:   staged,
		obsolete: obsolete,
		retarget: retargets,
		logger:   logger.Named("embed_pack"),
	}, nil
}

// obsoletePacksFor returns controlled resident modules whose version is not the
// desired version (changed) or which are no longer desired at all (removed).
// Packs outside controlled are unrelated to this dependency operation and must
// remain live. A staged pack for the new version is keyed by its own path, so
// unregistering the old version never affects the new pack.
func obsoletePacksFor(current map[string]string, desired map[string]string, controlled map[string]struct{}) []obsoletePack {
	obsolete := make([]obsoletePack, 0)
	for module, version := range current {
		if _, ok := controlled[module]; !ok {
			continue
		}
		if version == "" {
			continue
		}
		if want, ok := desired[module]; ok && want == version {
			continue
		}
		obsolete = append(obsolete, obsoletePack{module: module, version: version})
	}
	return obsolete
}

// modulePackPath returns the on-disk path the module would be registered under
// and whether that path is a .wapp pack. Replacement (local source) and
// unpacked modules are directories and are reported as non-pack.
func (h *DependencyHandler) modulePackPath(ctx context.Context, mod ResolvedModule) (string, bool, error) {
	if h.moduleUsesDirectoryMode(mod.Org + "/" + mod.Name) {
		return "", false, nil
	}
	path, err := h.ensureModuleAvailable(ctx, mod)
	if err != nil {
		return "", false, err
	}
	return path, filepath.Ext(path) == ".wapp", nil
}

func newOpenPackError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "open embedded pack failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path})).
		WithCause(cause)
}

func newReadPackError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "read embedded pack failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path})).
		WithCause(cause)
}

func newRegisterPackError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "register embedded pack failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path})).
		WithCause(cause)
}
