// SPDX-License-Identifier: MPL-2.0

package code

import (
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
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
		return nil, nil, false
	}
	if entry.Meta.SchemaVersion != cache.SchemaVersion {
		cm.deleteCacheKey(key)
		return nil, nil, false
	}
	if entry.Meta.TypecheckFingerprint != fingerprint || entry.Meta.EntryID != id.String() {
		cm.deleteCacheKey(key)
		return nil, nil, false
	}
	if len(entry.Manifest) == 0 {
		cm.deleteCacheKey(key)
		return nil, nil, false
	}
	manifest, ok := cache.DecodeManifestSafe(entry.Manifest)
	if !ok {
		cm.deleteCacheKey(key)
		return nil, nil, false
	}
	diags := entry.Diagnostics
	if diags == nil {
		diags = []diag.Diagnostic{}
	}
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
			Deps:                 deps,
		},
		Manifest:    manifestBytes,
		Diagnostics: diagnostics,
	}
	_ = cm.cacheStore.Put(cm.typecheckCacheKey(fingerprint), entry)
}

func (cm *Manager) loadCompileCache(id registry.ID, fingerprint string) (*glua.FunctionProto, bool) {
	cfg := cm.cacheConfig()
	if !cfg.CompileEnabled || !cm.cacheAllowsRead() {
		return nil, false
	}
	key := cm.compileCacheKey(fingerprint)
	entry, ok, err := cm.cacheStore.Get(key)
	if err != nil || !ok || entry == nil {
		return nil, false
	}
	if entry.Meta.SchemaVersion != cache.SchemaVersion {
		cm.deleteCacheKey(key)
		return nil, false
	}
	if entry.Meta.CompileFingerprint != fingerprint || entry.Meta.EntryID != id.String() {
		cm.deleteCacheKey(key)
		return nil, false
	}
	if len(entry.Proto) == 0 {
		cm.deleteCacheKey(key)
		return nil, false
	}
	proto, err := bytecode.Undump(entry.Proto)
	if err != nil {
		cm.deleteCacheKey(key)
		return nil, false
	}
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
			Deps:               deps,
		},
		Proto: data,
	}
	_ = cm.cacheStore.Put(cm.compileCacheKey(fingerprint), entry)
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
	return fp, meta[id], nil
}

func (cm *Manager) compileFingerprints(ids []registry.ID) map[registry.ID]string {
	if len(ids) == 0 {
		return nil
	}
	memo := make(map[registry.ID]string)
	meta := make(map[registry.ID][]cache.DepMeta)
	out := make(map[registry.ID]string, len(ids))
	for _, id := range ids {
		fp, err := cm.compileFingerprintMemo(cm.memGraph, id, memo, meta)
		if err == nil && fp != "" {
			out[id] = fp
		}
	}
	return out
}

func (cm *Manager) compileFingerprintMemo(memGraph *MemoryGraph, id registry.ID, memo map[registry.ID]string, meta map[registry.ID][]cache.DepMeta) (string, error) {
	if v, ok := memo[id]; ok {
		return v, nil
	}
	node, err := memGraph.GetNode(id)
	if err != nil {
		return "", err
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
	fp := CompileFingerprint(node.ID.String(), node.Kind, nodeContentHash(node), node.Method, depFPs)
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
	return fp, meta[id], nil
}

func (cm *Manager) typecheckFingerprints(ids []registry.ID) map[registry.ID]string {
	if len(ids) == 0 {
		return nil
	}
	memo := make(map[registry.ID]string)
	meta := make(map[registry.ID][]cache.DepMeta)
	out := make(map[registry.ID]string, len(ids))
	for _, id := range ids {
		fp, err := cm.typecheckFingerprintMemo(cm.memGraph, id, memo, meta)
		if err == nil && fp != "" {
			out[id] = fp
		}
	}
	return out
}

func (cm *Manager) typecheckFingerprintMemo(memGraph *MemoryGraph, id registry.ID, memo map[registry.ID]string, meta map[registry.ID][]cache.DepMeta) (string, error) {
	if v, ok := memo[id]; ok {
		return v, nil
	}
	node, err := memGraph.GetNode(id)
	if err != nil {
		return "", err
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
	fp := TypecheckFingerprint(node.ID.String(), node.Kind, nodeContentHash(node), node.Method, cm.typeCfgHash, cm.builtinHash, depFPs)
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

func (cm *Manager) deleteCacheFingerprints(compileFPs, typecheckFPs map[registry.ID]string) {
	if !cm.cacheAllowsWrite() {
		return
	}
	deleter, ok := cm.cacheDeleter()
	if !ok {
		return
	}
	for _, fp := range compileFPs {
		if fp == "" {
			continue
		}
		_ = deleter.Delete(cm.compileCacheKey(fp))
	}
	for _, fp := range typecheckFPs {
		if fp == "" {
			continue
		}
		_ = deleter.Delete(cm.typecheckCacheKey(fp))
	}
}

// CacheStore exposes the cache store (nil if disabled).
func (cm *Manager) CacheStore() cache.Store {
	return cm.cacheStore
}

// CacheConfig exposes the cache configuration.
func (cm *Manager) CacheConfig() cache.Config {
	return cm.cacheConfig()
}

// BuiltinManifestHash returns the built-in manifest hash used for cache keys.
func (cm *Manager) BuiltinManifestHash() string {
	return cm.builtinHash
}

// TypecheckConfigHash returns the typecheck config hash used for cache keys.
func (cm *Manager) TypecheckConfigHash() string {
	return cm.typeCfgHash
}
