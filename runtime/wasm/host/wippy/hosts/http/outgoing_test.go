// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	secapi "github.com/wippyai/runtime/api/security"
	httpapi "github.com/wippyai/runtime/api/service/http"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	wasiio "github.com/wippyai/wasm-runtime/wasi/preview2/io"
)

type resourceSnapshotMarker struct{}

func (*resourceSnapshotMarker) Type() preview2.ResourceType { return 255 }
func (*resourceSnapshotMarker) Drop()                       {}

func snapshotResourceCount(resources *preview2.ResourceTable) int {
	markerHandle := resources.Add(&resourceSnapshotMarker{})
	count := 0
	for handle := uint32(1); handle <= markerHandle; handle++ {
		if _, ok := resources.Get(handle); ok {
			count++
		}
	}
	return count
}

func TestH01PathQuerySplit(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	requestHandle := host.ConstructorOutgoingRequest(ctx, 0)
	if result := host.MethodOutgoingRequestSetAuthority(ctx, requestHandle, true, "example.com"); result != 0 {
		t.Fatalf("set authority result = %d, want 0", result)
	}
	if result := host.MethodOutgoingRequestSetPathWithQuery(ctx, requestHandle, true, "/items?q=a%20b"); result != 0 {
		t.Fatalf("set path-with-query result = %d, want 0", result)
	}

	resource, ok := resources.Get(requestHandle)
	if !ok {
		t.Fatalf("outgoing request handle %d not found", requestHandle)
	}
	cmd := host.buildRequestCmd(resource.(*outgoingRequestResource))
	defer cmd.Release()

	builtURL, err := url.Parse(cmd.URL)
	if err != nil {
		t.Fatalf("parse built URL %q: %v", cmd.URL, err)
	}
	if builtURL.Path != "/items" {
		t.Fatalf("URL.Path = %q, want %q", builtURL.Path, "/items")
	}
	if builtURL.RawQuery != "q=a%20b" {
		t.Fatalf("URL.RawQuery = %q, want %q", builtURL.RawQuery, "q=a%20b")
	}
	if strings.Contains(cmd.URL, "%3F") {
		t.Fatalf("built URL %q contains an escaped question mark", cmd.URL)
	}
}

