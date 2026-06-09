// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/auth"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	entrypkg "github.com/wippyai/runtime/internal/entry"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

const (
	metaModuleKey        = "module"
	metaModuleVersionKey = "module_version"
)

type DependencyHandlerOptions struct {
	Hub             HubClient
	Logger          *zap.Logger
	LockPath        string
	VendorDir       string
	ResolveTimeout  time.Duration
	DownloadTimeout time.Duration
}

type DependencyHandler struct {
	hub             HubClient
	manifestCache   *ManifestCache
	logger          *zap.Logger
	lockPath        string
	vendorDir       string
	resolveTimeout  time.Duration
	downloadTimeout time.Duration
}

// HubClient defines the hub operations required for dependency handling.
//
//nolint:revive // keeps explicit package-disambiguated API name.
type HubClient interface {
	ManifestProvider
	GetDownloadURL(ctx context.Context, params *DownloadParams) (*DownloadInfo, error)
	DownloadToFile(ctx context.Context, url, destPath string) error
}

// DependencyDefinition represents the data structure of an ns.dependency entry.
type DependencyDefinition struct {
	Component  string      `json:"component" yaml:"component"`
	Version    string      `json:"version" yaml:"version"`
	Parameters []Parameter `json:"parameters" yaml:"parameters"`
}

// Parameter represents a single parameter in a dependency definition.
type Parameter struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type desiredDependency struct {
	entry      regapi.Entry
	definition DependencyDefinition
}

func NewDependencyHandler(opts DependencyHandlerOptions) (*DependencyHandler, error) {
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	client := opts.Hub
	if client == nil {
		hubClient, err := newHubClientFromAuth()
		if err != nil {
			return nil, err
		}
		client = hubClient
	}

	lockPath := opts.LockPath
	if lockPath == "" {
		if found, err := lock.Find(".", lock.DefaultFilename); err == nil {
			lockPath = found
		}
	}

	vendorDir := opts.VendorDir
	if vendorDir == "" && lockPath != "" {
		if lockObj, err := lock.New(lockPath); err == nil {
			lockDir := filepath.Dir(lockObj.Path())
			vendorDir = filepath.Join(lockDir, lockObj.GetVendorPath())
		}
	}
	if vendorDir == "" {
		vendorDir = filepath.Join(".wippy", "vendor")
	}

	return &DependencyHandler{
		hub:             client,
		manifestCache:   NewManifestCache(client),
		logger:          logger,
		lockPath:        lockPath,
		vendorDir:       vendorDir,
		resolveTimeout:  opts.ResolveTimeout,
		downloadTimeout: opts.DownloadTimeout,
	}, nil
}

