#!/bin/bash
# =============================================================================
# Test runner for all OpenTofu/Terraform tests across all modules
#
# Usage:
#   ./testing/run_tofu_tests.sh                    # Run all tests
#   ./testing/run_tofu_tests.sh frontend           # Run tests for frontend module only
#   ./testing/run_tofu_tests.sh frontend/deployment/provider/aws/modules  # Run specific test directory
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

# Check if tofu is installed
if ! command -v tofu &> /dev/null; then
  echo -e "${RED}OpenTofu is not installed${NC}"
  echo ""
  echo "Install with:"
  echo "  brew install opentofu     # macOS"
  echo "  apt install tofu          # Ubuntu/Debian"
  echo "  apk add opentofu          # Alpine"
  echo "  choco install opentofu    # Windows"
  echo ""
  echo "See https://opentofu.org/docs/intro/install/"
  exit 1
fi

# Find all directories with .tftest.hcl files
find_test_dirs() {
  find . -name "*.tftest.hcl" -not -path "*/node_modules/*" 2>/dev/null | xargs -I{} dirname {} | sort -u
}

# Get module name from test path
get_module_name() {
  local path="$1"
  echo "$path" | sed 's|^\./||' | cut -d'/' -f1
}

# Run tests for a specific directory
run_tests_in_dir() {
  local test_dir="$1"
  local module_name=$(get_module_name "$test_dir")

  # Check if there are .tftest.hcl files
  if ! ls "$test_dir"/*.tftest.hcl &>/dev/null; then
    return 0
  fi

  echo -e "${CYAN}[$module_name]${NC} Running OpenTofu tests in $test_dir"
  echo ""

  (
    cd "$test_dir"

    # Initialize if needed (without backend)
    if [ ! -d ".terraform" ]; then
      tofu init -backend=false -input=false >/dev/null 2>&1 || true
    fi

    # Run tests
    tofu test
  )

  echo ""
}

echo ""
echo "========================================"
echo "  OpenTofu Tests"
echo "========================================"
echo ""

if [ -n "$1" ]; then
  # Run tests for specific module or directory
  if [ -d "$1" ] && ls "$1"/*.tftest.hcl &>/dev/null; then
    # Direct test directory path with .tftest.hcl files
    run_tests_in_dir "$1"
  elif [ -d "$1" ]; then
    # Module name (e.g., "frontend") - find all test directories under it
    module_test_dirs=$(find "$1" -name "*.tftest.hcl" 2>/dev/null | xargs -I{} dirname {} | sort -u)
    if [ -z "$module_test_dirs" ]; then
      echo -e "${RED}No OpenTofu test files found in: $1${NC}"
      exit 1
    fi
    for test_dir in $module_test_dirs; do
      run_tests_in_dir "$test_dir"
    done
  else
    echo -e "${RED}Directory not found: $1${NC}"
    echo ""
    echo "Available modules with OpenTofu tests:"
    for dir in $(find_test_dirs); do
      echo "  - $(get_module_name "$dir")"
    done | sort -u
    exit 1
  fi
else
  # Run all tests
  test_dirs=$(find_test_dirs)

  if [ -z "$test_dirs" ]; then
    echo -e "${YELLOW}No OpenTofu test files found${NC}"
    exit 0
  fi

  for test_dir in $test_dirs; do
    run_tests_in_dir "$test_dir"
  done
fi

echo -e "${GREEN}All OpenTofu tests passed!${NC}"
