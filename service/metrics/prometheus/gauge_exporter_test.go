// SPDX-License-Identifier: MPL-2.0

package prometheus

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/metrics"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
	impl "github.com/wippyai/runtime/service/metrics"
)

// TestExporter_GaugeIncDecBalancesToZero is the regression test for the gauge
// Inc/Dec anomaly: GaugeInc/GaugeDec must be treated as additive deltas by the
// exporter, not absolute sets. With the bug, a balanced Inc(+1)/Dec(-1) was
// emitted as Set(-1), leaving gauges like wippy_function_in_flight reading -1.
func TestExporter_GaugeIncDecBalancesToZero(t *testing.T) {
	coll := impl.NewCollector(apicfg.Config{})
	exp := NewExporter()
	require.NoError(t, coll.RegisterExporter(exp))

	labels := api.Labels{"k": "v"}
	coll.GaugeInc("wippy_test_in_flight", labels)
	coll.GaugeInc("wippy_test_in_flight", labels)
	coll.GaugeDec("wippy_test_in_flight", labels)
	coll.GaugeDec("wippy_test_in_flight", labels)

	require.NoError(t, coll.Close())

	rec := httptest.NewRecorder()
	exp.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/metrics", nil))
	body := rec.Body.String()
	require.Contains(t, body, "wippy_test_in_flight", "gauge series must be present")

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "wippy_test_in_flight{") {
			assert.True(t, strings.HasSuffix(strings.TrimSpace(line), " 0"),
				"balanced GaugeInc/GaugeDec must read 0, got: %s", line)
			return
		}
	}
	t.Fatalf("wippy_test_in_flight sample line not found in output")
}

// GaugeSet is absolute and must overwrite the running delta total.
func TestExporter_GaugeSetIsAbsolute(t *testing.T) {
	coll := impl.NewCollector(apicfg.Config{})
	exp := NewExporter()
	require.NoError(t, coll.RegisterExporter(exp))

	labels := api.Labels{"k": "v"}
	coll.GaugeInc("wippy_test_gauge", labels) // delta +1
	coll.GaugeSet("wippy_test_gauge", 42, labels)
	coll.GaugeInc("wippy_test_gauge", labels) // delta +1 -> 43

	require.NoError(t, coll.Close())

	rec := httptest.NewRecorder()
	exp.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/metrics", nil))
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "wippy_test_gauge{") {
			assert.True(t, strings.HasSuffix(strings.TrimSpace(line), " 43"),
				"Set(42) then Inc must read 43, got: %s", line)
			return
		}
	}
	t.Fatalf("wippy_test_gauge sample line not found")
}