func (h *DependencyHandler) Expand(ctx context.Context, op regapi.Operation, snapshot regapi.State) (regapi.DirectiveResult, error) {
	if h == nil || h.hub == nil {
		return regapi.DirectiveResult{}, ErrDependencyHandlerNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return regapi.DirectiveResult{}, err
	}

	entry, ok := resolveOperationEntry(op, snapshot)
	if !ok {
		return regapi.DirectiveResult{}, nil
	}
	if entry.Kind != regapi.NamespaceDependency {
		return regapi.DirectiveResult{}, nil
	}
	if !isRootDependency(entry) {
		return regapi.DirectiveResult{}, nil
	}

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return regapi.DirectiveResult{}, ErrDependencyTranscoderMissing
	}

	overallStart := time.Now()

	step := time.Now()
	lockedVersions := snapshotModuleVersions(snapshot)
	dLockedVersions := time.Since(step)

	step = time.Now()
	controlledModules, err := h.collectControlledModules(ctx, snapshot, transcoder)
	dCollectControlled := time.Since(step)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	step = time.Now()
	desiredDeps, err := h.collectDesiredDependencies(ctx, op, snapshot, transcoder)
	dCollectDesired := time.Since(step)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	desiredDepEntries := make([]regapi.Entry, 0, len(desiredDeps))
	for _, dep := range desiredDeps {
		desiredDepEntries = append(desiredDepEntries, dep.entry)
	}

	var resolved []ResolvedModule
	desiredRoots := dependencyDefinitions(desiredDeps)
	step = time.Now()
	if len(desiredRoots) > 0 {
		var err error
		resolved, err = h.resolveModules(ctx, desiredRoots, lockedVersions)
		if err != nil {
			return regapi.DirectiveResult{}, err
		}
	}
	dResolveModules := time.Since(step)

	step = time.Now()
	moduleEntries, err := h.loadModuleEntries(ctx, resolved, transcoder)
	dLoadEntries := time.Since(step)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	linkDeps := mergeLinkDependencies(desiredDepEntries, moduleEntries)
	opComponent := ""
	for _, dep := range desiredDeps {
		if dep.entry.ID == op.Entry.ID {
			opComponent = dep.definition.Component
			break
		}
	}
	strictModules := touchedModuleNames(resolved, snapshot, opComponent)
	step = time.Now()
	mutableModules, err := h.operationModules(ctx, op, snapshot, transcoder)
	dOpModules := time.Since(step)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	step = time.Now()
	combined := make([]regapi.Entry, 0, len(snapshot)+len(moduleEntries))
	for _, e := range snapshot {
		if entryModule(e) != "" {
			continue
		}
		combined = append(combined, e)
	}
	combined = append(combined, moduleEntries...)
	dCombineFilter := time.Since(step)

	step = time.Now()
	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(stages.WithDependencies(linkDeps), stages.WithStrictRequirementModules(strictModules)),
		stages.Override(),
	)
	if err := pipeline.Execute(ctx, &combined); err != nil {
		return regapi.DirectiveResult{}, NewDependencyPipelineError(err)
	}
	dPipeline := time.Since(step)

	step = time.Now()
	additional, err := buildOperations(snapshot, combined, op.Entry.ID, controlledModules, mutableModules)
	dBuildOps := time.Since(step)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	if h.logger != nil {
		h.logger.Info("hub Expand phase timings",
			zap.String("entry_id", op.Entry.ID.String()),
			zap.Duration("total", time.Since(overallStart)),
			zap.Duration("locked_versions", dLockedVersions),
			zap.Duration("collect_controlled", dCollectControlled),
			zap.Duration("collect_desired", dCollectDesired),
			zap.Duration("resolve_modules", dResolveModules),
			zap.Duration("load_module_entries", dLoadEntries),
			zap.Duration("op_modules", dOpModules),
			zap.Duration("combine_filter", dCombineFilter),
			zap.Duration("pipeline_execute", dPipeline),
			zap.Duration("build_ops", dBuildOps),
			zap.Int("snapshot_entries", len(snapshot)),
			zap.Int("combined_entries", len(combined)),
		)
	}

	scoped := make([]regapi.ScopedOperation, 0, len(additional))
	for _, op := range additional {
		scoped = append(scoped, regapi.ScopedOperation{
			Operation: op,
			Scope:     regapi.ScopeBaseline,
		})
	}

	return regapi.DirectiveResult{
		Applied:    true,
		Additional: scoped,
	}, nil
}

func (h *DependencyHandler) collectSnapshotDependencies(
	ctx context.Context,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) ([]desiredDependency, error) {
	deps := make([]desiredDependency, 0)
	for _, entry := range snapshot {
		if !isRootDependency(entry) {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}
		deps = append(deps, desiredDependency{
			entry:      entry,
			definition: def,
		})
	}
	return deps, nil
}

func (h *DependencyHandler) collectControlledModules(
	ctx context.Context,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) (map[string]struct{}, error) {
	controlled := make(map[string]struct{})
	dependencyLinks := make(map[string][]string)

	for _, entry := range snapshot {
		if entry.Kind != regapi.NamespaceDependency {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}

		if owner := entryModule(entry); owner != "" {
			dependencyLinks[owner] = append(dependencyLinks[owner], def.Component)
			continue
		}
		controlled[def.Component] = struct{}{}
	}

	queue := make([]string, 0, len(controlled))
	for module := range controlled {
		queue = append(queue, module)
	}
	for len(queue) > 0 {
		module := queue[0]
		queue = queue[1:]
		for _, dep := range dependencyLinks[module] {
			if _, seen := controlled[dep]; seen {
				continue
			}
			controlled[dep] = struct{}{}
			queue = append(queue, dep)
		}
	}

	return controlled, nil
}

