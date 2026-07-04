// SPDX-License-Identifier: MPL-2.0

package context

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
)

func pairResolver(order int, key *Key, value string) (string, FrameResolver) {
	return value, func(_ context.Context, _ attrs.Attributes) ([]Pair, error) {
		return []Pair{{Key: key, Value: value}}, nil
	}
}

func TestFrameResolvers_ResolveAppliesInOrder(t *testing.T) {
	r := NewFrameResolvers()
	key := &Key{Name: "test.key"}

	// Register out of order; Resolve must apply ascending by order.
	_, third := pairResolver(30, key, "c")
	_, first := pairResolver(10, key, "a")
	_, second := pairResolver(20, key, "b")
	require.NoError(t, r.Register("third", 30, third))
	require.NoError(t, r.Register("first", 10, first))
	require.NoError(t, r.Register("second", 20, second))

	out, err := r.Resolve(context.Background(), nil, nil)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "a", out[0].Value)
	assert.Equal(t, "b", out[1].Value)
	assert.Equal(t, "c", out[2].Value)
}

func TestFrameResolvers_ResolveAppendsToInput(t *testing.T) {
	r := NewFrameResolvers()
	key := &Key{Name: "test.key"}
	_, fn := pairResolver(10, key, "added")
	require.NoError(t, r.Register("one", 10, fn))

	existing := []Pair{{Key: &Key{Name: "pre"}, Value: "keep"}}
	out, err := r.Resolve(context.Background(), nil, existing)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "keep", out[0].Value, "input pairs must be preserved")
	assert.Equal(t, "added", out[1].Value)
}

func TestFrameResolvers_NilReceiverIsNoOp(t *testing.T) {
	var r *FrameResolvers
	existing := []Pair{{Key: &Key{Name: "pre"}, Value: "keep"}}
	out, err := r.Resolve(context.Background(), nil, existing)
	require.NoError(t, err)
	assert.Equal(t, existing, out)
}

func TestFrameResolvers_FirstErrorStopsAndWraps(t *testing.T) {
	sentinel := errors.New("boom")
	r := NewFrameResolvers()
	key := &Key{Name: "test.key"}
	_, ok := pairResolver(10, key, "a")
	require.NoError(t, r.Register("ok", 10, ok))
	require.NoError(t, r.Register("bad", 20, func(_ context.Context, _ attrs.Attributes) ([]Pair, error) {
		return nil, sentinel
	}))
	require.NoError(t, r.Register("after", 30, func(_ context.Context, _ attrs.Attributes) ([]Pair, error) {
		t.Fatal("resolver after a failing one must not run")
		return nil, nil
	}))

	out, err := r.Resolve(context.Background(), nil, nil)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.True(t, errors.Is(err, sentinel), "cause must be preserved for errors.Is")
	assert.Contains(t, err.Error(), "bad", "error must name the failing resolver")
}

func TestFrameResolvers_RegisterRejectsDuplicateAndNil(t *testing.T) {
	r := NewFrameResolvers()
	_, fn := pairResolver(10, &Key{Name: "k"}, "v")
	require.NoError(t, r.Register("dup", 10, fn))
	require.Error(t, r.Register("dup", 20, fn), "duplicate name must be rejected")
	require.Error(t, r.Register("nilfn", 30, nil), "nil function must be rejected")
}

func TestFrameResolvers_ContextRoundTrip(t *testing.T) {
	assert.Nil(t, FrameResolversFrom(context.Background()), "no registry on a bare ctx")

	reg := NewFrameResolvers()
	ctx := WithFrameResolvers(NewRootContext(), reg)
	assert.Same(t, reg, FrameResolversFrom(ctx))
}

func BenchmarkFrameResolvers_Resolve(b *testing.B) {
	r := NewFrameResolvers()
	key := &Key{Name: "bench"}
	for i, name := range []string{"a", "b"} {
		_, fn := pairResolver(i*10, key, name)
		_ = r.Register(name, i*10, fn)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := r.Resolve(ctx, nil, nil)
		if err != nil || len(out) != 2 {
			b.Fatal(err)
		}
	}
}

func BenchmarkFrameResolvers_ResolveEmpty(b *testing.B) {
	var r *FrameResolvers // nil registry — the common no-overlay case
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Resolve(ctx, nil, nil)
	}
}
