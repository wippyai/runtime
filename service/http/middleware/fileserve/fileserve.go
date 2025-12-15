package fileserve

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	fsapi "github.com/wippyai/runtime/api/fs"
)

const (
	// MiddlewareName is the name to register this middleware with
	MiddlewareName = "sendfile"

	// OptionFS is an option key (dot-separated, preferred)
	OptionFS = "sendfile.fs"

	// Legacy option keys (deprecated, for backward compatibility)
	legacyFS = "fs"

	// XSendfileHeader is a header name - support both RoadRunner standard and Wippy legacy
	XSendfileHeader = "X-Sendfile"  // RoadRunner standard
	XFilePathHeader = "X-File-Path" // Wippy legacy
	XFileNameHeader = "X-File-Name" // Download filename
)

// FSRegistry interface for filesystem registry
type FSRegistry interface {
	GetFS(path string) (fsapi.FS, bool)
}

// responseRecorder captures response before writing
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		written:        false,
	}
}

func (rec *responseRecorder) WriteHeader(code int) {
	if !rec.written {
		rec.statusCode = code
		rec.ResponseWriter.WriteHeader(code)
		rec.written = true
	}
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	if !rec.written {
		rec.WriteHeader(http.StatusOK)
	}
	return rec.ResponseWriter.Write(b)
}

// CreateFileServeMiddleware creates middleware that serves files when X-Sendfile or X-File-Path header is set
func CreateFileServeMiddleware(options map[string]string, fsRegistry FSRegistry) func(http.Handler) http.Handler {
	// Check new key first, then fallback to legacy key
	fsID := options[OptionFS]
	if fsID == "" {
		fsID = options[legacyFS]
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := newResponseRecorder(w)
			next.ServeHTTP(rec, r)

			// Check if file response requested (support both headers)
			filePath := rec.Header().Get(XSendfileHeader)
			if filePath == "" {
				filePath = rec.Header().Get(XFilePathHeader)
			}
			if filePath == "" {
				// No file response
				return
			}

			// Remove headers that triggered file serving
			rec.Header().Del(XSendfileHeader)
			rec.Header().Del(XFilePathHeader)

			// Validate filesystem is configured
			if fsID == "" {
				http.Error(w, "sendfile middleware: fs option not configured", http.StatusInternalServerError)
				return
			}

			// Get filesystem from registry
			fs, ok := fsRegistry.GetFS(fsID)
			if !ok {
				http.Error(w, fmt.Sprintf("sendfile middleware: filesystem %q not found", fsID), http.StatusInternalServerError)
				return
			}

			// Validate file path
			if filePath == "" || filePath == "." || filePath == ".." {
				http.Error(w, "sendfile middleware: invalid file path", http.StatusBadRequest)
				return
			}

			// Open file from FS
			file, err := fs.Open(filePath)
			if err != nil {
				http.Error(w, fmt.Sprintf("sendfile middleware: file not found: %s", filePath), http.StatusNotFound)
				return
			}
			defer func() { _ = file.Close() }()

			// Get file stat for ServeContent
			stat, err := file.Stat()
			if err != nil {
				http.Error(w, "sendfile middleware: failed to stat file", http.StatusInternalServerError)
				return
			}

			// Handle download filename
			if filename := rec.Header().Get(XFileNameHeader); filename != "" {
				rec.Header().Del(XFileNameHeader)
				w.Header().Set("Content-Disposition",
					fmt.Sprintf("attachment; filename=%q", filename))
			}

			// Serve file using http.ServeContent
			seeker, ok := file.(io.ReadSeeker)
			if !ok {
				http.Error(w, "sendfile middleware: file does not support seeking", http.StatusInternalServerError)
				return
			}
			http.ServeContent(w, r, filepath.Base(filePath), stat.ModTime(), seeker)
		})
	}
}