func dependencyDefinitions(deps []desiredDependency) []DependencyDefinition {
	roots := make([]DependencyDefinition, 0, len(deps))
	for _, dep := range deps {
		roots = append(roots, dep.definition)
	}
	return roots
}

func (h *DependencyHandler) collectDesiredDependencies(
	ctx context.Context,
	op regapi.Operation,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) ([]desiredDependency, error) {
	deps := make(map[regapi.ID]desiredDependency)
	lockedVersions := snapshotModuleVersions(snapshot)
	operationID := op.Entry.ID

	current, err := h.collectSnapshotDependencies(ctx, snapshot, transcoder)
	if err != nil {
		return nil, err
	}
	for _, dep := range current {
		if dep.entry.ID != operationID {
			dep.definition = pinExistingDependencyVersion(dep.definition, lockedVersions)
		}
		deps[dep.entry.ID] = dep
	}

	switch op.Kind {
	case regapi.EntryDelete:
		delete(deps, op.Entry.ID)
	case regapi.EntryCreate, regapi.EntryUpdate:
		entry, ok := resolveOperationEntry(op, snapshot)
		if !ok {
			return nil, NewDependencyEntryMissingError(op.Entry.ID.String())
		}
		if !isRootDependency(entry) {
			break
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		deps[entry.ID] = desiredDependency{
			entry:      entry,
			definition: def,
		}
	}

	result := make([]desiredDependency, 0, len(deps))
	for id, dep := range deps {
		if dep.definition.Component == "" {
			return nil, NewDependencyEntryInvalidError(id.String(), "component is required", "")
		}
		result = append(result, dep)
	}
	return result, nil
}

func snapshotModuleVersions(snapshot regapi.State) map[string]string {
	versions := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, entry := range snapshot {
		module := entryModule(entry)
		if module == "" || entry.Meta == nil {
			continue
		}
		raw, ok := entry.Meta[metaModuleVersionKey]
		if !ok {
			continue
		}
		version, ok := raw.(string)
		if !ok || version == "" {
			continue
		}
		if _, bad := ambiguous[module]; bad {
			continue
		}
		if existing, seen := versions[module]; seen && existing != version {
			delete(versions, module)
			ambiguous[module] = struct{}{}
			continue
		}
		versions[module] = version
	}
	return versions
}

func pinExistingDependencyVersion(def DependencyDefinition, moduleVersions map[string]string) DependencyDefinition {
	if def.Component == "" {
		return def
	}
	if version := moduleVersions[def.Component]; version != "" {
		def.Version = version
	}
	return def
}

func mergeLinkDependencies(explicitDeps, moduleEntries []regapi.Entry) []regapi.Entry {
	merged := make([]regapi.Entry, 0, len(explicitDeps)+len(moduleEntries))
	seen := make(map[regapi.ID]struct{}, len(explicitDeps)+len(moduleEntries))

	appendDep := func(entry regapi.Entry) {
		if entry.Kind != regapi.NamespaceDependency {
			return
		}
		if _, ok := seen[entry.ID]; ok {
			return
		}
		seen[entry.ID] = struct{}{}
		merged = append(merged, entry)
	}

	for _, entry := range explicitDeps {
		appendDep(entry)
	}
	for _, entry := range moduleEntries {
		appendDep(entry)
	}

	return merged
}

func (h *DependencyHandler) resolveModules(ctx context.Context, deps []DependencyDefinition, lockedVersions map[string]string) ([]ResolvedModule, error) {
	roots := make([]DependencySpec, 0, len(deps))
	for _, dep := range deps {
		name, err := graph.ParseName(dep.Component)
		if err != nil {
			return nil, NewDependencyEntryInvalidError("", "invalid component", dep.Component)
		}
		roots = append(roots, DependencySpec{
			Org:        name.Organization,
			Name:       name.Module,
			Constraint: dep.Version,
		})
	}

	resolveCtx, cancel := withOptionalTimeout(ctx, h.resolveTimeout)
	defer cancel()

	provider := ManifestProvider(h.hub)
	if h.manifestCache != nil {
		provider = h.manifestCache
	}
	result, err := Resolve(resolveCtx, provider, roots, &ResolveOptions{
		LockedVersions: lockedVersions,
		LockedDigests:  h.lockedModuleDigests(),
	})
	if err != nil {
		return nil, NewDependencyResolutionError(err)
	}
	if len(result.Errors) > 0 {
		return nil, NewDependencyResolutionErrors(result.Errors)
	}

	return result.Modules, nil
}

// touchedModuleNames returns the resolved modules this operation actually
// affects: those new or version-changed relative to the snapshot, plus the
// module of the dependency entry being changed in this operation. Modules
// already installed at the same version that this operation does not target are
// trusted — they were validated when installed — and are excluded from strict
// requirement enforcement, so a partial update does not re-validate
// dependencies it did not touch.
func touchedModuleNames(modules []ResolvedModule, snapshot regapi.State, opComponent string) []string {
	installed := snapshotModuleVersions(snapshot)
	names := make([]string, 0, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		name := mod.Org + "/" + mod.Name
		version, known := installed[name]
		if !known || version != mod.Version || name == opComponent {
			names = append(names, name)
		}
	}
	return names
}

func (h *DependencyHandler) loadModuleEntries(ctx context.Context, modules []ResolvedModule, transcoder payload.Transcoder) ([]regapi.Entry, error) {
	entries := make([]regapi.Entry, 0)

	for _, mod := range modules {
		moduleName := mod.Org + "/" + mod.Name
		moduleEntries, err := h.loadEntriesForModule(ctx, transcoder, mod)
		if err != nil {
			return nil, err
		}
		for i := range moduleEntries {
			moduleEntries[i] = markModuleMeta(moduleEntries[i], moduleName, mod.Version)
		}
		entries = append(entries, moduleEntries...)
	}

	return entries, nil
}

func (h *DependencyHandler) loadEntriesForModule(ctx context.Context, transcoder payload.Transcoder, mod ResolvedModule) ([]regapi.Entry, error) {
	modulePath, err := h.ensureModuleAvailable(ctx, mod)
	if err != nil {
		return nil, err
	}
	return loadRawEntriesFromPaths(ctx, []string{modulePath}, h.logger, transcoder)
}

func (h *DependencyHandler) ensureModuleAvailable(ctx context.Context, mod ResolvedModule) (string, error) {
	if err := os.MkdirAll(h.vendorDir, 0755); err != nil {
		return "", NewDependencyDownloadError(modKey(mod), err)
	}

	name, err := graph.ParseName(mod.Org + "/" + mod.Name)
	if err != nil {
		return "", NewDependencyEntryInvalidError("", "invalid component", mod.Org+"/"+mod.Name)
	}
	moduleName := name.String()

	if replacementPath, ok := h.replacementPath(moduleName); ok {
		stat, err := os.Stat(replacementPath)
		if err != nil {
			return "", NewDependencyLoadError(replacementPath, err)
		}
		if !stat.IsDir() {
			return "", NewDependencyLoadError(replacementPath, fmt.Errorf("replacement path is not a directory"))
		}
		return replacementPath, nil
	}

	expectedDigest := mod.Digest
	expectedSize := mod.SizeBytes
	wappPath := filepath.Join(h.vendorDir, lock.WappPath(name, mod.Version))
	if exists(wappPath) {
		if err := verifyDownloadedArtifact(wappPath, expectedDigest, expectedSize); err == nil {
			return wappPath, nil
		}
		h.logger.Warn("cached dependency artifact failed integrity check; redownloading",
			zap.String("module", modKey(mod)),
			zap.String("path", wappPath))
		_ = os.Remove(wappPath)
	}

	dirPath := filepath.Join(h.vendorDir, lock.ModulePath(name))
	if exists(dirPath) {
		if installed, ok := h.installedVersion(name.String()); ok && installed == mod.Version {
			return dirPath, nil
		}
	}

	url := mod.URL
	if url == "" {
		downloadURLCtx, cancel := withOptionalTimeout(ctx, h.downloadTimeout)
		defer cancel()

		info, err := h.hub.GetDownloadURL(downloadURLCtx, &DownloadParams{
			Org:       mod.Org,
			Module:    mod.Name,
			Version:   mod.Version,
			VersionID: mod.VersionID,
		})
		if err != nil {
			return "", NewDependencyDownloadError(modKey(mod), err)
		}
		url = info.URL
		if expectedDigest == "" {
			expectedDigest = info.Digest
		}
		if expectedSize == 0 {
			expectedSize = info.Size
		}
	}
	if url == "" {
		return "", NewDependencyDownloadError(modKey(mod), ErrDependencyNoContent)
	}

	downloadCtx, cancel := withOptionalTimeout(ctx, h.downloadTimeout)
	defer cancel()

	if err := h.hub.DownloadToFile(downloadCtx, url, wappPath); err != nil {
		return "", NewDependencyDownloadError(modKey(mod), err)
	}
	if err := verifyDownloadedArtifact(wappPath, expectedDigest, expectedSize); err != nil {
		_ = os.Remove(wappPath)
		return "", NewDependencyIntegrityError(modKey(mod), err, expectedDigest, expectedSize)
	}

	return wappPath, nil
}

func (h *DependencyHandler) replacementPath(moduleName string) (string, bool) {
	if h.lockPath == "" {
		return "", false
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return "", false
	}
	replacement, ok := lockObj.GetReplacement(moduleName)
	if !ok || strings.TrimSpace(replacement.To) == "" {
		return "", false
	}
	path := replacement.To
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(lockObj.Path()), path)
	}
	return path, true
}

