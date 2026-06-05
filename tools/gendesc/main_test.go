// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

type fakeDoer struct {
	resp *http.Response
	err  error
	got  *http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.got = req
	return f.resp, f.err
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func withClient(t *testing.T, d httpDoer) {
	t.Helper()
	prev := httpClient
	httpClient = d
	t.Cleanup(func() { httpClient = prev })
}

func outputJSON(text string) string {
	return `{"output":[{"content":[{"type":"output_text","text":` + quote(text) + `}]}]}`
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestBuildRequestBody(t *testing.T) {
	body, err := buildRequestBody("gpt-5.4-mini", "- a1 feat: thing")
	if err != nil {
		t.Fatalf("buildRequestBody: %v", err)
	}

	var r request
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if r.Model != "gpt-5.4-mini" {
		t.Errorf("model = %q", r.Model)
	}
	if r.Input != "- a1 feat: thing" {
		t.Errorf("input = %q", r.Input)
	}
	if !strings.Contains(r.Instructions, "two short sentences") {
		t.Errorf("instructions missing guidance: %q", r.Instructions)
	}
	if r.MaxOutputTokens != maxTokens {
		t.Errorf("max_output_tokens = %d want %d", r.MaxOutputTokens, maxTokens)
	}
}

func TestParseResponse(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{"single", outputJSON("Hello world"), "Hello world", false},
		{
			name: "multi_segment",
			body: `{"output":[{"content":[{"type":"output_text","text":"Hello "},` +
				`{"type":"output_text","text":"world"}]}]}`,
			want: "Hello world",
		},
		{
			name: "ignores_non_text",
			body: `{"output":[{"content":[{"type":"reasoning","text":"think"},` +
				`{"type":"output_text","text":"final"}]}]}`,
			want: "final",
		},
		{"api_error", `{"error":{"message":"bad key"}}`, "", true},
		{"no_text", `{"output":[{"content":[{"type":"reasoning","text":"x"}]}]}`, "", true},
		{"empty_text", outputJSON("   "), "", true},
		{"malformed", `{not json`, "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseResponse([]byte(c.body))
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"  hello   world \n", "hello world"},
		{"line one\nline two", "line one line two"},
		{"a\t\tb", "a b"},
		{strings.Repeat("x", maxLen+50), strings.Repeat("x", maxLen)},
		{strings.Repeat("word ", 100), strings.TrimRight(strings.Repeat("word ", maxLen/5), " ")},
	}

	for _, c := range cases {
		got := normalize(c.in)
		if got != c.want {
			t.Errorf("normalize(%.20q...) = %q (len %d) want %q (len %d)", c.in, got, len(got), c.want, len(c.want))
		}
		if len(got) > maxLen {
			t.Errorf("normalize exceeded maxLen: %d", len(got))
		}
	}
}

func TestNormalizeValidUTF8OnCut(t *testing.T) {
	got := normalize("x" + strings.Repeat("é", maxLen))
	if len(got) > maxLen {
		t.Errorf("len %d > maxLen", len(got))
	}
	if !utf8.ValidString(got) {
		t.Errorf("normalize produced invalid UTF-8: %q", got)
	}
}

func TestBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                            defaultBaseURL,
		"https://gw.example.com/v1":   "https://gw.example.com/v1",
		"https://gw.example.com/v1/":  "https://gw.example.com/v1",
		"https://gw.example.com/v1//": "https://gw.example.com/v1",
	}
	for in, want := range cases {
		t.Setenv("OPENAI_BASE_URL", in)
		if got := baseURL(); got != want {
			t.Errorf("baseURL(%q) = %q want %q", in, got, want)
		}
	}
}

func TestGenerateSuccess(t *testing.T) {
	resp := jsonResponse(200, outputJSON("Adds shared-Raft KV stores.  "))
	defer func() { _ = resp.Body.Close() }()
	d := &fakeDoer{resp: resp}
	withClient(t, d)

	got, err := generate("sk-test", "gpt-5.4-mini", "- a1 feat(store): shared raft")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got != "Adds shared-Raft KV stores." {
		t.Errorf("got %q", got)
	}

	if h := d.got.Header.Get("Authorization"); h != "Bearer sk-test" {
		t.Errorf("auth header = %q", h)
	}
	if ct := d.got.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestGenerateHTTPStatus(t *testing.T) {
	resp := jsonResponse(401, `{"error":{"message":"invalid api key"}}`)
	defer func() { _ = resp.Body.Close() }()
	d := &fakeDoer{resp: resp}
	withClient(t, d)

	_, err := generate("sk-bad", "gpt-5.4-mini", "- a1 feat: x")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v", err)
	}
}

func TestGenerateAgainstServer(t *testing.T) {
	var gotBody request
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, outputJSON("Adds shared-Raft KV stores.\n\nFixes two bugs."))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_BASE_URL", srv.URL)

	got, err := generate("sk-live", "gpt-5.4-mini", "- a1 feat(store): raft")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got != "Adds shared-Raft KV stores. Fixes two bugs." {
		t.Errorf("got %q", got)
	}
	if gotPath != "/responses" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer sk-live" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotBody.Model != "gpt-5.4-mini" || gotBody.Input != "- a1 feat(store): raft" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestGenerateTransportError(t *testing.T) {
	d := &fakeDoer{err: errors.New("dial timeout")}
	withClient(t, d)

	if _, err := generate("sk-test", "gpt-5.4-mini", "- a1 feat: x"); err == nil {
		t.Fatal("expected transport error")
	}
}

func runWithStdin(t *testing.T, stdin string) (string, error) {
	t.Helper()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() { os.Stdin, os.Stdout = origIn, origOut })

	go func() {
		_, _ = io.WriteString(inW, stdin)
		_ = inW.Close()
	}()

	runErr := run()
	_ = outW.Close()
	out, _ := io.ReadAll(outR)

	return string(out), runErr
}

func TestRun(t *testing.T) {
	var gotInput string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotInput = req.Input
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, outputJSON("Clean one-line summary."))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "sk-run")
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("RELEASE_TAG", "v9.9.9")

	out, err := runWithStdin(t, "- a1 feat(store): raft\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "Clean one-line summary.\n" {
		t.Errorf("stdout = %q", out)
	}
	if !strings.HasPrefix(gotInput, "Release v9.9.9\n\n") {
		t.Errorf("RELEASE_TAG not prefixed: %q", gotInput)
	}
}

func TestRunErrors(t *testing.T) {
	t.Run("missing_key", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		if _, err := runWithStdin(t, "- a1 feat: x\n"); err == nil {
			t.Fatal("expected error for missing key")
		}
	})

	t.Run("empty_stdin", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-run")
		if _, err := runWithStdin(t, "   \n"); err == nil {
			t.Fatal("expected error for empty stdin")
		}
	})
}
