// SPDX-License-Identifier: MPL-2.0

// Package client provides HTTP client command handlers for the dispatcher system.
package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	gohttp "net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	httpapi "github.com/wippyai/runtime/api/service/http"
	streamhandler "github.com/wippyai/runtime/system/stream"
)

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithPoolConfig sets custom pool configuration.
func WithPoolConfig(cfg PoolConfig) Option {
	return func(d *Dispatcher) {
		d.poolCfg = cfg
	}
}

// PoolConfig configures the HTTP client pool.
type PoolConfig struct {
	Timeout         time.Duration
	MaxIdleConns    int
	MaxIdlePerHost  int
	IdleConnTimeout time.Duration
}

// Dispatcher handles HTTP client commands.
type Dispatcher struct {
	pool    *Pool
	poolCfg PoolConfig
}

// NewDispatcher creates a new HTTP client dispatcher.
func NewDispatcher(opts ...Option) *Dispatcher {
	d := &Dispatcher{}
	for _, opt := range opts {
		opt(d)
	}
	if d.poolCfg.Timeout > 0 || d.poolCfg.MaxIdleConns > 0 {
		d.pool = NewClientPoolWithConfig(d.poolCfg)
	} else {
		d.pool = NewClientPool()
	}
	return d
}

// Start initializes the dispatcher.
func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

// Stop shuts down the dispatcher.
func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

// RegisterAll registers all HTTP client handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(httpapi.Request, dispatcher.HandlerFunc(d.handleRequest))
	register(httpapi.RequestBatch, dispatcher.HandlerFunc(d.handleRequestBatch))
}

func (d *Dispatcher) handleRequest(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	req := cmd.(*httpapi.RequestCmd)

	go func() {
		result := executeRequest(ctx, d.pool, req, true)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, result, nil)
		}
	}()

	return nil
}

func (d *Dispatcher) handleRequestBatch(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	batch := cmd.(*httpapi.RequestBatchCmd)

	if len(batch.Requests) == 0 {
		receiver.CompleteYield(tag, httpapi.BatchResponse{Responses: []httpapi.Response{}}, nil)
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

		if ctx.Err() == nil {
			receiver.CompleteYield(tag, httpapi.BatchResponse{Responses: responses}, nil)
		}
	}()

	return nil
}

const defaultMaxResponseBody int64 = 120 * 1024 * 1024 // 120MB default limit

// executeRequest performs a single HTTP request and returns the response.
func executeRequest(ctx context.Context, pool *Pool, req *httpapi.RequestCmd, allowStream bool) httpapi.Response {
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

	switch {
	case len(req.Files) > 0:
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		for k, v := range req.Form {
			if err := writer.WriteField(k, v); err != nil {
				return httpapi.Response{Error: fmt.Sprintf("write form field: %v", err)}
			}
		}
		for _, f := range req.Files {
			if f.FieldName == "" {
				return httpapi.Response{Error: "file field name required"}
			}
			part, err := writer.CreateFormFile(f.FieldName, f.FileName)
			if err != nil {
				return httpapi.Response{Error: fmt.Sprintf("create form file: %v", err)}
			}
			if _, err := part.Write(f.Data); err != nil {
				return httpapi.Response{Error: fmt.Sprintf("write file data: %v", err)}
			}
		}
		if err := writer.Close(); err != nil {
			return httpapi.Response{Error: fmt.Sprintf("close multipart: %v", err)}
		}
		body = &buf
		contentType = writer.FormDataContentType()
	case len(req.Form) > 0:
		formData := url.Values{}
		for k, v := range req.Form {
			formData.Set(k, v)
		}
		body = strings.NewReader(formData.Encode())
		contentType = "application/x-www-form-urlencoded"
	case len(req.Body) > 0:
		body = bytes.NewReader(req.Body)
	}

	httpReq, err := gohttp.NewRequestWithContext(ctx, req.Method, reqURL, body)
	if err != nil {
		return httpapi.Response{Error: err.Error()}
	}

	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for k, vs := range req.Headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	for name, value := range req.Cookies {
		// Outgoing client request: AddCookie serializes only Name + Value
		// into the Cookie: header per RFC 6265. Secure/HttpOnly/SameSite
		// are server-set attributes ignored on this side, so G124 is a
		// false positive here.
		httpReq.AddCookie(&gohttp.Cookie{Name: name, Value: value}) //nolint:gosec // G124 — client-side cookie
	}
	if req.BasicAuthUser != "" {
		httpReq.SetBasicAuth(req.BasicAuthUser, req.BasicAuthPass)
	}

	var client *gohttp.Client
	if req.TLS != nil {
		var tlsErr error
		client, tlsErr = pool.GetClientWithTLS(req.Timeout, req.UnixSocket, req.TLS)
		if tlsErr != nil {
			return httpapi.Response{Error: tlsErr.Error()}
		}
	} else {
		client = pool.GetClient(req.Timeout, req.UnixSocket)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		// Per Go docs, resp may be non-nil even on error and body needs closing
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return httpapi.Response{Error: err.Error()}
	}

	headers := make(map[string][]string, len(resp.Header))
	for k, vs := range resp.Header {
		headers[k] = vs
	}

	cookies := make(map[string]string)
	for _, c := range resp.Cookies() {
		cookies[c.Name] = c.Value
	}

	// Streaming response
	if allowStream && req.Stream {
		table := resource.GetTable(ctx)
		if table == nil {
			_ = resp.Body.Close()
			return httpapi.Response{Error: "resource table not available"}
		}
		streamID := streamhandler.Insert(table, resp.Body)

		return httpapi.Response{
			StatusCode: resp.StatusCode,
			Headers:    headers,
			Cookies:    cookies,
			URL:        resp.Request.URL.String(),
			StreamID:   streamID,
		}
	}

	defer func() { _ = resp.Body.Close() }()

	maxBody := req.MaxResponseBody
	if maxBody <= 0 {
		maxBody = defaultMaxResponseBody
	}

	limitedReader := io.LimitReader(resp.Body, maxBody+1)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return httpapi.Response{Error: err.Error()}
	}

	if int64(len(respBody)) > maxBody {
		return httpapi.Response{Error: fmt.Sprintf("response body too large (max %d bytes)", maxBody)}
	}

	return httpapi.Response{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Cookies:    cookies,
		Body:       respBody,
		URL:        resp.Request.URL.String(),
	}
}
