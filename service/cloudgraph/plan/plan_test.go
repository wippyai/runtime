// SPDX-License-Identifier: MPL-2.0

package plan

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/service/cloudgraph/resource"
)

func demoSpecs() []resource.Spec {
	return []resource.Spec{
		{
			ResourceID:   "poc/postgres",
			Type:         "container",
			ProviderType: "docker",
			Desired:      json.RawMessage(`{"image":"postgres:18-alpine","env":{"POSTGRES_PASSWORD":"secret"}}`),
		},
		{
			ResourceID:   "poc/app",
			Type:         "container",
			ProviderType: "docker",
			Desired:      json.RawMessage(`{"image":"nginx:alpine","env":{"DATABASE_URL":{"$ref":{"resource_id":"poc/postgres","output_key":"dsn"}}}}`),
		},
	}
}

func TestComputeGoldenDemoPlan(t *testing.T) {
	doc, err := Compute("d1", demoSpecs())
	require.NoError(t, err)

	require.Equal(t, []OpEntry{
		{OpID: "d1/poc/app", ResourceID: "poc/app", Action: resource.ActionCreate},
		{OpID: "d1/poc/postgres", ResourceID: "poc/postgres", Action: resource.ActionCreate},
	}, doc.Operations)

	require.Equal(t, []EdgeEntry{
		{Source: "poc/app", Target: "poc/postgres", Kind: resource.DepConfigure},
	}, doc.Edges)

	require.Equal(t, [][]string{
		{"d1/poc/postgres"},
		{"d1/poc/app"},
	}, doc.Waves)

	require.Equal(t, "plan/d1/"+doc.SpecHash[:12], doc.PlanID)
}

func TestComputeDeterministicAcrossOrder(t *testing.T) {
	specs := []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p", Desired: json.RawMessage(`{"x":1}`)},
		{ResourceID: "s/b", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"a": {Target: "s/a", Kind: resource.DepCreate},
				"c": {Target: "s/c", Kind: resource.DepOrdering},
			}},
		{ResourceID: "s/c", Type: "t", ProviderType: "p",
			Desired: json.RawMessage(`{"ref":{"$ref":{"resource_id":"s/a","output_key":"o"}}}`)},
		{ResourceID: "s/d", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"pref": {Target: "s/b", Kind: resource.DepSoft},
			}},
	}

	baseline, err := Compute("d1", specs)
	require.NoError(t, err)
	baselineJSON, err := json.Marshal(baseline)
	require.NoError(t, err)

	rng := rand.New(rand.NewSource(42))
	for range 50 {
		shuffled := make([]resource.Spec, len(specs))
		copy(shuffled, specs)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		doc, err := Compute("d1", shuffled)
		require.NoError(t, err)
		docJSON, err := json.Marshal(doc)
		require.NoError(t, err)
		require.Equal(t, string(baselineJSON), string(docJSON), "plan must be byte-identical across spec order")
	}
}

func TestComputeSoftEdgesNeverSchedule(t *testing.T) {
	specs := []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"b": {Target: "s/b", Kind: resource.DepCreate},
			}},
		{ResourceID: "s/b", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"a": {Target: "s/a", Kind: resource.DepSoft},
			}},
	}

	doc, err := Compute("d1", specs)
	require.NoError(t, err, "hard edge plus reverse soft edge must not be a cycle")
	require.Len(t, doc.Edges, 2, "soft edges stay recorded for observability")
	require.Equal(t, [][]string{{"d1/s/b"}, {"d1/s/a"}}, doc.Waves)
}

func TestComputeRejectsCycles(t *testing.T) {
	specs := []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"b": {Target: "s/b", Kind: resource.DepCreate},
			}},
		{ResourceID: "s/b", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"a": {Target: "s/a", Kind: resource.DepConfigure},
			}},
	}

	_, err := Compute("d1", specs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency_cycle")
	require.Contains(t, err.Error(), "s/a -> s/b")
}

func TestComputeValidation(t *testing.T) {
	_, err := Compute("d1", nil)
	require.Error(t, err, "empty deploy must be rejected")

	_, err = Compute("d1", []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p"},
		{ResourceID: "s/a", Type: "t", ProviderType: "p"},
	})
	require.Error(t, err, "duplicate resource_id must be rejected")

	_, err = Compute("d1", []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"x": {Target: "s/missing", Kind: resource.DepCreate},
			}},
	})
	require.Error(t, err, "unknown dependency target must be rejected")

	_, err = Compute("d1", []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"x": {Target: "s/a", Kind: resource.DepCreate},
			}},
	})
	require.Error(t, err, "self dependency must be rejected")

	_, err = Compute("d1", []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p",
			Dependencies: map[string]resource.DependencyRef{
				"x": {Target: "s/a", Kind: "bogus"},
			}},
	})
	require.Error(t, err, "invalid dependency kind must be rejected")

	_, err = Compute("d1", []resource.Spec{
		{ResourceID: "s/a", Type: "t", ProviderType: "p",
			Desired: json.RawMessage(`{"$ref":{"resource_id":"s/ghost","output_key":"o"}}`)},
	})
	require.Error(t, err, "unknown $ref target must be rejected")
}

func TestRefsAndMaterialize(t *testing.T) {
	desired := json.RawMessage(`{
		"env": {
			"DATABASE_URL": {"$ref":{"resource_id":"poc/postgres","output_key":"dsn"}},
			"BUCKET": {"$ref":{"resource_id":"poc/minio","output_key":"bucket"}},
			"STATIC": "value"
		},
		"list": [{"$ref":{"resource_id":"poc/postgres","output_key":"dsn"}}]
	}`)

	refs, err := Refs(desired)
	require.NoError(t, err)
	require.Equal(t, []OutputRef{
		{ResourceID: "poc/minio", OutputKey: "bucket"},
		{ResourceID: "poc/postgres", OutputKey: "dsn"},
	}, refs)

	materialized, err := Materialize(desired, func(ref OutputRef) (any, error) {
		return ref.ResourceID + ":" + ref.OutputKey, nil
	})
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(materialized, &doc))
	env := doc["env"].(map[string]any)
	require.Equal(t, "poc/postgres:dsn", env["DATABASE_URL"])
	require.Equal(t, "poc/minio:bucket", env["BUCKET"])
	require.Equal(t, "value", env["STATIC"])
	require.Equal(t, "poc/postgres:dsn", doc["list"].([]any)[0])

	_, err = Refs(json.RawMessage(`{"$ref":"bad"}`))
	require.Error(t, err, "malformed $ref must be rejected")
}
