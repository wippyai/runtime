// SPDX-License-Identifier: MPL-2.0

// Package function integrates Lua handlers with the function subsystem.
//
// Explicit parameter annotations on the selected Lua callable are validated
// automatically before its body starts. No entry configuration is required,
// and static type-check diagnostics need not be enabled. All pool types use the
// same engine boundary. Failures return a non-retryable INVALID error containing
// the one-based argument index and, for type failures, the parameter and field.
//
// Validation uses the existing Lua Type:is rules without coercion: absent values
// are nil, optional parameters accept nil, untyped parameters remain unchecked,
// and typed varargs check each extra value. Ordinary extra arguments retain Lua
// semantics. Records allow additional fields; arrays and string-keyed maps
// validate their respective table portions. Callable and opaque platform types
// check runtime kinds, not callback signatures or userdata identity. Generic
// type parameters are not instantiated from call arguments at runtime.
//
// Contracts belong to the actual callable prototype, including aliases and
// reexports, and survive new bytecode dumps and compile caches. Bytecode built
// before declaration metadata existed must be rebuilt to enable these checks.
// Module initialization can run before validation; the rejected method cannot.
// Ordinary Lua calls, process startup, workflows and return values are unaffected.
package function
