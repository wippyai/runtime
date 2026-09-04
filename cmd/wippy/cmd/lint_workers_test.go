// SPDX-License-Identifier: MPL-2.0

package cmd

import "testing"

func TestBoundedLintWorkers(t *testing.T) {
	for _, test := range []struct {
		name  string
		procs int
		want  int
	}{
		{name: "zero", procs: 0, want: 1},
		{name: "negative", procs: -1, want: 1},
		{name: "available", procs: 4, want: 4},
		{name: "cap", procs: maxLintWorkers + 1, want: maxLintWorkers},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := boundedLintWorkers(test.procs); got != test.want {
				t.Fatalf("boundedLintWorkers(%d) = %d, want %d", test.procs, got, test.want)
			}
		})
	}
}

func TestDefaultLintWorkersIsBounded(t *testing.T) {
	got := defaultLintWorkers()
	if got < 1 || got > maxLintWorkers {
		t.Fatalf("defaultLintWorkers() = %d, want [1,%d]", got, maxLintWorkers)
	}
}
