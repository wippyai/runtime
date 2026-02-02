#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$SCRIPT_DIR/../app"
WIPPY_CMD="$SCRIPT_DIR/../../cmd/wippy"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

cleanup() {
    echo -e "${YELLOW}Cleaning up...${NC}"
    if [ -n "$SERVER_PID" ]; then
        kill $SERVER_PID 2>/dev/null || true
        wait $SERVER_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

echo -e "${YELLOW}Starting wippy server...${NC}"
cd "$APP_DIR"
OTEL_SDK_DISABLED=true go run "$WIPPY_CMD" run -c -v &
SERVER_PID=$!

# Wait for server to be ready (check for temporal client)
echo -e "${YELLOW}Waiting for server to be ready...${NC}"
for i in {1..30}; do
    if curl -s http://localhost:7233 >/dev/null 2>&1 || [ $i -eq 30 ]; then
        break
    fi
    sleep 1
done
sleep 3  # Extra time for temporal workers to register

echo -e "${GREEN}Server ready. Running tests...${NC}"
cd "$SCRIPT_DIR"
OTEL_SDK_DISABLED=true go run . "$@"
TEST_EXIT=$?

echo -e "${YELLOW}Tests completed with exit code: $TEST_EXIT${NC}"
exit $TEST_EXIT
