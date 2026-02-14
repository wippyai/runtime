package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/hub"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	require.NoError(t, w.Close())
	os.Stdout = old
	return <-done
}

func TestPrintReadmeJSON(t *testing.T) {
	t.Parallel()

	ref := &moduleRef{Org: "wippy", Module: "terminal"}
	info := &hub.ReadmeInfo{
		Content:  "# Terminal",
		Filename: "README.md",
		Version:  "1.2.3",
	}

	out := captureStdout(t, func() {
		err := printReadmeJSON(ref, info)
		require.NoError(t, err)
	})

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "wippy", got["org"])
	assert.Equal(t, "terminal", got["name"])
	assert.Equal(t, "README.md", got["filename"])
	assert.Equal(t, "1.2.3", got["version"])
	assert.Equal(t, "# Terminal", got["content"])
}

func TestPrintReadmeText_WithMetadata(t *testing.T) {
	t.Parallel()

	ref := &moduleRef{Org: "wippy", Module: "terminal"}
	info := &hub.ReadmeInfo{
		Content:  "# Heading\nline\n",
		Filename: "README.md",
		Version:  "1.2.3",
	}

	out := captureStdout(t, func() {
		err := printReadmeText(ref, info)
		require.NoError(t, err)
	})

	assert.Contains(t, out, "Module: wippy/terminal\n")
	assert.Contains(t, out, "Version: 1.2.3\n")
	assert.Contains(t, out, "File: README.md\n")
	assert.True(t, strings.HasSuffix(out, "line\n"))
}

func TestPrintReadmeText_AddsTrailingNewlineWhenMissing(t *testing.T) {
	t.Parallel()

	ref := &moduleRef{Org: "wippy", Module: "terminal"}
	info := &hub.ReadmeInfo{Content: "no newline"}

	out := captureStdout(t, func() {
		err := printReadmeText(ref, info)
		require.NoError(t, err)
	})

	assert.True(t, strings.HasSuffix(out, "no newline\n"))
}

func TestRunReadme_InvalidReference(t *testing.T) {
	t.Parallel()

	err := runReadme(readmeCmd, []string{"invalid-ref"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid module reference")
}

func TestReadmeErrors(t *testing.T) {
	t.Parallel()

	errParse := NewReadmeParseError(errors.New("boom"))
	assert.Contains(t, errParse.Error(), "invalid module reference")
	assert.Contains(t, errParse.Error(), "boom")

	errClient := NewReadmeClientError("https://hub.example", errors.New("dial"))
	assert.Contains(t, errClient.Error(), "failed to create hub client")
	assert.Contains(t, errClient.Error(), "https://hub.example")

	errReadme := NewReadmeError("wippy/terminal", "https://hub.example", errors.New("missing"))
	assert.Contains(t, errReadme.Error(), "readme fetch failed")
	assert.Contains(t, errReadme.Error(), "wippy/terminal")
}
