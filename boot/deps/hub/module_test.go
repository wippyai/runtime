package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	modulev1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1/modulev1connect"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
)

type testModuleService struct {
	modulev1connect.UnimplementedModuleServiceHandler
	getReadme func(context.Context, *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error)
}

func (s *testModuleService) GetReadme(ctx context.Context, req *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error) {
	if s.getReadme != nil {
		return s.getReadme(ctx, req)
	}
	return connect.NewResponse(&modulev1.GetReadmeResponse{}), nil
}

func newModuleTestClient(t *testing.T, svc modulev1connect.ModuleServiceHandler) *Client {
	t.Helper()

	mux := http.NewServeMux()
	path, handler := modulev1connect.NewModuleServiceHandler(svc)
	mux.Handle(path, handler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := NewClient(Options{BaseURL: server.URL})
	require.NoError(t, err)
	return client
}

func TestGetReadme_WithVersion(t *testing.T) {
	var gotReq *modulev1.GetReadmeRequest

	client := newModuleTestClient(t, &testModuleService{
		getReadme: func(_ context.Context, req *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error) {
			gotReq = req.Msg
			return connect.NewResponse(&modulev1.GetReadmeResponse{
				Content:  "# Terminal",
				Filename: "README.md",
				Version:  "1.2.3",
			}), nil
		},
	})

	info, err := client.GetReadme(context.Background(), &GetReadmeParams{
		Org:     "wippy",
		Module:  "terminal",
		Version: "1.2.3",
	})
	require.NoError(t, err)
	require.NotNil(t, gotReq)
	assert.Equal(t, "# Terminal", info.Content)
	assert.Equal(t, "README.md", info.Filename)
	assert.Equal(t, "1.2.3", info.Version)
	assert.Equal(t, "wippy", gotReq.GetModule().GetName().GetOrg())
	assert.Equal(t, "terminal", gotReq.GetModule().GetName().GetName())

	v := gotReq.GetVersion()
	require.NotNil(t, v)
	ver, ok := v.GetValue().(*versionv1.VersionRef_Version)
	require.True(t, ok)
	assert.Equal(t, "1.2.3", ver.Version)
}

func TestGetReadme_WithLabel(t *testing.T) {
	var gotReq *modulev1.GetReadmeRequest

	client := newModuleTestClient(t, &testModuleService{
		getReadme: func(_ context.Context, req *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error) {
			gotReq = req.Msg
			return connect.NewResponse(&modulev1.GetReadmeResponse{
				Content:  "# Terminal Latest",
				Filename: "README.md",
				Version:  "1.3.0",
			}), nil
		},
	})

	info, err := client.GetReadme(context.Background(), &GetReadmeParams{
		Org:    "wippy",
		Module: "terminal",
		Label:  "latest",
	})
	require.NoError(t, err)
	require.NotNil(t, gotReq)
	assert.Equal(t, "# Terminal Latest", info.Content)

	v := gotReq.GetVersion()
	require.NotNil(t, v)
	label, ok := v.GetValue().(*versionv1.VersionRef_Label)
	require.True(t, ok)
	assert.Equal(t, "latest", label.Label)
}

func TestGetReadme_NotFound(t *testing.T) {
	client := newModuleTestClient(t, &testModuleService{
		getReadme: func(_ context.Context, _ *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("missing"))
		},
	})

	info, err := client.GetReadme(context.Background(), &GetReadmeParams{
		Org:    "wippy",
		Module: "missing",
	})
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, ErrModuleNotFound)
}

func TestGetReadme_WithoutVersionOrLabel(t *testing.T) {
	var gotReq *modulev1.GetReadmeRequest

	client := newModuleTestClient(t, &testModuleService{
		getReadme: func(_ context.Context, req *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error) {
			gotReq = req.Msg
			return connect.NewResponse(&modulev1.GetReadmeResponse{
				Content:  "# Default",
				Filename: "README.md",
				Version:  "2.0.0",
			}), nil
		},
	})

	info, err := client.GetReadme(context.Background(), &GetReadmeParams{
		Org:    "wippy",
		Module: "terminal",
	})
	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, gotReq)
	assert.Nil(t, gotReq.GetVersion())
}

func TestGetReadme_VersionPreferredOverLabel(t *testing.T) {
	var gotReq *modulev1.GetReadmeRequest

	client := newModuleTestClient(t, &testModuleService{
		getReadme: func(_ context.Context, req *connect.Request[modulev1.GetReadmeRequest]) (*connect.Response[modulev1.GetReadmeResponse], error) {
			gotReq = req.Msg
			return connect.NewResponse(&modulev1.GetReadmeResponse{}), nil
		},
	})

	_, err := client.GetReadme(context.Background(), &GetReadmeParams{
		Org:     "wippy",
		Module:  "terminal",
		Version: "1.2.3",
		Label:   "latest",
	})
	require.NoError(t, err)
	require.NotNil(t, gotReq)

	v := gotReq.GetVersion()
	require.NotNil(t, v)
	ver, ok := v.GetValue().(*versionv1.VersionRef_Version)
	require.True(t, ok)
	assert.Equal(t, "1.2.3", ver.Version)
}
