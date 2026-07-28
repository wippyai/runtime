// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
	wasiio "github.com/wippyai/wasm-runtime/wasi/preview2/io"
)

func TestH03OutgoingResponseBodyOneShot(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	responseHandle := host.ConstructorOutgoingResponse(ctx, 0)

	firstHandle, firstErr := host.MethodOutgoingResponseBody(ctx, responseHandle)
	if firstErr != 0 || firstHandle == 0 {
		t.Fatalf("first body take = (%d, %d), want non-zero handle and no error", firstHandle, firstErr)
	}
	streamHandle, writeErr := host.MethodOutgoingBodyWrite(ctx, firstHandle)
	if writeErr != 0 || streamHandle == 0 {
		t.Fatalf("first body write stream = (%d, %d), want usable stream", streamHandle, writeErr)
	}
	resourcesBeforeSecondTake := snapshotResourceCount(resources)

	secondHandle, secondErr := host.MethodOutgoingResponseBody(ctx, responseHandle)
	if secondErr == 0 || secondHandle != 0 {
		t.Fatalf("second body take = (%d, %d), want zero handle and error", secondHandle, secondErr)
	}
	if _, ok := resources.Get(firstHandle); !ok {
		t.Fatal("second body take invalidated the first body resource")
	}
	if resourcesAfterSecondTake := snapshotResourceCount(resources); resourcesAfterSecondTake != resourcesBeforeSecondTake+1 {
		t.Fatalf("active resources after second take = %d, want %d (only the snapshot marker added)", resourcesAfterSecondTake, resourcesBeforeSecondTake+1)
	}
}

func TestH04IncomingRequestConsumeOneShot(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	host.SetRequest(&Request{
		Request: &http.Request{Method: http.MethodPost},
		Body:    []byte("request-body"),
	})

	firstHandle, firstErr := host.MethodIncomingRequestConsume(ctx, 1)
	if firstErr != 0 || firstHandle == 0 {
		t.Fatalf("first request consume = (%d, %d), want non-zero handle and no error", firstHandle, firstErr)
	}
	if _, ok := resources.Get(firstHandle); !ok {
		t.Fatalf("first incoming body handle %d not found", firstHandle)
	}
	resourcesBeforeSecondConsume := snapshotResourceCount(resources)

	secondHandle, secondErr := host.MethodIncomingRequestConsume(ctx, 1)
	if secondErr == 0 || secondHandle != 0 {
		t.Fatalf("second request consume = (%d, %d), want zero handle and error", secondHandle, secondErr)
	}
	if _, ok := resources.Get(firstHandle); !ok {
		t.Fatal("second consume invalidated the first incoming body resource")
	}
	if resourcesAfterSecondConsume := snapshotResourceCount(resources); resourcesAfterSecondConsume != resourcesBeforeSecondConsume+1 {
		t.Fatalf("active resources after second consume = %d, want %d (only the snapshot marker added)", resourcesAfterSecondConsume, resourcesBeforeSecondConsume+1)
	}
}

func TestH05SetRequestClearsInvocationState(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	responseHandle := host.ConstructorOutgoingResponse(ctx, 0)
	host.SetResponseOutparamHandle(91)
	host.StaticResponseOutparamSet(ctx, 91, true, responseHandle)
	if host.GetResponse() == nil {
		t.Fatal("previous invocation response was not installed")
	}

	host.SetRequest(&Request{Request: &http.Request{Method: http.MethodGet}})

	if response := host.GetResponse(); response != nil {
		t.Fatalf("new invocation retained response %#v", response)
	}
	if outparam := host.GetResponseOutparamHandle(); outparam != 0 {
		t.Fatalf("new invocation retained response outparam %d", outparam)
	}
}

func TestH06IncomingRequestProjection(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, "https://api.example:8443/items?q=a%20b", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header["X-Trace"] = []string{"first", "second"}
	host.SetRequest(&Request{Request: request})

	if method := host.MethodIncomingRequestMethod(ctx, 1); method != http.MethodPatch {
		t.Fatalf("method = %q, want %q", method, http.MethodPatch)
	}
	if path, present := host.MethodIncomingRequestPathWithQuery(ctx, 1); !present || path != "/items?q=a%20b" {
		t.Fatalf("path-with-query = (%q, %t), want literal encoded query", path, present)
	}
	if scheme, present := host.MethodIncomingRequestScheme(ctx, 1); !present || scheme != 1 {
		t.Fatalf("scheme = (%d, %t), want HTTPS discriminant", scheme, present)
	}
	if authority, present := host.MethodIncomingRequestAuthority(ctx, 1); !present || authority != "api.example:8443" {
		t.Fatalf("authority = (%q, %t), want source host", authority, present)
	}
	headerHandle := host.MethodIncomingRequestHeaders(ctx, 1)
	headerResource, ok := resources.Get(headerHandle)
	if !ok {
		t.Fatalf("incoming header handle %d not found", headerHandle)
	}
	if got, want := headerResource.(*fieldsResource).Values()["X-Trace"], []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("X-Trace values = %#v, want %#v", got, want)
	}
}