func TestH02OutgoingRequestBodyOneShot(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	requestHandle := host.ConstructorOutgoingRequest(ctx, 0)

	firstHandle, firstErr := host.MethodOutgoingRequestBody(ctx, requestHandle)
	if firstErr != 0 || firstHandle == 0 {
		t.Fatalf("first body take = (%d, %d), want non-zero handle and no error", firstHandle, firstErr)
	}
	if _, ok := resources.Get(firstHandle); !ok {
		t.Fatalf("first body handle %d is not usable", firstHandle)
	}
	resourcesBeforeSecondTake := snapshotResourceCount(resources)

	secondHandle, secondErr := host.MethodOutgoingRequestBody(ctx, requestHandle)
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

func TestH09BuildRequestSnapshotsGuestState(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	requestHandle := host.ConstructorOutgoingRequest(ctx, 0)
	if result := host.MethodOutgoingRequestSetMethod(ctx, requestHandle, "PATCH"); result != 0 {
		t.Fatalf("set method result = %d, want 0", result)
	}
	if result := host.MethodOutgoingRequestSetScheme(ctx, requestHandle, true, 1); result != 0 {
		t.Fatalf("set scheme result = %d, want 0", result)
	}
	if result := host.MethodOutgoingRequestSetAuthority(ctx, requestHandle, true, "api.example"); result != 0 {
		t.Fatalf("set authority result = %d, want 0", result)
	}
	if result := host.MethodOutgoingRequestSetPathWithQuery(ctx, requestHandle, true, "/v1/items?limit=2"); result != 0 {
		t.Fatalf("set path result = %d, want 0", result)
	}

	resource, ok := resources.Get(requestHandle)
	if !ok {
		t.Fatalf("outgoing request handle %d not found", requestHandle)
	}
	request := resource.(*outgoingRequestResource)
	request.headers["X-Trace"] = []string{"first", "second"}
	request.body.WriteString("guest-body")

	cmd := host.buildRequestCmd(request)
	defer cmd.Release()

	request.method = "DELETE"
	request.url.Scheme = "http"
	request.url.Host = "mutated.example"
	request.url.Path = "/changed"
	request.url.RawQuery = "changed=true"
	request.headers["X-Trace"][0] = "mutated"
	request.headers["X-Added"] = []string{"later"}
	copy(request.body.Bytes(), "XXXXXXXXXX")

	if cmd.Method != "PATCH" {
		t.Fatalf("command method = %q, want snapshot %q", cmd.Method, "PATCH")
	}
	if cmd.URL != "https://api.example/v1/items?limit=2" {
		t.Fatalf("command URL = %q, want original URL", cmd.URL)
	}
	if want := map[string][]string{"X-Trace": {"first", "second"}}; !reflect.DeepEqual(cmd.Headers, want) {
		t.Fatalf("command headers = %#v, want %#v", cmd.Headers, want)
	}
	if string(cmd.Body) != "guest-body" {
		t.Fatalf("command body = %q, want %q", cmd.Body, "guest-body")
	}
}

func TestH10SecurityDenialReadyFuture(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	async := wasmengine.NewAsyncify()
	scheduler := wasmengine.NewScheduler(async)
	ctx := wasmengine.WithScheduler(wasmengine.WithAsyncify(context.Background(), async), scheduler)
	requestHandle := host.ConstructorOutgoingRequest(ctx, 0)
	if result := host.MethodOutgoingRequestSetAuthority(ctx, requestHandle, true, "8.8.8.8"); result != 0 {
		t.Fatalf("set authority result = %d, want 0", result)
	}

	futureHandle, handleErr := host.Handle(ctx, requestHandle, false, 0)
	if handleErr != 0 || futureHandle == 0 {
		t.Fatalf("Handle() = (%d, %d), want ready future and no host error", futureHandle, handleErr)
	}
	if !async.IsNormal(ctx) {
		t.Fatal("security denial suspended for dispatch instead of returning synchronously")
	}
	pollableHandle := host.MethodFutureIncomingResponseSubscribe(ctx, futureHandle)
	pollableResource, ok := resources.Get(pollableHandle)
	if !ok || !pollableResource.(*preview2.PollableResource).Ready() {
		t.Fatal("denied future is not immediately ready")
	}
	responseHandle, present, responseErr := host.MethodFutureIncomingResponseGet(ctx, futureHandle)
	if !present || responseErr == 0 || responseHandle != 0 {
		t.Fatalf("denied future get = (%d, %t, %d), want present error and no response", responseHandle, present, responseErr)
	}
}

func TestH11MissingAsyncifyTrapsBeforeDispatch(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = secapi.SetStrictMode(ctx, false)
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	requestHandle := host.ConstructorOutgoingRequest(ctx, 0)
	if result := host.MethodOutgoingRequestSetAuthority(ctx, requestHandle, true, "8.8.8.8"); result != 0 {
		t.Fatalf("set authority result = %d, want 0", result)
	}

	var trapped any
	func() {
		defer func() { trapped = recover() }()
		host.Handle(ctx, requestHandle, false, 0)
	}()
	if trapped == nil {
		t.Fatal("Handle() did not trap without an asyncify context")
	}
	if got := fmt.Sprint(trapped); got != "http outgoing handle requires asyncify context" {
		t.Fatalf("trap = %q, want deterministic missing-asyncify trap", got)
	}
}

func TestH12FutureResponseSuccessProjection(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	futureHandle := resources.Add(host.buildFutureFromResponse(httpapi.Response{
		StatusCode: 206,
		Headers: map[string][]string{
			"Set-Cookie": {"a=1", "b=2"},
		},
		Body: []byte("response-body"),
	}))

	responseHandle, present, responseErr := host.MethodFutureIncomingResponseGet(ctx, futureHandle)
	if !present || responseErr != 0 || responseHandle == 0 {
		t.Fatalf("future get = (%d, %t, %d), want successful response", responseHandle, present, responseErr)
	}
	if status := host.MethodIncomingResponseStatus(ctx, responseHandle); status != 206 {
		t.Fatalf("response status = %d, want 206", status)
	}
	headerHandle := host.MethodIncomingResponseHeaders(ctx, responseHandle)
	headerResource, ok := resources.Get(headerHandle)
	if !ok {
		t.Fatalf("response header handle %d not found", headerHandle)
	}
	if got, want := headerResource.(*fieldsResource).Values()["Set-Cookie"], []string{"a=1", "b=2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Set-Cookie values = %#v, want %#v", got, want)
	}
	bodyHandle, consumeErr := host.MethodIncomingResponseConsume(ctx, responseHandle)
	if consumeErr != 0 {
		t.Fatalf("consume response error = %d, want 0", consumeErr)
	}
	body, streamErr := wasiio.NewStreamsHost(resources).MethodInputStreamRead(ctx, bodyHandle, 64)
	if streamErr != nil || string(body) != "response-body" {
		t.Fatalf("response body = %q, error = %v, want exact body", body, streamErr)
	}
}

