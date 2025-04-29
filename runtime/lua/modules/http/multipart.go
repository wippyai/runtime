package http

import (
	"fmt"
	"mime/multipart"
	basehttp "net/http"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
)

// MultipartFile represents a file from a multipart form
type MultipartFile struct {
	fileHeader *multipart.FileHeader
	request    *basehttp.Request // Keep a reference to the request to ensure its context is used
}

// checkMultipartFile gets and verifies MultipartFile userdata from Lua state
//
//nolint:unparam
func checkMultipartFile(l *lua.LState, n int) (*MultipartFile, error) {
	ud := l.CheckUserData(n)
	if ud == nil {
		return nil, fmt.Errorf("argument %d must be a MultipartFile", n)
	}

	if file, ok := ud.Value.(*MultipartFile); ok {
		return file, nil
	}
	return nil, fmt.Errorf("argument %d must be a MultipartFile, got %T", n, ud.Value)
}

// multipartFileStream creates a stream from a multipart file
func multipartFileStream(l *lua.LState) int {
	mpFile, err := checkMultipartFile(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	// Open the file
	file, err := mpFile.fileHeader.Open()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to open file: %v", err)))
		return 2
	}

	// Get UnitOfWork
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		// Clean up opened file if UoW not available
		file.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work available"))
		return 2
	}

	// Create stream
	s, err := stream.NewStream(uw.Context(), file)
	if err != nil {
		file.Close()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create stream: %v", err)))
		return 2
	}

	// Create Lua stream with UoW integration for cleanup
	luaStream := stream.NewLuaStream(uw, s, func() {
		file.Close()
	})

	ud := l.NewUserData()
	ud.Value = luaStream
	ud.Metatable = value.GetTypeMetatable(l, "Stream")

	l.Push(ud)
	return 1
}

// multipartFileSize returns the size of the multipart file
func multipartFileSize(l *lua.LState) int {
	mpFile, err := checkMultipartFile(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LNumber(mpFile.fileHeader.Size))
	l.Push(lua.LNil)
	return 2
}

// multipartFileName returns the original filename of the multipart file
func multipartFileName(l *lua.LState) int {
	mpFile, err := checkMultipartFile(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(mpFile.fileHeader.Filename))
	l.Push(lua.LNil)
	return 2
}

// multipartFileToString implements the __tostring metamethod for MultipartFile
func multipartFileToString(l *lua.LState) int {
	mpFile, err := checkMultipartFile(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(fmt.Sprintf("http.MultipartFile{name=%s, size=%d}",
		mpFile.fileHeader.Filename, mpFile.fileHeader.Size)))
	return 1
}

// registerMultipartFile registers the MultipartFile type and its methods
func registerMultipartFile(l *lua.LState) {
	mt := l.NewTypeMetatable("MultipartFile")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"stream": multipartFileStream,
		"size":   multipartFileSize,
		"name":   multipartFileName,
	}))
	l.SetField(mt, "__tostring", l.NewFunction(multipartFileToString))
}
