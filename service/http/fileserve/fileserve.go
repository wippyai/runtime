package fileserve

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	fsapi "github.com/ponyruntime/pony/api/fs"
)

const MiddlewareName = "fileserve"

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

// CreateFileServeMiddleware creates middleware that serves files when X-File-Path header is set
func CreateFileServeMiddleware(options map[string]string, fsRegistry FSRegistry) func(http.Handler) http.Handler {
	fsID := options["fs"]

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := newResponseRecorder(w)
			next.ServeHTTP(rec, r)

			// Check if file response requested
			filePath := rec.Header().Get("X-File-Path")
			if filePath == "" {
				// No file response
				return
			}

			rec.Header().Del("X-File-Path")

			// Validate filesystem is configured
			if fsID == "" {
				http.Error(w, "fileserve middleware: fs option not configured", http.StatusInternalServerError)
				return
			}

			// Get filesystem from registry
			fs, ok := fsRegistry.GetFS(fsID)
			if !ok {
				http.Error(w, fmt.Sprintf("fileserve middleware: filesystem %q not found", fsID), http.StatusInternalServerError)
				return
			}

			// Validate file path
			if filePath == "" || filePath == "." || filePath == ".." {
				http.Error(w, "fileserve middleware: invalid file path", http.StatusBadRequest)
				return
			}

			// Open file from FS
			file, err := fs.Open(filePath)
			if err != nil {
				http.Error(w, fmt.Sprintf("fileserve middleware: file not found: %s", filePath), http.StatusNotFound)
				return
			}
			defer file.Close()

			// Get file stat for ServeContent
			stat, err := file.Stat()
			if err != nil {
				http.Error(w, "fileserve middleware: failed to stat file", http.StatusInternalServerError)
				return
			}

			// Handle download filename
			if filename := rec.Header().Get("X-File-Name"); filename != "" {
				rec.Header().Del("X-File-Name")
				w.Header().Set("Content-Disposition",
					fmt.Sprintf("attachment; filename=%q", filename))
			}

			// Serve file using http.ServeContent
			// fsapi.File implements io.ReadSeeker
			http.ServeContent(w, r, filepath.Base(filePath), stat.ModTime(), file.(io.ReadSeeker))
		})
	}
}
