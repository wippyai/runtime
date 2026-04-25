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
	logger          *zap.Logger
	lockPath        string
	vendorDir       string
	resolveTimeout  time.Duration
	downloadTimeout time.Duration
	// hasToken records whether the underlying hub client was built with
	// credentials, so that auth-related errors get a more actionable hint.
	hasToken bool
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
	hasToken := false
	if client == nil {
		hubClient, gotToken, err := newHubClientFromAuth()
		if err != nil {
			return nil, err
		}
		client = hubClient
		hasToken = gotToken
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
		logger:          logger,
		lockPath:        lockPath,
		vendorDir:       vendorDir,
		resolveTimeout:  opts.ResolveTimeout,
		downloadTimeout: opts.DownloadTimeout,
		hasToken:        hasToken,
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

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return regapi.DirectiveResult{}, ErrDependencyTranscoderMissing
	}

	desiredDeps, err := h.collectDesiredDependencies(ctx, op, snapshot, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}

	desiredRoots := make([]DependencyDefinition, 0, len(desiredDeps))
	desiredDepEntries := make([]regapi.Entry, 0, len(desiredDeps))
	for _, dep := range desiredDeps {
		desiredRoots = append(desiredRoots, dep.definition)
		desiredDepEntries = append(desiredDepEntries, dep.entry)
	}

	var resolved []ResolvedModule
	if len(desiredRoots) > 0 {
		var err error
		resolved, err = h.resolveModules(ctx, desiredRoots)
		if err != nil {
			return regapi.DirectiveResult{}, err
		}
	}

	moduleEntries, err := h.loadModuleEntries(ctx, resolved, transcoder)
	if err != nil {
		return regapi.DirectiveResult{}, err
	}
	linkDeps := mergeLinkDependencies(desiredDepEntries, moduleEntries)

	combined := make([]regapi.Entry, 0, len(snapshot)+len(moduleEntries))
	for _, e := range snapshot {
		if entryModule(e) != "" {
			continue
		}
		combined = append(combined, e)
	}
	combined = append(combined, moduleEntries...)

	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(stages.WithDependencies(linkDeps)),
		stages.Override(),
	)
	if err := pipeline.Execute(ctx, &combined); err != nil {
		return regapi.DirectiveResult{}, NewDependencyPipelineError(err)
	}

	additional, err := buildOperations(snapshot, combined, op.Entry.ID)
	if err != nil {
		return regapi.DirectiveResult{}, err
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

func (h *DependencyHandler) collectDesiredDependencies(
	ctx context.Context,
	op regapi.Operation,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) ([]desiredDependency, error) {
	deps := make(map[regapi.ID]desiredDependency)

	for _, entry := range snapshot {
		if entry.Kind != regapi.NamespaceDependency {
			continue
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

	switch op.Kind {
	case regapi.EntryDelete:
		delete(deps, op.Entry.ID)
	case regapi.EntryCreate, regapi.EntryUpdate:
		entry, ok := resolveOperationEntry(op, snapshot)
		if !ok {
			return nil, NewDependencyEntryMissingError(op.Entry.ID.String())
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

func (h *DependencyHandler) resolveModules(ctx context.Context, deps []DependencyDefinition) ([]ResolvedModule, error) {
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

	result, err := Resolve(resolveCtx, h.hub, roots, nil)
	if err != nil {
		return nil, NewDependencyResolutionError(DecorateAuthError(err, h.hasToken))
	}
	if len(result.Errors) > 0 {
		return nil, NewDependencyResolutionErrors(result.Errors)
	}

	return result.Modules, nil
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
			Org:     mod.Org,
			Module:  mod.Name,
			Version: mod.Version,
		})
		if err != nil {
			return "", NewDependencyDownloadError(modKey(mod), DecorateAuthError(err, h.hasToken))
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

func verifyDownloadedArtifact(path, expectedDigest string, expectedSize uint64) error {
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

func buildOperations(current regapi.State, desired []regapi.Entry, originalID regapi.ID) ([]regapi.Operation, error) {
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
			ops = append(ops, regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: id}})
		}
	}

	return ops, nil
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

// newHubClientFromAuth builds a hub client using the user's stored credentials.
// The second return reports whether a token was actually loaded — callers use
// it to render more specific errors when an anonymous request hits a private
// module (the server returns NotFound to hide existence).
func newHubClientFromAuth() (*Client, bool, error) {
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
		return nil, false, err
	}
	return client, token != "", nil
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