func TestH13FutureResponseErrorProjection(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	futureHandle := resources.Add(host.buildFutureFromResponse(httpapi.Response{Error: "dispatcher unavailable"}))
	resourcesBeforeGet := snapshotResourceCount(resources)

	responseHandle, present, responseErr := host.MethodFutureIncomingResponseGet(ctx, futureHandle)
	if !present || responseErr == 0 {
		t.Fatalf("future get = (%d, %t, %d), want error discriminant", responseHandle, present, responseErr)
	}
	if responseHandle != 0 {
		t.Fatalf("error future allocated response handle %d", responseHandle)
	}
	resourcesAfterGet := snapshotResourceCount(resources)
	if resourcesAfterGet != resourcesBeforeGet+1 {
		t.Fatalf("active resources after error get = %d, want %d (only the snapshot marker added)", resourcesAfterGet, resourcesBeforeGet+1)
	}
	if repeatedHandle, repeatedPresent, repeatedErr := host.MethodFutureIncomingResponseGet(ctx, futureHandle); repeatedPresent || repeatedErr != 0 || repeatedHandle != 0 {
		t.Fatalf("second error future get = (%d, %t, %d), want consumed result", repeatedHandle, repeatedPresent, repeatedErr)
	}
	if resourcesAfterRepeatedGet := snapshotResourceCount(resources); resourcesAfterRepeatedGet != resourcesAfterGet+1 {
		t.Fatalf("active resources after repeated error get = %d, want %d (only the snapshot marker added)", resourcesAfterRepeatedGet, resourcesAfterGet+1)
	}
}

func TestH14FutureResponseOwnershipChain(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	host := NewOutgoingHandlerHost(resources)
	streams := wasiio.NewStreamsHost(resources)
	futureHandle := resources.Add(host.buildFutureFromResponse(httpapi.Response{
		StatusCode: 200,
		Body:       []byte("owned-once"),
	}))

	responseHandle, present, responseErr := host.MethodFutureIncomingResponseGet(ctx, futureHandle)
	if !present || responseErr != 0 || responseHandle == 0 {
		t.Fatalf("first future get = (%d, %t, %d), want one response", responseHandle, present, responseErr)
	}
	if repeatedHandle, repeatedPresent, repeatedErr := host.MethodFutureIncomingResponseGet(ctx, futureHandle); repeatedPresent || repeatedErr != 0 || repeatedHandle != 0 {
		t.Fatalf("second future get = (%d, %t, %d), want no second response", repeatedHandle, repeatedPresent, repeatedErr)
	}

	bodyHandle, consumeErr := host.MethodIncomingResponseConsume(ctx, responseHandle)
	if consumeErr != 0 || bodyHandle == 0 {
		t.Fatalf("first response consume = (%d, %d), want one body", bodyHandle, consumeErr)
	}
	if repeatedHandle, repeatedErr := host.MethodIncomingResponseConsume(ctx, responseHandle); repeatedErr == 0 || repeatedHandle != 0 {
		t.Fatalf("second response consume = (%d, %d), want no second body", repeatedHandle, repeatedErr)
	}
	body, streamErr := streams.MethodInputStreamRead(ctx, bodyHandle, 64)
	if streamErr != nil || string(body) != "owned-once" {
		t.Fatalf("owned body = %q, error = %v, want exact bytes", body, streamErr)
	}

	streams.ResourceDropInputStream(ctx, bodyHandle)
	streams.ResourceDropInputStream(ctx, bodyHandle)
	if body, streamErr = streams.MethodInputStreamRead(ctx, bodyHandle, 1); streamErr == nil || body != nil {
		t.Fatalf("dropped body read = %q, error = %v, want invalid handle", body, streamErr)
	}
	host.ResourceDropIncomingResponse(ctx, responseHandle)
	host.ResourceDropIncomingResponse(ctx, responseHandle)
	if status := host.MethodIncomingResponseStatus(ctx, responseHandle); status != 0 {
		t.Fatalf("dropped response status = %d, want invalid handle", status)
	}
	host.ResourceDropFutureIncomingResponse(ctx, futureHandle)
	host.ResourceDropFutureIncomingResponse(ctx, futureHandle)
	if handle, present, responseErr := host.MethodFutureIncomingResponseGet(ctx, futureHandle); present || responseErr != 0 || handle != 0 {
		t.Fatalf("dropped future get = (%d, %t, %d), want invalid handle", handle, present, responseErr)
	}
}
