// SPDX-License-Identifier: MPL-2.0

package random

import (
	"context"
	"testing"
)

func TestR14RandomHostsCapOversize(t *testing.T) {
	ctx := context.Background()
	hosts := []struct {
		get  func(context.Context, uint64) []byte
		name string
	}{
		{name: "secure", get: NewSecureRandomHost().GetRandomBytes},
		{name: "insecure", get: NewInsecureRandomHost().GetInsecureRandomBytes},
	}

	for _, host := range hosts {
		got := host.get(ctx, MaxRandomBytes+1)
		if len(got) != MaxRandomBytes {
			t.Fatalf("%s oversize result length = %d, want cap %d", host.name, len(got), MaxRandomBytes)
		}
	}
}
