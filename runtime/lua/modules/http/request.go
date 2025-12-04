package http

import (
	"fmt"
	"io"
	"mime/multipart"
	basehttp "net/http"
	"strings"

	"github.com/wippyai/runtime/api/runtime/resource"
	httpservice "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonmod "github.com/wippyai/runtime/runtime/lua/modules/json"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	streamservice "github.com/wippyai/runtime/service/fs/stream"
	lua "github.com/yuin/gopher-lua"
)

type Request struct {
	request *basehttp.Request
}

// MultipartFile represents a file from a multipart form.
type MultipartFile struct {
	fileHeader *multipart.FileHeader
	request    *basehttp.Request
}

var requestMethods = map[string]lua.LGFunction{
	"method":          requestMethod,
	"path":            requestPath,
	"query":           requestQuery,
	"query_params":    requestQueryParams,
	"header":          requestHeader,
	"content_type":    requestContentType,
	"content_length":  requestContentLength,
	"host":            requestHost,
	"remote_addr":     requestRemoteAddr,
	"body":            requestBody,
	"body_json":       requestBodyJSON,
	"has_body":        requestHasBody,
	"accepts":         requestAccepts,
	"is_content_type": requestIsContentType,
	"param":           requestParam,
	"params":          requestParams,
	"stream":          requestStream,
	"parse_multipart": requestParseMultipart,
}

var multipartFileMethods = map[string]lua.LGFunction{
	"stream": multipartFileStream,
	"size":   multipartFileSize,
	"name":   multipartFileName,
}

func pushRequest(l *lua.LState, req *Request) {
	value.PushTypedUserData(l, req, requestTypeName)
}

func newRequest(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context available").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reqCtx, ok := httpservice.GetRequestContext(ctx)
	if !ok {
		err := lua.NewLuaError(l, "no HTTP request context found").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	pushRequest(l, &Request{request: reqCtx.Request()})
	l.Push(lua.LNil)
	return 2
}

func checkRequest(l *lua.LState, idx int) *Request { //nolint:unparam
	ud := l.CheckUserData(idx)
	if req, ok := ud.Value.(*Request); ok {
		return req
	}
	l.ArgError(idx, "http.Request expected")
	return nil
}

func requestMethod(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.Method))
	l.Push(lua.LNil)
	return 2
}

func requestPath(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.URL.Path))
	l.Push(lua.LNil)
	return 2
}

func requestQuery(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	key := l.CheckString(2)
	values := req.request.URL.Query()[key]
	if len(values) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}
	l.Push(lua.LString(values[0]))
	l.Push(lua.LNil)
	return 2
}

func requestQueryParams(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	tbl := l.CreateTable(0, len(req.request.URL.Query()))
	for k, vals := range req.request.URL.Query() {
		if len(vals) > 0 {
			tbl.RawSetString(k, lua.LString(strings.Join(vals, ",")))
		}
	}
	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}

func requestHeader(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	key := l.CheckString(2)
	values := req.request.Header[key]
	if len(values) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}
	l.Push(lua.LString(strings.Join(values, ", ")))
	l.Push(lua.LNil)
	return 2
}

func requestContentType(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	ct := req.request.Header.Get("Content-Type")
	if ct == "" {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}
	l.Push(lua.LString(ct))
	l.Push(lua.LNil)
	return 2
}

func requestContentLength(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LNumber(req.request.ContentLength))
	l.Push(lua.LNil)
	return 2
}

func requestHost(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.Host))
	l.Push(lua.LNil)
	return 2
}

func requestRemoteAddr(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(req.request.RemoteAddr))
	l.Push(lua.LNil)
	return 2
}

func requestBody(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	if req.request.Body == nil {
		err := lua.NewLuaError(l, "no body").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	body, err := io.ReadAll(req.request.Body)
	defer func() { _ = req.request.Body.Close() }()
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "failed to read body").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}
	l.Push(lua.LString(body))
	l.Push(lua.LNil)
	return 2
}

func requestBodyJSON(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	if req.request.Body == nil {
		err := lua.NewLuaError(l, "no body").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	body, err := io.ReadAll(req.request.Body)
	defer func() { _ = req.request.Body.Close() }()
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "failed to read body").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}
	val, err := jsonmod.Decode(body)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "invalid JSON").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}
	l.Push(val)
	l.Push(lua.LNil)
	return 2
}

func requestHasBody(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	has := req.request.Body != nil && req.request.ContentLength > 0
	l.Push(lua.LBool(has))
	l.Push(lua.LNil)
	return 2
}

func requestAccepts(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	ct := l.CheckString(2)
	accept := req.request.Header.Get("Accept")
	if accept == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LNil)
		return 2
	}
	for _, a := range strings.Split(accept, ",") {
		a = strings.TrimSpace(a)
		if a == ct || a == "*/*" {
			l.Push(lua.LTrue)
			l.Push(lua.LNil)
			return 2
		}
	}
	l.Push(lua.LFalse)
	l.Push(lua.LNil)
	return 2
}

