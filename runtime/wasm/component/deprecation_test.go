package component

import (
	"testing"

	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testOptionDeprecations []wasmapi.DeprecatedOptionPath

func (d testOptionDeprecations) DeprecatedOptionPaths() []wasmapi.DeprecatedOptionPath {
	return d
}

func TestOptionWarningContainsOnlyIdentityAndMigrationPaths(t *testing.T) {
	core, observed := observer.New(zap.WarnLevel)
	log := zap.New(core)
	id := registry.NewID("app", "converter")
	LogOptionDeprecations(log, id, testOptionDeprecations{{Path: "limits", Replacement: "options.limits"}})
	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("warnings=%d", len(entries))
	}
	fields := entries[0].ContextMap()
	if len(fields) != 4 || fields["code"] != "entry.options.deprecated_path" || fields["entry"] != id.String() || fields["path"] != "limits" || fields["replacement"] != "options.limits" {
		t.Fatalf("unexpected warning fields: %#v", fields)
	}
	LogOptionDeprecations(log, id, testOptionDeprecations{})
	LogOptionDeprecations(log, id, nil)
	LogOptionDeprecations(nil, id, testOptionDeprecations{{Path: "limits"}})
	if observed.Len() != 1 {
		t.Fatal("empty or nil source produced a warning")
	}
}
