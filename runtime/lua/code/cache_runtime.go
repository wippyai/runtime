// SPDX-License-Identifier: MPL-2.0

package code

import (
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
)

func (cm *Manager) cacheConfig() cache.Config {
	return cm.cacheCfg.Normalize()
}

func (cm *Manager) cacheEnabled() bool {
	cfg := cm.cacheConfig()
	return cfg.Enabled && cm.cacheStore != nil
}

func (cm *Manager) cacheAllowsRead() bool {
	if !cm.cacheEnabled() {
		return false
	}
	return cm.cacheConfig().AllowsRead()
}

func (cm *Manager) cacheAllowsWrite() bool {
	if !cm.cacheEnabled() {
		return false
	}
	return cm.cacheConfig().AllowsWrite()
}

func (cm *Manager) cacheDeleter() (cache.Deleter, bool) {
	if cm.cacheStore == nil {
		return nil, false
	}
	deleter, ok := cm.cacheStore.(cache.Deleter)
	return deleter, ok
}

func (cm *Manager) deleteCacheKey(key string) {
	if !cm.cacheAllowsWrite() {
		return
	}
	if deleter, ok := cm.cacheDeleter(); ok {
		_ = deleter.Delete(key)
	}
}

func (cm *Manager) compileCacheKey(fingerprint string) string {
	return cache.CompileKey(fingerprint)
}

func (cm *Manager) typecheckCacheKey(fingerprint string) string {
	return cache.TypecheckKey(fingerprint)
}

func (cm *Manager) loadTypecheckCache(id registry.ID, fingerprint string) (*io.Manifest, []diag.Diagnostic, bool) {
	cfg := cm.cacheConfig()
	if !cfg.TypecheckEnabled || !cm.cacheAllowsRead() {
		return nil, nil, false
	}
	key := cm.typecheckCacheKey(fingerprint)
	entry, ok, err := cm.cacheStore.Get(key)
	if err != nil || !ok || entry == nil {
		cm.typecheckCacheMisses.Add(1)
		return nil, nil, false
	}
	if entry.Meta.SchemaVersion != cache.SchemaVersion {
		cm.deleteCacheKey(key)
		cm.typecheckCacheMisses.Add(1)
		return nil, nil, false
	}
	if entry.Meta.TypecheckFingerprint != fingerprint || entry.Meta.EntryID != id.String() {
		cm.deleteCacheKey(key)
		cm.typecheckCacheMisses.Add(1)
		return nil, nil, false
	}
	if len(entry.Manifest) == 0 {
		cm.deleteCacheKey(key)
		cm.typecheckCacheMisses.Add(1)
		return nil, nil, false
	}
	manifest, ok := cache.DecodeManifestSafe(entry.Manifest)
	if !ok {
		cm.deleteCacheKey(key)
		cm.typecheckCacheMisses.Add(1)
		return nil, nil, false
	}
	diags := entry.Diagnostics
	if diags == nil {
		diags = []diag.Diagnostic{}
	}
	cm.typecheckCacheHits.Add(1)
	return manifest, diags, true
}

func (cm *Manager) saveTypecheckCache(node *Node, fingerprint string, deps []cache.DepMeta, manifest *io.Manifest, diagnostics []diag.Diagnostic) {
	cfg := cm.cacheConfig()
	if !cfg.TypecheckEnabled || !cm.cacheAllowsWrite() || node == nil {
		return
	}
	if cm.cacheStore == nil {
		return
	}
	var manifestBytes []byte
	if manifest != nil {
		if data, err := manifest.Encode(); err == nil {
			manifestBytes = data
		}
	}
	entry := &cache.Entry{
		Meta: cache.Meta{
			SchemaVersion:        cache.SchemaVersion,
			TypecheckFingerprint: fingerprint,
			EntryID:              node.ID.String(),
			Kind:                 node.Kind,
			Method:               node.Method,
			SourceHash:           nodeContentHash(node),
			BuiltinHash:          cm.builtinHash,
			TypecheckConfigHash:  cm.typeCfgHash,
			Deps:                 append([]cache.DepMeta(nil), deps...),
		},
		Manifest:    manifestBytes,
		Diagnostics: diagnostics,
	}
	cm.putCacheEntry(cm.typecheckCacheKey(fingerprint), entry)
}

