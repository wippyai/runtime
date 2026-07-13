// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	manifestv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1"
	manifestv1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1/manifestv1connect"
)

func TestResolutionError_String_WithConstraint(t *testing.T) {
	e := ResolutionError{
		Org:        "wippyai",
		Name:       "stdlib",
		Constraint: ">=1.0.0",
		Message:    "no matching version found",
	}
	assert.Equal(t, "wippyai/stdlib@>=1.0.0: no matching version found", e.String())
}

func TestResolutionError_String_WithoutConstraint(t *testing.T) {
	e := ResolutionError{
		Org:     "wippyai",
		Name:    "stdlib",
		Message: "module not found",
	}
	assert.Equal(t, "wippyai/stdlib: module not found", e.String())
}

type constraintManifestService struct {
	manifestv1connect.UnimplementedManifestServiceHandler
}

func (s *constraintManifestService) GetManifest(
	_ context.Context,
	_ *connect.Request[manifestv1.GetManifestRequest],
) (*connect.Response[manifestv1.GetManifestResponse], error) {
	return connect.NewResponse(&manifestv1.GetManifestResponse{
		Manifest: &manifestv1.ModuleManifest{
			Org:     "keeper",
			Name:    "keeper",
			Version: "0.5.57",
			Dependencies: []*manifestv1.ResolvedDependency{{
				Org:        "wippy",
				Name:       "dataflow",
				Version:    "0.5.2",
				Constraint: ">=v0.4.10",
			}},
		},
	}), nil
}

// The declared constraint must survive the wire: hub sends the range it was
// published with, and the client must decode it rather than seeing only the
// version hub resolved that range to.
func TestGetManifest_DecodesDeclaredConstraintFromWire(t *testing.T) {
	mux := http.NewServeMux()
	path, handler := manifestv1connect.NewManifestServiceHandler(&constraintManifestService{})
	mux.Handle(path, handler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := NewClient(Options{BaseURL: server.URL})
	require.NoError(t, err)

	manifest, err := client.GetManifest(context.Background(), "keeper", "keeper", "0.5.57")
	require.NoError(t, err)
	require.Len(t, manifest.Dependencies, 1)

	assert.Equal(t, ">=v0.4.10", manifest.Dependencies[0].Constraint)
	assert.Equal(t, "0.5.2", manifest.Dependencies[0].Version)
}
