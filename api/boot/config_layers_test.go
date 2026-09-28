package boot

import "testing"

func TestConfigLayerScopesMatchResolvedViews(t *testing.T) {
	base := NewConfig(WithSection("override", map[string]any{"nested.value": 10}))
	for _, prefixes := range [][]string{{"override"}, {"override", "nested"}, {""}, {"", "override"}, {"override", ""}} {
		var resolved = base
		layered := WithConfigLayers(base, base)
		for _, prefix := range prefixes {
			resolved = resolved.Sub(prefix)
			layered = layered.Sub(prefix)
		}
		layers := ConfigLayers(layered)
		if len(layers) != 1 {
			t.Fatal("missing input layer")
		}
		want, got := resolved.Keys(), layers[0].Keys()
		if len(want) != len(got) {
			t.Fatalf("prefixes %q: keys %q != %q", prefixes, got, want)
		}
		for _, key := range want {
			a, okA := resolved.Get(key)
			b, okB := layers[0].Get(key)
			if a != b || okA != okB {
				t.Fatalf("prefixes %q: key %q differs", prefixes, key)
			}
		}
	}
}

func TestConfigLayersScopeAndOwnership(t *testing.T) {
	a := NewConfig(WithSection("override", map[string]any{"app:worker:limits.max_execution_ms": 10}))
	b := NewConfig(WithSection("override", map[string]any{"app:worker:options.limits.max_execution_ms": 20}))
	c := WithConfigLayers(b, WithConfigLayers(a, a), b)
	layers := ConfigLayers(c.Sub("override"))
	if len(layers) != 2 {
		t.Fatalf("layers=%d", len(layers))
	}
	if got, ok := layers[0].Get("app:worker:limits.max_execution_ms"); !ok || got != 10 {
		t.Fatalf("first layer=%v,%v", got, ok)
	}
	if got, ok := layers[1].Get("app:worker:options.limits.max_execution_ms"); !ok || got != 20 {
		t.Fatalf("second layer=%v,%v", got, ok)
	}
	layers[0] = nil
	if ConfigLayers(c.Sub("override"))[0] == nil {
		t.Fatal("caller changed retained layer slice")
	}
	if got, ok := c.Get("override.app:worker:options.limits.max_execution_ms"); !ok || got != 20 {
		t.Fatal("resolved config changed")
	}
	if ConfigLayers(nil) != nil || WithConfigLayers(nil, a) != nil {
		t.Fatal("nil config changed")
	}
}
