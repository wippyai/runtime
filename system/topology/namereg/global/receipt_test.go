// SPDX-License-Identifier: MPL-2.0

package global

import "testing"

func TestConsistentRetryReturnsEstablishingIndex(t *testing.T) {
	f := NewFSM()
	p := makePID("node", "host", "owner")
	cmd := &Command{Type: CmdRegister, Name: "claim", PID: p}
	first := applyAt(t, f, cmd, 10).(*RegisterResult)
	retry := applyAt(t, f, cmd, 20).(*RegisterResult)
	if first.Err != nil || retry.Err != nil {
		t.Fatalf("register: %v, %v", first.Err, retry.Err)
	}
	if first.FenceToken != 10 || retry.FenceToken != first.FenceToken {
		t.Fatalf("retry invented a new claim receipt: first=%d retry=%d", first.FenceToken, retry.FenceToken)
	}
	applyAt(t, f, &Command{Type: CmdUnregister, Name: "claim"}, 30)
	reclaimed := applyAt(t, f, cmd, 40).(*RegisterResult)
	if reclaimed.Err != nil || reclaimed.FenceToken != 40 {
		t.Fatalf("reclaim: %+v", reclaimed)
	}
	last := applyAt(t, f, cmd, 50).(*RegisterResult)
	if last.Err != nil || last.FenceToken != 40 {
		t.Fatalf("reclaimed retry: %+v", last)
	}
}
