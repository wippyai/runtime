// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

type fingerprintContext struct {
	toolchain  string
	typeConfig string
	builtins   string
}

type fingerprintResult struct {
	revision    uint64
	context     fingerprintContext
	fingerprint string
	deps        []cache.DepMeta
}

type fingerprintStage uint8

const (
	runtimeStage fingerprintStage = iota
	compileStage
	typecheckStage
)

// fingerprintCache belongs to one graph generation. Snapshots share it, while
// any node or edge mutation starts a new generation, invalidating transitive
// dependents even when their own revisions haven't changed. Each stage retains
// at most one result per node; configuration changes replace that result.
type fingerprintCache struct {
	mu     sync.RWMutex
	stages [3]map[registry.ID]fingerprintResult
}

func (m *MemoryGraph) fingerprintMemo() *fingerprintCache {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.unversioned != 0 {
		return nil
	}
	return m.fingerprints
}

func (c *fingerprintCache) get(stage fingerprintStage, node *Node, context fingerprintContext) (fingerprintResult, bool) {
	// Unversioned graphs permit direct source mutation and cannot be memoized.
	if c == nil || !versionedNode(node) {
		return fingerprintResult{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.stages[stage][node.ID]
	return result, ok && result.revision == node.Version.Revision && result.context == context
}

func (c *fingerprintCache) put(stage fingerprintStage, node *Node, context fingerprintContext, fp string, deps []cache.DepMeta) {
	if c == nil || !versionedNode(node) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stages[stage] == nil {
		c.stages[stage] = make(map[registry.ID]fingerprintResult)
	}
	c.stages[stage][node.ID] = fingerprintResult{revision: node.Version.Revision, context: context, fingerprint: fp, deps: deps}
}

func versionedNode(node *Node) bool {
	return node.Version.Revision != 0 && node.Version.Hash != ""
}
