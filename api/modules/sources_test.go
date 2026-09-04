// SPDX-License-Identifier: MPL-2.0

package modules

import (
	"context"
	"errors"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	regapi "github.com/wippyai/runtime/api/registry"
)

func TestSourceRegistryLoadAndResourceRoots(t *testing.T) {
	registry := NewSourceRegistry()
	registry.Set(Sources{
		ApplicationSourceID: {
			LoadPath: "/repo/app",
			Owner:    ApplicationSourceID,
		},
		"acme/ui": {
			LoadPath:     "/repo/ui/src",
			ResourceRoot: "/repo/ui",
			Owner:        "acme/ui",
		},
		"acme/packed": {
			LoadPath: "/repo/packed.wapp",
		},
	})

	registry.SetLoader(func(context.Context, Sources) ([]regapi.Entry, error) { return nil, nil })
	loaded, err := registry.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slicesEqual(loaded.Owners, []string{ApplicationSourceID, "acme/ui"}) {
		t.Fatalf("owners = %v", loaded.Owners)
	}
	if root, ok := registry.ResourceRoot("acme/ui"); !ok || root != "/repo/ui" {
		t.Fatalf("resource root = %q, %v", root, ok)
	}
	if _, ok := registry.ResourceRoot("acme/packed"); ok {
		t.Fatal("packed normalization input must not expose a resource root")
	}
}

func TestSourceRegistrySnapshotIsDetached(t *testing.T) {
	registry := NewSourceRegistry()
	registry.Set(Sources{"acme/app": {LoadPath: "/pack", Version: "1.0.0"}})

	snapshot := registry.Snapshot()
	snapshot["acme/app"] = Source{LoadPath: "/changed"}
	delete(snapshot, "acme/app")

	if got := registry.Snapshot()["acme/app"].LoadPath; got != "/pack" {
		t.Fatalf("stored source changed through snapshot: %q", got)
	}
}

