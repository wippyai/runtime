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

// RequestCmd represents an HTTP request to be executed.
// All fields are serializable for WASM/cross-boundary execution.
type RequestCmd struct {
	Method     string
	URL        string
	Headers    map[string]string
	Body       []byte
	Timeout    time.Duration
	UnixSocket string

	// Query params (appended to URL)
	Query map[string]string

	// Cookies to send
	Cookies map[string]string

	// Form data (url-encoded, mutually exclusive with Body)
	Form map[string]string

	// Multipart form files
	Files []FileUpload

	// Basic auth
	BasicAuthUser string
	BasicAuthPass string

	// Streaming response (returns stream ID instead of body)
	Stream bool

	// MaxResponseBody limits response body size (0 = use default 120MB)
	MaxResponseBody int64
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
	c.Stream = false
	c.MaxResponseBody = 0
	requestCmdPool.Put(c)
}

// Response represents the result of an HTTP request.
// Returned via emit() by the handler.
type Response struct {
	StatusCode int
	Headers    map[string]string
	Cookies    map[string]string
	Body       []byte
	URL        string
	Error      string

	// StreamID is set when Stream=true, body is nil in that case
	StreamID uint64
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
