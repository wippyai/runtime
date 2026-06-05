// SPDX-License-Identifier: MPL-2.0

// Command gendesc turns a release commit list (read from stdin) into a short,
// plain-English release summary using the OpenAI Responses API. The release
// workflow uses it to fill the release description when a human did not provide
// one. It prints the summary to stdout and fails loudly on any error so a
// release never ships an empty changelog.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-5.4-mini"
	maxLen         = 280
	maxTokens      = 160
	instructions   = "Write a one-line, plain-English release summary of these commits, at most two short sentences and under 280 characters. " +
		"No headings, no commit hashes, no markdown, no bullet lists, no trailing newline. " +
		"Highlight user-facing features and fixes; ignore dependency bumps, CI, and docs-only changes."
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

var httpClient httpDoer = &http.Client{Timeout: 60 * time.Second}

type request struct {
	Model           string `json:"model"`
	Instructions    string `json:"instructions"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type response struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func buildRequestBody(model, input string) ([]byte, error) {
	return json.Marshal(request{Model: model, Instructions: instructions, Input: input, MaxOutputTokens: maxTokens})
}

func parseResponse(body []byte) (string, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if r.Error != nil {
		return "", fmt.Errorf("openai error: %s", r.Error.Message)
	}

	var b strings.Builder
	for _, out := range r.Output {
		for _, c := range out.Content {
			if c.Type == "output_text" {
				b.WriteString(c.Text)
			}
		}
	}

	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", errors.New("openai returned no text")
	}

	return text, nil
}

func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}

	return defaultBaseURL
}

func normalize(s string) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	if len(collapsed) <= maxLen {
		return collapsed
	}

	cut := collapsed[:maxLen]
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}

	return strings.ToValidUTF8(strings.TrimRight(cut, " "), "")
}

func generate(apiKey, model, input string) (string, error) {
	body, err := buildRequestBody(model, input)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai responded %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	text, err := parseResponse(respBody)
	if err != nil {
		return "", err
	}

	return normalize(text), nil
}

func run() error {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return errors.New("OPENAI_API_KEY is required")
	}

	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = defaultModel
	}

	stdin, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	input := strings.TrimSpace(string(stdin))
	if input == "" {
		return errors.New("no commit list on stdin")
	}

	if tag := strings.TrimSpace(os.Getenv("RELEASE_TAG")); tag != "" {
		input = "Release " + tag + "\n\n" + input
	}

	text, err := generate(apiKey, model, input)
	if err != nil {
		return err
	}

	fmt.Println(text)

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gendesc:", err)
		os.Exit(1)
	}
}