func (cm *Manager) loadCompileCache(id registry.ID, fingerprint string) (*glua.FunctionProto, bool) {
	cfg := cm.cacheConfig()
	if !cfg.CompileEnabled || !cm.cacheAllowsRead() {
		return nil, false
	}
	memoryKey := compileBytesKey{id: id, fingerprint: fingerprint}
	if data, ok := cm.compileBytes.get(memoryKey); ok {
		if proto, err := bytecode.Undump(data); err == nil {
			cm.compileCacheHits.Add(1)
			return proto, true
		}
	}
	key := cm.compileCacheKey(fingerprint)
	entry, ok, err := cm.cacheStore.Get(key)
	if err != nil || !ok || entry == nil {
		cm.compileCacheMisses.Add(1)
		return nil, false
	}
	if entry.Meta.SchemaVersion != cache.SchemaVersion {
		cm.deleteCacheKey(key)
		cm.compileCacheMisses.Add(1)
		return nil, false
	}
	if entry.Meta.CompileFingerprint != fingerprint || entry.Meta.EntryID != id.String() {
		cm.deleteCacheKey(key)
		cm.compileCacheMisses.Add(1)
		return nil, false
	}
	if len(entry.Proto) == 0 {
		cm.deleteCacheKey(key)
		cm.compileCacheMisses.Add(1)
		return nil, false
	}
	proto, err := bytecode.Undump(entry.Proto)
	if err != nil {
		cm.deleteCacheKey(key)
		cm.compileCacheMisses.Add(1)
		return nil, false
	}
	cm.compileBytes.put(memoryKey, entry.Proto)
	cm.compileCacheHits.Add(1)
	return proto, true
}

func (cm *Manager) saveCompileCache(node *Node, fingerprint string, deps []cache.DepMeta, proto *glua.FunctionProto) {
	cfg := cm.cacheConfig()
	if !cfg.CompileEnabled || !cm.cacheAllowsWrite() || node == nil || proto == nil {
		return
	}
	if cm.cacheStore == nil {
		return
	}
	data, err := bytecode.Dump(proto)
	if err != nil {
		return
	}
	entry := &cache.Entry{
		Meta: cache.Meta{
			SchemaVersion:      cache.SchemaVersion,
			CompileFingerprint: fingerprint,
			EntryID:            node.ID.String(),
			Kind:               node.Kind,
			Method:             node.Method,
			SourceHash:         nodeContentHash(node),
			Deps:               append([]cache.DepMeta(nil), deps...),
		},
		Proto: data,
	}
	cm.putCacheEntry(cm.compileCacheKey(fingerprint), entry)
	cm.compileBytes.put(compileBytesKey{id: node.ID, fingerprint: fingerprint}, data)
}

func (cm *Manager) compileFingerprint(id registry.ID) (string, []cache.DepMeta, error) {
	return cm.compileFingerprintFromGraph(cm.memGraph, id)
}

func (cm *Manager) compileFingerprintFromGraph(memGraph *MemoryGraph, id registry.ID) (string, []cache.DepMeta, error) {
	memo := make(map[registry.ID]string)
	meta := make(map[registry.ID][]cache.DepMeta)
	fp, err := cm.compileFingerprintMemo(memGraph, id, memo, meta)
	if err != nil {
		return "", nil, err
	}
	return fp, append([]cache.DepMeta(nil), meta[id]...), nil
}

func (cm *Manager) compileFingerprintMemo(memGraph *MemoryGraph, id registry.ID, memo map[registry.ID]string, meta map[registry.ID][]cache.DepMeta) (string, error) {
	if v, ok := memo[id]; ok {
		return v, nil
	}
	node, err := memGraph.GetNode(id)
	if err != nil {
		return "", err
	}
	shared := memGraph.fingerprintMemo()
	context := fingerprintContext{toolchain: cm.toolchainIdentity}
	if result, ok := shared.get(compileStage, node, context); ok {
		memo[id] = result.fingerprint
		meta[id] = result.deps
		return result.fingerprint, nil
	}
	deps, _ := memGraph.GetDependenciesWithAliases(id)
	depFPs := make([]cache.DepFingerprint, 0, len(deps))
	depMeta := make([]cache.DepMeta, 0, len(deps))
	for _, dep := range deps {
		fp, err := cm.compileFingerprintMemo(memGraph, dep.ID, memo, meta)
		if err != nil {
			return "", err
		}
		alias := dep.Name
		depFPs = append(depFPs, cache.DepFingerprint{
			Alias:       alias,
			ID:          dep.ID.String(),
			Fingerprint: fp,
		})
		depMeta = append(depMeta, cache.DepMeta{
			Alias:              alias,
			ID:                 dep.ID.String(),
			CompileFingerprint: fp,
		})
	}
	fp := CompileFingerprint(cm.toolchainIdentity, node.ID.String(), node.Kind, nodeContentHash(node), node.Method, depFPs)
	shared.put(compileStage, node, context, fp, depMeta)
	memo[id] = fp
	meta[id] = depMeta
	return fp, nil
}

func (cm *Manager) typecheckFingerprint(id registry.ID) (string, []cache.DepMeta, error) {
	return cm.typecheckFingerprintFromGraph(cm.memGraph, id)
}

