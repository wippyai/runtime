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
	streamhandler "github.com/wippyai/runtime/service/fs/stream"
)

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithDebug enables debug output to the given writer.
// TODO: remove after testing is complete
func WithDebug(w io.Writer) Option {
	return func(d *Dispatcher) {
		d.debug = w
	}
}

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
	debug   io.Writer
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

// Start logs dispatcher start.
func (d *Dispatcher) Start(_ context.Context) error {
	if d.debug != nil {
		fmt.Fprintf(d.debug, "[http] dispatcher started\n")
	}
	return nil
}

// Stop logs dispatcher stop.
func (d *Dispatcher) Stop(_ context.Context) error {
	if d.debug != nil {
		fmt.Fprintf(d.debug, "[http] dispatcher stopped pool_size=%d\n", d.pool.Size())
	}
	return nil
}

// RegisterAll registers all HTTP client handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(httpapi.CmdRequest, dispatcher.HandlerFunc(d.handleRequest))
	register(httpapi.CmdRequestBatch, dispatcher.HandlerFunc(d.handleRequestBatch))
}

func (d *Dispatcher) handleRequest(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	req := cmd.(*httpapi.RequestCmd)

	if d.debug != nil {
		fmt.Fprintf(d.debug, "[http] request %s %s stream=%v\n", req.Method, req.URL, req.Stream)
	}

	go func() {
		start := time.Now()
		result := executeRequest(ctx, d.pool, req, true)

		if d.debug != nil {
			fmt.Fprintf(d.debug, "[http] response %s %s status=%d duration=%v err=%s\n",
				req.Method, req.URL, result.StatusCode, time.Since(start), result.Error)
		}

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

	if d.debug != nil {
		fmt.Fprintf(d.debug, "[http] batch request count=%d\n", len(batch.Requests))
	}

	go func() {
		start := time.Now()
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

		if d.debug != nil {
			fmt.Fprintf(d.debug, "[http] batch response count=%d duration=%v\n", len(responses), time.Since(start))
		}

		if ctx.Err() == nil {
			receiver.CompleteYield(tag, httpapi.BatchResponse{Responses: responses}, nil)
		}
	}()

	return nil
}

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
		table := resource.GetTable(ctx)
		if table == nil {
			resp.Body.Close()
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
