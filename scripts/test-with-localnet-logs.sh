#!/usr/bin/env bash
# Runs dome tests against a local-testnet while capturing Docker container logs.
# Container names are hardcoded to match the local-testnet docker-compose setup.
#
# Usage: ./scripts/test-with-localnet-logs.sh [TEST_NAME] [TIMEOUT]
#   TEST_NAME  - test name filter (default: all tests)
#   TIMEOUT    - test timeout (default: 300s)
#
# Environment variables:
#   LOGS_DIR    - directory for log files (default: logs)
#   LOG_LEVEL   - log level passed to the binary (default: INFO)
#   CONFIG_PATH - external config file path (optional)
#   CONTAINERS  - space-separated list of Docker container names to capture logs from
#                 (default: local-testnet containers)
#
# Logs are saved to $LOGS_DIR/<container>.log
# Test output is saved to $LOGS_DIR/test.log

set -euo pipefail

TEST_NAME="${1:-}"
TIMEOUT="${2:-300s}"
LOGS_DIR="${LOGS_DIR:-logs}"
LOG_LEVEL="${LOG_LEVEL:-INFO}"

DEFAULT_CONTAINERS=(
  publisher
  sidecar-a
  sidecar-b
  op-rbuilder-a
  op-rbuilder-b
  rollup-boost-a
  rollup-boost-b
  op-reth-a
  op-reth-b
  op-node-a
  op-node-b
)

# Allow overriding the container list via CONTAINERS env var (space-separated).
# Example: CONTAINERS="my-publisher my-sidecar" ./scripts/test-with-localnet-logs.sh
if [[ -n "${CONTAINERS:-}" ]]; then
  read -ra CONTAINERS <<< "$CONTAINERS"
else
  CONTAINERS=("${DEFAULT_CONTAINERS[@]}")
fi

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
