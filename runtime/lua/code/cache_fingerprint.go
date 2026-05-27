// SPDX-License-Identifier: MPL-2.0

package code

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

const cacheCompilerVersion = "lua-cache-v3"

// CacheCompilerVersion returns the cache compiler version string.
func CacheCompilerVersion() string {
	return cacheCompilerVersion
}

// TypecheckConfigHash hashes typecheck configuration for cache keys.
func TypecheckConfigHash(cfg TypeCheckConfig) string {
	return cache.HashStrings(
		"typecheck",
		strconv.FormatBool(cfg.Enabled),
		strconv.FormatBool(cfg.Strict),
		strconv.FormatBool(cfg.RequireAnnotations),
		strconv.FormatBool(cfg.SkipUntyped),
		strconv.FormatBool(cfg.DisableCache),
		strconv.FormatBool(cfg.Rules.TypeCheck),
		strconv.FormatBool(cfg.Rules.NilCheck),
		strconv.FormatBool(cfg.Rules.Unused),
		strconv.FormatBool(cfg.Rules.Unreachable),
		strconv.FormatBool(cfg.Rules.Exhaustive),
		strconv.FormatBool(cfg.Rules.Readonly),
		strconv.FormatBool(cfg.Rules.Undefined),
		strconv.FormatBool(cfg.Rules.MissingReturn),
	)
}

// ManifestHash hashes a manifest's binary encoding.
func ManifestHash(m *io.Manifest) string {
	if m == nil {
		return ""
	}
	data, err := m.Encode()
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// BuiltinManifestHash hashes a set of builtin manifests.
func BuiltinManifestHash(manifests map[string]*io.Manifest) string {
	if len(manifests) == 0 {
		return ""
	}
	keys := make([]string, 0, len(manifests))
	for k := range manifests {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		mh := ManifestHash(manifests[k])
		_, _ = h.Write([]byte(mh))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CompileFingerprint computes the compile fingerprint for a node.
func CompileFingerprint(entryID, kind, sourceHash, method string, deps []cache.DepFingerprint) string {
	self := cache.HashStrings("compile", cacheCompilerVersion, entryID, kind, method, sourceHash)
	return cache.Fingerprint(self, deps)
}

// TypecheckFingerprint computes the typecheck fingerprint for a node.
func TypecheckFingerprint(entryID, kind, sourceHash, method, typecheckHash, builtinHash string, deps []cache.DepFingerprint) string {
	self := cache.HashStrings("typecheck", cacheCompilerVersion, entryID, kind, method, sourceHash, typecheckHash, builtinHash)
	return cache.Fingerprint(self, deps)
}

// RuntimeFingerprint computes the in-memory compiled-code cache tag. It
// intentionally includes the mutable registry revision so delete/recreate and
// bytecode replacement cannot collide with older compiled artifacts for the
// same registry ID.
func RuntimeFingerprint(entryID, kind, contentHash, method string, revision uint64, deps []cache.DepFingerprint) string {
	self := cache.HashStrings(
		"runtime",
		cacheCompilerVersion,
		entryID,
		kind,
		method,
		contentHash,
		strconv.FormatUint(revision, 10),
	)
	return cache.Fingerprint(self, deps)
}

func BuildOptionsFingerprint(options *BuildOptions) string {
	if options == nil {
		options = NewBuildOptions()
	}

	parts := []string{
		"build-options",
		strconv.Itoa(int(options.Mode)),
	}
	parts = appendSortedIDs(parts, "allowed", options.Allowed)
	parts = appendSortedIDs(parts, "denied", options.Denied)
	parts = appendSortedIDs(parts, "required", options.Required)
	parts = appendSortedStrings(parts, "allowed-class", options.AllowedClasses)
	parts = appendSortedStrings(parts, "denied-class", options.DeniedClasses)

	parts = append(parts, "preloaded", strconv.Itoa(len(options.Preloaded)))
	for _, pre := range options.Preloaded {
		parts = append(parts, pre.Name, pre.ModuleID.String())
	}

	return cache.HashStrings(parts...)
}

func appendSortedIDs(parts []string, label string, ids []registry.ID) []string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	sort.Strings(values)
	parts = append(parts, label, strconv.Itoa(len(values)))
	return append(parts, values...)
}

func appendSortedStrings(parts []string, label string, values []string) []string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	parts = append(parts, label, strconv.Itoa(len(copied)))
	return append(parts, copied...)
}

func runtimeFingerprintMemo(memGraph *MemoryGraph, id registry.ID, memo map[registry.ID]string) (string, error) {
	if v, ok := memo[id]; ok {
		return v, nil
	}
	node, err := memGraph.GetNode(id)
	if err != nil {
		return "", err
	}
	deps, _ := memGraph.GetDependenciesWithAliases(id)
	depFPs := make([]cache.DepFingerprint, 0, len(deps))
	for _, dep := range deps {
		fp, err := runtimeFingerprintMemo(memGraph, dep.ID, memo)
		if err != nil {
			return "", err
		}
		depFPs = append(depFPs, cache.DepFingerprint{
			Alias:       dep.Name,
			ID:          dep.ID.String(),
			Fingerprint: fp,
		})
	}
	fp := RuntimeFingerprint(
		node.ID.String(),
		node.Kind,
		nodeContentHash(node),
		node.Method,
		node.Version.Revision,
		depFPs,
	)
	memo[id] = fp
	return fp, nil
}

func nodeContentHash(node *Node) string {
	if node == nil {
		return ""
	}
	if node.Version.Hash != "" {
		return node.Version.Hash
	}
	return HashNode(node)
}
