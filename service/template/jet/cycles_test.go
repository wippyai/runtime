// SPDX-License-Identifier: MPL-2.0

package jet

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/template"
	servicetemplate "github.com/wippyai/runtime/service/template"
)

func TestTemplateDependencyCycles(t *testing.T) {
	tests := []struct {
		sources            map[string]string
		name, entry, chain string
		duringRender       bool
	}{
		{map[string]string{"a": `{{ extends "a" }}`}, "self-extends", "a", "/a -> /a", false},
		{map[string]string{
			"a": `{{ extends "b" }}`, "b": `{{ extends "a" }}`,
		}, "mutual-extends", "a", "/a -> /b -> /a", false},
		{map[string]string{"a": `{{ import "a" }}`}, "self-import", "a", "/a -> /a", false},
		{map[string]string{
			"a": `{{ import "b" }}`, "b": `{{ import "a" }}`,
		}, "mutual-import", "a", "/a -> /b -> /a", false},
		{map[string]string{
			"a": `{{ import "b" }}`, "b": `{{ import "c" }}`, "c": `{{ import "a" }}`,
		}, "three-imports", "a", "/a -> /b -> /c -> /a", false},
		{map[string]string{
			"entry": `{{ extends "a" }}`, "a.jet": `{{ import "nested/b" }}`, "nested/b": `{{ extends "../a.jet" }}`,
		}, "entry", "entry", "/entry -> /a.jet -> /nested/b -> /a.jet", false},
		{map[string]string{
			"entry": `{{ include target }}`, "a": `{{ extends "b" }}`, "b": `{{ import "a" }}`,
		}, "cold-include", "entry", "/a -> /b -> /a", true},
		{map[string]string{
			"entry": `{{ includeIfExists(target) }}`, "a": `{{ import "a" }}`,
		}, "cold-optional-include", "entry", "/a -> /a", true},
		{map[string]string{
			"entry": `{{ exec(target, nil) }}`, "a": `{{ extends "a" }}`,
		}, "cold-exec", "entry", "/a -> /a", true},
	}
	for _, development := range []bool{false, true} {
		for _, tc := range tests {
			t.Run(fmt.Sprintf("%s/development=%t", tc.name, development), func(t *testing.T) {
				const child = "WIPPY_JET_CYCLE_TEST"
				if os.Getenv(child) != t.Name() {
					cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$")
					cmd.Env = append(os.Environ(), child+"="+t.Name(), "GOTRACEBACK=none")
					output, err := cmd.CombinedOutput()
					require.NoError(t, err, "%s", output)
					return
				}
				defer debug.SetMaxStack(debug.SetMaxStack(512 << 10))

				set := newDependencyTestSet(t, development, tc.sources)
				require.NoError(t, set.AddTemplate("healthy", "healthy"))
				for range 2 {
					result, err := set.RenderTemplate(tc.entry, map[string]any{"target": "a"})
					require.Empty(t, result)
					if tc.duringRender {
						require.ErrorIs(t, err, servicetemplate.ErrRenderFailed)
					} else {
						require.ErrorContains(t, err, "failed to get compiled template")
					}
					require.ErrorContains(t, err, "template dependency cycle: "+tc.chain)
					result, err = set.RenderTemplate("healthy", nil)
					require.NoError(t, err)
					require.Equal(t, "healthy", result)
				}
			})
		}
	}
}

func TestAcyclicTemplateDependencies(t *testing.T) {
	for _, development := range []bool{false, true} {
		t.Run(fmt.Sprintf("development=%t", development), func(t *testing.T) {
			sources := map[string]string{
				"entry":         `{{ extends "base" }}{{ import "shared" }}`,
				"base":          `{{ import "shared" }}base {{ yield shared() }}`,
				"shared":        `{{ block shared() }}ok{{ end }}`,
				"aliased-entry": `{{ extends "/folder/.." }}`,
				"/":             `{{ extends "next" }}`,
				"folder/next":   `{{ extends "/" }}`,
				"next":          "alias ok",
			}
			const chainLength = 256
			for i := range chainLength {
				sources[fmt.Sprintf("chain/%d", i)] = fmt.Sprintf(`{{ extends "%d" }}`, i+1)
			}
			sources[fmt.Sprintf("chain/%d", chainLength)] = "tail"
			set := newDependencyTestSet(t, development, sources)
			for name, want := range map[string]string{"entry": "base ok", "chain/0": "tail", "aliased-entry": "alias ok"} {
				result, err := set.RenderTemplate(name, nil)
				require.NoError(t, err)
				require.Equal(t, want, result)
			}
		})
	}
}

