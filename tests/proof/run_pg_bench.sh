#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
#
# PG join/leave micro-benchmark regression gate.
#
# Runs the Go benchmarks in system/pg/pg_bench_test.go, compares results
# against the committed baseline (tests/proof/pg_bench/baseline.txt) using
# benchstat, and fails if any benchmark shows >THRESHOLD_NSOP% ns/op
# regression or >THRESHOLD_ALLOCS% allocs/op regression.
#
# pg did not exist before the feature/pg-process-groups branch, so the
# committed baseline is the "as shipped" measurement, not a parent commit.
# Future PRs that touch pg regenerate bench.new.txt and compare it here.
#
# Refresh the baseline with: bash tests/proof/run_pg_bench.sh --refresh
#
# Env overrides:
#   BENCHTIME=1s         per-benchmark time budget
#   COUNT=10             benchstat sample count
#   THRESHOLD_NSOP=10    fail threshold on ns/op delta (percent)
#   THRESHOLD_ALLOCS=25  fail threshold on allocs/op delta (percent)

set -euo pipefail

PROOF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$PROOF_DIR/.." && pwd)"
BENCH_DIR="$PROOF_DIR/pg_bench"
BASELINE="$BENCH_DIR/baseline.txt"
NEW="$BENCH_DIR/bench.new.txt"
COMPARE="$BENCH_DIR/compare.txt"

BENCHTIME="${BENCHTIME:-1s}"
COUNT="${COUNT:-10}"
THRESHOLD_NSOP="${THRESHOLD_NSOP:-10}"
THRESHOLD_ALLOCS="${THRESHOLD_ALLOCS:-25}"

mkdir -p "$BENCH_DIR"

REFRESH=0
if [ "${1:-}" = "--refresh" ]; then
	REFRESH=1
fi

ensure_benchstat() {
	if ! command -v benchstat >/dev/null 2>&1; then
		echo ">> installing benchstat"
		go install golang.org/x/perf/cmd/benchstat@latest
		export PATH="$(go env GOPATH)/bin:$PATH"
	fi
}

run_bench() {
	local out="$1"
	echo ">> running benchmarks (benchtime=$BENCHTIME, count=$COUNT) -> $out"
	(
		cd "$REPO"
		go test -run='^$' -bench='^BenchmarkPG' \
			-benchtime="$BENCHTIME" -count="$COUNT" -benchmem \
			./system/pg/
	) | tee "$out"
}

ensure_benchstat

if [ "$REFRESH" -eq 1 ]; then
	echo ">> refreshing baseline on $(git -C "$REPO" rev-parse --short HEAD)"
	run_bench "$BASELINE"
	echo "PG-BENCH: BASELINE-REFRESHED at $BASELINE"
	exit 0
fi

if [ ! -s "$BASELINE" ]; then
	echo ">> no baseline at $BASELINE — generating it from HEAD"
	run_bench "$BASELINE"
	echo "PG-BENCH: PASS (initial baseline established)"
	exit 0
fi

run_bench "$NEW"

echo ">> comparing $BASELINE vs $NEW"
benchstat "$BASELINE" "$NEW" | tee "$COMPARE"

# benchstat v2 emits a "geomean" row and per-benchmark rows; the second-to-last
# column is the percent delta for the metric (sec/op, B/op, allocs/op).
# We parse only the rows whose first token starts with "BenchmarkPG" or "PG"
# (after the leading "Benchmark" is stripped by benchstat).
FAIL_REASONS=()

check_metric() {
	local metric="$1"
	local threshold="$2"
	local section_started=0
	while IFS= read -r line; do
		case "$line" in
			*"$metric"*)
				section_started=1
				continue
				;;
			"")
				section_started=0
				continue
				;;
		esac
		if [ "$section_started" -eq 1 ]; then
			# Extract a "+X.YZ%" or "-X.YZ%" token from the line.
			pct=$(echo "$line" | grep -oE '[+-][0-9]+\.[0-9]+%' | head -1 | tr -d '%+')
			if [ -n "$pct" ]; then
				name=$(echo "$line" | awk '{print $1}')
				abs=$(echo "$pct" | awk '{print ($1<0)? -$1 : $1}')
				if echo "$pct" | grep -q '^-'; then
					continue
				fi
				if awk -v a="$abs" -v t="$threshold" 'BEGIN{exit !(a>t)}'; then
					FAIL_REASONS+=("$metric: $name regressed by +${pct}% (threshold ${threshold}%)")
				fi
			fi
		fi
	done < "$COMPARE"
}

check_metric "sec/op" "$THRESHOLD_NSOP"
check_metric "allocs/op" "$THRESHOLD_ALLOCS"

if [ "${#FAIL_REASONS[@]}" -gt 0 ]; then
	echo
	echo "PG-BENCH: FAIL"
	for r in "${FAIL_REASONS[@]}"; do
		echo "  - $r"
	done
	exit 1
fi

echo
echo "PG-BENCH: PASS (within +${THRESHOLD_NSOP}% sec/op, +${THRESHOLD_ALLOCS}% allocs/op of baseline)"
exit 0
