// SPDX-License-Identifier: MPL-2.0

package graph

import (
	"strings"
)

// Name represents a module name in org/module format.
type Name struct {
	Organization string
	Module       string
}

// ParseName parses a module name string into Name.
func ParseName(s string) (Name, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return Name{}, NewInvalidModuleNameError(s)
	}
	if parts[0] == "" || parts[1] == "" {
		return Name{}, NewEmptyModuleNameError(s)
	}
	return Name{
		Organization: parts[0],
		Module:       parts[1],
	}, nil
}

// MustParseName parses a module name or panics.
func MustParseName(s string) Name {
	n, err := ParseName(s)
	if err != nil {
		panic(err)
	}
	return n
}

// String returns the module name as org/module.
func (n Name) String() string {
	return n.Organization + "/" + n.Module
}

// IsZero returns true if the name is empty.
func (n Name) IsZero() bool {
	return n.Organization == "" && n.Module == ""
}
