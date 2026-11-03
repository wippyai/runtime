// SPDX-License-Identifier: MPL-2.0

package lua

import (
	glua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrorMetadata projects the kind, retryability and details at the root of an
// apierror chain onto the metadata Lua errors expose. It is the extractor the
// engine configures for errors wrapped with glua.WrapErrorWithLua.
func ErrorMetadata(err error) *glua.ErrorMetadata {
	if err == nil {
		return nil
	}

	chain := apierror.BuildChain(err)
	if chain == nil {
		return nil
	}
	root := chain.Root()
	if root == nil {
		return nil
	}

	meta := &glua.ErrorMetadata{}
	if root.Kind != "" {
		meta.Kind = glua.Kind(root.Kind)
	}
	if root.Retryable != nil {
		b := *root.Retryable
		meta.Retryable = &b
	}
	if len(root.Details) > 0 {
		meta.Details = make(map[string]any, len(root.Details))
		for k, v := range root.Details {
			meta.Details[k] = v
		}
	}

	if meta.Kind == "" && meta.Retryable == nil && meta.Details == nil {
		return nil
	}
	return meta
}