func TestSourceLoaderReceivesIsolatedSnapshot(t *testing.T) {
	registry := NewSourceRegistry()
	source := Source{LoadPath: "/repo/ui", Owner: "acme/ui", Version: "1.2.3", Digest: "sha256:ui", Sequence: 1}
	registry.Set(Sources{"acme/ui": source})
	var loaded Sources
	registry.SetLoader(func(_ context.Context, sources Sources) ([]regapi.Entry, error) {
		loaded = sources
		return []regapi.Entry{{ID: regapi.NewID("example", "entry"), Kind: "registry.entry"}}, nil
	})

	result, err := registry.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || loaded["acme/ui"] != source {
		t.Fatalf("load = %v, %#v", result.Entries, loaded)
	}
	loaded["acme/ui"] = Source{LoadPath: "/mutated"}
	registry.SetLoader(func(_ context.Context, sources Sources) ([]regapi.Entry, error) {
		if sources["acme/ui"] != source {
			t.Fatalf("loader mutated registry: %#v", sources)
		}
		return nil, nil
	})
	_, err = registry.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestSourceRegistrySetReplacesCompleteSnapshot(t *testing.T) {
	registry := NewSourceRegistry()
	registry.Set(Sources{
		ApplicationSourceID:        {LoadPath: "/repo/app", Owner: ApplicationSourceID},
		ApplicationSourceID + "#1": {LoadPath: "/repo/extra", Owner: ApplicationSourceID},
	})
	registry.Set(Sources{
		ApplicationSourceID: {LoadPath: "/repo/next", Owner: ApplicationSourceID},
	})
	registry.SetLoader(func(_ context.Context, sources Sources) ([]regapi.Entry, error) {
		if len(sources) != 1 || sources[ApplicationSourceID].LoadPath != "/repo/next" {
			t.Fatalf("sources = %#v", sources)
		}
		return nil, nil
	})
	if _, err := registry.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	registry.Set(nil)
	registry.SetLoader(func(_ context.Context, sources Sources) ([]regapi.Entry, error) {
		if len(sources) != 0 {
			t.Fatalf("sources after empty set = %#v", sources)
		}
		return nil, nil
	})
	if _, err := registry.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestSourceTransitionPreservesCompleteIdentity(t *testing.T) {
	registry := NewSourceRegistry()
	oldSource := Source{LoadPath: "/repo/v1", ResourceRoot: "/repo/v1", Owner: "acme/ui", Version: "1.0.0", Digest: "sha256:v1", Sequence: 1, Replacement: true}
	nextSource := Source{LoadPath: "/repo/v2", ResourceRoot: "/repo/v2", Owner: "acme/ui", Version: "2.0.0", Digest: "sha256:v2", Sequence: 1}
	registry.Set(Sources{"acme/ui": oldSource})

	previous, err := registry.Transition(Sources{"acme/ui": nextSource}, nil, "acme/ui", "acme/removed")
	if err != nil || previous["acme/ui"] != oldSource {
		t.Fatalf("transition = %#v, %v", previous, err)
	}
	restored, err := registry.Transition(previous, nil, "acme/ui", "acme/removed")
	if err != nil || restored["acme/ui"] != nextSource {
		t.Fatalf("restore = %#v, %v", restored, err)
	}
}

func TestSourceTransitionWaitsForActiveReload(t *testing.T) {
	registry := NewSourceRegistry()
	oldSource := Source{LoadPath: "/repo/v1", Owner: "acme/ui", Version: "1.0.0", Sequence: 1}
	nextSource := Source{LoadPath: "/repo/v2", Owner: "acme/ui", Version: "2.0.0", Sequence: 1}
	registry.Set(Sources{"acme/ui": oldSource})

	loadStarted := make(chan Sources, 1)
	releaseLoad := make(chan struct{})
	registry.SetLoader(func(_ context.Context, sources Sources) ([]regapi.Entry, error) {
		loadStarted <- sources
		<-releaseLoad
		return nil, nil
	})
	loadDone := make(chan error, 1)
	go func() {
		_, err := registry.Load(context.Background())
		loadDone <- err
	}()
	if loaded := <-loadStarted; loaded["acme/ui"] != oldSource {
		t.Fatalf("active reload source = %#v", loaded)
	}

	transitionCalled := make(chan struct{})
	swapStarted := make(chan struct{})
	swapDone := make(chan error, 1)
	go func() {
		close(swapStarted)
		_, err := registry.Transition(Sources{"acme/ui": nextSource}, func() error {
			close(transitionCalled)
			return nil
		}, "acme/ui")
		swapDone <- err
	}()
	<-swapStarted
	select {
	case <-transitionCalled:
		t.Fatal("backing source changed during active reload")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseLoad)
	if err := <-loadDone; err != nil {
		t.Fatal(err)
	}
	if err := <-swapDone; err != nil {
		t.Fatal(err)
	}
}

func TestSourceTransitionFailureKeepsPreviousIdentity(t *testing.T) {
	registry := NewSourceRegistry()
	oldSource := Source{LoadPath: "/repo/v1", Owner: "acme/ui", Sequence: 1}
	registry.Set(Sources{"acme/ui": oldSource})
	previous, err := registry.Transition(
		Sources{"acme/ui": {LoadPath: "/repo/v2", Owner: "acme/ui"}},
		func() error { return errors.New("transition failed") },
		"acme/ui",
	)
	if err == nil || len(previous) != 0 {
		t.Fatalf("failed transition = %#v, %v", previous, err)
	}
	registry.SetLoader(func(_ context.Context, sources Sources) ([]regapi.Entry, error) {
		if sources["acme/ui"] != oldSource {
			t.Fatalf("source changed after failure: %#v", sources)
		}
		return nil, nil
	})
	_, err = registry.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestSourceRegistryContextOwnership(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	registry := NewSourceRegistry()
	ctx = WithSourceRegistry(ctx, registry)
	if got := GetSourceRegistry(ctx); got != registry {
		t.Fatalf("registry = %p, want %p", got, registry)
	}
	ctxapi.AppFromContext(ctx).Seal()
	if got := WithSourceRegistry(ctx, NewSourceRegistry()); got != ctx {
		t.Fatal("sealed AppContext must retain its registry")
	}
	if got := GetSourceRegistry(context.Background()); got != nil {
		t.Fatalf("registry without AppContext = %p", got)
	}
}

func TestSourceLoadRequiresRegisteredLoader(t *testing.T) {
	_, err := NewSourceRegistry().Load(context.Background())
	if !errors.Is(err, ErrSourceLoaderUnavailable) {
		t.Fatalf("load error = %v", err)
	}
}
