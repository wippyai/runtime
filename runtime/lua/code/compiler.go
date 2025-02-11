package lua

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	lru "github.com/ponyruntime/pony/internal/cache"

	"github.com/ponyruntime/pony/api/registry"
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
	glua "github.com/yuin/gopher-lua"
)

const defaultCapacity = 9000

// Compiler composes a MemoryGraph with an LRU cache for compiled nodes.
// compileFn is injected from outside to compile Lua source code into a *glua.FunctionProto.
type Compiler struct {
	memGraph  *MemoryGraph
	cache     *lru.Cache[string, *glua.FunctionProto]
	compileFn func(source string) (*glua.FunctionProto, error)
}

// NewCompiler returns a new Compiler with a MemoryGraph and an LRU cache.
// 'compileFn' is provided by the caller and is used to compile Lua source code.
// 'capacity' and 'ttl' configure the cache.
func NewCompiler(
	compileFn func(source string) (*glua.FunctionProto, error),
) *Compiler {
	return &Compiler{
		memGraph:  NewMemoryGraph(),
		cache:     lru.New[string, *glua.FunctionProto](lru.WithCapacity(defaultCapacity)),
		compileFn: compileFn,
	}
}

// cacheKey computes a hash key based on node.Code and node.Method.
func cacheKey(node *runtime.Node) string {
	h := sha256.New()
	h.Write([]byte(node.Source))
	h.Write([]byte(node.Method))
	return hex.EncodeToString(h.Sum(nil))
}

// getCompiledProto retrieves a node's compiled function prototype from cache or compiles it.
func (c *Compiler) getCompiledProto(node *runtime.Node) (*glua.FunctionProto, error) {
	key := cacheKey(node)
	if proto, ok := c.cache.Get(key); ok {
		return proto, nil
	}

	compiled, err := c.compileFn(node.Source)
	if err != nil {
		return nil, err
	}
	c.cache.Set(key, compiled)
	return compiled, nil
}

// Compile builds the runtime using MemoryGraph.Build, compiles the main node and its dependencies,
// and returns the compiled *glua.FunctionProto for the main node.
// Note: In this simplified example, dependency linking is not performed;
// dependencies are compiled and cached for potential later use.
func (c *Compiler) Compile(entrypoint registry.ID) (*glua.FunctionProto, error) {
	rt, err := c.memGraph.Build(entrypoint)
	if err != nil {
		return nil, err
	}

	mainProto, err := c.getCompiledProto(rt.Main)
	if err != nil {
		return nil, fmt.Errorf("failed to compile main node: %w", err)
	}

	// Compile dependencies to ensure they are cached.
	for _, aliased := range rt.DepProtos {
		if _, err := c.getCompiledProto(aliased.Node); err != nil {
			return nil, fmt.Errorf("failed to compile dependency node %v: %w", aliased.Node.ID, err)
		}
	}

	// In this example, the final result is just the main node's compiled function proto.
	return mainProto, nil
}
