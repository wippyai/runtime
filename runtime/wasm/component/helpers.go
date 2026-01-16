package component

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	"github.com/wippyai/runtime/system/eventbus"
	eventhandlers "github.com/wippyai/runtime/system/registry/events"
)

// EntityHandler interface for component managers.
type EntityHandler interface {
	registry.EntryListener
}

// Handler wraps entity handling with registry event routing.
type Handler struct {
	inner eventbus.EventHandler
}

// NewHandler creates a handler for the specified entry kinds.
func NewHandler(kinds registry.Kind, entityHandler EntityHandler) *Handler {
	return &Handler{
		inner: eventhandlers.NewRegistryHandler(kinds, entityHandler),
	}
}

// Pattern returns the event pattern to match.
func (h *Handler) Pattern() eventbus.Pattern {
	return eventbus.Pattern{
		System: "registry",
		Kind:   "entry.(create|update|delete)",
	}
}

// Handle processes registry events.
func (h *Handler) Handle(ctx context.Context, evt event.Event) error {
	return h.inner.Handle(ctx, evt)
}

// UnpackConfig unpacks entry configuration with validation.
func UnpackConfig[T any](ctx context.Context, entry registry.Entry) (*T, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, runtimewasm.ErrCouldNotAccessRegistry
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, runtimewasm.NewUnmarshalConfigError(err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, runtimewasm.NewValidationError(err)
		}
	}

	return cfg, nil
}

// LoadWASM reads WASM bytes from the specified filesystem and path.
func LoadWASM(fsReg fsapi.Registry, fsID, path string) ([]byte, error) {
	fs, ok := fsReg.GetFS(fsID)
	if !ok {
		return nil, runtimewasm.NewFilesystemNotFoundError(fsID)
	}

	file, err := fs.Open(path)
	if err != nil {
		return nil, runtimewasm.NewOpenFileError(path, err)
	}
	defer func() { _ = file.Close() }()

	return io.ReadAll(file)
}

// VerifyHash checks that the data bytes match the expected hash.
// Expected format: "sha256:hexstring"
func VerifyHash(data []byte, expected string) error {
	parts := strings.SplitN(expected, ":", 2)
	if len(parts) != 2 {
		return runtimewasm.NewInvalidHashFormatError(expected)
	}

	algorithm := parts[0]
	expectedHash := parts[1]

	var actualHash string
	switch algorithm {
	case "sha256":
		h := sha256.Sum256(data)
		actualHash = hex.EncodeToString(h[:])
	default:
		return runtimewasm.NewUnsupportedHashAlgorithmError(algorithm)
	}

	if actualHash != expectedHash {
		return runtimewasm.NewHashMismatchError(expectedHash, actualHash)
	}

	return nil
}

// LoadAndVerifyWASM loads WASM from filesystem and verifies hash.
func LoadAndVerifyWASM(fsReg fsapi.Registry, fsID, path, hash string) ([]byte, error) {
	data, err := LoadWASM(fsReg, fsID, path)
	if err != nil {
		return nil, err
	}

	if err := VerifyHash(data, hash); err != nil {
		return nil, err
	}

	return data, nil
}
