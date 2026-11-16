// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cloudstorage"
	apierror "github.com/wippyai/runtime/api/error"
)

type recordedRequest struct {
	header http.Header
	method string
	path   string
	query  string
}

// copyServer is a fake S3 endpoint that serves the requests of a copy.
type copyServer struct {
	server    *httptest.Server
	headMeta  map[string]string
	headSize  string
	requests  []recordedRequest
	mu        sync.Mutex
	failPart  int
	completed bool
}

func newCopyServer(t *testing.T, headSize string) *copyServer {
	t.Helper()
	cs := &copyServer{headSize: headSize}
	cs.server = httptest.NewServer(http.HandlerFunc(cs.handle))
	t.Cleanup(cs.server.Close)
	return cs
}

func (cs *copyServer) handle(w http.ResponseWriter, r *http.Request) {
	cs.mu.Lock()
	cs.requests = append(cs.requests, recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone()})
	cs.mu.Unlock()

	modified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339)
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodHead:
		w.Header().Set("ETag", `"src-etag"`)
		w.Header().Set("Content-Length", cs.headSize)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Format(http.TimeFormat))
		for k, v := range cs.headMeta {
			w.Header().Set("x-amz-meta-"+k, v)
		}
	case r.Method == http.MethodPut && r.Header.Get("x-amz-copy-source") != "" && q.Get("uploadId") == "":
		fmt.Fprintf(w, `<CopyObjectResult><ETag>"dst-etag"</ETag><LastModified>%s</LastModified></CopyObjectResult>`, modified)
	case r.Method == http.MethodPost && q.Has("uploads"):
		_, _ = w.Write([]byte(`<InitiateMultipartUploadResult><UploadId>up-1</UploadId></InitiateMultipartUploadResult>`))
	case r.Method == http.MethodPut && q.Get("uploadId") != "":
		if cs.failPart > 0 && q.Get("partNumber") == fmt.Sprint(cs.failPart) {
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = w.Write([]byte(`<Error><Code>PreconditionFailed</Code><Message>source changed</Message></Error>`))
			return
		}
		fmt.Fprintf(w, `<CopyPartResult><ETag>"part-%s"</ETag></CopyPartResult>`, q.Get("partNumber"))
	case r.Method == http.MethodPost && q.Get("uploadId") != "":
		cs.mu.Lock()
		cs.completed = true
		cs.mu.Unlock()
		_ = xml.NewEncoder(w).Encode(struct {
			XMLName xml.Name `xml:"CompleteMultipartUploadResult"`
			ETag    string
		}{ETag: `"multi-etag"`})
	case r.Method == http.MethodDelete && q.Get("uploadId") != "":
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func (cs *copyServer) find(method, query string) []recordedRequest {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var out []recordedRequest
	for _, r := range cs.requests {
		if r.method == method && strings.Contains(r.query, query) {
			out = append(out, r)
		}
	}
	return out
}

func TestCopyObject_SingleRequest(t *testing.T) {
	cs := newCopyServer(t, "10")
	storage := newErrorTestStorage(t, cs.server.URL, nil)

	result, err := storage.CopyObject(context.Background(), "dir/a b+c.txt", "dst.txt", &cloudstorage.CopyOptions{
		MetadataDirective: cloudstorage.MetadataDirectiveReplace,
		Metadata:          map[string]string{"k": "v"},
		ContentType:       "application/json",
		IfMatch:           `"src-etag"`,
		IfNoneMatch:       "*",
	})
	require.NoError(t, err)
	assert.Equal(t, `"dst-etag"`, result.ETag)
	assert.True(t, result.LastModified.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))

	puts := cs.find(http.MethodPut, "")
	require.Len(t, puts, 1)
	h := puts[0].header
	assert.Equal(t, "/bucket/dst.txt", puts[0].path)
	assert.Equal(t, "bucket/dir/a%20b%2Bc.txt", h.Get("x-amz-copy-source"))
	assert.Equal(t, "REPLACE", h.Get("x-amz-metadata-directive"))
	assert.Equal(t, "v", h.Get("x-amz-meta-k"))
	assert.Equal(t, "application/json", h.Get("Content-Type"))
	assert.Equal(t, `"src-etag"`, h.Get("x-amz-copy-source-if-match"))
	assert.Equal(t, "*", h.Get("If-None-Match"))
}

