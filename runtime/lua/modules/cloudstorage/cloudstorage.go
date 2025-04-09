package cloudstorage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	csapi "github.com/ponyruntime/pony/api/cloudstorage"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// CloudStorage represents a cloud storage wrapper.
type CloudStorage struct {
	storage   csapi.Storage
	resource  resource.Resource[any] // Add resource tracking
	onRelease context.CancelFunc     // Add UoW cleanup function
}

// NewCloudStorage creates a new CloudStorage instance with resource tracking
func NewCloudStorage(storage csapi.Storage, res resource.Resource[any], onRelease context.CancelFunc) *CloudStorage {
	return &CloudStorage{
		storage:   storage,
		resource:  res,
		onRelease: onRelease,
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
		"release":           storageRelease,
	}

	value.RegisterTypeMethods(l, "cloudstorage.Storage", nil, methods)
}

func storageRelease(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)

	// Release the resource if it exists
	if cs.resource != nil {
		cs.resource.Release()
		cs.resource = nil
	}

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if cs.onRelease != nil {
		cs.onRelease()
		cs.onRelease = nil
	}

	l.Push(lua.LBool(true))
	return 1
}

func storageListObjects(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)

	opts := &csapi.ListObjectsOptions{}
	if l.Get(2) != lua.LNil {
		optsTable := l.CheckTable(2)

		if prefix := optsTable.RawGetString("prefix"); prefix != lua.LNil {
			opts.Prefix = prefix.String()
		}

		if maxKeys := optsTable.RawGetString("max_keys"); maxKeys != lua.LNil {
			opts.MaxKeys = int(lua.LVAsNumber(maxKeys))
		}

		if token := optsTable.RawGetString("continuation_token"); token != lua.LNil {
			opts.ContinuationToken = token.String()
		}
	}

	result, err := cs.storage.ListObjects(l.Context(), opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("cloudstorage.list_objects: %s", err.Error())))
		return 2
	}

	l.Push(pushListObjectsResult(l, result))
	return 1
}

func storageDownloadObject(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	// Properly extract io.Writer from userdata
	ud := l.CheckUserData(3)
	writer, ok := ud.Value.(io.Writer)
	if !ok {
		l.RaiseError("third argument must implement io.Writer")
		return 0
	}

	opts := &csapi.DownloadOptions{}
	if l.Get(4) != lua.LNil {
		optsTable := l.CheckTable(4)
		if rang := optsTable.RawGetString("range"); rang != lua.LNil {
			opts.Range = rang.String()
		}
	}

	err := cs.storage.DownloadObject(l.Context(), key, writer, opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("cloudstorage.download_object: %s", err.Error())))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}

func storageUploadObject(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	if l.Get(3) == lua.LNil {
		l.RaiseError("content argument required")
		return 0
	}

	v := l.Get(3)
	var reader io.Reader

	switch v := v.(type) {
	case lua.LString:
		reader = bytes.NewReader([]byte(v))
	case *lua.LUserData:
		if r, ok := v.Value.(io.Reader); ok {
			reader = r
		} else {
			l.RaiseError("input does not implement io.Reader")
			return 0
		}
	default:
		l.RaiseError("invalid input type, expected string or Reader")
		return 0
	}

	err := cs.storage.UploadObject(l.Context(), key, reader)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("cloudstorage.upload_object: %s", err.Error())))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}

func storageDeleteObjects(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	keysTable := l.CheckTable(2)

	keys := make([]string, keysTable.Len())
	keysTable.ForEach(func(idx, value lua.LValue) {
		if idx.Type() == lua.LTNumber {
			i := int(lua.LVAsNumber(idx)) - 1
			if i >= 0 && i < len(keys) {
				keys[i] = value.String()
			}
		}
	})

	err := cs.storage.DeleteObjects(l.Context(), keys)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("cloudstorage.delete_objects: %s", err.Error())))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}

func storagePresignedGetURL(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	opts := &csapi.PresignedGetOptions{
		Expiration: time.Hour, // Default 1 hour
	}

	if l.Get(3) != lua.LNil {
		optsTable := l.CheckTable(3)
		if exp := optsTable.RawGetString("expiration"); exp != lua.LNil {
			opts.Expiration = time.Duration(lua.LVAsNumber(exp)) * time.Second
		}
	}

	url, err := cs.storage.PresignedGetURL(l.Context(), key, opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("cloudstorage.presigned_get_url: %s", err.Error())))
		return 2
	}

	l.Push(lua.LString(url))
	return 1
}

func storagePresignedPutURL(l *lua.LState) int {
	cs := CheckCloudStorage(l, 1)
	key := l.CheckString(2)
	if key == "" {
		l.RaiseError("key required")
		return 0
	}

	opts := &csapi.PresignedPutOptions{
		Expiration: time.Hour, // Default 1 hour
	}

	if l.Get(3) != lua.LNil {
		optsTable := l.CheckTable(3)
		if exp := optsTable.RawGetString("expiration"); exp != lua.LNil {
			opts.Expiration = time.Duration(lua.LVAsNumber(exp)) * time.Second
		}
		if contentType := optsTable.RawGetString("content_type"); contentType != lua.LNil {
			opts.ContentType = contentType.String()
		}
		if contentLength := optsTable.RawGetString("content_length"); contentLength != lua.LNil {
			opts.ContentLength = int64(lua.LVAsNumber(contentLength))
		}
	}

	url, err := cs.storage.PresignedPutURL(l.Context(), key, opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("cloudstorage.presigned_put_url: %s", err.Error())))
		return 2
	}

	l.Push(lua.LString(url))
	return 1
}
