package bootconfig

import (
	"testing"

	"github.com/wippyai/runtime/api/boot"
)

func TestMergePreservesOverrideLayerOrderWithoutChangingValues(t *testing.T) {
	a := boot.NewConfig(boot.WithSection("override", map[string]any{"app:worker:limits.max_execution_ms": 10, "app:other:port": 1}))
	b := boot.NewConfig(boot.WithSection("override", map[string]any{"app:worker:options.limits.max_execution_ms": 20, "app:other:port": 2}))
	c := boot.NewConfig(boot.WithSection("override", map[string]any{"app:worker:meta.options.limits.max_execution_ms": 30}))
	merged := Merge(Merge(a, b), c)
	layers := boot.ConfigLayers(merged.Sub("override"))
	if len(layers) != 3 {
		t.Fatalf("layers=%d", len(layers))
	}
	for i, key := range []string{"app:worker:limits.max_execution_ms", "app:worker:options.limits.max_execution_ms", "app:worker:meta.options.limits.max_execution_ms"} {
		if got, ok := layers[i].Get(key); !ok || got != (i+1)*10 {
			t.Fatalf("layer %d: %v,%v", i, got, ok)
		}
	}
	if got := merged.Sub("override").GetInt("app:other:port", 0); got != 2 {
		t.Fatalf("existing merge semantics changed: %d", got)
	}
	if Merge(nil, a) != a || Merge(a, nil) != a {
		t.Fatal("nil merge behavior changed")
	}
}