func (cm *Manager) typecheckFingerprintFromGraph(memGraph *MemoryGraph, id registry.ID) (string, []cache.DepMeta, error) {
	memo := make(map[registry.ID]string)
	meta := make(map[registry.ID][]cache.DepMeta)
	fp, err := cm.typecheckFingerprintMemo(memGraph, id, memo, meta)
	if err != nil {
		return "", nil, err
	}
	return fp, append([]cache.DepMeta(nil), meta[id]...), nil
}

func (cm *Manager) typecheckFingerprintMemo(memGraph *MemoryGraph, id registry.ID, memo map[registry.ID]string, meta map[registry.ID][]cache.DepMeta) (string, error) {
	if v, ok := memo[id]; ok {
		return v, nil
	}
	node, err := memGraph.GetNode(id)
	if err != nil {
		return "", err
	}
	shared := memGraph.fingerprintMemo()
	context := fingerprintContext{toolchain: cm.toolchainIdentity, typeConfig: cm.typeCfgHash, builtins: cm.builtinHash}
	if result, ok := shared.get(typecheckStage, node, context); ok {
		memo[id] = result.fingerprint
		meta[id] = result.deps
		return result.fingerprint, nil
	}
	deps, _ := memGraph.GetDependenciesWithAliases(id)
	depFPs := make([]cache.DepFingerprint, 0, len(deps))
	depMeta := make([]cache.DepMeta, 0, len(deps))
	for _, dep := range deps {
		fp, err := cm.typecheckFingerprintMemo(memGraph, dep.ID, memo, meta)
		if err != nil {
			return "", err
		}
		alias := dep.Name
		depFPs = append(depFPs, cache.DepFingerprint{
			Alias:       alias,
			ID:          dep.ID.String(),
			Fingerprint: fp,
		})
		depMeta = append(depMeta, cache.DepMeta{
			Alias:                alias,
			ID:                   dep.ID.String(),
			TypecheckFingerprint: fp,
		})
	}
	fp := TypecheckFingerprint(cm.toolchainIdentity, node.ID.String(), node.Kind, nodeContentHash(node), node.Method, cm.typeCfgHash, cm.builtinHash, depFPs)
	shared.put(typecheckStage, node, context, fp, depMeta)
	memo[id] = fp
	meta[id] = depMeta
	return fp, nil
}

func (cm *Manager) refreshBuiltinHash() {
	manifests := make(map[string]*io.Manifest)
	cm.memGraph.mu.RLock()
	defer cm.memGraph.mu.RUnlock()
	for _, node := range cm.memGraph.nodes {
		if node.Module == nil || node.Manifest == nil {
			continue
		}
		manifests[node.ID.Name] = node.Manifest
	}
	cm.builtinHash = BuiltinManifestHash(manifests)
}

// CacheStore exposes the cache store (nil if disabled).
func (cm *Manager) CacheStore() cache.Store {
	return cm.cacheStore
}

// CacheConfig exposes the cache configuration.
func (cm *Manager) CacheConfig() cache.Config {
	return cm.cacheConfig()
}

// CacheStats returns the artifact cache hit and miss counts.
func (cm *Manager) CacheStats() CacheStats {
	if cm == nil {
		return CacheStats{}
	}
	return CacheStats{
		CompileHits:     cm.compileCacheHits.Load(),
		CompileMisses:   cm.compileCacheMisses.Load(),
		TypecheckHits:   cm.typecheckCacheHits.Load(),
		TypecheckMisses: cm.typecheckCacheMisses.Load(),
	}
}

// BuiltinManifestHash returns the built-in manifest hash used for cache keys.
func (cm *Manager) BuiltinManifestHash() string {
	return cm.builtinHash
}

// TypecheckConfigHash returns the typecheck config hash used for cache keys.
func (cm *Manager) TypecheckConfigHash() string {
	return cm.typeCfgHash
}

// TypeCheckConfig returns the effective runtime type-check configuration.
func (cm *Manager) TypeCheckConfig() TypeCheckConfig {
	if cm == nil || cm.typeChecker == nil {
		return DefaultTypeCheckConfig()
	}
	return cm.typeChecker.config
}

// ToolchainIdentity returns the toolchain identity used for cache keys.
func (cm *Manager) ToolchainIdentity() string {
	if cm == nil {
		return ""
	}
	return cm.toolchainIdentity
}

// Warn once per manager if a cache that passed the startup probe later stops
// accepting writes. Recompilation remains usable and the cause stays visible.
func (cm *Manager) putCacheEntry(key string, entry *cache.Entry) {
	if err := cm.cacheStore.Put(key, entry); err != nil {
		cm.cacheWriteWarning.Do(func() {
			cm.log.Warn("lua persistent cache write failed; subsequent boots may recompile",
				zap.String("dir", cm.cacheConfig().Dir), zap.Error(err))
		})
	}
}
