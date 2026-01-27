#!/bin/bash
# =============================================================================
# Test runner for all integration tests (BATS) across all modules
#
# Tests run inside a Docker container with:
# - LocalStack for AWS emulation
# - Moto for CloudFront emulation
# - Smocker for nullplatform API mocking
#
# Usage:
#   ./testing/run_integration_tests.sh                    # Run all tests
#   ./testing/run_integration_tests.sh frontend           # Run tests for frontend module only
#   ./testing/run_integration_tests.sh --build            # Rebuild containers before running
#   ./testing/run_integration_tests.sh -v|--verbose       # Show output of passing tests
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Parse arguments
MODULE=""
BUILD_FLAG=""
VERBOSE=""

for arg in "$@"; do
  case $arg in
  --build)
    BUILD_FLAG="--build"
    ;;
  -v|--verbose)
    VERBOSE="--show-output-of-passing-tests"
    ;;
  *)
    MODULE="$arg"
    ;;
  esac
done

# Docker compose file location
COMPOSE_FILE="$SCRIPT_DIR/docker/docker-compose.integration.yml"

# Check if docker is installed
if ! command -v docker &> /dev/null; then
  echo -e "${RED}docker is not installed${NC}"
  echo ""
  echo "Install with:"
  echo "  brew install docker       # macOS"
  echo "  apt install docker.io     # Ubuntu/Debian"
  echo "  apk add docker            # Alpine"
  echo "  choco install docker      # Windows"
  exit 1
fi

# Check if docker compose file exists
if [ ! -f "$COMPOSE_FILE" ]; then
  echo -e "${RED}Docker compose file not found: $COMPOSE_FILE${NC}"
  exit 1
fi

# Generate certificates if they don't exist
CERT_DIR="$SCRIPT_DIR/docker/certs"
if [ ! -f "$CERT_DIR/cert.pem" ] || [ ! -f "$CERT_DIR/key.pem" ]; then
  echo -e "${CYAN}Generating TLS certificates...${NC}"
  "$SCRIPT_DIR/docker/generate-certs.sh"
fi

# Find all integration test directories
find_test_dirs() {
  find . -type d -name "integration" -path "*/tests/*" -not -path "*/node_modules/*" 2>/dev/null | sort
}

# Get module name from test path
get_module_name() {
  local path="$1"
  echo "$path" | sed 's|^\./||' | cut -d'/' -f1
}

# Cleanup function
cleanup() {
  echo ""
  echo -e "${CYAN}Stopping containers...${NC}"
  docker compose -f "$COMPOSE_FILE" down -v 2>/dev/null || true
}

echo ""
echo "========================================"
echo "  Integration Tests (Containerized)"
echo "========================================"
echo ""

# Print available test helpers reference
source "$SCRIPT_DIR/integration_helpers.sh"
test_help
echo ""

# Set trap for cleanup
trap cleanup EXIT

# Build test runner and azure-mock images if needed
echo -e "${CYAN}Building containers...${NC}"
docker compose -f "$COMPOSE_FILE" build $BUILD_FLAG test-runner azure-mock 2>&1 | grep -v "^$" || true
echo ""

# Start infrastructure services
echo -e "${CYAN}Starting infrastructure services...${NC}"
docker compose -f "$COMPOSE_FILE" up -d localstack moto azure-mock smocker nginx-proxy 2>&1 | grep -v "^$" || true

# Wait for services to be healthy
echo -n "Waiting for services to be ready"
max_attempts=30
attempt=0

