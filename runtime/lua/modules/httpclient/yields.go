package httpclient

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/dispatcher/http"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
)

// RequestYield wraps the generic RequestCmd for Lua.
type RequestYield struct {
	*httpapi.RequestCmd
}

var requestYieldPool = sync.Pool{
	New: func() any { return &RequestYield{} },
}

func AcquireRequestYield() *RequestYield {
	y := requestYieldPool.Get().(*RequestYield)
	y.RequestCmd = httpapi.AcquireRequestCmd()
	return y
}

func ReleaseRequestYield(y *RequestYield) {
	if y.RequestCmd != nil {
		y.RequestCmd.Release()
		y.RequestCmd = nil
	}
	requestYieldPool.Put(y)
}

func (y *RequestYield) String() string                { return "<http_request_yield>" }
func (y *RequestYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *RequestYield) CmdID() dispatcher.CommandID   { return httpapi.CmdRequest }
func (y *RequestYield) ToCommand() dispatcher.Command { return y.RequestCmd }
func (y *RequestYield) Release()                      { ReleaseRequestYield(y) }

// HandleResult converts HTTP response to Lua values.
func (y *RequestYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(httpapi.Response)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != "" {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error)}
	}

	tbl := l.CreateTable(0, 6)
	tbl.RawSetString("status_code", lua.LNumber(resp.StatusCode))
	tbl.RawSetString("url", lua.LString(resp.URL))

	if len(resp.Body) > 0 {
		tbl.RawSetString("body", lua.LString(resp.Body))
	}
	if len(resp.Headers) > 0 {
		headers := l.CreateTable(0, len(resp.Headers))
		for k, v := range resp.Headers {
			headers.RawSetString(k, lua.LString(v))
		}
		tbl.RawSetString("headers", headers)
	}
	if len(resp.Cookies) > 0 {
		cookies := l.CreateTable(0, len(resp.Cookies))
		for k, v := range resp.Cookies {
			cookies.RawSetString(k, lua.LString(v))
		}
		tbl.RawSetString("cookies", cookies)
	}
	if resp.StreamID > 0 {
		tbl.RawSetString("stream", stream.NewStream(l, resp.StreamID))
	}

	return []lua.LValue{tbl, lua.LNil}
}

// RequestBatchYield wraps batch HTTP requests for Lua.
type RequestBatchYield struct {
	*httpapi.RequestBatchCmd
}

var requestBatchYieldPool = sync.Pool{
	New: func() any { return &RequestBatchYield{} },
}

func AcquireRequestBatchYield() *RequestBatchYield {
	y := requestBatchYieldPool.Get().(*RequestBatchYield)
	y.RequestBatchCmd = httpapi.AcquireRequestBatchCmd()
	return y
}

func ReleaseRequestBatchYield(y *RequestBatchYield) {
	if y.RequestBatchCmd != nil {
		y.RequestBatchCmd.Release()
		y.RequestBatchCmd = nil
	}
	requestBatchYieldPool.Put(y)
}

func (y *RequestBatchYield) String() string                { return "<http_request_batch_yield>" }
func (y *RequestBatchYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *RequestBatchYield) CmdID() dispatcher.CommandID   { return httpapi.CmdRequestBatch }
func (y *RequestBatchYield) ToCommand() dispatcher.Command { return y.RequestBatchCmd }
func (y *RequestBatchYield) Release()                      { ReleaseRequestBatchYield(y) }

// HandleResult converts batch HTTP responses to Lua values.
func (y *RequestBatchYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	batch, ok := data.(httpapi.BatchResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid batch response type")}
	}

	responses := l.CreateTable(len(batch.Responses), 0)
	errors := l.CreateTable(len(batch.Responses), 0)
	hasErrors := false

	for i, resp := range batch.Responses {
		idx := i + 1 // Lua is 1-indexed
		if resp.Error != "" {
			responses.RawSetInt(idx, lua.LNil)
			errors.RawSetInt(idx, lua.LString(resp.Error))
			hasErrors = true
			continue
		}

		tbl := l.CreateTable(0, 6)
		tbl.RawSetString("status_code", lua.LNumber(resp.StatusCode))
		tbl.RawSetString("url", lua.LString(resp.URL))

		if len(resp.Body) > 0 {
			tbl.RawSetString("body", lua.LString(resp.Body))
		}
		if len(resp.Headers) > 0 {
			headers := l.CreateTable(0, len(resp.Headers))
			for k, v := range resp.Headers {
				headers.RawSetString(k, lua.LString(v))
			}
			tbl.RawSetString("headers", headers)
		}
		if len(resp.Cookies) > 0 {
			cookies := l.CreateTable(0, len(resp.Cookies))
			for k, v := range resp.Cookies {
				cookies.RawSetString(k, lua.LString(v))
			}
			tbl.RawSetString("cookies", cookies)
		}

		responses.RawSetInt(idx, tbl)
		errors.RawSetInt(idx, lua.LNil)
	}

	if hasErrors {
		return []lua.LValue{responses, errors}
	}
	return []lua.LValue{responses, lua.LNil}
}
