package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	moduleconnect "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1/modulev1connect"
)

type searchAuthService struct {
	moduleconnect.UnimplementedModuleServiceHandler
	authorization string
	query         string
}

func (s *searchAuthService) SearchModules(_ context.Context, req *connect.Request[modulev1.SearchModulesRequest]) (*connect.Response[modulev1.SearchModulesResponse], error) {
	s.authorization = req.Header().Get("Authorization")
	s.query = req.Msg.Query
	return connect.NewResponse(&modulev1.SearchModulesResponse{}), nil
}
func TestSearchUsesConfiguredCredentials(t *testing.T) {
	previousContext := searchCmd.Context()
	t.Cleanup(func() { searchCmd.SetContext(previousContext) })
	searchCmd.SetContext(context.Background())
	for _, token := range []string{"test-private-org-token", ""} {
		t.Run(token, func(t *testing.T) {
			t.Setenv("WIPPY_TOKEN", token)
			service := &searchAuthService{}
			mux := http.NewServeMux()
			path, handler := moduleconnect.NewModuleServiceHandler(service)
			mux.Handle(path, handler)
			server := httptest.NewServer(mux)
			defer server.Close()
			old := searchCmd.Flags().Lookup("registry").Value.String()
			require.NoError(t, searchCmd.Flags().Set("registry", server.URL))
			defer func() { _ = searchCmd.Flags().Set("registry", old) }()
			captureStdout(t, func() { require.NoError(t, runSearch(searchCmd, []string{"private-org"})) })
			want := ""
			if token != "" {
				want = "Bearer " + token
			}
			require.Equal(t, want, service.authorization)
			require.Equal(t, "private-org", service.query)
		})
	}
}
