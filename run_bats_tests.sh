#!/bin/bash
# =============================================================================
# Test runner for all BATS tests across all modules
#
# Usage:
#   ./testing/run_bats_tests.sh                    # Run all tests
#   ./testing/run_bats_tests.sh frontend           # Run tests for frontend module only
#   ./testing/run_bats_tests.sh frontend/deployment/tests  # Run specific test directory
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Track failed tests globally
FAILED_TESTS=()
CURRENT_TEST_FILE=""

# Check if bats is installed
if ! command -v bats &> /dev/null; then
  echo -e "${RED}bats-core is not installed${NC}"
  echo ""
  echo "Install with:"
  echo "  brew install bats-core    # macOS"
  echo "  apt install bats          # Ubuntu/Debian"
  echo "  apk add bats              # Alpine"
  echo "  choco install bats        # Windows"
  exit 1
fi

# Check if jq is installed
if ! command -v jq &> /dev/null; then
  echo -e "${RED}jq is not installed${NC}"
  echo ""
  echo "Install with:"
  echo "  brew install jq           # macOS"
  echo "  apt install jq            # Ubuntu/Debian"
  echo "  apk add jq                # Alpine"
  echo "  choco install jq          # Windows"
  exit 1
fi

# Find all test directories
find_test_dirs() {
  find . -mindepth 3 -maxdepth 3 -type d -name "tests" -not -path "*/node_modules/*" 2>/dev/null | sort
}

# Get module name from test path
get_module_name() {
  local path="$1"
  echo "$path" | sed 's|^\./||' | cut -d'/' -f1
}

# Run tests for a specific directory
run_tests_in_dir() {
  local test_dir="$1"
  local module_name
  module_name=$(get_module_name "$test_dir")

  # Find all .bats files, excluding integration directory (integration tests are run separately)
  local bats_files
  bats_files=$(find "$test_dir" -name "*.bats" -not -path "*/integration/*" 2>/dev/null)

  if [ -z "$bats_files" ]; then
    return 0
  fi

  echo -e "${CYAN}[$module_name]${NC} Running BATS tests in $test_dir"
  echo ""

  # Create temp file to capture output
  local temp_output
  temp_output=$(mktemp)

  local exit_code=0
  (
    cd "$test_dir"
    # Use script to force TTY for colored output
    # Exclude integration directory - those tests are run by run_integration_tests.sh
    # --print-output-on-failure: only show test output when a test fails
    script -q /dev/null bats --formatter pretty --print-output-on-failure $(find . -name "*.bats" -not -path "*/integration/*" | sort)
  ) 2>&1 | tee "$temp_output" || exit_code=$?

  # Extract failed tests from output
  # Strip all ANSI escape codes (colors, cursor movements, etc.)
  local clean_output
  clean_output=$(perl -pe 's/\e\[[0-9;]*[a-zA-Z]//g; s/\e\][^\a]*\a//g' "$temp_output" 2>/dev/null || cat "$temp_output")

  local current_file=""
  while IFS= read -r line; do
    # Track current test file (lines containing .bats without test markers)
    if [[ "$line" == *".bats"* ]] && [[ "$line" != *"✗"* ]] && [[ "$line" != *"✓"* ]]; then
      # Extract the file path (e.g., network/route53/setup_test.bats)
      current_file=$(echo "$line" | grep -oE '[a-zA-Z0-9_/.-]+\.bats' | head -1)
    fi

    # Find failed test lines
    if [[ "$line" == *"✗"* ]]; then
      # Extract test name: get text after ✗, clean up any remaining control chars
      local failed_test_name
      failed_test_name=$(echo "$line" | sed 's/.*✗[[:space:]]*//' | sed 's/[[:space:]]*$//' | tr -d '\r')
      # Only add if we got a valid test name
      if [[ -n "$failed_test_name" ]]; then
        FAILED_TESTS+=("${module_name}|${current_file}|${failed_test_name}")
      fi
    fi
  done <<< "$clean_output"

  rm -f "$temp_output"
  echo ""

  return $exit_code
}

echo ""
echo "========================================"
echo "  BATS Tests (Unit)"
echo "========================================"
echo ""

# Print available test helpers reference
source "$SCRIPT_DIR/assertions.sh"
test_help
echo ""

# Export BASH_ENV to auto-source assertions.sh in all bats test subshells
export BASH_ENV="$SCRIPT_DIR/assertions.sh"

HAS_FAILURES=0

if [ -n "$1" ]; then
  # Run tests for specific module or directory
  if [ -d "$1" ] && [[ "$1" == *"/tests"* || "$1" == *"/tests" ]]; then
    # Direct test directory path
    run_tests_in_dir "$1" || HAS_FAILURES=1
  elif [ -d "$1" ]; then
    # Module name (e.g., "frontend") - find all test directories under it
    module_test_dirs=$(find "$1" -mindepth 2 -maxdepth 2 -type d -name "tests" 2>/dev/null | sort)
    if [ -z "$module_test_dirs" ]; then
      echo -e "${RED}No test directories found in: $1${NC}"
      exit 1
    fi
    for test_dir in $module_test_dirs; do
      run_tests_in_dir "$test_dir" || HAS_FAILURES=1
    done
  else
    echo -e "${RED}Directory not found: $1${NC}"
    echo ""
    echo "Available modules with tests:"
    for dir in $(find_test_dirs); do
      echo "  - $(get_module_name "$dir")"
    done | sort -u
    exit 1
  fi
else
  # Run all tests
  test_dirs=$(find_test_dirs)

  if [ -z "$test_dirs" ]; then
    echo -e "${YELLOW}No test directories found${NC}"
    exit 0
  fi

  for test_dir in $test_dirs; do
    run_tests_in_dir "$test_dir" || HAS_FAILURES=1
  done
fi

# Show summary of failed tests
if [ ${#FAILED_TESTS[@]} -gt 0 ]; then
  echo ""
  echo "========================================"
  echo "  Failed Tests Summary"
  echo "========================================"
  echo ""
  for failed_test in "${FAILED_TESTS[@]}"; do
    # Parse module|file|test_name format
    module_name=$(echo "$failed_test" | cut -d'|' -f1)
    file_name=$(echo "$failed_test" | cut -d'|' -f2)
    test_name=$(echo "$failed_test" | cut -d'|' -f3)
    echo -e "  ${RED}✗${NC} ${CYAN}[$module_name]${NC} ${RED}$file_name${NC} $test_name"
  done
  echo ""
  exit 1
fi

echo -e "${GREEN}All BATS tests passed!${NC}"