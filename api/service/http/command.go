// SPDX-License-Identifier: MPL-2.0

// Package http provides HTTP command types for the dispatcher system.
package http

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("http", Request, RequestBatch)
}

// Command IDs for HTTP operations.
// Range 60-79 is reserved for HTTP commands (stream uses 50-55, ws uses 80-84).
const (
	Request      dispatcher.CommandID = 60 // Execute HTTP request
	RequestBatch dispatcher.CommandID = 61 // Execute multiple HTTP requests concurrently
)

// TLSConfig holds per-request TLS settings for mTLS and custom CA support.
type TLSConfig struct {
	ServerName         string
	CertPEM            []byte
	KeyPEM             []byte
	CAPEM              []byte
	InsecureSkipVerify bool
}

// RequestCmd represents an HTTP request to be executed.
// All fields are serializable for WASM/cross-boundary execution.
type RequestCmd struct {
	Headers         map[string][]string
	Query           map[string]string
	Cookies         map[string]string
	Form            map[string]string
	TLS             *TLSConfig
	Method          string
	URL             string
	BasicAuthPass   string
	UnixSocket      string
	BasicAuthUser   string
	OverlayNetwork  string
	Body            []byte
	Files           []FileUpload
	Timeout         time.Duration
	MaxResponseBody int64
	Stream          bool
}

// FileUpload represents a file to upload in multipart form.
type FileUpload struct {
	FieldName string // Form field name
	FileName  string // Original file name
	Data      []byte // File contents
}

var requestCmdPool = sync.Pool{
	New: func() any { return &RequestCmd{} },
}

// AcquireRequestCmd gets a command from pool.
func AcquireRequestCmd() *RequestCmd {
	return requestCmdPool.Get().(*RequestCmd)
}

// CmdID implements dispatcher.Command.
func (c *RequestCmd) CmdID() dispatcher.CommandID {
	return Request
}

// Release returns the command to pool.
func (c *RequestCmd) Release() {
	c.Method = ""
	c.URL = ""
	c.Headers = nil
	c.Body = nil
	c.Timeout = 0
	c.UnixSocket = ""
	c.Query = nil
	c.Cookies = nil
	c.Form = nil
	c.Files = nil
	c.BasicAuthUser = ""
	c.BasicAuthPass = ""
	c.TLS = nil
	c.Stream = false
	c.MaxResponseBody = 0
	c.OverlayNetwork = ""
	requestCmdPool.Put(c)
}

// Response represents the result of an HTTP request.
// Returned via emit() by the handler.
type Response struct {
	Headers    map[string][]string
	Cookies    map[string]string
	URL        string
	Error      string
	Body       []byte
	StatusCode int
	StreamID   uint64
}

// RequestBatchCmd executes multiple HTTP requests concurrently.
type RequestBatchCmd struct {
	Requests []*RequestCmd
}

var requestBatchCmdPool = sync.Pool{
	New: func() any { return &RequestBatchCmd{} },
}

// AcquireRequestBatchCmd returns a pooled RequestBatchCmd.
func AcquireRequestBatchCmd() *RequestBatchCmd {
	return requestBatchCmdPool.Get().(*RequestBatchCmd)
}

func (c *RequestBatchCmd) CmdID() dispatcher.CommandID {
	return RequestBatch
}

func (c *RequestBatchCmd) Release() {
	for _, req := range c.Requests {
		if req != nil {
			req.Release()
		}
	}
	c.Requests = nil
	requestBatchCmdPool.Put(c)
}

// BatchResponse holds results from batch HTTP requests.
type BatchResponse struct {
	Responses []Response
}
