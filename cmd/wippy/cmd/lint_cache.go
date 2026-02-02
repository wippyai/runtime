package cmd

import (
	"os"

	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	regapi "github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
)

type lintCache struct {
	store          cache.Store
	cfg            cache.Config
	builtinHash    string
	typecheckHash  string
	builtinModules []string
}

type lintFingerprints struct {
	compile     map[regapi.ID]string
	typecheck   map[regapi.ID]string
	compileDeps map[regapi.ID][]cache.DepMeta
	typeDeps    map[regapi.ID][]cache.DepMeta
}

func computeLintFingerprints(levels [][]regapi.Entry, dataMap map[regapi.ID]entryData, lcache lintCache) lintFingerprints {
	fp := lintFingerprints{
		compile:     make(map[regapi.ID]string),
		typecheck:   make(map[regapi.ID]string),
		compileDeps: make(map[regapi.ID][]cache.DepMeta),
		typeDeps:    make(map[regapi.ID][]cache.DepMeta),
	}

	for _, name := range lcache.builtinModules {
		id := regapi.NewID("", name)
		sourceHash := cache.SourceHash("", "")
		fp.compile[id] = code.CompileFingerprint(id.String(), luaapi.ModuleKind, sourceHash, "", nil)
		fp.typecheck[id] = code.TypecheckFingerprint(id.String(), luaapi.ModuleKind, sourceHash, "", lcache.typecheckHash, lcache.builtinHash, nil)
	}

	for _, levelEntries := range levels {
		for _, entry := range levelEntries {
			data := dataMap[entry.ID]
			sourceHash := cache.SourceHash(data.Source, data.Method)

			compileDeps := make([]cache.DepFingerprint, 0, len(data.Imports))
			compileMeta := make([]cache.DepMeta, 0, len(data.Imports))
			typeDeps := make([]cache.DepFingerprint, 0, len(data.Imports))
			typeMeta := make([]cache.DepMeta, 0, len(data.Imports))

			for alias, importID := range data.Imports {
				if depFP, ok := fp.compile[importID]; ok {
					compileDeps = append(compileDeps, cache.DepFingerprint{
						Alias:       alias,
						ID:          importID.String(),
						Fingerprint: depFP,
					})
					compileMeta = append(compileMeta, cache.DepMeta{
						Alias:              alias,
						ID:                 importID.String(),
						CompileFingerprint: depFP,
					})
				}
				if depFP, ok := fp.typecheck[importID]; ok {
					typeDeps = append(typeDeps, cache.DepFingerprint{
						Alias:       alias,
						ID:          importID.String(),
						Fingerprint: depFP,
					})
					typeMeta = append(typeMeta, cache.DepMeta{
						Alias:                alias,
						ID:                   importID.String(),
						TypecheckFingerprint: depFP,
					})
				}
			}

			fp.compile[entry.ID] = code.CompileFingerprint(entry.ID.String(), entry.Kind, sourceHash, data.Method, compileDeps)
			fp.compileDeps[entry.ID] = compileMeta
			fp.typecheck[entry.ID] = code.TypecheckFingerprint(entry.ID.String(), entry.Kind, sourceHash, data.Method, lcache.typecheckHash, lcache.builtinHash, typeDeps)
			fp.typeDeps[entry.ID] = typeMeta
		}
	}

	return fp
}