func (h *DependencyHandler) operationModules(
	ctx context.Context,
	op regapi.Operation,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) (map[string]struct{}, error) {
	modules := make(map[string]struct{})
	entry, ok := resolveOperationEntry(op, snapshot)
	if !ok || !isRootDependency(entry) {
		return modules, nil
	}
	def, err := decodeDependency(ctx, transcoder, entry)
	if err != nil {
		return nil, err
	}
	if def.Component != "" {
		modules[def.Component] = struct{}{}
	}
	return modules, nil
}

func VerifyDownloadedArtifact(path, expectedDigest string, expectedSize uint64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if expectedSize > 0 && uint64(info.Size()) != expectedSize {
		return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", expectedSize, info.Size())
	}
	if expectedDigest == "" {
		return nil
	}

	alg, wantDigest, err := parseExpectedDigest(expectedDigest)
	if err != nil {
		return err
	}
	if alg != "sha256" {
		return fmt.Errorf("unsupported digest algorithm %q", alg)
	}

	gotDigest, err := sha256FileHex(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(gotDigest, wantDigest) {
		return fmt.Errorf("digest mismatch: expected %s, got sha256:%s", expectedDigest, gotDigest)
	}
	return nil
}

func verifyDownloadedArtifact(path, expectedDigest string, expectedSize uint64) error {
	return VerifyDownloadedArtifact(path, expectedDigest, expectedSize)
}

func parseExpectedDigest(raw string) (algorithm string, value string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("empty digest")
	}
	if !strings.Contains(trimmed, ":") {
		return "sha256", trimmed, nil
	}

	parts := strings.SplitN(trimmed, ":", 2)
	algorithm = strings.ToLower(strings.TrimSpace(parts[0]))
	value = strings.TrimSpace(parts[1])
	if algorithm == "" || value == "" {
		return "", "", fmt.Errorf("invalid digest format %q", raw)
	}
	return algorithm, value, nil
}