while [ $attempt -lt $max_attempts ]; do
  # Check health via curl (most reliable)
  localstack_ok=$(curl -s "http://localhost:4566/_localstack/health" 2>/dev/null | jq -e '.services.s3 == "running"' >/dev/null 2>&1 && echo "yes" || echo "no")
  moto_ok=$(curl -s "http://localhost:5555/moto-api/" >/dev/null 2>&1 && echo "yes" || echo "no")
  azure_mock_ok=$(curl -s "http://localhost:8090/health" 2>/dev/null | jq -e '.status == "ok"' >/dev/null 2>&1 && echo "yes" || echo "no")
  smocker_ok=$(curl -s "http://localhost:8081/version" >/dev/null 2>&1 && echo "yes" || echo "no")
  nginx_ok=$(curl -sk "https://localhost:8443/mocks" >/dev/null 2>&1 && echo "yes" || echo "no")

  if [[ "$localstack_ok" == "yes" ]] && [[ "$moto_ok" == "yes" ]] && [[ "$azure_mock_ok" == "yes" ]] && [[ "$smocker_ok" == "yes" ]] && [[ "$nginx_ok" == "yes" ]]; then
    echo ""
    echo -e "${GREEN}All services ready${NC}"
    break
  fi

  attempt=$((attempt + 1))
  sleep 2
  echo -n "."
done

if [ $attempt -eq $max_attempts ]; then
  echo ""
  echo -e "${RED}Services failed to start${NC}"
  docker compose -f "$COMPOSE_FILE" logs
  exit 1
fi

echo ""

# Get smocker container IP for DNS resolution
SMOCKER_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' integration-smocker 2>/dev/null || echo "172.28.0.10")
export SMOCKER_IP

# Determine which tests to run
if [ -n "$MODULE" ]; then
  if [ -d "$MODULE" ]; then
    TEST_PATHS=$(find "$MODULE" -type d -name "integration" -path "*/tests/*" 2>/dev/null | sort)
    if [ -z "$TEST_PATHS" ]; then
      echo -e "${RED}No integration test directories found in: $MODULE${NC}"
      exit 1
    fi
  else
    echo -e "${RED}Directory not found: $MODULE${NC}"
    echo ""
    echo "Available modules with integration tests:"
    for dir in $(find_test_dirs); do
      echo "  - $(get_module_name "$dir")"
    done | sort -u
    exit 1
  fi
else
  TEST_PATHS=$(find_test_dirs)
  if [ -z "$TEST_PATHS" ]; then
    echo -e "${YELLOW}No integration test directories found${NC}"
    exit 0
  fi
fi

# Run tests for each directory
TOTAL_FAILED=0

for test_dir in $TEST_PATHS; do
  module_name=$(get_module_name "$test_dir")

  # Find .bats files recursively (supports test_cases/ subfolder structure)
  bats_files=$(find "$test_dir" -name "*.bats" 2>/dev/null | sort)
  if [ -z "$bats_files" ]; then
    continue
  fi

  echo -e "${CYAN}[$module_name]${NC} Running integration tests in $test_dir"
  echo ""

  # Strip leading ./ from test_dir for cleaner paths
  container_test_dir="${test_dir#./}"

  # Build list of test files for bats (space-separated, container paths)
  container_bats_files=""
  for bats_file in $bats_files; do
    container_path="/workspace/${bats_file#./}"
    container_bats_files="$container_bats_files $container_path"
  done

  # Run tests inside the container
  docker compose -f "$COMPOSE_FILE" run --rm \
    -e PROJECT_ROOT=/workspace \
    -e SMOCKER_HOST=http://smocker:8081 \
    -e LOCALSTACK_ENDPOINT=http://localstack:4566 \
    -e MOTO_ENDPOINT=http://moto:5000 \
    -e AWS_ENDPOINT_URL=http://localstack:4566 \
    test-runner \
    -c "update-ca-certificates 2>/dev/null; bats --formatter pretty $VERBOSE $container_bats_files" || TOTAL_FAILED=$((TOTAL_FAILED + 1))

  echo ""
done

if [ $TOTAL_FAILED -gt 0 ]; then
  echo -e "${RED}Some integration tests failed${NC}"
  exit 1
else
  echo -e "${GREEN}All integration tests passed!${NC}"
fi