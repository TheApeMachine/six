#!/usr/bin/env bash
set -euo pipefail

VIZ_PID=""
HTTP_PORT="${VIZ_HTTP_PORT:-8257}"

cleanup() {
  if [[ -n "${VIZ_PID}" ]] && kill -0 "${VIZ_PID}" 2>/dev/null; then
    kill "${VIZ_PID}" 2>/dev/null || true
    wait "${VIZ_PID}" 2>/dev/null || true
  fi
}

trap cleanup INT TERM EXIT

go run main.go viz --listen --http ":${HTTP_PORT}" &
VIZ_PID=$!

ready=0
for _ in $(seq 1 60); do
  if curl -sf -o /dev/null "http://127.0.0.1:${HTTP_PORT}/"; then
    ready=1
    break
  fi
  sleep 0.2
done

if [[ "${ready}" -ne 1 ]]; then
  echo "viz server did not become ready on port ${HTTP_PORT}" >&2
  exit 1
fi

set +e
go test -v ./experiment/task -run TestPipeline/Text_Classification -timeout 2m
TEST_EXIT=$?
set -e

cleanup
trap - INT TERM EXIT
exit "${TEST_EXIT}"