func sha256FileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (h *DependencyHandler) installedVersion(moduleName string) (string, bool) {
	if h.lockPath == "" {
		return "", false
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return "", false
	}
	mod, ok := lockObj.GetModule(moduleName)
	if !ok {
		return "", false
	}
	return mod.Version, true
}

func (h *DependencyHandler) lockedModuleDigests() map[string]string {
	if h.lockPath == "" {
		return nil
	}
	lockObj, err := lock.New(h.lockPath)
	if err != nil {
		return nil
	}
	modules := lockObj.GetModules()
	if len(modules) == 0 {
		return nil
	}
	digests := make(map[string]string, len(modules))
	for _, mod := range modules {
		if mod.Hash == "" || mod.Name == "" {
			continue
		}
		digests[mod.Name] = mod.Hash
	}
	if len(digests) == 0 {
		return nil
	}
	return digests
}

func loadRawEntriesFromPaths(
	ctx context.Context,
	paths []string,
	logger *zap.Logger,
	transcoder payload.Transcoder,
) ([]regapi.Entry, error) {
	if transcoder == nil {
		return nil, ErrDependencyTranscoderMissing
	}

	ldr := loaderFromContext(ctx, logger, transcoder)

	var entries []regapi.Entry
	for _, path := range paths {
		var loaded []regapi.Entry
		if filepath.Ext(path) == ".wapp" {
			var err error
			loaded, err = loadEntriesFromWapp(path)
			if err != nil {
				return nil, NewDependencyLoadError(path, err)
			}
		} else {
			stat, err := os.Stat(path)
			if os.IsNotExist(err) {
				logger.Warn("path not found, skipping", zap.String("path", path))
				continue
			}
			if err != nil {
				return nil, NewDependencyLoadError(path, err)
			}
			if stat.IsDir() {
				dirFS := os.DirFS(path)
				loaded, err = ldr.LoadFS(ctx, dirFS)
				if err != nil {
					return nil, NewDependencyLoadError(path, err)
				}
			} else {
				logger.Warn("unknown path type, skipping", zap.String("path", path))
				continue
			}
		}
		entries = append(entries, loaded...)
	}
	return entries, nil
}

