package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// DefaultTimeout is the default timeout for HTTP requests.
const DefaultTimeout = 90 * time.Second

// fileOption represents a file to be uploaded
type fileOption struct {
	Name        string    // Form field name
	Filename    string    // Filename to use in the request
	ContentType string    // Content type
	Content     string    // File content as string (if provided directly)
	Reader      io.Reader // Reader for file content (if using a reader)
}

// requestOptions holds parsed request options
type requestOptions struct {
	headers    map[string]string
	cookies    map[string]string
	body       string
	query      string
	timeout    time.Duration
	auth       *struct{ user, pass string }
	stream     bool          // Flag to indicate streaming should be used
	files      []*fileOption // Files to upload
	unixSocket string        // Unix socket path for requests
}

// parseOptions parses Lua value into requestOptions
func parseOptions(value lua.LValue) (*requestOptions, error) {
	opts := &requestOptions{
		headers: make(map[string]string),
		cookies: make(map[string]string),
		timeout: DefaultTimeout, // Set default timeout
	}

	if value == nil || value.Type() != lua.LTTable {
		return opts, nil
	}

	table := value.(*lua.LTable)

	if headers := table.RawGetString("headers"); headers != lua.LNil {
		if t, ok := headers.(*lua.LTable); ok {
			t.ForEach(func(k, v lua.LValue) {
				opts.headers[k.String()] = v.String()
			})
		}
	}

	if cookies := table.RawGetString("cookies"); cookies != lua.LNil {
		if t, ok := cookies.(*lua.LTable); ok {
			t.ForEach(func(k, v lua.LValue) {
				opts.cookies[k.String()] = v.String()
			})
		}
	}

	if body := table.RawGetString("body"); body != lua.LNil {
		opts.body = body.String()
	} else if form := table.RawGetString("form"); form != lua.LNil {
		opts.body = form.String()
		opts.headers["Content-type"] = "application/x-www-form-urlencoded"
	}

	if query := table.RawGetString("query"); query != lua.LNil {
		opts.query = query.String()
	}

	if timeout := table.RawGetString("timeout"); timeout != lua.LNil {
		switch t := timeout.(type) {
		case lua.LNumber:
			opts.timeout = time.Duration(t) * time.Second
		case lua.LString:
			if d, err := time.ParseDuration(string(t)); err == nil {
				opts.timeout = d
			}
		}
	}

	if auth := table.RawGetString("auth"); auth != lua.LNil {
		if t, ok := auth.(*lua.LTable); ok {
			user := t.RawGetString("user")
			pass := t.RawGetString("pass")
			if user.Type() != lua.LTString || pass.Type() != lua.LTString {
				return nil, ErrInvalidAuth
			}
			opts.auth = &struct{ user, pass string }{
				user: user.String(),
				pass: pass.String(),
			}
		}
	}

	if streamOpts := table.RawGetString("stream"); streamOpts != lua.LNil {
		// If stream is present, enable streaming
		opts.stream = true
	}

	// Parse unix_socket option
	if unixSocket := table.RawGetString("unix_socket"); unixSocket != lua.LNil {
		if unixSocket.Type() == lua.LTString {
			opts.unixSocket = unixSocket.String()
		}
	}

	// Parse files for upload
	if filesValue := table.RawGetString("files"); filesValue != lua.LNil {
		if filesTable, ok := filesValue.(*lua.LTable); ok {
			opts.files = make([]*fileOption, 0)

			filesTable.ForEach(func(_, v lua.LValue) {
				if v.Type() != lua.LTTable {
					return // Skip non-table values
				}

				fileTable := v.(*lua.LTable)
				file := &fileOption{}

				// Get required field: name
				nameValue := fileTable.RawGetString("name")
				if nameValue.Type() != lua.LTString {
					return // Skip if name is not a string
				}
				file.Name = nameValue.String()

				// Get filename (required)
				filenameValue := fileTable.RawGetString("filename")
				if filenameValue.Type() != lua.LTString {
					return // Skip if filename is not a string
				}
				file.Filename = filenameValue.String()

				// Get content type (optional)
				contentTypeValue := fileTable.RawGetString("content_type")
				if contentTypeValue.Type() == lua.LTString {
					file.ContentType = contentTypeValue.String()
				} else {
					// Default based on common mime types
					file.ContentType = "application/octet-stream"
				}

				// Get content source - either content string or reader
				contentValue := fileTable.RawGetString("content")
				readerValue := fileTable.RawGetString("reader")

				switch {
				case contentValue != lua.LNil && contentValue.Type() == lua.LTString:
					// Handle string content
					file.Content = contentValue.String()
				case readerValue != lua.LNil && readerValue.Type() == lua.LTUserData:
					// Handle reader object
					if reader, ok := readerValue.(*lua.LUserData).Value.(io.Reader); ok {
						file.Reader = reader
					} else {
						return // Skip if not a valid reader
					}
				default:
					// Neither content nor reader provided, skip this entry
					return
				}

				opts.files = append(opts.files, file)
			})
		}
	}

	return opts, nil
}

