// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"io"
	http "net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

type ownershipRoundTripper func(*http.Request) (*http.Response, error)

func (f ownershipRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := f(req)
	if response != nil && response.Request == nil {
		response.Request = req
	}
	return response, err
}

type closeRecordingBody struct {
	reader io.Reader
	closes atomic.Int32
}

func (b *closeRecordingBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *closeRecordingBody) Close() error {
	b.closes.Add(1)
	return nil
}

func ownershipPool(rt http.RoundTripper) *Pool {
	pool := NewClientPool()
	pool.defaultClient = &http.Client{Transport: rt}
	return pool
}

func ownershipResponse(body io.ReadCloser, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       body,
	}
}

func requireErrorOnly(t *testing.T, response httpapi.Response) {
	t.Helper()
	require.NotEmpty(t, response.Error)
	require.Zero(t, response.StatusCode)
	require.Empty(t, response.Body)
	require.Zero(t, response.StreamID)
	require.Nil(t, response.Headers)
	require.Nil(t, response.Cookies)
	require.Empty(t, response.URL)
}

func TestN02HTTPBodyClosesWithoutResourceTable(t *testing.T) {
	body := &closeRecordingBody{reader: strings.NewReader("stream")}
	pool := ownershipPool(ownershipRoundTripper(func(*http.Request) (*http.Response, error) {
		return ownershipResponse(body, http.StatusOK), nil
	}))

	response := executeRequest(context.Background(), pool, nil, &httpapi.RequestCmd{
		Method: http.MethodGet,
		URL:    "http://local.invalid/stream",
		Stream: true,
	}, true)

	requireErrorOnly(t, response)
	require.Equal(t, int32(1), body.closes.Load())
}

func TestN03HTTPBodyOwnershipTransfersToTable(t *testing.T) {
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))
	defer func() { require.NoError(t, store.Close()) }()

	body := &closeRecordingBody{reader: strings.NewReader("stream")}
	pool := ownershipPool(ownershipRoundTripper(func(*http.Request) (*http.Response, error) {
		return ownershipResponse(body, http.StatusOK), nil
	}))

	response := executeRequest(ctx, pool, nil, &httpapi.RequestCmd{
		Method: http.MethodGet,
		URL:    "http://local.invalid/stream",
		Stream: true,
	}, true)

	require.Empty(t, response.Error)
	require.NotZero(t, response.StreamID)
	require.Equal(t, int32(0), body.closes.Load(), "adoption must transfer ownership without closing")
	_, removed := store.Table().Remove(resource.Handle(response.StreamID))
	require.True(t, removed)
	require.Equal(t, int32(1), body.closes.Load(), "table removal must close the adopted body once")
}

func TestN04RedirectFailureDoesNotDoubleCloseBody(t *testing.T) {
	redirectBody := &closeRecordingBody{reader: strings.NewReader("redirect")}
	redirectErr := errors.New("redirect rejected")
	client := &http.Client{
		Transport: ownershipRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/first" {
				response := ownershipResponse(redirectBody, http.StatusFound)
				response.Header.Set("Location", "/second")
				return response, nil
			}
			return nil, errors.New("unexpected redirected request")
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error { return redirectErr },
	}
	pool := NewClientPool()
	pool.defaultClient = client

	response := executeRequest(context.Background(), pool, nil, &httpapi.RequestCmd{
		Method: http.MethodGet,
		URL:    "http://local.invalid/first",
	}, true)

	requireErrorOnly(t, response)
	require.ErrorContains(t, errors.New(response.Error), redirectErr.Error())
	require.Equal(t, int32(1), redirectBody.closes.Load(), "net/http owns redirect-error body cleanup")
}

type partialErrorReader struct {
	read bool
	err  error
}

func (r *partialErrorReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, "partial"), nil
	}
	return 0, r.err
}

func TestN05PartialBodyReadReturnsErrorOnly(t *testing.T) {
	readErr := errors.New("injected body read failure")
	body := &closeRecordingBody{reader: &partialErrorReader{err: readErr}}
	pool := ownershipPool(ownershipRoundTripper(func(*http.Request) (*http.Response, error) {
		return ownershipResponse(body, http.StatusOK), nil
	}))

	response := executeRequest(context.Background(), pool, nil, &httpapi.RequestCmd{
		Method: http.MethodGet,
		URL:    "http://local.invalid/partial",
	}, true)

	requireErrorOnly(t, response)
	require.Contains(t, response.Error, readErr.Error())
	require.Equal(t, int32(1), body.closes.Load())
}

func TestN06OverLimitBodyReturnsErrorOnly(t *testing.T) {
	body := &closeRecordingBody{reader: strings.NewReader("12345")}
	pool := ownershipPool(ownershipRoundTripper(func(*http.Request) (*http.Response, error) {
		return ownershipResponse(body, http.StatusOK), nil
	}))

	response := executeRequest(context.Background(), pool, nil, &httpapi.RequestCmd{
		Method:          http.MethodGet,
		URL:             "http://local.invalid/large",
		MaxResponseBody: 4,
	}, true)

	requireErrorOnly(t, response)
	require.Contains(t, response.Error, "too large")
	require.Equal(t, int32(1), body.closes.Load())
}