func loaderFromContext(ctx context.Context, logger *zap.Logger, transcoder payload.Transcoder) boot.Loader {
	if ldr := boot.GetLoader(ctx); ldr != nil {
		return ldr
	}

	interpolator := interpolate.NewEntryInterpolator(transcoder,
		interpolate.WithInterpolator(interpolate.LoadFile),
	)
	return loader.NewLoader(transcoder, logger.Named("loader"), interpolator)
}

func loadEntriesFromWapp(path string) ([]regapi.Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return nil, err
	}

	wappEntries, err := reader.GetEntries()
	if err != nil {
		return nil, err
	}

	entries := make([]regapi.Entry, len(wappEntries))
	for i, we := range wappEntries {
		entries[i] = regapi.Entry{
			ID:   regapi.NewID(we.ID.Namespace, we.ID.Name),
			Kind: we.Kind,
			Meta: attrs.NewBagFrom(we.Meta),
			Data: payload.New(unwrapPayloadData(we.Data)),
		}
	}
	return entries, nil
}

func unwrapPayloadData(data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	innerData, hasData := m["Data"]
	_, hasFormat := m["Format"]
	if hasData && hasFormat && len(m) == 2 {
		return innerData
	}
	return data
}

func buildOperations(
	current regapi.State,
	desired []regapi.Entry,
	originalID regapi.ID,
	controlledModules map[string]struct{},
	mutableModules map[string]struct{},
) ([]regapi.Operation, error) {
	currentByID := make(map[regapi.ID]regapi.Entry, len(current))
	for _, entry := range current {
		currentByID[entry.ID] = entry
	}

	desiredByID := make(map[regapi.ID]regapi.Entry, len(desired))
	for _, entry := range desired {
		desiredByID[entry.ID] = entry
	}

	ops := make([]regapi.Operation, 0)

	for id, entry := range desiredByID {
		if id == originalID {
			continue
		}
		if existing, ok := currentByID[id]; ok {
			if entryConflict(existing, entry) {
				return nil, NewDependencyEntryConflictError(id.String(), entryModule(existing), entryModule(entry))
			}
			if !entriesEqual(existing, entry) {
				if sameImmutableModuleVersion(existing, entry, mutableModules) {
					continue
				}
				ops = append(ops, regapi.Operation{Kind: regapi.EntryUpdate, Entry: entry})
			}
		} else {
			ops = append(ops, regapi.Operation{Kind: regapi.EntryCreate, Entry: entry})
		}
	}

	for id, entry := range currentByID {
		if id == originalID {
			continue
		}
		if _, ok := desiredByID[id]; ok {
			continue
		}
		if module := entryModule(entry); module != "" {
			if controlledModules != nil {
				if _, ok := controlledModules[module]; !ok {
					continue
				}
			}
			ops = append(ops, regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: id}})
		}
	}

	return ops, nil
}

