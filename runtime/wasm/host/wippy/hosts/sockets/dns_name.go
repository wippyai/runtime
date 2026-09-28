// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/tetratelabs/wazero/api"
	"golang.org/x/net/idna"
)

// Bound canonical input before string lifting/IDNA processing allocates host memory.
const maxResolveNameBytes = 1024

func validateResolveNameMemory(_ context.Context, module api.Module, stack []uint64) error {
	if len(stack) != 4 || uint32(stack[2]) > maxResolveNameBytes {
		return errors.New("DNS name exceeds host byte limit")
	}
	if module == nil || module.Memory() == nil {
		return errors.New("DNS name memory unavailable")
	}
	if _, ok := module.Memory().Read(uint32(stack[1]), uint32(stack[2])); !ok {
		return errors.New("DNS name outside guest memory")
	}
	return nil
}

func normalizeResolveName(name string) (string, *IPAddress, *NetworkError) {
	invalid := func() (string, *IPAddress, *NetworkError) {
		return "", nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if len(name) == 0 || len(name) > maxResolveNameBytes || !utf8.ValidString(name) {
		return invalid()
	}
	if address := parseIPAddress(name); address != nil {
		return address.String(), address, nil
	}
	ascii, err := idna.Lookup.ToASCII(name)
	if err != nil {
		return invalid()
	}
	ascii = strings.ToLower(ascii)
	if ascii == "." {
		return ascii, nil, nil
	}
	domain := strings.TrimSuffix(ascii, ".")
	if len(domain) == 0 || len(domain) > 253 {
		return invalid()
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return invalid()
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return invalid()
			}
		}
	}
	return ascii, nil, nil
}