func requestIsContentType(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	expected := l.CheckString(2)
	actual := req.request.Header.Get("Content-Type")
	l.Push(lua.LBool(strings.HasPrefix(actual, expected)))
	l.Push(lua.LNil)
	return 2
}

func requestParam(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	name := l.CheckString(2)
	routeInfo, ok := httpservice.GetRouteInfo(req.request.Context())
	if !ok {
		err := lua.NewLuaError(l, "no route parameters found").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	val, exists := routeInfo.Params[name]
	if !exists {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}
	l.Push(lua.LString(val))
	l.Push(lua.LNil)
	return 2
}

func requestParams(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	routeInfo, ok := httpservice.GetRouteInfo(req.request.Context())
	if !ok {
		err := lua.NewLuaError(l, "no route parameters found").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	params := l.CreateTable(0, len(routeInfo.Params))
	for k, v := range routeInfo.Params {
		params.RawSetString(k, lua.LString(v))
	}
	l.Push(params)
	l.Push(lua.LNil)
	return 2
}

func requestToString(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("http.Request{method=%s, path=%s}",
		req.request.Method, req.request.URL.Path)))
	return 1
}

// requestStream returns a stream for reading the request body.
func requestStream(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}

	if req.request.Body == nil {
		err := lua.NewLuaError(l, "no body").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	table := resource.GetTable(l.Context())
	if table == nil {
		err := lua.NewLuaError(l, "no resource table available").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	id := streamservice.InsertWithSize(table, req.request.Body, req.request.ContentLength)
	l.Push(streammod.NewStream(l, id))
	l.Push(lua.LNil)
	return 2
}

// requestParseMultipart parses multipart form data from the request.
func requestParseMultipart(l *lua.LState) int {
	req := checkRequest(l, 1)
	if req == nil {
		return 0
	}

	contentType := req.request.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		err := lua.NewLuaError(l, "content type is not multipart/form-data").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	maxMemory := int64(32 << 20) // 32MB default
	if l.GetTop() > 1 {
		maxMemory = int64(l.CheckNumber(2))
	}

	if err := req.request.ParseMultipartForm(maxMemory); err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "failed to parse multipart form").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	result := l.CreateTable(0, 2)

	// Add files
	files := l.CreateTable(0, len(req.request.MultipartForm.File))
	for key, fileHeaders := range req.request.MultipartForm.File {
		filesList := l.CreateTable(len(fileHeaders), 0)
		for i, fh := range fileHeaders {
			value.PushTypedUserData(l, &MultipartFile{
				fileHeader: fh,
				request:    req.request,
			}, multipartFileTypeName)
			filesList.RawSetInt(i+1, l.Get(-1))
			l.Pop(1)
		}
		files.RawSetString(key, filesList)
	}
	result.RawSetString("files", files)

	// Add form values
	if req.request.MultipartForm.Value != nil {
		values := l.CreateTable(0, len(req.request.MultipartForm.Value))
		for key, vals := range req.request.MultipartForm.Value {
			if len(vals) == 1 {
				values.RawSetString(key, lua.LString(vals[0]))
			} else if len(vals) > 1 {
				valList := l.CreateTable(len(vals), 0)
				for i, v := range vals {
					valList.RawSetInt(i+1, lua.LString(v))
				}
				values.RawSetString(key, valList)
			}
		}
		result.RawSetString("values", values)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

// MultipartFile methods

func checkMultipartFile(l *lua.LState, idx int) *MultipartFile { //nolint:unparam
	ud := l.CheckUserData(idx)
	if mf, ok := ud.Value.(*MultipartFile); ok {
		return mf
	}
	l.ArgError(idx, "http.MultipartFile expected")
	return nil
}

func multipartFileStream(l *lua.LState) int {
	mf := checkMultipartFile(l, 1)
	if mf == nil {
		return 0
	}

	file, err := mf.fileHeader.Open()
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "failed to open file").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	table := resource.GetTable(l.Context())
	if table == nil {
		file.Close()
		err := lua.NewLuaError(l, "no resource table available").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	id := streamservice.InsertWithSize(table, file, mf.fileHeader.Size)
	l.Push(streammod.NewStream(l, id))
	l.Push(lua.LNil)
	return 2
}

func multipartFileSize(l *lua.LState) int {
	mf := checkMultipartFile(l, 1)
	if mf == nil {
		return 0
	}
	l.Push(lua.LNumber(mf.fileHeader.Size))
	l.Push(lua.LNil)
	return 2
}

func multipartFileName(l *lua.LState) int {
	mf := checkMultipartFile(l, 1)
	if mf == nil {
		return 0
	}
	l.Push(lua.LString(mf.fileHeader.Filename))
	l.Push(lua.LNil)
	return 2
}

func multipartFileToString(l *lua.LState) int {
	mf := checkMultipartFile(l, 1)
	if mf == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("http.MultipartFile{name=%s, size=%d}",
		mf.fileHeader.Filename, mf.fileHeader.Size)))
	return 1
}