func TestH07IncomingBodyLifecycle(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	streams := wasiio.NewStreamsHost(resources)
	host.SetRequest(&Request{
		Request: &http.Request{Method: http.MethodPost},
		Body:    []byte{0, 1, 2, 3, 255},
	})

	bodyHandle, consumeErr := host.MethodIncomingRequestConsume(ctx, 1)
	if consumeErr != 0 || bodyHandle == 0 {
		t.Fatalf("request consume = (%d, %d), want body", bodyHandle, consumeErr)
	}
	streamHandle, streamResult := host.MethodIncomingBodyStream(ctx, bodyHandle)
	if streamResult != 0 || streamHandle == 0 {
		t.Fatalf("incoming body stream = (%d, %d), want stream", streamHandle, streamResult)
	}
	body, streamErr := streams.MethodInputStreamRead(ctx, streamHandle, 64)
	if streamErr != nil || !reflect.DeepEqual(body, []byte{0, 1, 2, 3, 255}) {
		t.Fatalf("stream read = %#v, error = %v, want exact bytes", body, streamErr)
	}
	trailersHandle := host.StaticIncomingBodyFinish(ctx, bodyHandle)
	if trailersHandle == 0 {
		t.Fatal("incoming body finish returned zero trailers handle")
	}
	if reusedHandle, reusedErr := host.MethodIncomingBodyStream(ctx, bodyHandle); reusedErr == 0 || reusedHandle != 0 {
		t.Fatalf("finished body reuse = (%d, %d), want invalid handle", reusedHandle, reusedErr)
	}
}

func TestH08OutgoingResponseProjection(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	streams := wasiio.NewStreamsHost(resources)
	headerHandle := host.ConstructorFields(ctx)
	if result := host.MethodFieldsAppend(ctx, headerHandle, "Set-Cookie", []byte("a=1")); result != 0 {
		t.Fatalf("append first header result = %d, want 0", result)
	}
	if result := host.MethodFieldsAppend(ctx, headerHandle, "Set-Cookie", []byte("b=2")); result != 0 {
		t.Fatalf("append second header result = %d, want 0", result)
	}
	selectedResponseHandle := host.ConstructorOutgoingResponse(ctx, headerHandle)
	discardedResponseHandle := host.ConstructorOutgoingResponse(ctx, 0)
	if response := host.GetResponse(); response != nil {
		t.Fatalf("uncommitted response became visible: %#v", response)
	}
	if result := host.MethodOutgoingResponseSetStatusCode(ctx, selectedResponseHandle, 201); result != 0 {
		t.Fatalf("set response status result = %d, want 0", result)
	}
	selectedBodyHandle, selectedBodyErr := host.MethodOutgoingResponseBody(ctx, selectedResponseHandle)
	if selectedBodyErr != 0 || selectedBodyHandle == 0 {
		t.Fatalf("take selected outgoing body = (%d, %d), want body", selectedBodyHandle, selectedBodyErr)
	}
	selectedStreamHandle, selectedStreamResult := host.MethodOutgoingBodyWrite(ctx, selectedBodyHandle)
	if selectedStreamResult != 0 || selectedStreamHandle == 0 {
		t.Fatalf("selected outgoing body write = (%d, %d), want stream", selectedStreamHandle, selectedStreamResult)
	}
	if streamErr := streams.MethodOutputStreamWrite(ctx, selectedStreamHandle, []byte("created")); streamErr != nil {
		t.Fatalf("write selected response body: %v", streamErr)
	}
	if result := host.StaticOutgoingBodyFinish(ctx, selectedBodyHandle, false, 0); result != 0 {
		t.Fatalf("finish selected response body result = %d, want 0", result)
	}
	discardedBodyHandle, discardedBodyErr := host.MethodOutgoingResponseBody(ctx, discardedResponseHandle)
	if discardedBodyErr != 0 || discardedBodyHandle == 0 {
		t.Fatalf("take discarded outgoing body = (%d, %d), want body", discardedBodyHandle, discardedBodyErr)
	}
	discardedStreamHandle, discardedStreamResult := host.MethodOutgoingBodyWrite(ctx, discardedBodyHandle)
	if discardedStreamResult != 0 || discardedStreamHandle == 0 {
		t.Fatalf("discarded outgoing body write = (%d, %d), want stream", discardedStreamHandle, discardedStreamResult)
	}
	if streamErr := streams.MethodOutputStreamWrite(ctx, discardedStreamHandle, []byte("discarded")); streamErr != nil {
		t.Fatalf("write discarded response body: %v", streamErr)
	}
	if result := host.StaticOutgoingBodyFinish(ctx, discardedBodyHandle, false, 0); result != 0 {
		t.Fatalf("finish discarded response body result = %d, want 0", result)
	}
	host.SetResponseOutparamHandle(73)
	host.StaticResponseOutparamSet(ctx, 73, true, selectedResponseHandle)

	response := host.GetResponse()
	if response == nil {
		t.Fatal("committed response is nil")
	}
	if response.StatusCode != 201 {
		t.Fatalf("response status = %d, want 201", response.StatusCode)
	}
	if got, want := response.Headers["Set-Cookie"], []string{"a=1", "b=2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response headers = %#v, want %#v", got, want)
	}
	if string(response.Body) != "created" {
		t.Fatalf("response body = %q, want %q", response.Body, "created")
	}
}
