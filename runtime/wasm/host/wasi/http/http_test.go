package http

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wippyai/runtime/runtime/wasm/resource"
)

func TestOutgoingHost(t *testing.T) {
	t.Run("creates with shared resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewOutgoingHost(res)

		if host.Resources() != res {
			t.Error("expected same resources instance")
		}
	})

	t.Run("info returns correct namespace", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewOutgoingHost(res)
		info := host.Info()

		if info.Namespace != OutgoingNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, OutgoingNamespace)
		}
	})

	t.Run("register returns functions", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewOutgoingHost(res)
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}
		if _, ok := reg.Functions["handle"]; !ok {
			t.Error("expected 'handle' function")
		}
	})

	t.Run("outgoing requests use typed table", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewOutgoingHost(res)

		req := &OutgoingRequest{
			Method:  "POST",
			URL:     "https://example.com/api",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"key":"value"}`),
			Timeout: 30 * time.Second,
		}

		handle := host.OutgoingRequests().Insert(req)
		if handle == 0 {
			t.Fatal("expected non-zero handle")
		}

		got, ok := host.OutgoingRequests().Get(handle)
		if !ok {
			t.Fatal("expected request")
		}
		if got.Method != "POST" {
			t.Errorf("method = %s, want POST", got.Method)
		}

		// Also accessible via underlying table
		if res.Len() != 1 {
			t.Errorf("resource count = %d, want 1", res.Len())
		}
	})

	t.Run("resources cleaned up on close", func(t *testing.T) {
		res := resource.NewInstanceResources()
		host := NewOutgoingHost(res)

		host.OutgoingRequests().Insert(&OutgoingRequest{Method: "GET"})
		host.Responses().Insert(&IncomingResponse{Status: 200})

		if res.Len() != 2 {
			t.Errorf("resource count = %d, want 2", res.Len())
		}

		res.Close()

		if res.Len() != 0 {
			t.Errorf("resource count after close = %d, want 0", res.Len())
		}
	})
}

func TestIncomingHost(t *testing.T) {
	t.Run("creates with shared resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewIncomingHost(res)

		if host.Resources() != res {
			t.Error("expected same resources instance")
		}
	})

	t.Run("setup request creates handles", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewIncomingHost(res)
		req := httptest.NewRequest("POST", "/api/users", strings.NewReader(`{"name":"test"}`))
		req.Header.Set("Content-Type", "application/json")

		reqHandle, bodyHandle := host.SetupRequest(req)

		if reqHandle == 0 {
			t.Error("expected non-zero request handle")
		}
		if bodyHandle == 0 {
			t.Error("expected non-zero body handle")
		}

		// Both should be in the shared resource table
		if res.Len() != 2 {
			t.Errorf("resource count = %d, want 2", res.Len())
		}
	})

	t.Run("fields implement dropper", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewIncomingHost(res)

		fields := &HTTPFields{
			Values: map[string][]string{"Content-Type": {"application/json"}},
		}
		handle := host.fields.Insert(fields)

		// Remove should call Drop()
		res.Table().Remove(handle)

		if fields.Values != nil {
			t.Error("expected Values to be nil after Drop")
		}
	})

	t.Run("resource isolation between instances", func(t *testing.T) {
		res1 := resource.NewInstanceResources()
		res2 := resource.NewInstanceResources()
		defer res1.Close()
		defer res2.Close()

		host1 := NewIncomingHost(res1)
		host2 := NewIncomingHost(res2)

		req := httptest.NewRequest("GET", "/", nil)
		h1, _ := host1.SetupRequest(req)
		h2, _ := host2.SetupRequest(req)

		// Same handle values but different tables
		if h1 != h2 {
			t.Log("handles differ, which is fine")
		}

		// Each has its own resources
		if res1.Len() != 2 || res2.Len() != 2 {
			t.Error("expected 2 resources in each table")
		}

		// Close one doesn't affect other
		res1.Close()
		if res2.Len() != 2 {
			t.Error("expected res2 unchanged after res1 close")
		}
	})
}

func TestSharedResourceTable(t *testing.T) {
	t.Run("multiple hosts share same table", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		outgoing := NewOutgoingHost(res)
		incoming := NewIncomingHost(res)

		outgoing.OutgoingRequests().Insert(&OutgoingRequest{Method: "GET"})
		req := httptest.NewRequest("POST", "/", nil)
		incoming.SetupRequest(req)

		// All in same table
		if res.Len() != 3 {
			t.Errorf("total resources = %d, want 3", res.Len())
		}
	})

	t.Run("type isolation", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		outgoing := NewOutgoingHost(res)
		incoming := NewIncomingHost(res)

		// Insert into outgoing
		h := outgoing.OutgoingRequests().Insert(&OutgoingRequest{Method: "GET"})

		// Can't get via incoming's typed table
		_, ok := incoming.incomingRequests.Get(h)
		if ok {
			t.Error("expected type mismatch to return false")
		}
	})
}

func BenchmarkSharedResourceTable(b *testing.B) {
	res := resource.NewInstanceResources()
	defer res.Close()

	host := NewOutgoingHost(res)
	req := &OutgoingRequest{Method: "GET", URL: "https://example.com"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := host.OutgoingRequests().Insert(req)
		host.OutgoingRequests().Get(h)
		res.Table().Remove(h)
	}
}

func BenchmarkIncomingSetup(b *testing.B) {
	res := resource.NewInstanceResources()
	defer res.Close()

	host := NewIncomingHost(res)
	req := httptest.NewRequest("GET", "/", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reqH, bodyH := host.SetupRequest(req)
		res.Table().Remove(reqH)
		res.Table().Remove(bodyH)
	}
}

func TestHTTPResourceDroppers(t *testing.T) {
	t.Run("HTTPIncomingRequest.Drop clears request", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader("test body"))
		resource := &HTTPIncomingRequest{Request: req}

		resource.Drop()

		if resource.Request != nil {
			t.Error("expected Request to be nil after Drop")
		}
	})

	t.Run("HTTPIncomingBody.Drop clears data", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader("test body"))
		body := &HTTPIncomingBody{Request: req, Data: []byte("cached")}

		body.Drop()

		if body.Request != nil {
			t.Error("expected Request to be nil after Drop")
		}
		if body.Data != nil {
			t.Error("expected Data to be nil after Drop")
		}
	})

	t.Run("HTTPOutgoingResponse.Drop clears headers", func(t *testing.T) {
		resp := &HTTPOutgoingResponse{
			StatusCode: 200,
			Headers:    map[string][]string{"Content-Type": {"application/json"}},
		}

		resp.Drop()

		if resp.Headers != nil {
			t.Error("expected Headers to be nil after Drop")
		}
	})

	t.Run("HTTPOutgoingBody.Drop clears data and response", func(t *testing.T) {
		resp := &HTTPOutgoingResponse{StatusCode: 200}
		body := &HTTPOutgoingBody{Response: resp, Data: []byte("response body")}

		body.Drop()

		if body.Response != nil {
			t.Error("expected Response to be nil after Drop")
		}
		if body.Data != nil {
			t.Error("expected Data to be nil after Drop")
		}
	})

	t.Run("HTTPFields.Drop clears values", func(t *testing.T) {
		fields := &HTTPFields{
			Values: map[string][]string{"X-Custom": {"value1", "value2"}},
		}

		fields.Drop()

		if fields.Values != nil {
			t.Error("expected Values to be nil after Drop")
		}
	})
}

func TestHTTPResourceDropOnTableRemove(t *testing.T) {
	t.Run("removing incoming request calls Drop", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewIncomingHost(res)
		req := httptest.NewRequest("GET", "/", nil)
		reqHandle, _ := host.SetupRequest(req)

		incomingReq, _ := host.incomingRequests.Get(reqHandle)
		if incomingReq.Request == nil {
			t.Fatal("expected non-nil request before remove")
		}

		res.Table().Remove(reqHandle)

		if incomingReq.Request != nil {
			t.Error("expected Request to be nil after table remove")
		}
	})

	t.Run("table close calls Drop on all HTTP resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		host := NewIncomingHost(res)

		req := httptest.NewRequest("POST", "/", strings.NewReader("body"))
		reqHandle, bodyHandle := host.SetupRequest(req)

		incomingReq, _ := host.incomingRequests.Get(reqHandle)
		incomingBody, _ := host.incomingBodies.Get(bodyHandle)

		respHandle := host.outgoingResponses.Insert(&HTTPOutgoingResponse{
			StatusCode: 200,
			Headers:    map[string][]string{"X-Test": {"value"}},
		})
		outResp, _ := host.outgoingResponses.Get(respHandle)

		res.Close()

		if incomingReq.Request != nil {
			t.Error("expected incoming request.Request to be nil")
		}
		if incomingBody.Request != nil {
			t.Error("expected incoming body.Request to be nil")
		}
		if outResp.Headers != nil {
			t.Error("expected outgoing response.Headers to be nil")
		}
	})
}

func TestOutgoingResourceDroppers(t *testing.T) {
	t.Run("OutgoingRequest.Drop clears headers and body", func(t *testing.T) {
		req := &OutgoingRequest{
			Method:  "POST",
			URL:     "https://example.com",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"key":"value"}`),
			Timeout: 30 * time.Second,
		}

		req.Drop()

		if req.Headers != nil {
			t.Error("expected Headers to be nil after Drop")
		}
		if req.Body != nil {
			t.Error("expected Body to be nil after Drop")
		}
	})

	t.Run("IncomingResponse.Drop clears headers and body", func(t *testing.T) {
		resp := &IncomingResponse{
			Status:  200,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"result":"ok"}`),
		}

		resp.Drop()

		if resp.Headers != nil {
			t.Error("expected Headers to be nil after Drop")
		}
		if resp.Body != nil {
			t.Error("expected Body to be nil after Drop")
		}
	})

	t.Run("Body.Drop clears data", func(t *testing.T) {
		body := &Body{
			Data:   []byte("chunk of data"),
			Offset: 5,
		}

		body.Drop()

		if body.Data != nil {
			t.Error("expected Data to be nil after Drop")
		}
	})

	t.Run("outgoing table close calls Drop", func(t *testing.T) {
		res := resource.NewInstanceResources()
		host := NewOutgoingHost(res)

		reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
			Method:  "GET",
			Headers: map[string]string{"Accept": "application/json"},
			Body:    []byte("request body"),
		})
		outReq, _ := host.OutgoingRequests().Get(reqHandle)

		respHandle := host.Responses().Insert(&IncomingResponse{
			Status:  200,
			Headers: map[string]string{"Content-Type": "text/plain"},
			Body:    []byte("response body"),
		})
		inResp, _ := host.Responses().Get(respHandle)

		res.Close()

		if outReq.Headers != nil {
			t.Error("expected OutgoingRequest.Headers to be nil")
		}
		if outReq.Body != nil {
			t.Error("expected OutgoingRequest.Body to be nil")
		}
		if inResp.Headers != nil {
			t.Error("expected IncomingResponse.Headers to be nil")
		}
		if inResp.Body != nil {
			t.Error("expected IncomingResponse.Body to be nil")
		}
	})
}
