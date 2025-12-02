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
	streamhandler "github.com/wippyai/runtime/service/fs/stream"
)

// Dispatcher handles HTTP client commands.
type Dispatcher struct {
	pool *ClientPool
}

// NewDispatcher creates a new HTTP client dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		pool: NewClientPool(),
	}
}

// NewDispatcherWithPool creates dispatcher with custom pool.
func NewDispatcherWithPool(pool *ClientPool) *Dispatcher {
	return &Dispatcher{pool: pool}
}

// Start is a no-op for HTTP client dispatcher.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op for HTTP client dispatcher.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all HTTP client handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(httpapi.CmdRequest, dispatcher.HandlerFunc(d.handleRequest))
	register(httpapi.CmdRequestBatch, dispatcher.HandlerFunc(d.handleRequestBatch))
}

func (d *Dispatcher) handleRequest(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	req := cmd.(*httpapi.RequestCmd)

	go func() {
		emit.Emit(executeRequest(ctx, d.pool, req, true), nil)
	}()

	return nil
}

func (d *Dispatcher) handleRequestBatch(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	batch := cmd.(*httpapi.RequestBatchCmd)

	if len(batch.Requests) == 0 {
		emit.Emit(httpapi.BatchResponse{Responses: []httpapi.Response{}}, nil)
		return nil
	}

	go func() {
		responses := make([]httpapi.Response, len(batch.Requests))
		var wg sync.WaitGroup
		wg.Add(len(batch.Requests))

		for i, req := range batch.Requests {
			go func(idx int, req *httpapi.RequestCmd) {
				defer wg.Done()
				responses[idx] = executeRequest(ctx, d.pool, req, false)
			}(i, req)
		}

		wg.Wait()
		emit.Emit(httpapi.BatchResponse{Responses: responses}, nil)
	}()

	return nil
}

// executeRequest performs a single HTTP request and returns the response.
func executeRequest(ctx context.Context, pool *ClientPool, req *httpapi.RequestCmd, allowStream bool) httpapi.Response {
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

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	cookies := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookies[c.Name] = c.Value
	}

	// Streaming response
	if allowStream && req.Stream {
		registry := streamhandler.GetOrCreateStreamRegistry(ctx)
		streamID := registry.RegisterStream(resp.Body)

		return httpapi.Response{
			StatusCode: resp.StatusCode,
			Headers:    headers,
			Cookies:    cookies,
			URL:        resp.Request.URL.String(),
			StreamID:   streamID,
		}
	}

	defer resp.Body.Close()

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
