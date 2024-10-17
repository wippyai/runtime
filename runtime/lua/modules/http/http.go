package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git.spiralscout.com/estimation-engine/go-lua"
	"go.uber.org/zap"
)

type httpDo func(req *http.Request) (*http.Response, error)

type Module struct {
	log *zap.Logger
	do  httpDo
}

type empty struct{}

func NewHTTPModule(client *http.Client, log *zap.Logger) *Module {
	return NewHTTPModuleWithDo(client.Do, log)
}

func NewHTTPModuleWithDo(do httpDo, log *zap.Logger) *Module {
	return &Module{
		log: log,
		do:  do,
	}
}

func (h *Module) Loader(l *lua.LState) int {
	mod := l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"get":                h.get,
		"delete":             h.delete,
		"head":               h.head,
		"patch":              h.patch,
		"post":               h.post,
		"put":                h.put,
		"request":            h.request,
		"request_batch":      h.requestBatch,
		"encodeURIComponent": h.encodeURIComponent,
		"decodeURIComponent": h.decodeURIComponent,
	})
	registerHTTPResponseType(mod, l)
	l.Push(mod)
	return 1
}

func (h *Module) get(l *lua.LState) int {
	return h.doRequestAndPush(l, "get", l.ToString(1), l.ToTable(2))
}

func (h *Module) delete(l *lua.LState) int {
	return h.doRequestAndPush(l, "delete", l.ToString(1), l.ToTable(2))
}

func (h *Module) encodeURIComponent(l *lua.LState) int {
	if l.GetTop() != 1 {
		l.Push(lua.LNil)
		h.log.Error("should be exactly 1 string argument")
		return 1
	}

	encoded := url.QueryEscape(l.ToString(1))
	l.Push(lua.LString(encoded))
	return 1
}

func (h *Module) decodeURIComponent(l *lua.LState) int {
	if l.GetTop() != 1 {
		l.Push(lua.LNil)
		h.log.Error("should be exactly 1 string argument")
		return 1
	}

	decoded, err := url.QueryUnescape(l.ToString(1))
	if err != nil {
		l.Push(lua.LNil)
		h.log.Error("failed to unescape the string", zap.Error(err))
		return 1
	}

	l.Push(lua.LString(decoded))
	return 1
}

func (h *Module) head(l *lua.LState) int {
	return h.doRequestAndPush(l, "head", l.ToString(1), l.ToTable(2))
}

func (h *Module) patch(l *lua.LState) int {
	return h.doRequestAndPush(l, "patch", l.ToString(1), l.ToTable(2))
}

func (h *Module) post(l *lua.LState) int {
	return h.doRequestAndPush(l, "post", l.ToString(1), l.ToTable(2))
}

func (h *Module) put(l *lua.LState) int {
	return h.doRequestAndPush(l, "put", l.ToString(1), l.ToTable(2))
}

func (h *Module) request(l *lua.LState) int {
	return h.doRequestAndPush(l, l.ToString(1), l.ToString(2), l.ToTable(3))
}

func (h *Module) requestBatch(l *lua.LState) int {
	requests := l.ToTable(1)
	amountRequests := requests.Len()

	errs := make([]error, amountRequests)
	responses := make([]*lua.LUserData, amountRequests)
	sem := make(chan empty, amountRequests)

	i := 0

	requests.ForEach(func(_ lua.LValue, value lua.LValue) {
		requestTable := toTable(value)

		if requestTable != nil {
			method := requestTable.RawGet(lua.LNumber(1)).String()
			u := requestTable.RawGet(lua.LNumber(2)).String()
			options := toTable(requestTable.RawGet(lua.LNumber(3)))

			go func(i int, L *lua.LState, method string, url string, options *lua.LTable) {
				response, err := h.doRequest(L, method, url, options)

				if err == nil {
					errs[i] = nil
					responses[i] = response
				} else {
					errs[i] = err
					responses[i] = nil
				}

				sem <- empty{}
			}(i, l, method, u, options)
		} else {
			errs[i] = errors.New("request must be a table")
			responses[i] = nil
			sem <- empty{}
		}

		i++
	})

	for i = 0; i < amountRequests; i++ {
		<-sem
	}

	hasErrors := false
	errorsTable := l.NewTable()
	responsesTable := l.NewTable()
	for i = 0; i < amountRequests; i++ {
		if errs[i] == nil {
			responsesTable.Append(responses[i])
			errorsTable.Append(lua.LNil)
		} else {
			responsesTable.Append(lua.LNil)
			errorsTable.Append(lua.LString(fmt.Sprintf("%s", errs[i])))
			hasErrors = true
		}
	}

	if hasErrors {
		l.Push(responsesTable)
		l.Push(errorsTable)
		return 2
	}

	l.Push(responsesTable)
	return 1
}

