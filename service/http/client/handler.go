// SPDX-License-Identifier: MPL-2.0

// Package client provides HTTP client command handlers for the dispatcher system.
package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	gohttp "net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/resource"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/security"
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

// WithNetworkRegistry sets the network registry for overlay network resolution.
func WithNetworkRegistry(reg netapi.NetworkRegistry) Option {
	return func(d *Dispatcher) {
		d.networkReg = reg
	}
}

// PoolConfig configures the HTTP client pool.
type PoolConfig struct {
	Timeout         time.Duration
	MaxIdleConns    int
	MaxIdlePerHost  int
	IdleConnTimeout time.Duration
	// MaxClients caps the number of distinct pooled client entries. When
	// the cap is exceeded the least-recently-used entry is evicted and its
	// idle connections are closed. Zero means unbounded.
	MaxClients int
}

// Dispatcher handles HTTP client commands.
type Dispatcher struct {
	networkReg netapi.NetworkRegistry
	pool       *Pool
	poolCfg    PoolConfig
}

// NewDispatcher creates a new HTTP client dispatcher.
func NewDispatcher(opts ...Option) *Dispatcher {
	d := &Dispatcher{}
	for _, opt := range opts {
		opt(d)
	}
	if d.poolCfg.Timeout > 0 || d.poolCfg.MaxIdleConns > 0 || d.poolCfg.MaxClients > 0 {
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
	networkReg := d.networkReg

	go func() {
		result := executeRequest(ctx, d.pool, networkReg, req, true)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, result, nil)
		}
	}()

	return nil
}

func (d *Dispatcher) handleRequestBatch(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	batch := cmd.(*httpapi.RequestBatchCmd)
	networkReg := d.networkReg

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
				responses[idx] = executeRequest(ctx, d.pool, networkReg, req, false)
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

// checkOverlayPrivateIP validates that the request URL does not target a
// private/loopback/link-local IP literal when routed through an overlay network.
//
// IMPORTANT: This function intentionally does NOT resolve DNS for hostnames.
// Overlay networks (Tor, I2P) resolve DNS at the exit node / remote end.
// Performing local DNS resolution here would leak the target hostname to the
// local DNS resolver, defeating the privacy guarantees of the overlay.
//
// Only literal IP addresses (e.g. http://127.0.0.1/) are checked.
// Hostnames are passed through to the overlay for remote resolution.
func checkOverlayPrivateIP(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	host := u.Hostname()
	if host == "" {
		return nil
	}

	// Only check literal IP addresses — never resolve DNS for overlay traffic.
	ip := net.ParseIP(host)
	if ip == nil {
		return nil // hostname — let the overlay resolve it remotely
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		if !security.IsAllowed(ctx, "http_client.private_ip", host, nil) {
			return fmt.Errorf("not allowed: private IP %s via overlay network", host)
		}
	}
	return nil
}

// executeRequest performs a single HTTP request and returns the response.
func executeRequest(ctx context.Context, pool *Pool, networkReg netapi.NetworkRegistry, req *httpapi.RequestCmd, allowStream bool) httpapi.Response {
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
		// Outbound HTTP request cookies; Secure/HttpOnly/SameSite are
		// response-side attributes the client cannot set meaningfully.
		httpReq.AddCookie(&gohttp.Cookie{Name: name, Value: value})
	}
	if req.BasicAuthUser != "" {
		httpReq.SetBasicAuth(req.BasicAuthUser, req.BasicAuthPass)
	}

	// Resolve overlay network: explicit per-request > function default > clearnet
	overlayID := req.OverlayNetwork
	if overlayID == "" {
		overlayID = netapi.GetDefaultNetwork(ctx)
	}

	var client *gohttp.Client
	if overlayID != "" {
		// Refuse to silently fall back to clearnet when an overlay is asked
		// for but the registry is missing — that would leak DNS and the
		// target IP to the local network.
		if networkReg == nil {
			return httpapi.Response{Error: fmt.Sprintf("overlay network %q requested but network registry is not configured", overlayID)}
		}

		// SSRF protection: overlay dialers resolve DNS internally, so check
		// the target URL against private IP ranges before handing off.
		if err := checkOverlayPrivateIP(ctx, reqURL); err != nil {
			return httpapi.Response{Error: err.Error()}
		}

		nid := registry.ParseID(overlayID)
		netSvc, netErr := networkReg.GetNetwork(nid)
		if netErr != nil {
			return httpapi.Response{Error: fmt.Sprintf("overlay network %q: %v", overlayID, netErr)}
		}
		client = pool.GetClientWithDialer(req.Timeout, overlayID, netSvc)
	} else if req.TLS != nil {
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
