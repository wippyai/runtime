// Package client provides HTTP client command handlers for the dispatcher system.
package client

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	gohttp "net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/dispatcher/http"
	streamhandler "github.com/wippyai/runtime/service/dispatcher/stream"
)

// RequestHandler processes HTTP request commands.
type RequestHandler struct {
	pool *ClientPool
}

// NewRequestHandler creates a new HTTP request handler with client pooling.
func NewRequestHandler() *RequestHandler {
	return &RequestHandler{
		pool: NewClientPool(),
	}
}

// NewRequestHandlerWithPool creates handler with custom pool.
func NewRequestHandlerWithPool(pool *ClientPool) *RequestHandler {
	return &RequestHandler{pool: pool}
}

// Handle implements dispatcher.Handler.
func (h *RequestHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	req := cmd.(*httpapi.RequestCmd)

	// Build URL with query params
	reqURL := req.URL
	if len(req.Query) > 0 {
		u, err := url.Parse(reqURL)
		if err != nil {
			emit(httpapi.Response{Error: err.Error()})
			return nil
		}
		q := u.Query()
		for k, v := range req.Query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}

	// Build request body
	var body io.Reader
	var contentType string

	if len(req.Files) > 0 {
		// Multipart form with files
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		// Add form fields
		for k, v := range req.Form {
			_ = writer.WriteField(k, v)
		}

		// Add files
		for _, f := range req.Files {
			part, err := writer.CreateFormFile(f.FieldName, f.FileName)
			if err != nil {
				emit(httpapi.Response{Error: err.Error()})
				return nil
			}
			_, _ = part.Write(f.Data)
		}
		writer.Close()

		body = &buf
		contentType = writer.FormDataContentType()
	} else if len(req.Form) > 0 {
		// URL-encoded form
		formData := url.Values{}
		for k, v := range req.Form {
			formData.Set(k, v)
		}
		body = strings.NewReader(formData.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}

	httpReq, err := gohttp.NewRequestWithContext(ctx, req.Method, reqURL, body)
	if err != nil {
		emit(httpapi.Response{Error: err.Error()})
		return nil
	}

	// Set content type if we generated it
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	// Set headers (after content-type so user can override)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Set cookies
	for name, value := range req.Cookies {
		httpReq.AddCookie(&gohttp.Cookie{Name: name, Value: value})
	}

	// Set basic auth
	if req.BasicAuthUser != "" {
		httpReq.SetBasicAuth(req.BasicAuthUser, req.BasicAuthPass)
	}

	client := h.pool.GetClient(req.Timeout, req.UnixSocket)

	resp, err := client.Do(httpReq)
	if err != nil {
		emit(httpapi.Response{Error: err.Error()})
		return nil
	}

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	cookies := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookies[c.Name] = c.Value
	}

	// Streaming response
	if req.Stream {
		registry := streamhandler.GetOrCreateStreamRegistry(ctx)
		streamID := registry.RegisterStream(resp.Body)

		emit(httpapi.Response{
			StatusCode: resp.StatusCode,
			Headers:    headers,
			Cookies:    cookies,
			URL:        resp.Request.URL.String(),
			StreamID:   streamID,
		})
		return nil
	}

	// Normal response - read body
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		emit(httpapi.Response{Error: err.Error()})
		return nil
	}

	emit(httpapi.Response{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Cookies:    cookies,
		Body:       respBody,
		URL:        resp.Request.URL.String(),
	})

	return nil
}

// Service bundles HTTP handlers for convenient registration.
type Service struct {
	Request      *RequestHandler
	RequestBatch *RequestBatchHandler
	pool         *ClientPool
}

// NewService creates a new HTTP service with all handlers initialized.
func NewService() *Service {
	pool := NewClientPool()
	return &Service{
		Request:      NewRequestHandlerWithPool(pool),
		RequestBatch: NewRequestBatchHandlerWithPool(pool),
		pool:         pool,
	}
}

// RegisterAll registers all HTTP handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(httpapi.CmdRequest, s.Request)
	register(httpapi.CmdRequestBatch, s.RequestBatch)
}

// RequestBatchHandler processes batch HTTP request commands.
type RequestBatchHandler struct {
	pool *ClientPool
}

// NewRequestBatchHandler creates a new batch HTTP request handler.
func NewRequestBatchHandler() *RequestBatchHandler {
	return &RequestBatchHandler{pool: NewClientPool()}
}

// NewRequestBatchHandlerWithPool creates handler with custom pool.
func NewRequestBatchHandlerWithPool(pool *ClientPool) *RequestBatchHandler {
	return &RequestBatchHandler{pool: pool}
}

// Handle implements dispatcher.Handler.
func (h *RequestBatchHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	batch := cmd.(*httpapi.RequestBatchCmd)

	if len(batch.Requests) == 0 {
		emit(httpapi.BatchResponse{Responses: []httpapi.Response{}})
		return nil
	}

	responses := make([]httpapi.Response, len(batch.Requests))
	var wg sync.WaitGroup
	wg.Add(len(batch.Requests))

	for i, req := range batch.Requests {
		go func(idx int, req *httpapi.RequestCmd) {
			defer wg.Done()
			responses[idx] = executeRequest(ctx, h.pool, req)
		}(i, req)
	}

	wg.Wait()
	emit(httpapi.BatchResponse{Responses: responses})
	return nil
}

// executeRequest performs a single HTTP request and returns the response.
func executeRequest(ctx context.Context, pool *ClientPool, req *httpapi.RequestCmd) httpapi.Response {
	reqURL := req.URL
	if len(req.Query) > 0 {
		u, err := url.Parse(reqURL)
		if err != nil {
			return httpapi.Response{Error: err.Error()}
		}
		q := u.Query()
		for k, v := range req.Query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}

	var body io.Reader
	var contentType string

	if len(req.Files) > 0 {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		for k, v := range req.Form {
			_ = writer.WriteField(k, v)
		}
		for _, f := range req.Files {
			part, err := writer.CreateFormFile(f.FieldName, f.FileName)
			if err != nil {
				return httpapi.Response{Error: err.Error()}
			}
			_, _ = part.Write(f.Data)
		}
		_ = writer.Close()
		body = &buf
		contentType = writer.FormDataContentType()
	} else if len(req.Form) > 0 {
		formData := url.Values{}
		for k, v := range req.Form {
			formData.Set(k, v)
		}
		body = strings.NewReader(formData.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}

	httpReq, err := gohttp.NewRequestWithContext(ctx, req.Method, reqURL, body)
	if err != nil {
		return httpapi.Response{Error: err.Error()}
	}

	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	for name, value := range req.Cookies {
		httpReq.AddCookie(&gohttp.Cookie{Name: name, Value: value})
	}
	if req.BasicAuthUser != "" {
		httpReq.SetBasicAuth(req.BasicAuthUser, req.BasicAuthPass)
	}

	client := pool.GetClient(req.Timeout, req.UnixSocket)
	resp, err := client.Do(httpReq)
	if err != nil {
		return httpapi.Response{Error: err.Error()}
	}
	defer resp.Body.Close()

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	cookies := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookies[c.Name] = c.Value
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpapi.Response{Error: err.Error()}
	}

	return httpapi.Response{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Cookies:    cookies,
		Body:       respBody,
		URL:        resp.Request.URL.String(),
	}
}
