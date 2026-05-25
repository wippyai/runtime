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
		name    string
		err     error
		missing bool
		modNF   bool
	}{
		{"bare 404 (old hub, no route)", &hubStatusError{statusCode: 404, body: "404 page not found\n"}, true, false},
		{"empty 404", &hubStatusError{statusCode: 404, body: ""}, true, false},
		{"structured module not_found", &hubStatusError{statusCode: 404, body: `{"error":{"code":"not_found","message":"resource not found"}}`}, false, true},
		{"structured other not_found code", &hubStatusError{statusCode: 404, body: `{"error":{"code":"version_not_found"}}`}, false, false},
		{"405 method not allowed", &hubStatusError{statusCode: http.StatusMethodNotAllowed, body: ""}, true, false},
		{"409 version exists", &hubStatusError{statusCode: 409, body: `{"error":{"code":"version_exists"}}`}, false, false},
		{"403 forbidden", &hubStatusError{statusCode: 403, body: `{"error":{"code":"forbidden"}}`}, false, false},
		{"non-hub error", assertErr("boom"), false, false},
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
