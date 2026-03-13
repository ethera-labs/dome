#!/usr/bin/env bash
# Runs dome tests while capturing Docker logs from key containers.
#
# Usage: ./scripts/test-with-logs.sh [TEST_NAME] [TIMEOUT]
#   TEST_NAME  — test name filter (default: all tests)
#   TIMEOUT    — test timeout (default: 300s)
#
# Environment variables:
#   LOGS_DIR    — directory for log files (default: logs)
#   LOG_LEVEL   — log level passed to the binary (default: INFO)
#   CONFIG_PATH — external config file path (optional)
#
# Logs are saved to $LOGS_DIR/<container>.log
# Test output is saved to $LOGS_DIR/test.log

set -euo pipefail

TEST_NAME="${1:-}"
TIMEOUT="${2:-300s}"
LOGS_DIR="${LOGS_DIR:-logs}"
LOG_LEVEL="${LOG_LEVEL:-INFO}"

CONTAINERS=(
  publisher
  sidecar-a
  sidecar-b
  op-rbuilder-a
  op-rbuilder-b
  rollup-boost-a
  rollup-boost-b
  op-geth-a
  op-geth-b
  op-node-a
  op-node-b
)

mkdir -p "$LOGS_DIR"

LOG_PIDS=()
for container in "${CONTAINERS[@]}"; do
  if docker inspect "$container" &>/dev/null; then
    docker logs -f --timestamps "$container" > "$LOGS_DIR/${container}.log" 2>&1 &
    LOG_PIDS+=($!)
    echo "[logs] $container -> $LOGS_DIR/${container}.log"
  fi
done

cleanup() {
  echo "Stopping log capture..."
  for pid in "${LOG_PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT INT TERM

echo ""

TEST_ARGS="-test.v -test.timeout $TIMEOUT"
if [[ -n "$TEST_NAME" ]]; then
  TEST_ARGS="$TEST_ARGS -test.run $TEST_NAME"
fi

echo "Running: LOG_LEVEL=$LOG_LEVEL ./bin/dome $TEST_ARGS"
echo "Test output -> $LOGS_DIR/test.log"
echo ""

env_prefix="LOG_LEVEL=$LOG_LEVEL"
if [[ -n "${CONFIG_PATH:-}" ]]; then
  env_prefix="$env_prefix CONFIG_PATH=$CONFIG_PATH"
fi

eval "env $env_prefix ./bin/dome $TEST_ARGS" 2>&1 | tee "$LOGS_DIR/test.log"