func TestConcurrentFirstTemplateRenders(t *testing.T) {
	for _, development := range []bool{false, true} {
		t.Run(fmt.Sprintf("development=%t", development), func(t *testing.T) {
			sources := map[string]string{
				"shared": "shared",
				"child":  `{{ extends "shared" }}`,
				"extra":  `{{ import "shared" }}extra`,
			}
			const workers = 64
			for i := range workers {
				sources[fmt.Sprintf("entry-%d", i)] = `{{ include target }}`
			}
			set := newDependencyTestSet(t, development, sources)

			var ready sync.WaitGroup
			ready.Add(workers)
			start := make(chan struct{})
			results := make(chan error, workers)
			for i := range workers {
				go func() {
					name, target, want := "child", "child", "shared"
					if i%2 == 0 {
						name, target, want = fmt.Sprintf("entry-%d", i), "extra", "extra"
					}
					ready.Done()
					<-start
					result, err := set.RenderTemplate(name, map[string]any{"target": target})
					if err == nil && result != want {
						err = fmt.Errorf("render %q: got %q, want %q", name, result, want)
					}
					results <- err
				}()
			}
			ready.Wait()
			close(start)
			for range workers {
				require.NoError(t, <-results)
			}
		})
	}
}

func newDependencyTestSet(t testing.TB, development bool, sources map[string]string) *Set {
	t.Helper()
	set, err := NewSet(registry.ID{}, &template.SetConfig{
		Engine: template.EngineConfig{DevelopmentMode: development},
	}, nil)
	require.NoError(t, err)
	for name, source := range sources {
		require.NoError(t, set.AddTemplate(name, source))
	}
	return set
}

func TestJetSetDependencyCycles(t *testing.T) {
	const child = "WIPPY_DIRECT_JET_CYCLE_TEST"
	if os.Getenv(child) != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$")
		cmd.Env = append(os.Environ(), child+"=1", "GOTRACEBACK=none")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	defer debug.SetMaxStack(debug.SetMaxStack(512 << 10))

	for _, development := range []bool{false, true} {
		set := newDependencyTestSet(t, development, map[string]string{
			"loop": `{{ import "loop" }}`, "healthy": "healthy",
		})
		engine := set.JetSet()
		_, err := engine.GetTemplate("loop")
		require.ErrorContains(t, err, "template dependency cycle: /loop -> /loop")
		_, err = engine.Parse("inline", `{{ import "loop" }}`)
		require.ErrorContains(t, err, "template dependency cycle: /loop -> /loop")
		_, err = engine.Parse("inline", `{{ import "healthy" }}`)
		require.NoError(t, err)
		_, err = engine.GetTemplate("healthy")
		require.NoError(t, err)
	}
}

func TestTemplateIncludeDepthLimit(t *testing.T) {
	for name, sources := range map[string]map[string]string{
		"self": {"entry": `{{ include "entry" }}`},
		"pair": {"entry": `{{ include "other" }}`, "other": `{{ include "entry" }}`},
	} {
		t.Run(name, func(t *testing.T) {
			set := newDependencyTestSet(t, false, sources)
			require.NoError(t, set.AddTemplate("healthy", "healthy"))
			_, err := set.RenderTemplate("entry", nil)
			require.ErrorIs(t, err, servicetemplate.ErrRenderFailed)
			require.ErrorContains(t, err, "maximum 'include' depth (100000) exceeded")
			result, err := set.RenderTemplate("healthy", nil)
			require.NoError(t, err)
			require.Equal(t, "healthy", result)
		})
	}
}

func BenchmarkTemplateDependencies(b *testing.B) {
	sources := map[string]string{
		"entry":  `{{ extends "base" }}{{ import "shared" }}`,
		"base":   `{{ import "shared" }}Hello {{ yield shared() }}`,
		"shared": `{{ block shared() }}{{ name }}{{ end }}`,
	}

	b.Run("cold-compile", func(b *testing.B) {
		set := newDependencyTestSet(b, false, sources)
		b.ReportAllocs()
		for b.Loop() {
			set.cache.reset()
			if _, err := set.JetSet().GetTemplate("entry"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("cached-render", func(b *testing.B) {
		set := newDependencyTestSet(b, false, sources)
		vars := map[string]any{"name": "world"}
		result, err := set.RenderTemplate("entry", vars)
		require.NoError(b, err)
		require.Equal(b, "Hello world", result)
		b.ReportAllocs()
		for b.Loop() {
			if _, err := set.RenderTemplate("entry", vars); err != nil {
				b.Fatal(err)
			}
		}
	})
}
