#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COVERAGE_FILE="${ROOT_DIR}/coverage_internal.out"
COVERAGE_MIN=80.0
RUN_STRESS=0

if [[ "${1:-}" == "--stress" ]]; then
  RUN_STRESS=1
fi

cd "${ROOT_DIR}"

echo "[1/4] Running default test suite"
go test ./...

echo "[2/4] Enforcing internal package coverage >= ${COVERAGE_MIN}%"
go test ./internal/... -covermode=atomic -coverprofile "${COVERAGE_FILE}"
TOTAL_COVERAGE="$(go tool cover -func="${COVERAGE_FILE}" | awk '/^total:/ {gsub("%", "", $3); print $3}')"
if [[ -z "${TOTAL_COVERAGE}" ]]; then
  echo "failed to parse total coverage from ${COVERAGE_FILE}" >&2
  exit 1
fi
awk -v total="${TOTAL_COVERAGE}" -v min="${COVERAGE_MIN}" 'BEGIN { exit !(total+0 >= min+0) }' || {
  echo "coverage threshold failure: got ${TOTAL_COVERAGE}% want >= ${COVERAGE_MIN}% (internal/* only)" >&2
  exit 1
}
echo "coverage check passed: ${TOTAL_COVERAGE}%"

echo "[3/4] Running race tests for all packages"
CGO_ENABLED=1 go test -race ./...

if [[ "${RUN_STRESS}" -eq 1 ]]; then
  echo "[4/4] Running stress-tagged tests"
  go test -tags=stress ./...
else
  echo "[4/4] Stress-tagged tests skipped (use --stress to include)"
fi
