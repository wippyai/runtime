// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func startMembershipServiceForTest(ctx context.Context, t *testing.T, label string, service *Service) {
	t.Helper()

	err := service.Start(ctx)
	if err == nil {
		return
	}
	if isWindowsUDPBindDenied(err) {
		if label != "" {
			t.Skipf("%s: windows runner denied loopback UDP bind for memberlist: %v", label, err)
		}
		t.Skipf("windows runner denied loopback UDP bind for memberlist: %v", err)
	}
	if label != "" {
		t.Fatalf("%s: start membership: %v", label, err)
	}
	t.Fatalf("start membership: %v", err)
}

func isWindowsUDPBindDenied(err error) bool {
	if err == nil || runtime.GOOS != "windows" {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "listen udp") &&
		strings.Contains(msg, "forbidden by its access permissions")
}
