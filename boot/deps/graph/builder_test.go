package graph

import (
	"context"
	"fmt"
	"testing"
)

func TestBuilder_Build(t *testing.T) {
	ctx := context.Background()

	t.Run("empty dependencies", func(t *testing.T) {
		provider := newMockProvider()
		builder := NewBuilder(provider)

		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.ResolvedModules) != 0 {
			t.Errorf("expected 0 resolved modules, got %d", len(result.ResolvedModules))
		}
	})

	t.Run("single dependency", func(t *testing.T) {
		nameA := MustParseName("wippy/actor")
		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "abc123").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.ResolvedModules) != 1 {
			t.Errorf("expected 1 resolved module, got %d", len(result.ResolvedModules))
		}

		key := ModuleKey{Name: nameA, Version: "1.0.0"}
		if _, ok := result.ResolvedModules[key]; !ok {
			t.Errorf("expected module %s to be resolved", key)
		}
	})

	t.Run("simple transitive dependency", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")
		nameC := MustParseName("wippy/c")

		provider := newMockProvider()
		provider.addModule(nameA).
			withVersion("1.0.0", "abc").
			withDependency(nameB, "^1.0.0").
			build()
		provider.addModule(nameB).
			withVersion("1.0.0", "def").
			withDependency(nameC, "^1.0.0").
			build()
		provider.addModule(nameC).
			withVersion("1.0.0", "ghi").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.ResolvedModules) != 3 {
			t.Errorf("expected 3 resolved modules, got %d", len(result.ResolvedModules))
		}

		if result.Stats.TotalLevels != 3 {
			t.Errorf("expected 3 levels, got %d", result.Stats.TotalLevels)
		}
	})

	t.Run("diamond dependency with compatible constraints", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")
		nameC := MustParseName("wippy/c")
		nameD := MustParseName("wippy/d")

		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			withDependency(nameB, "^1.0.0").
			withDependency(nameC, "^1.0.0").
			and().
			addModule(nameB).
			withVersion("1.0.0", "b1").
			withDependency(nameD, "^1.2.0").
			and().
			addModule(nameC).
			withVersion("1.0.0", "c1").
			withDependency(nameD, "^1.0.0").
			and().
			addModule(nameD).
			withVersion("1.0.0", "d1").
			withVersion("1.2.0", "d2").
			withVersion("1.3.0", "d3").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should resolve to 1.3.0 (highest matching both ^1.2.0 and ^1.0.0)
		keyD := ModuleKey{Name: nameD, Version: "1.3.0"}
		if _, ok := result.ResolvedModules[keyD]; !ok {
			t.Errorf("expected D to be resolved to 1.3.0")
		}

		if len(result.Conflicts) != 0 {
			t.Errorf("expected no conflicts, got %d", len(result.Conflicts))
		}
	})

	t.Run("diamond dependency with incompatible constraints", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")
		nameC := MustParseName("wippy/c")
		nameD := MustParseName("wippy/d")

		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			withDependency(nameB, "^1.0.0").
			withDependency(nameC, "^1.0.0").
			and().
			addModule(nameB).
			withVersion("1.0.0", "b1").
			withDependency(nameD, "~1.2.0").
			and().
			addModule(nameC).
			withVersion("1.0.0", "c1").
			withDependency(nameD, "~1.3.0").
			and().
			addModule(nameD).
			withVersion("1.2.0", "d2").
			withVersion("1.3.0", "d3").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Conflicts) == 0 {
			t.Error("expected conflicts, got none")
		}

		if result.Conflicts[0].Reason != ConflictIncompatibleConstraints {
			t.Errorf("expected ConflictIncompatibleConstraints, got %v", result.Conflicts[0].Reason)
		}
	})

	t.Run("circular dependency", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")

		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			withDependency(nameB, "^1.0.0").
			and().
			addModule(nameB).
			withVersion("1.0.0", "b1").
			withDependency(nameA, "^1.0.0").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Conflicts) == 0 {
			t.Error("expected circular dependency conflict, got none")
		}

		hasCircularConflict := false
		for _, c := range result.Conflicts {
			if c.Reason == ConflictCircularDependency {
				hasCircularConflict = true
				break
			}
		}

		if !hasCircularConflict {
			t.Error("expected ConflictCircularDependency in conflicts")
		}
	})

	t.Run("indirect circular dependency", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")
		nameC := MustParseName("wippy/c")

		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			withDependency(nameB, "^1.0.0").
			and().
			addModule(nameB).
			withVersion("1.0.0", "b1").
			withDependency(nameC, "^1.0.0").
			and().
			addModule(nameC).
			withVersion("1.0.0", "c1").
			withDependency(nameA, "^1.0.0").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		hasCircularConflict := false
		for _, c := range result.Conflicts {
			if c.Reason == ConflictCircularDependency {
				hasCircularConflict = true
				break
			}
		}

		if !hasCircularConflict {
			t.Error("expected ConflictCircularDependency for indirect cycle")
		}
	})

	t.Run("multiple root dependencies", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")

		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			and().
			addModule(nameB).
			withVersion("2.0.0", "b1").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
				{Name: nameB, Constraint: "^2.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.ResolvedModules) != 2 {
			t.Errorf("expected 2 resolved modules, got %d", len(result.ResolvedModules))
		}
	})

	t.Run("local dependencies are skipped", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")

		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			withLocalDependency(nameB, "../local/b").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should only have A, not B (local)
		if len(result.ResolvedModules) != 1 {
			t.Errorf("expected 1 resolved module, got %d", len(result.ResolvedModules))
		}
	})

	t.Run("provider error is propagated", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		provider := newMockProvider()
		provider.setError(nameA, fmt.Errorf("network error"))

		builder := NewBuilder(provider)
		_, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err == nil {
			t.Error("expected error from provider, got nil")
		}
	})

	t.Run("no matching version error", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			build()

		builder := NewBuilder(provider)
		_, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^2.0.0"}, // No 2.x versions
			},
		})

		if err == nil {
			t.Error("expected error for no matching version, got nil")
		}
	})

	t.Run("wide graph with many dependencies", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		provider := newMockProvider()

		// Create A with 10 dependencies
		vb := provider.addModule(nameA).withVersion("1.0.0", "a1")
		for i := 1; i <= 10; i++ {
			depName := MustParseName(fmt.Sprintf("wippy/dep%d", i))
			vb = vb.withDependency(depName, "^1.0.0")

			// Add each dependency module
			provider.addModule(depName).
				withVersion("1.0.0", fmt.Sprintf("dep%d", i)).
				build()
		}
		vb.build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.ResolvedModules) != 11 {
			t.Errorf("expected 11 resolved modules, got %d", len(result.ResolvedModules))
		}
	})

	t.Run("deep graph with many levels", func(t *testing.T) {
		provider := newMockProvider()

		// Create chain: a → b → c → d → e
		names := []Name{
			MustParseName("wippy/a"),
			MustParseName("wippy/b"),
			MustParseName("wippy/c"),
			MustParseName("wippy/d"),
			MustParseName("wippy/e"),
		}

		for i := 0; i < len(names); i++ {
			vb := provider.addModule(names[i]).withVersion("1.0.0", fmt.Sprintf("%c", 'a'+i))
			if i < len(names)-1 {
				vb = vb.withDependency(names[i+1], "^1.0.0")
			}
			vb.build()
		}

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: names[0], Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.ResolvedModules) != 5 {
			t.Errorf("expected 5 resolved modules, got %d", len(result.ResolvedModules))
		}

		if result.Stats.TotalLevels != 5 {
			t.Errorf("expected 5 levels, got %d", result.Stats.TotalLevels)
		}
	})

	t.Run("deduplication same module different levels", func(t *testing.T) {
		nameA := MustParseName("wippy/a")
		nameB := MustParseName("wippy/b")
		nameC := MustParseName("wippy/c")
		nameD := MustParseName("wippy/d")

		provider := newMockProvider().
			addModule(nameA).
			withVersion("1.0.0", "a1").
			withDependency(nameB, "^1.0.0").
			withDependency(nameD, "^1.0.0"). // Direct
			and().
			addModule(nameB).
			withVersion("1.0.0", "b1").
			withDependency(nameC, "^1.0.0").
			and().
			addModule(nameC).
			withVersion("1.0.0", "c1").
			withDependency(nameD, "^1.0.0"). // Transitive
			and().
			addModule(nameD).
			withVersion("1.0.0", "d1").
			build()

		builder := NewBuilder(provider)
		result, err := builder.Build(ctx, BuildInput{
			RootDependencies: []DependencyRequest{
				{Name: nameA, Constraint: "^1.0.0"},
			},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// D should only be resolved once despite multiple requests
		count := 0
		for key := range result.ResolvedModules {
			if key.Name == nameD {
				count++
			}
		}

		if count != 1 {
			t.Errorf("expected D to be resolved once, got %d times", count)
		}
	})
}

func TestBuilder_NilProvider(t *testing.T) {
	builder := NewBuilder(nil)
	_, err := builder.Build(context.Background(), BuildInput{
		RootDependencies: []DependencyRequest{
			{Name: MustParseName("wippy/a"), Constraint: "^1.0.0"},
		},
	})

	if err == nil {
		t.Error("expected error for nil provider, got nil")
	}
}
