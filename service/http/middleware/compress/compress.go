package compress

import (
	"compress/gzip"
	"net/http"
	"strconv"

	"github.com/klauspost/compress/gzhttp"
)

const (
	// MiddlewareName is the name to register this middleware with
	MiddlewareName = "compress"

	// Option keys (dot-separated)
	OptionLevel     = "compress.level"
	OptionMinLength = "compress.min.length"

	// Default values
	DefaultLevel     = "default"
	DefaultMinLength = 1024
)

// CreateCompressMiddleware creates an HTTP compression middleware using klauspost/compress
func CreateCompressMiddleware(options map[string]string) func(http.Handler) http.Handler {
	// Parse compression level
	level := options[OptionLevel]
	if level == "" {
		level = DefaultLevel
	}

	var compressionLevel int
	switch level {
	case "fastest":
		compressionLevel = gzip.BestSpeed
	case "best":
		compressionLevel = gzip.BestCompression
	case "default":
		fallthrough
	default:
		compressionLevel = gzip.DefaultCompression
	}

	// Parse minimum length
	minLength := DefaultMinLength
	if minLengthStr := options[OptionMinLength]; minLengthStr != "" {
		if parsed, err := strconv.Atoi(minLengthStr); err == nil && parsed > 0 {
			minLength = parsed
		}
	}

	return func(next http.Handler) http.Handler {
		// Create gzhttp wrapper with options
		wrapper, err := gzhttp.NewWrapper(
			gzhttp.CompressionLevel(compressionLevel),
			gzhttp.MinSize(minLength),
		)
		if err != nil {
			// Fallback to default if configuration fails
			wrapper, _ = gzhttp.NewWrapper()
		}

		return wrapper(next)
	}
}