// escapeQuotes escapes quotes in MIME header values
func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// makeRequest creates an HTTP request with the given method, URL, and options
func makeRequest(
	method, url string,
	opts *requestOptions,
) (*http.Request, error) {
	if method == "" {
		return nil, errors.New("method cannot be empty")
	}

	if url == "" {
		return nil, errors.New("URL cannot be empty")
	}

	// Check if we need a multipart request (files present)
	if len(opts.files) > 0 {
		return makeMultipartRequest(method, url, opts)
	}

	// Standard request processing
	req, err := http.NewRequestWithContext(context.Background(), strings.ToUpper(method), url, nil)
	if err != nil {
		return nil, err
	}

	// Apply options
	if opts.query != "" {
		req.URL.RawQuery = opts.query
	}

	for k, v := range opts.headers {
		req.Header.Set(k, v)
	}

	for k, v := range opts.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	if opts.body != "" {
		req.Body = io.NopCloser(strings.NewReader(opts.body))
		req.ContentLength = int64(len(opts.body))
	}

	if opts.auth != nil {
		req.SetBasicAuth(opts.auth.user, opts.auth.pass)
	}

	return req, nil
}

// makeMultipartRequest creates a multipart request for file uploads
func makeMultipartRequest(
	method, url string,
	opts *requestOptions,
) (*http.Request, error) {
	// Create a pipe for streaming the multipart data
	pr, pw := io.Pipe()

	// Create multipart writer
	mpw := multipart.NewWriter(pw)

	// Start a goroutine to write the multipart data
	go func() {
		var err error
		defer func() {
			// Close the multipart writer
			if mpw != nil {
				mErr := mpw.Close()
				if err == nil {
					err = mErr
				}
			}
			// Always close the pipe writer with any error that occurred
			pw.CloseWithError(err)
		}()

		// Add form fields if form data is provided
		if opts.body != "" && opts.headers["Content-type"] == "application/x-www-form-urlencoded" {
			// Parse form data and add as fields
			formValues, err := parseFormValues(opts.body)
			if err != nil {
				return
			}

			for key, values := range formValues {
				for _, val := range values {
					if err = mpw.WriteField(key, val); err != nil {
						return
					}
				}
			}
		}

		// Add file fields
		for _, file := range opts.files {
			// Create a form file part with headers
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition",
				fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
					escapeQuotes(file.Name), escapeQuotes(file.Filename)))

			if file.ContentType != "" {
				h.Set("Content-Type", file.ContentType)
			}

			part, err := mpw.CreatePart(h)
			if err != nil {
				return
			}

			// Write the content to the part
			if file.Content != "" {
				// Write from string content
				if _, err = part.Write([]byte(file.Content)); err != nil {
					return
				}
			} else if file.Reader != nil {
				// Write from reader
				if _, err = io.Copy(part, file.Reader); err != nil {
					return
				}
			}
		}
	}()

	// Create a new request with the pipe reader as the body
	req, err := http.NewRequestWithContext(
		context.Background(),
		strings.ToUpper(method),
		url,
		pr,
	)
	if err != nil {
		return nil, err
	}

	// Set the content type header with boundary
	req.Header.Set("Content-Type", mpw.FormDataContentType())

	// Apply other options
	if opts.query != "" {
		req.URL.RawQuery = opts.query
	}

	for k, v := range opts.headers {
		// Skip Content-type as we've already set it
		if strings.ToLower(k) != "content-type" {
			req.Header.Set(k, v)
		}
	}

	for k, v := range opts.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	if opts.auth != nil {
		req.SetBasicAuth(opts.auth.user, opts.auth.pass)
	}

	return req, nil
}

// parseFormValues parses a form encoded string into a map
//
//nolint:unparam // ok for now
func parseFormValues(form string) (map[string][]string, error) {
	values := make(map[string][]string)
	for _, pair := range strings.Split(form, "&") {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		key := kv[0]
		value := ""
		if len(kv) == 2 {
			value = kv[1]
			// We're intentionally not URL-decoding values here to match
			// the existing behavior in the tests
		}
		values[key] = append(values[key], value)
	}
	return values, nil
}