func (h *Module) doRequest(l *lua.LState, method string, url string, options *lua.LTable) (*lua.LUserData, error) {
	req, err := http.NewRequest(strings.ToUpper(method), url, nil)
	if err != nil {
		return nil, err
	}

	// set the request timeout
	if options != nil && options.RawGet(lua.LString("timeout")) != lua.LNil {
		reqTimeout := options.RawGet(lua.LString("timeout"))

		duration := time.Duration(0)
		switch tt := reqTimeout.(type) {
		case lua.LNumber:
			duration = time.Second * time.Duration(tt)
		case lua.LString:
			duration, err = time.ParseDuration(string(tt))
			if err != nil {
				return nil, err
			}
		}

		ctx, cancel := context.WithTimeout(req.Context(), duration)
		defer cancel()

		req = req.WithContext(ctx)
	} else {
		ctx, cancel := context.WithTimeout(req.Context(), time.Second*30)
		defer cancel()

		req = req.WithContext(ctx)
	}

	if options != nil {
		if reqCookies, ok := options.RawGet(lua.LString("cookies")).(*lua.LTable); ok {
			reqCookies.ForEach(func(key lua.LValue, value lua.LValue) {
				req.AddCookie(&http.Cookie{Name: key.String(), Value: value.String()})
			})
		}

		switch reqQuery := options.RawGet(lua.LString("query")).(type) {
		case lua.LString:
			req.URL.RawQuery = reqQuery.String()
		default:
		}

		body := options.RawGet(lua.LString("body"))
		if _, ok := body.(lua.LString); !ok {
			// "form" is deprecated.
			body = options.RawGet(lua.LString("form"))
			// Only set the Content-Type to application/x-www-form-urlencoded
			// when someone uses "form", not for "body".
			if _, ok := body.(lua.LString); ok {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
		}

		switch reqBody := body.(type) {
		case lua.LString:
			bd := reqBody.String()
			req.ContentLength = int64(len(bd))
			req.Body = io.NopCloser(strings.NewReader(bd))
		default:
		}

		// Basic auth
		if reqAuth, ok := options.RawGet(lua.LString("auth")).(*lua.LTable); ok {
			user := reqAuth.RawGetString("user")
			pass := reqAuth.RawGetString("pass")
			if !lua.LVIsFalse(user) && !lua.LVIsFalse(pass) {
				req.SetBasicAuth(user.String(), pass.String())
			} else {
				return nil, fmt.Errorf("auth table must contain no nil user and pass fields")
			}
		}

		// Set these last. That way the code above doesn't overwrite them.
		if reqHeaders, ok := options.RawGet(lua.LString("headers")).(*lua.LTable); ok {
			reqHeaders.ForEach(func(key lua.LValue, value lua.LValue) {
				req.Header.Set(key.String(), value.String())
			})
		}
	}

	res, err := h.do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = res.Body.Close()
	}()

	body, err := io.ReadAll(res.Body)

	if err != nil {
		return nil, err
	}

	return newHTTPResponse(res, &body, len(body), l), nil
}

func (h *Module) doRequestAndPush(l *lua.LState, method string, url string, options *lua.LTable) int {
	response, err := h.doRequest(l, method, url, options)

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("%s", err)))
		return 2
	}

	l.Push(response)
	return 1
}

func toTable(v lua.LValue) *lua.LTable {
	if lv, ok := v.(*lua.LTable); ok {
		return lv
	}
	return nil
}
