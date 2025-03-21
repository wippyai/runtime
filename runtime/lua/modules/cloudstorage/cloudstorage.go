package cloudstorage

import (
	"bytes"
	"fmt"
	"io"
	"time"

	csapi "github.com/ponyruntime/pony/api/cloudstorage"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// CloudStorage represents a cloud storage wrapper.
type CloudStorage struct {
	storage csapi.Storage
}

// NewCloudStorage creates a new CloudStorage instance
func NewCloudStorage(storage csapi.Storage) *CloudStorage {
	return &CloudStorage{
		storage: storage,
	}
}

// registerCloudStorage registers the CloudStorage module and its functions.
func registerCloudStorage(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		// Core operations
		"list_objects":      storageListObjects,
		"download_object":   storageDownloadObject,
		"upload_object":     storageUploadObject,
		"delete_objects":    storageDeleteObjects,
		"presigned_get_url": storagePresignedGetURL,
		"presigned_put_url": storagePresignedPutURL,
	}

	// Register the type with methods
	value.RegisterTypeMethods(l, "cloudstorage.Storage", nil, methods)
}

// storageListObjects lists objects in cloud storage with the given options.
func storageListObjects(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)

	// Get options table (optional)
	optsTable := l.OptTable(2, l.NewTable())

	// Parse options
	opts := &csapi.ListObjectsOptions{
		Prefix:            optsTable.RawGetString("prefix").String(),
		MaxKeys:           int(lua.LVAsNumber(optsTable.RawGetString("max_keys"))),
		ContinuationToken: optsTable.RawGetString("continuation_token").String(),
	}

	// Get context from Lua state
	ctx := l.Context()

	coroutine.Wrap(l, func() *engine.Update {
		result, err := cs.storage.ListObjects(ctx, opts)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.list_objects: %s", err.Error()))
		}

		return engine.NewUpdate(nil, []lua.LValue{pushListObjectsResult(l, result)}, nil)
	})

	return -1 // Yield
}

// storageDownloadObject downloads an object from cloud storage.
func storageDownloadObject(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	// Check if the third argument is a writable userdata type
	//if !lua.LVCanWrite(l.Get(3)) {
	//	l.RaiseError("third argument must be a writable target (like a file)")
	//	return 0
	//}

	// Get the writer from the userdata
	writer, ok := l.Get(3).(io.Writer)
	if !ok {
		l.RaiseError("third argument must implement io.Writer")
		return 0
	}

	// Get options table (optional)
	optsTable := l.OptTable(4, l.NewTable())

	// Parse options
	opts := &csapi.DownloadOptions{
		Range: optsTable.RawGetString("range").String(),
	}

	// Get context from Lua state
	ctx := l.Context()

	coroutine.Wrap(l, func() *engine.Update {
		err := cs.storage.DownloadObject(ctx, key, writer, opts)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.download_object: %s", err.Error()))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LBool(true)}, nil)
	})

	return -1 // Yield
}

// storageUploadObject uploads an object to cloud storage.
func storageUploadObject(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	// Validate third argument is present and can be read from
	if l.Get(3) == lua.LNil {
		l.RaiseError("content argument required")
		return 0
	}

	v := l.Get(3)

	// Get context from Lua state
	ctx := l.Context()

	coroutine.Wrap(l, func() *engine.Update {
		// Determine the reader based on input type
		var reader io.Reader

		switch v := v.(type) {
		case lua.LString:
			reader = bytes.NewReader([]byte(string(v)))

		case *lua.LUserData:
			// Check if the userdata implements io.Reader
			if r, ok := v.Value.(io.Reader); ok {
				reader = r
			} else {
				return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.upload_object: input does not implement io.Reader"))
			}

		default:
			return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.upload_object: invalid input type, expected string or Reader"))
		}

		err := cs.storage.UploadObject(ctx, key, reader)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.upload_object: %s", err.Error()))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LBool(true)}, nil)
	})

	return -1 // Yield
}

// storageDeleteObjects deletes objects from cloud storage.
func storageDeleteObjects(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)

	// Extract keys from the keys table
	keysTable := l.CheckTable(2)
	keys := make([]string, keysTable.Len())

	keysTable.ForEach(func(idx, value lua.LValue) {
		if idx.Type() == lua.LTNumber {
			i := int(lua.LVAsNumber(idx)) - 1 // Lua tables start at 1
			if i >= 0 && i < len(keys) {
				keys[i] = value.String()
			}
		}
	})

	// Get context from Lua state
	ctx := l.Context()

	coroutine.Wrap(l, func() *engine.Update {
		err := cs.storage.DeleteObjects(ctx, keys)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.delete_objects: %s", err.Error()))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LBool(true)}, nil)
	})

	return -1 // Yield
}

// storagePresignedGetURL generates a presigned URL for downloading an object.
func storagePresignedGetURL(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	// Get options table (optional)
	optsTable := l.OptTable(3, l.NewTable())

	// Parse options
	expirationSeconds := lua.LVAsNumber(optsTable.RawGetString("expiration"))
	if expirationSeconds <= 0 {
		expirationSeconds = 3600 // Default to 1 hour
	}

	opts := &csapi.PresignedGetOptions{
		Expiration: time.Duration(expirationSeconds) * time.Second,
	}

	// Get context from Lua state
	ctx := l.Context()

	coroutine.Wrap(l, func() *engine.Update {
		url, err := cs.storage.PresignedGetURL(ctx, key, opts)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.presigned_get_url: %s", err.Error()))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LString(url)}, nil)
	})

	return -1 // Yield
}

// storagePresignedPutURL generates a presigned URL for uploading an object.
func storagePresignedPutURL(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	// Get options table (optional)
	optsTable := l.OptTable(3, l.NewTable())

	// Parse options
	expirationSeconds := lua.LVAsNumber(optsTable.RawGetString("expiration"))
	if expirationSeconds <= 0 {
		expirationSeconds = 3600 // Default to 1 hour
	}

	opts := &csapi.PresignedPutOptions{
		Expiration:    time.Duration(expirationSeconds) * time.Second,
		ContentType:   optsTable.RawGetString("content_type").String(),
		ContentLength: int64(lua.LVAsNumber(optsTable.RawGetString("content_length"))),
	}

	// Get context from Lua state
	ctx := l.Context()

	coroutine.Wrap(l, func() *engine.Update {
		url, err := cs.storage.PresignedPutURL(ctx, key, opts)
		if err != nil {
			return engine.NewUpdate(nil, nil, fmt.Errorf("cloudstorage.presigned_put_url: %s", err.Error()))
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LString(url)}, nil)
	})

	return -1 // Yield
}
