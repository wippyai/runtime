// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/code/lint"
	"go.uber.org/zap"
)

func TestLintWarmCacheIsReadOnly(t *testing.T) {
	runLintCacheHarness(t, 32, false)
}

func TestLintWarmCacheDoesNotDuplicateRequireDiagnostics(t *testing.T) {
	dir := t.TempDir()
	typeCfg := code.TypeCheckConfig{Enabled: true, Strict: true}
	lcache := lintCache{
		store: cache.NewDiskStore(dir),
		cfg: cache.Config{
			Enabled: true, CompileEnabled: true, TypecheckEnabled: true,
		},
		typecheckHash:   code.TypecheckConfigHash(typeCfg),
		builtinHash:     code.BuiltinManifestHash(nil),
		requireBuiltins: map[string]struct{}{},
	}
	entry := makeLuaSourceEntry(
		regapi.NewID("cache", "require"), nil, `return require("undeclared")`,
	)
	entries := []regapi.Entry{entry}
	report := map[regapi.ID]bool{entry.ID: true}
	linter := lint.New(code.NewTypeChecker(typeCfg, nil), lint.NewRegistry())

	cold := lintEntries(entries, report, linter, lcache, lintConfig{minSeverity: severityError}, nil)
	warm := lintEntries(entries, report, linter, lcache, lintConfig{minSeverity: severityError}, nil)

	require.Equal(t, cold.Diagnostics, warm.Diagnostics)
	require.Len(t, warm.Diagnostics, 1)
	require.Equal(t, "E0007", warm.Diagnostics[0].Code)
}

func TestLintLargeApplicationCacheHarness(t *testing.T) {
	raw := os.Getenv("WIPPY_LINT_SCALE_ENTRIES")
	if raw == "" {
		t.Skip("set WIPPY_LINT_SCALE_ENTRIES to run the large-application harness")
	}
	count, err := strconv.Atoi(raw)
	require.NoError(t, err)
	require.Positive(t, count)
	runLintCacheHarness(t, count, os.Getenv("WIPPY_LINT_SCALE_SHAPE") == "chain")
}

func runLintCacheHarness(t *testing.T, count int, chain bool) {
	t.Helper()
	dir := t.TempDir()
	store := cache.NewBoundedDiskStore(dir, 1<<30, count*3, 64)
	typeCfg := code.TypeCheckConfig{Enabled: true, Strict: true}
	lcache := lintCache{
		store:           store,
		cfg:             cache.Config{Enabled: true, CompileEnabled: true, TypecheckEnabled: true},
		typecheckHash:   code.TypecheckConfigHash(typeCfg),
		builtinHash:     code.BuiltinManifestHash(nil),
		requireBuiltins: map[string]struct{}{},
	}
	entries := make([]regapi.Entry, 0, count)
	report := make(map[regapi.ID]bool, count)
	ids := make([]regapi.ID, count)
	for i := 0; i < count; i++ {
		ids[i] = regapi.NewID("scale", fmt.Sprintf("entry_%06d", i))
	}
	for i, id := range ids {
		var imports map[string]regapi.ID
		if chain && i > 0 {
			imports = map[string]regapi.ID{"previous": ids[i-1]}
		} else if !chain && i == len(ids)-1 && len(ids) > 1 {
			imports = make(map[string]regapi.ID, len(ids)-1)
			for depIndex, depID := range ids[:len(ids)-1] {
				imports[fmt.Sprintf("dep_%06d", depIndex)] = depID
			}
		}
		entries = append(entries, makeLuaEntry(id, imports))
		report[id] = true
	}

	linter := lint.New(code.NewTypeChecker(typeCfg, nil), lint.NewRegistry())
	started := time.Now()
	cold := lintEntries(entries, report, linter, lcache, lintConfig{minSeverity: severityError}, nil)
	require.Zero(t, cold.ErrorCount)
	require.NoError(t, store.Prune())
	coldDuration := time.Since(started)
	before := cacheFileModTimes(t, dir)

	started = time.Now()
	warm := lintEntries(entries, report, linter, lcache, lintConfig{minSeverity: severityError}, nil)
	require.Zero(t, warm.ErrorCount)
	warmDuration := time.Since(started)
	after := cacheFileModTimes(t, dir)
	require.Equal(t, before, after, "a warm lint must not rewrite cache hits")

	manager, err := code.NewCodeManager(zap.NewNop(), lintCacheEventBus{}, code.Config{
		Cache: cache.Config{
			Dir: dir, Enabled: true, CompileEnabled: true, TypecheckEnabled: true,
			MaxBytes: 1 << 30, MaxEntries: count * 3, PruneInterval: 64,
		},
		TypeCheck: typeCfg,
	})
	require.NoError(t, err)
	for _, entry := range entries {
		data := extractEntryData(entry)
		deps := make([]code.Import, 0, len(data.Imports))
		for alias, id := range data.Imports {
			deps = append(deps, code.Import{Alias: alias, ID: id})
		}
		require.NoError(t, manager.AddNode(context.Background(), code.Node{
			ID: entry.ID, Kind: entry.Kind, Source: data.Source, Method: data.Method,
		}, deps))
	}
	runtimeStarted := time.Now()
	_, err = manager.Compile(entries[len(entries)-1].ID, nil)
	require.NoError(t, err)
	runtimeDuration := time.Since(runtimeStarted)
	runtimeAfter := cacheFileModTimes(t, dir)
	require.Equal(t, after, runtimeAfter, "runtime must consume lint artifacts without rewriting them")

	shape := "fan-in"
	if chain {
		shape = "chain"
	}
	t.Logf("entries=%d shape=%s cold=%s warm=%s runtime=%s files=%d",
		count, shape, coldDuration, warmDuration, runtimeDuration, len(runtimeAfter))
}

type lintCacheEventBus struct{}

func (lintCacheEventBus) Send(context.Context, event.Event) {}

func (lintCacheEventBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "lint-cache-test", nil
}

func (lintCacheEventBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "lint-cache-test", nil
}

func (lintCacheEventBus) Unsubscribe(context.Context, event.SubscriberID) {}

func cacheFileModTimes(t *testing.T, root string) map[string]time.Time {
	t.Helper()
	out := make(map[string]time.Time)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = info.ModTime()
		return nil
	}))
	return out
}
