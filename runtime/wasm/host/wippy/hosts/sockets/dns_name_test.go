// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/wasm-runtime/transcoder"
	"go.bytecodealliance.org/wit"
)

func TestNormalizeResolveName(t *testing.T) {
	for _, test := range []struct {
		input, want string
		literal     bool
	}{
		{"EXAMPLE.COM.", "example.com.", false},
		{".", ".", false},
		{"bücher.example", "xn--bcher-kva.example", false},
		{"192.0.2.1", "192.0.2.1", true},
		{"2001:DB8::1", "2001:db8::1", true},
		{"::ffff:192.0.2.1", "192.0.2.1", true},
	} {
		t.Run(test.input, func(t *testing.T) {
			name, ip, err := normalizeResolveName(test.input)
			require.Nil(t, err)
			require.Equal(t, test.want, name)
			require.Equal(t, test.literal, ip != nil)
		})
	}
	for _, input := range []string{"", "a..b", "-a.example", "a-.example", "a_b.example", "host:80", "[::1]", "fe80::1%1", "a\x00b", "\xff", strings.Repeat("a", 64) + ".example", strings.Repeat("a", maxResolveNameBytes+1)} {
		_, _, err := normalizeResolveName(input)
		requireNetworkError(t, err, NetworkErrorInvalidArgument)
	}
}

func TestIPAddressCanonicalVariantHasNoSocketFields(t *testing.T) {
	v4 := &wit.TypeDef{Kind: &wit.Tuple{Types: []wit.Type{wit.U8{}, wit.U8{}, wit.U8{}, wit.U8{}}}}
	v6 := &wit.TypeDef{Kind: &wit.Tuple{Types: []wit.Type{wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{}}}}
	ip := &wit.TypeDef{Kind: &wit.Variant{Cases: []wit.Case{{Name: "ipv4", Type: v4}, {Name: "ipv6", Type: v6}}}}
	compiled, err := transcoder.NewCompiler().Compile(ip, reflect.TypeOf(IPAddress{}))
	require.NoError(t, err)
	require.Equal(t, 9, compiled.FlatCount)
	for _, test := range []struct {
		name string
		want []uint64
	}{
		{"192.0.2.1", []uint64{0, 192, 0, 2, 1, 0, 0, 0, 0}},
		{"::ffff:192.0.2.1", []uint64{0, 192, 0, 2, 1, 0, 0, 0, 0}},
		{"2001:db8::1234", []uint64{1, 0x2001, 0xdb8, 0, 0, 0, 0, 0, 0x1234}},
	} {
		value := parseIPAddress(test.name)
		require.NotNil(t, value)
		stack := make([]uint64, compiled.FlatCount)
		consumed, err := transcoder.NewEncoder().LowerToStack(compiled, unsafe.Pointer(value), stack, nil, nil)
		require.NoError(t, err)
		require.Equal(t, 9, consumed)
		require.Equal(t, test.want, stack)
	}
}