func sameImmutableModuleVersion(existing, desired regapi.Entry, mutableModules map[string]struct{}) bool {
	if mutableModules == nil {
		return false
	}
	module := entryModule(desired)
	if module == "" || module != entryModule(existing) {
		return false
	}
	if _, mutable := mutableModules[module]; mutable {
		return false
	}
	existingVersion := moduleVersion(existing)
	desiredVersion := moduleVersion(desired)
	if existingVersion != "" && desiredVersion != "" && existingVersion != desiredVersion {
		return false
	}
	return true
}

func entryConflict(existing, desired regapi.Entry) bool {
	desiredModule := entryModule(desired)
	if desiredModule == "" {
		return false
	}
	existingModule := entryModule(existing)
	return existingModule == "" || existingModule != desiredModule
}

func entryModule(entry regapi.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[metaModuleKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func moduleVersion(entry regapi.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if v, ok := entry.Meta[metaModuleVersionKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func isRootDependency(entry regapi.Entry) bool {
	return entry.Kind == regapi.NamespaceDependency && entryModule(entry) == ""
}

func markModuleMeta(entry regapi.Entry, moduleName, moduleVersion string) regapi.Entry {
	meta := entry.Meta
	if meta == nil {
		meta = attrs.NewBag()
	} else {
		meta = attrs.NewBagFrom(meta)
	}
	meta.Set(metaModuleKey, moduleName)
	if moduleVersion != "" {
		meta.Set(metaModuleVersionKey, moduleVersion)
	}
	entry.Meta = meta
	return entry
}

func entriesEqual(a, b regapi.Entry) bool {
	if a.ID != b.ID || a.Kind != b.Kind {
		return false
	}
	if !reflect.DeepEqual(a.Meta, b.Meta) {
		return false
	}
	switch {
	case a.Data == nil && b.Data == nil:
		return true
	case a.Data == nil || b.Data == nil:
		return false
	}
	if a.Data.Format() != b.Data.Format() {
		return false
	}
	return reflect.DeepEqual(a.Data.Data(), b.Data.Data())
}

func resolveOperationEntry(op regapi.Operation, snapshot regapi.State) (regapi.Entry, bool) {
	if op.Entry.Kind != "" && op.Entry.Data != nil {
		return op.Entry, true
	}
	for _, entry := range snapshot {
		if entry.ID == op.Entry.ID {
			return entry, true
		}
	}
	return regapi.Entry{}, false
}

func decodeDependency(ctx context.Context, transcoder payload.Transcoder, entry regapi.Entry) (DependencyDefinition, error) {
	def, err := entrypkg.DecodeEntryConfig[DependencyDefinition](ctx, transcoder, entry)
	if err != nil {
		return DependencyDefinition{}, NewDependencyEntryDecodeError(entry.ID.String(), err)
	}
	return *def, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func modKey(mod ResolvedModule) string {
	return mod.Org + "/" + mod.Name + "@" + mod.Version
}

func formatResolutionErrors(errs []ResolutionError) string {
	if len(errs) == 0 {
		return ""
	}
	msg := errs[0].String()
	for i := 1; i < len(errs); i++ {
		msg += "; " + errs[i].String()
	}
	return msg
}

func newHubClientFromAuth() (*Client, error) {
	projectDir, _ := os.Getwd()
	authCfg := auth.NewConfig(projectDir)
	store := auth.NewStore(authCfg)

	registryURL := store.DefaultRegistry()
	cred, _ := store.Get(registryURL)

	var token string
	if cred != nil {
		token = cred.Token
	}

	client, err := NewClient(Options{
		BaseURL: registryURL,
		Token:   token,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

var (
	ErrDependencyHandlerNotConfigured = apierror.New(apierror.Internal, "dependency handler not configured").WithRetryable(apierror.False)
	ErrDependencyTranscoderMissing    = apierror.New(apierror.Internal, "payload transcoder not available").WithRetryable(apierror.False)
	ErrDependencyNoContent            = apierror.New(apierror.NotFound, "no download URL available").WithRetryable(apierror.False)
)

func NewDependencyEntryInvalidError(entryID, detail, component string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id":  entryID,
			"detail":    detail,
			"component": component,
		}))
}

func NewDependencyEntryDecodeError(entryID string, cause error) apierror.Error {
	return apierror.New(apierror.Invalid, "decode dependency entry").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID})).
		WithCause(cause)
}

func NewDependencyEntryMissingError(entryID string) apierror.Error {
	return apierror.New(apierror.NotFound, "dependency entry not found").
		WithDetails(attrs.NewBagFrom(map[string]any{"entry_id": entryID}))
}

func NewDependencyResolutionError(cause error) apierror.Error {
	err := apierror.New(apierror.Unavailable, "dependency resolution failed").
		WithRetryable(apierror.False).
		WithCause(cause)
	if cause != nil {
		err = err.WithDetails(attrs.NewBagFrom(map[string]any{"reason": cause.Error()}))
	}
	return err
}

func NewDependencyResolutionErrors(errs []ResolutionError) apierror.Error {
	details := make([]map[string]any, 0, len(errs))
	for _, e := range errs {
		details = append(details, map[string]any{
			"module":     e.Org + "/" + e.Name,
			"constraint": e.Constraint,
			"message":    e.Message,
		})
	}

	return apierror.New(apierror.Conflict, "dependency resolution failed").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"count":   len(errs),
			"summary": formatResolutionErrors(errs),
			"errors":  details,
		}))
}

