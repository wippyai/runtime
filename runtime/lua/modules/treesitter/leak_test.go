// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	tslua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	tsc "github.com/tree-sitter/go-tree-sitter"
	ctxapi "github.com/wippyai/runtime/api/context"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
)

func setupLeakCtx(t *testing.T) (context.Context, func()) {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	store := rtresource.NewStore()
	require.NoError(t, rtresource.SetStore(ctx, store))
	return ctx, func() {
		_ = store.Close()
		ctxapi.ReleaseFrameContext(fc)
	}
}

func TestParserWrapperCloseFreesUnderlyingCParser(t *testing.T) {
	ctx, cleanup := setupLeakCtx(t)
	defer cleanup()

	p := tsc.NewParser()
	w := NewParser(ctx, p)
	require.False(t, w.closed)
	require.NotNil(t, w.parser)

	w.Close()

	require.True(t, w.closed, "closed flag must be set after Close()")
	require.Nil(t, w.parser, "parser field must be nil after Close(); "+
		"non-nil parser means the underlying C ts_parser was never freed (CVE: Close() cancels the store cleanup without invoking it)")
}

func TestTreeWrapperCloseFreesUnderlyingCTree(t *testing.T) {
	ctx, cleanup := setupLeakCtx(t)
	defer cleanup()

	parser := tsc.NewParser()
	defer parser.Close()
	require.NoError(t, parser.SetLanguage(tsc.NewLanguage(tslua.Language())))

	src := []byte("local x = 1\n")
	tree := parser.ParseWithOptions(
		func(off int, _ tsc.Point) []byte {
			if off >= len(src) {
				return nil
			}
			return src[off:]
		},
		nil, nil,
	)
	require.NotNil(t, tree)

	w := NewTree(ctx, tree, string(src))
	require.False(t, w.closed)
	require.NotNil(t, w.tree)

	w.Close()

	require.True(t, w.closed, "closed flag must be set after Close()")
	require.Nil(t, w.tree, "tree field must be nil after Close(); "+
		"non-nil tree means the underlying C ts_tree was never freed")
}

func TestCursorWrapperCloseFreesUnderlyingCCursor(t *testing.T) {
	ctx, cleanup := setupLeakCtx(t)
	defer cleanup()

	parser := tsc.NewParser()
	defer parser.Close()
	require.NoError(t, parser.SetLanguage(tsc.NewLanguage(tslua.Language())))

	src := []byte("local x = 1\n")
	tree := parser.ParseWithOptions(
		func(off int, _ tsc.Point) []byte {
			if off >= len(src) {
				return nil
			}
			return src[off:]
		},
		nil, nil,
	)
	require.NotNil(t, tree)
	defer tree.Close()

	cur := tree.Walk()
	require.NotNil(t, cur)

	empty := ""
	w := NewCursor(ctx, cur, &empty)
	require.False(t, w.closed)
	require.NotNil(t, w.cursor)

	w.Close()

	require.True(t, w.closed, "closed flag must be set after Close()")
	require.Nil(t, w.cursor, "cursor field must be nil after Close(); "+
		"non-nil cursor means the underlying C ts_tree_cursor was never freed")
}

func TestQueryWrapperCloseFreesUnderlyingCQuery(t *testing.T) {
	ctx, cleanup := setupLeakCtx(t)
	defer cleanup()

	lang := tsc.NewLanguage(tslua.Language())
	q, qerr := tsc.NewQuery(lang, "(identifier) @id")
	require.Nil(t, qerr, "query compile error")
	require.NotNil(t, q)
	qc := tsc.NewQueryCursor()

	w := NewQuery(ctx, q, qc)
	require.False(t, w.closed)
	require.NotNil(t, w.query)
	require.NotNil(t, w.cursor)

	w.Close()

	require.True(t, w.closed, "closed flag must be set after Close()")
	require.Nil(t, w.query, "query field must be nil after Close(); "+
		"non-nil query means the underlying C ts_query was never freed")
	require.Nil(t, w.cursor, "cursor field must be nil after Close(); "+
		"non-nil cursor means the underlying C ts_query_cursor was never freed")
}

func TestWrapperCloseDoesNotLeakUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in -short mode")
	}
	ctx, cleanup := setupLeakCtx(t)
	defer cleanup()

	const iters = 20000
	parser := tsc.NewParser()
	defer parser.Close()
	require.NoError(t, parser.SetLanguage(tsc.NewLanguage(tslua.Language())))
	src := []byte("local x = 1\n")
	readCb := func(off int, _ tsc.Point) []byte {
		if off >= len(src) {
			return nil
		}
		return src[off:]
	}

	baselineRSS := readVMRSSKb(t)
	t.Logf("baseline VmRSS=%d KB", baselineRSS)

	for i := 0; i < iters; i++ {
		pw := NewParser(ctx, tsc.NewParser())
		tree := parser.ParseWithOptions(readCb, nil, nil)
		tw := NewTree(ctx, tree, string(src))
		cur := tree.Walk()
		empty := ""
		cw := NewCursor(ctx, cur, &empty)
		lang := tsc.NewLanguage(tslua.Language())
		q, qerr := tsc.NewQuery(lang, "(identifier) @id")
		require.Nil(t, qerr)
		qw := NewQuery(ctx, q, tsc.NewQueryCursor())

		pw.Close()
		tw.Close()
		cw.Close()
		qw.Close()

		if i > 0 && i%5000 == 0 {
			runtime.GC()
		}
	}
	runtime.GC()
	runtime.GC()

	finalRSS := readVMRSSKb(t)
	t.Logf("final VmRSS=%d KB after %d iterations (delta=%d KB)", finalRSS, iters, finalRSS-baselineRSS)

	if baselineRSS > 0 && finalRSS > 0 {
		const leakCeilingKB = 200 * 1024
		require.Less(t, finalRSS, int64(leakCeilingKB),
			"VmRSS=%d KB exceeds %d KB ceiling; wrappers are leaking native C memory under load",
			finalRSS, leakCeilingKB)
	}
}

func readVMRSSKb(t *testing.T) int64 {
	t.Helper()
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				v, err := strconv.ParseInt(fields[1], 10, 64)
				require.NoError(t, err)
				return v
			}
		}
	}
	return 0
}
