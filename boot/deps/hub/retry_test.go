package hub

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsHubEndpointMissing_StructuredVsBare guards the prod incident
// where a 404 "module not found" (structured hub error envelope) was
// misclassified as "the hub-mediated endpoint is missing", triggering a
// misleading legacy-flow fallback that also failed. Only a bare/non-JSON
// 404 (an older hub with no such route) is endpoint-missing.
func TestIsHubEndpointMissing_StructuredVsBare(t *testing.T) {
	cases := []struct {
		err     error
		name    string
		missing bool
		modNF   bool
	}{
		{name: "bare 404 (old hub, no route)", err: &hubStatusError{statusCode: 404, body: "404 page not found\n"}, missing: true, modNF: false},
		{name: "empty 404", err: &hubStatusError{statusCode: 404, body: ""}, missing: true, modNF: false},
		{name: "structured module not_found", err: &hubStatusError{statusCode: 404, body: `{"error":{"code":"not_found","message":"resource not found"}}`}, missing: false, modNF: true},
		{name: "structured other not_found code", err: &hubStatusError{statusCode: 404, body: `{"error":{"code":"version_not_found"}}`}, missing: false, modNF: false},
		{name: "405 method not allowed", err: &hubStatusError{statusCode: http.StatusMethodNotAllowed, body: ""}, missing: true, modNF: false},
		{name: "409 version exists", err: &hubStatusError{statusCode: 409, body: `{"error":{"code":"version_exists"}}`}, missing: false, modNF: false},
		{name: "403 forbidden", err: &hubStatusError{statusCode: 403, body: `{"error":{"code":"forbidden"}}`}, missing: false, modNF: false},
		{name: "non-hub error", err: assertErr("boom"), missing: false, modNF: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.missing, IsHubEndpointMissing(c.err), "IsHubEndpointMissing")
			assert.Equal(t, c.modNF, IsModuleNotFound(c.err), "IsModuleNotFound")
		})
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
