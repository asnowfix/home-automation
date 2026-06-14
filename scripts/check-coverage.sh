#!/usr/bin/env bash
# check-coverage.sh — Verify that aggregate Go coverage meets the minimum floor.
#
# Usage:
#   ./scripts/check-coverage.sh [MIN]
#   COVERAGE_MIN=35 ./scripts/check-coverage.sh
#
# MIN defaults to the content of .coverage-min (a single number, e.g. "34").
# Exits 1 if coverage is below the threshold.
set -euo pipefail

COVERAGE_FILE="${COVERAGE_FILE:-coverage.txt}"
MIN="${1:-${COVERAGE_MIN:-$(cat .coverage-min 2>/dev/null || echo "0")}}"

if [ ! -f "$COVERAGE_FILE" ]; then
  echo "ERROR: $COVERAGE_FILE not found. Run 'make cover' first." >&2
  exit 1
fi

TOTAL=$(go tool cover -func="$COVERAGE_FILE" | grep '^total:' | awk '{print $3}' | tr -d '%')

if [ -z "$TOTAL" ]; then
  echo "ERROR: could not parse total coverage from $COVERAGE_FILE" >&2
  exit 1
fi

echo "Coverage: ${TOTAL}%  (floor: ${MIN}%)"

if awk "BEGIN { exit !(${TOTAL} + 0 < ${MIN} + 0) }"; then
  echo "FAIL: coverage ${TOTAL}% is below the ${MIN}% floor."
  echo "      To raise the floor: update .coverage-min after improving tests."
  exit 1
fi

echo "PASS"