func NewDependencyDownloadError(module string, cause error) apierror.Error {
	return apierror.New(apierror.Unavailable, "module download failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"module": module})).
		WithCause(cause)
}

func NewDependencyLoadError(path string, cause error) apierror.Error {
	return apierror.New(apierror.Internal, "load module entries failed").
		WithDetails(attrs.NewBagFrom(map[string]any{"path": path})).
		WithCause(cause)
}

func NewDependencyIntegrityError(module string, cause error, expectedDigest string, expectedSize uint64) apierror.Error {
	details := map[string]any{"module": module}
	if expectedDigest != "" {
		details["expected_digest"] = expectedDigest
	}
	if expectedSize > 0 {
		details["expected_size"] = expectedSize
	}

	return apierror.New(apierror.Invalid, "downloaded module artifact failed integrity verification").
		WithDetails(attrs.NewBagFrom(details)).
		WithCause(cause).
		WithRetryable(apierror.False)
}

func NewDependencyPipelineError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "dependency pipeline failed").WithCause(cause)
}

func NewDependencyEntryConflictError(entryID, existingModule, desiredModule string) apierror.Error {
	msg := fmt.Sprintf("entry %q conflicts: owned by %q, wanted by %q", entryID, existingModule, desiredModule)
	return apierror.New(apierror.Conflict, msg).
		WithDetails(attrs.NewBagFrom(map[string]any{
			"entry_id":        entryID,
			"existing_module": existingModule,
			"desired_module":  desiredModule,
		})).
		WithRetryable(apierror.False)
}
