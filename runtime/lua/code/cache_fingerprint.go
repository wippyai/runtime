package code

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

const cacheCompilerVersion = "lua-cache-v2"

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