func filterTypecheckDiagnostics(diags []diag.Diagnostic) []diag.Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	filtered := make([]diag.Diagnostic, 0, len(diags))
	for _, d := range diags {
		if d.Code < lint.LintCodeBase {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

func lintCacheConfig(lcache lintCache) cache.Config {
	return lcache.cfg.Normalize()
}

func lintCacheEnabled(lcache lintCache) bool {
	cfg := lintCacheConfig(lcache)
	return cfg.Enabled && lcache.store != nil
}

func lintCacheAllowsRead(lcache lintCache) bool {
	if !lintCacheEnabled(lcache) {
		return false
	}
	return lintCacheConfig(lcache).AllowsRead()
}

func lintCacheAllowsWrite(lcache lintCache) bool {
	if !lintCacheEnabled(lcache) {
		return false
	}
	return lintCacheConfig(lcache).AllowsWrite()
}

func lintCacheDeleter(lcache lintCache) (cache.Deleter, bool) {
	if lcache.store == nil {
		return nil, false
	}
	deleter, ok := lcache.store.(cache.Deleter)
	return deleter, ok
}

func lintDeleteCacheKey(lcache lintCache, key string) {
	if !lintCacheAllowsWrite(lcache) {
		return
	}
	if deleter, ok := lintCacheDeleter(lcache); ok {
		_ = deleter.Delete(key)
	}
}

func lintCompileCacheKey(fingerprint string) string {
	return cache.CompileKey(fingerprint)
}

func lintTypecheckCacheKey(fingerprint string) string {
	return cache.TypecheckKey(fingerprint)
}

func resetLintCache(lcache lintCache) error {
	dir := lintCacheConfig(lcache).Dir
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

func lintLoadTypecheckCache(lcache lintCache, id regapi.ID, fingerprint string) (*io.Manifest, []diag.Diagnostic, bool) {
	cfg := lintCacheConfig(lcache)
	if fingerprint == "" || !cfg.TypecheckEnabled || !lintCacheAllowsRead(lcache) || lcache.store == nil {
		return nil, nil, false
	}
	key := lintTypecheckCacheKey(fingerprint)
	entry, ok, err := lcache.store.Get(key)
	if err != nil || !ok || entry == nil {
		return nil, nil, false
	}
	if entry.Meta.SchemaVersion != cache.SchemaVersion {
		lintDeleteCacheKey(lcache, key)
		return nil, nil, false
	}
	if entry.Meta.TypecheckFingerprint != fingerprint || entry.Meta.EntryID != id.String() {
		lintDeleteCacheKey(lcache, key)
		return nil, nil, false
	}
	if len(entry.Manifest) == 0 {
		lintDeleteCacheKey(lcache, key)
		return nil, nil, false
	}
	manifest, ok := cache.DecodeManifestSafe(entry.Manifest)
	if !ok {
		lintDeleteCacheKey(lcache, key)
		return nil, nil, false
	}
	diags := entry.Diagnostics
	if diags == nil {
		diags = []diag.Diagnostic{}
	}
	return manifest, diags, true
}

func lintSaveTypecheckCache(lcache lintCache, entry regapi.Entry, data entryData, fingerprint string, deps []cache.DepMeta, manifest *io.Manifest, diagnostics []diag.Diagnostic) {
	cfg := lintCacheConfig(lcache)
	if fingerprint == "" || !cfg.TypecheckEnabled || !lintCacheAllowsWrite(lcache) || lcache.store == nil || manifest == nil {
		return
	}
	manifestBytes, err := manifest.Encode()
	if err != nil {
		return
	}
	cacheEntry := &cache.Entry{
		Meta: cache.Meta{
			SchemaVersion:        cache.SchemaVersion,
			TypecheckFingerprint: fingerprint,
			EntryID:              entry.ID.String(),
			Kind:                 entry.Kind,
			Method:               data.Method,
			SourceHash:           cache.SourceHash(data.Source, data.Method),
			BuiltinHash:          lcache.builtinHash,
			TypecheckConfigHash:  lcache.typecheckHash,
			Deps:                 deps,
		},
		Manifest:    manifestBytes,
		Diagnostics: diagnostics,
	}
	_ = lcache.store.Put(lintTypecheckCacheKey(fingerprint), cacheEntry)
}

func lintEnsureCompileCache(lcache lintCache, entry regapi.Entry, data entryData, fingerprint string, deps []cache.DepMeta, stmts []ast.Stmt) {
	cfg := lintCacheConfig(lcache)
	if fingerprint == "" || !cfg.CompileEnabled || lcache.store == nil {
		return
	}
	if lintCacheAllowsRead(lcache) {
		key := lintCompileCacheKey(fingerprint)
		cacheEntry, ok, err := lcache.store.Get(key)
		if err == nil && ok && cacheEntry != nil {
			if cacheEntry.Meta.SchemaVersion != cache.SchemaVersion {
				lintDeleteCacheKey(lcache, key)
			} else if cacheEntry.Meta.CompileFingerprint == fingerprint && cacheEntry.Meta.EntryID == entry.ID.String() && len(cacheEntry.Proto) > 0 {
				return
			} else if cacheEntry.Meta.CompileFingerprint != "" || cacheEntry.Meta.EntryID != "" {
				lintDeleteCacheKey(lcache, key)
			}
		}
	}
	if !lintCacheAllowsWrite(lcache) {
		return
	}
	if entry.Kind == luaapi.ModuleKind || len(stmts) == 0 {
		return
	}
	proto, err := glua.Compile(stmts, entry.ID.String())
	if err != nil {
		return
	}
	dataBytes, err := bytecode.Dump(proto)
	if err != nil {
		return
	}
	cacheEntry := &cache.Entry{
		Meta: cache.Meta{
			SchemaVersion:      cache.SchemaVersion,
			CompileFingerprint: fingerprint,
			EntryID:            entry.ID.String(),
			Kind:               entry.Kind,
			Method:             data.Method,
			SourceHash:         cache.SourceHash(data.Source, data.Method),
			Deps:               deps,
		},
		Proto: dataBytes,
	}
	_ = lcache.store.Put(lintCompileCacheKey(fingerprint), cacheEntry)
}
