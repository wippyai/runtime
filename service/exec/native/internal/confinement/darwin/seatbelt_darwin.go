// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package darwin

/*
#cgo LDFLAGS: -lsandbox
#cgo CFLAGS: -Wno-deprecated-declarations
#include <sandbox.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

func applySeatbelt(profile string) error {
	value := C.CString(profile)
	defer C.free(unsafe.Pointer(value))
	var message *C.char
	if C.sandbox_init(value, 0, &message) == 0 {
		return nil
	}
	if message == nil {
		return errors.New("sandbox_init failed")
	}
	defer C.sandbox_free_error(message)
	return errors.New(C.GoString(message))
}