func TestCopyObject_DefaultsToCopyDirective(t *testing.T) {
	cs := newCopyServer(t, "10")
	storage := newErrorTestStorage(t, cs.server.URL, nil)

	_, err := storage.CopyObject(context.Background(), "a", "b", nil)
	require.NoError(t, err)

	puts := cs.find(http.MethodPut, "")
	require.Len(t, puts, 1)
	assert.Equal(t, "COPY", puts[0].header.Get("x-amz-metadata-directive"))
	assert.Empty(t, puts[0].header.Get("x-amz-copy-source-if-match"))
}

func TestCopyObject_RejectsInvalidRequests(t *testing.T) {
	cs := newCopyServer(t, "10")
	storage := newErrorTestStorage(t, cs.server.URL, nil)

	cases := map[string]struct {
		opts     *cloudstorage.CopyOptions
		src, dst string
	}{
		"empty source":        {src: "", dst: "b"},
		"empty destination":   {src: "a", dst: ""},
		"unknown directive":   {src: "a", dst: "b", opts: &cloudstorage.CopyOptions{MetadataDirective: "MERGE"}},
		"metadata with COPY":  {src: "a", dst: "b", opts: &cloudstorage.CopyOptions{Metadata: map[string]string{"k": "v"}}},
		"type with COPY":      {src: "a", dst: "b", opts: &cloudstorage.CopyOptions{ContentType: "text/plain"}},
		"self copy with COPY": {src: "a", dst: "a"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := storage.CopyObject(context.Background(), tc.src, tc.dst, tc.opts)
			requireClassified(t, err, apierror.Invalid, apierror.False)
		})
	}
	assert.Empty(t, cs.requests, "invalid requests never reach the provider")
}

func TestCopyObject_SelfCopyWithReplace(t *testing.T) {
	cs := newCopyServer(t, "10")
	storage := newErrorTestStorage(t, cs.server.URL, nil)

	_, err := storage.CopyObject(context.Background(), "same", "same", &cloudstorage.CopyOptions{
		MetadataDirective: cloudstorage.MetadataDirectiveReplace,
	})
	require.NoError(t, err)
	puts := cs.find(http.MethodPut, "")
	require.Len(t, puts, 1)
	assert.Equal(t, "/bucket/same", puts[0].path)
	assert.Equal(t, "bucket/same", puts[0].header.Get("x-amz-copy-source"))
}

func TestCopyObject_MultipartAboveSingleLimit(t *testing.T) {
	cs := newCopyServer(t, "25")
	cs.headMeta = map[string]string{"owner": "me"}
	storage := newErrorTestStorage(t, cs.server.URL, nil)
	storage.copySingleLimit = 10
	storage.copyPartSize = 10

	result, err := storage.CopyObject(context.Background(), "src", "dst", &cloudstorage.CopyOptions{IfNoneMatch: "*"})
	require.NoError(t, err)
	assert.Equal(t, `"multi-etag"`, result.ETag)
	assert.False(t, result.LastModified.IsZero())

	create := cs.find(http.MethodPost, "uploads")
	require.Len(t, create, 1)
	assert.Equal(t, "me", create[0].header.Get("x-amz-meta-owner"))
	assert.Equal(t, "text/plain", create[0].header.Get("Content-Type"))

	var ranges []string
	for _, part := range cs.find(http.MethodPut, "uploadId=up-1") {
		ranges = append(ranges, part.header.Get("x-amz-copy-source-range"))
		assert.Equal(t, `"src-etag"`, part.header.Get("x-amz-copy-source-if-match"))
	}
	assert.Equal(t, []string{"bytes=0-9", "bytes=10-19", "bytes=20-24"}, ranges)

	complete := cs.find(http.MethodPost, "uploadId=up-1")
	require.Len(t, complete, 1)
	assert.Equal(t, "*", complete[0].header.Get("If-None-Match"))
	assert.Empty(t, cs.find(http.MethodDelete, "uploadId"))
}

func TestCopyObject_MultipartAbortsOnPartFailure(t *testing.T) {
	cs := newCopyServer(t, "25")
	cs.failPart = 2
	storage := newErrorTestStorage(t, cs.server.URL, nil)
	storage.copySingleLimit = 10
	storage.copyPartSize = 10

	_, err := storage.CopyObject(context.Background(), "src", "dst", nil)
	requireClassified(t, err, apierror.Conflict, apierror.False)
	require.ErrorIs(t, err, cloudstorage.ErrPreconditionFailed)
	assert.Len(t, cs.find(http.MethodDelete, "uploadId=up-1"), 1)
	assert.False(t, cs.completed)
}
