#!/bin/bash
set -e

# Usage: ./scripts/test_integration.sh /path/to/defradb [PLAYGROUND]
# Or:   DEFRA_PATH=/path/to/defradb ./scripts/test_integration.sh [PLAYGROUND]

if [[ -z "$DEFRA_PATH" && -z "$1" ]]; then
  echo "ERROR: You must provide DEFRA_PATH as an env variable or first argument."
  echo "Usage: ./scripts/test_integration.sh /path/to/defradb [PLAYGROUND]"
  exit 1
fi

DEFRA_PATH_ARG="$DEFRA_PATH"
if [[ -z "$DEFRA_PATH_ARG" ]]; then
  DEFRA_PATH_ARG="$1"
  shift
fi

# Start services in the background
./scripts/bootstrap.sh "$DEFRA_PATH_ARG" "$@" &
BOOTSTRAP_PID=$!

# Ensure cleanup on exit or interruption
cleanup() {
  echo "===> Cleaning up bootstrap.sh (PID $BOOTSTRAP_PID) and child services..."
  kill $BOOTSTRAP_PID 2>/dev/null || true
  wait $BOOTSTRAP_PID 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait for ready file from bootstrap.sh
READY_FILE=".defra/ready"
echo "===> Waiting for $READY_FILE to be created by bootstrap.sh..."
for i in {1..30}; do
  if [ -f "$READY_FILE" ]; then
    echo "===> $READY_FILE found."
    break
  fi
  sleep 1
done

# Wait for GraphQL endpoint to be ready
GRAPHQL_URL="http://localhost:9181/api/v0/graphql"
echo "===> Waiting for GraphQL endpoint at $GRAPHQL_URL to be ready (schema applied)..."
for i in {1..30}; do
  RESPONSE=$(curl -s -X POST -H "Content-Type: application/json" --data '{"query":"{ Block { __typename } }"}' "$GRAPHQL_URL")
  if echo "$RESPONSE" | grep -q '"Block"'; then
    echo "===> GraphQL endpoint is up and schema is applied."
    break
  fi
  sleep 1
done

# Run integration tests
GO111MODULE=on go test -v -tags=integration ./integration/... > integration_test_output.txt 2>&1 || true
echo -e "\n\n===> Integration test output:"
cat integration_test_output.txt
rm integration_test_output.txt
echo -e "\n\n===> Starting cleanup..."