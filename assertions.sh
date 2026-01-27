# =============================================================================
# Shared assertion functions for BATS tests
#
# Usage: Add this line at the top of your .bats file's setup() function:
#   source "$PROJECT_ROOT/testing/assertions.sh"
# =============================================================================

# =============================================================================
# Assertion functions
# =============================================================================
assert_equal() {
  local actual="$1"
  local expected="$2"
  if [ "$actual" != "$expected" ]; then
    echo "Expected: '$expected'"
    echo "Actual:   '$actual'"
    return 1
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "Expected string to contain: '$needle'"
    echo "Actual: '$haystack'"
    return 1
  fi
}

assert_not_empty() {
  local value="$1"
  local name="${2:-value}"
  if [ -z "$value" ]; then
    echo "Expected $name to be non-empty, but it was empty"
    return 1
  fi
}

assert_empty() {
  local value="$1"
  local name="${2:-value}"
  if [ -n "$value" ]; then
    echo "Expected $name to be empty"
    echo "Actual: '$value'"
    return 1
  fi
}

assert_true() {
  local value="$1"
  local name="${2:-value}"
  if [[ "$value" != "true" ]]; then
    echo "Expected $name to be true"
    echo "Actual: '$value'"
    return 1
  fi
}

assert_false() {
  local value="$1"
  local name="${2:-value}"
  if [[ "$value" != "false" ]]; then
    echo "Expected $name to be false"
    echo "Actual: '$value'"
    return 1
  fi
}

assert_greater_than() {
  local actual="$1"
  local expected="$2"
  local name="${3:-value}"
  if [[ ! "$actual" -gt "$expected" ]]; then
    echo "Expected $name to be greater than $expected"
    echo "Actual: '$actual'"
    return 1
  fi
}

assert_less_than() {
  local actual="$1"
  local expected="$2"
  local name="${3:-value}"
  if [[ ! "$actual" -lt "$expected" ]]; then
    echo "Expected $name to be less than $expected"
    echo "Actual: '$actual'"
    return 1
  fi
}

# Assert that commands appear in a specific order in a log file
# Usage: assert_command_order "<log_file>" "command1" "command2" ["command3" ...]
# Example: assert_command_order "$LOG_FILE" "init" "apply"
assert_command_order() {
  local log_file="$1"
  shift
  local commands=("$@")

  if [[ ${#commands[@]} -lt 2 ]]; then
    echo "assert_command_order requires at least 2 commands"
    return 1
  fi

  if [[ ! -f "$log_file" ]]; then
    echo "Log file not found: $log_file"
    return 1
  fi

  local prev_line=0
  local prev_cmd=""

  for cmd in "${commands[@]}"; do
    local line_num
    line_num=$(grep -n "$cmd" "$log_file" | head -1 | cut -d: -f1)

    if [[ -z "$line_num" ]]; then
      echo "Command '$cmd' not found in log file"
      return 1
    fi

    if [[ $prev_line -gt 0 ]] && [[ $line_num -le $prev_line ]]; then
      echo "Expected: '$cmd'"
      echo "To be executed after: '$prev_cmd'"

      echo "Actual execution order:"
      echo "  '$prev_cmd' at line $prev_line"
      echo "  '$cmd' at line $line_num"
      return 1
    fi

    prev_line=$line_num
    prev_cmd=$cmd
  done
}

assert_directory_exists() {
  local dir="$1"
  if [ ! -d "$dir" ]; then
    echo "Expected directory to exist: '$dir'"
    return 1
  fi
}

assert_file_exists() {
  local file="$1"
  if [ ! -f "$file" ]; then
    echo "Expected file to exist: '$file'"
    return 1
  fi
}

assert_file_not_exists() {
  local file="$1"
  if [ -f "$file" ]; then
    echo "Expected file to not exist: '$file'"
    return 1
  fi
}

assert_json_equal() {
  local actual="$1"
  local expected="$2"
  local name="${3:-JSON}"

  local actual_sorted=$(echo "$actual" | jq -S .)
  local expected_sorted=$(echo "$expected" | jq -S .)

  if [ "$actual_sorted" != "$expected_sorted" ]; then
    echo "$name does not match expected structure"
    echo ""
    echo "Diff:"
    diff <(echo "$expected_sorted") <(echo "$actual_sorted") || true
    echo ""
    echo "Expected:"
    echo "$expected_sorted"
    echo ""
    echo "Actual:"
    echo "$actual_sorted"
    echo ""
    return 1
  fi
}

# =============================================================================
# Mock helpers
# =============================================================================

# Set up a mock response for the np CLI
# Usage: set_np_mock "<mock_file>" [exit_code]
set_np_mock() {
  local mock_file="$1"
  local exit_code="${2:-0}"
  export NP_MOCK_RESPONSE="$mock_file"
  export NP_MOCK_EXIT_CODE="$exit_code"
}


# Set up a mock response for the aws CLI
# Usage: set_aws_mock "<mock_file>" [exit_code]
# Requires: AWS_MOCKS_DIR to be set in the test setup
set_aws_mock() {
  local mock_file="$1"
  local exit_code="${2:-0}"
  export AWS_MOCK_RESPONSE="$mock_file"
  export AWS_MOCK_EXIT_CODE="$exit_code"
}

# Set up a mock response for the az CLI
# Usage: set_az_mock "<mock_file>" [exit_code]
# Requires: AZURE_MOCKS_DIR to be set in the test setup
set_az_mock() {
  local mock_file="$1"
  local exit_code="${2:-0}"
  export AZ_MOCK_RESPONSE="$mock_file"
  export AZ_MOCK_EXIT_CODE="$exit_code"
}

# =============================================================================
# Help / Documentation
# =============================================================================

# Display help for all available unit test assertion utilities
test_help() {
  cat <<'EOF'
================================================================================
                      Unit Test Assertions Reference
================================================================================

VALUE ASSERTIONS
----------------
  assert_equal "<actual>" "<expected>"
      Assert two string values are equal.
      Example: assert_equal "$result" "expected_value"

  assert_contains "<haystack>" "<needle>"
      Assert a string contains a substring.
      Example: assert_contains "$output" "success"

  assert_not_empty "<value>" ["<name>"]
      Assert a value is not empty.
      Example: assert_not_empty "$result" "API response"

  assert_empty "<value>" ["<name>"]
      Assert a value is empty.
      Example: assert_empty "$error" "error message"

  assert_true "<value>" ["<name>"]
      Assert a value equals the string "true".
      Example: assert_true "$enabled" "distribution enabled"

  assert_false "<value>" ["<name>"]
      Assert a value equals the string "false".
      Example: assert_false "$disabled" "feature disabled"

NUMERIC ASSERTIONS
------------------
  assert_greater_than "<actual>" "<expected>" ["<name>"]
      Assert a number is greater than another.
      Example: assert_greater_than "$count" "0" "item count"

  assert_less_than "<actual>" "<expected>" ["<name>"]
      Assert a number is less than another.
      Example: assert_less_than "$errors" "10" "error count"

COMMAND ORDER ASSERTIONS
------------------------
  assert_command_order "<log_file>" "cmd1" "cmd2" ["cmd3" ...]
      Assert commands appear in order in a log file.
      Example: assert_command_order "$LOG" "init" "apply" "output"

FILE SYSTEM ASSERTIONS
----------------------
  assert_file_exists "<path>"
      Assert a file exists.
      Example: assert_file_exists "/tmp/output.json"

  assert_file_not_exists "<path>"
      Assert a file does not exist.
      Example: assert_file_not_exists "/tmp/should_not_exist.json"

  assert_directory_exists "<path>"
      Assert a directory exists.
      Example: assert_directory_exists "/tmp/output"

JSON ASSERTIONS
---------------
  assert_json_equal "<actual>" "<expected>" ["<name>"]
      Assert two JSON structures are equal (order-independent).
      Example: assert_json_equal "$response" '{"status": "ok"}'

MOCK HELPERS
------------
  set_np_mock "<mock_file>" [exit_code]
      Set up a mock response for the np CLI.
      Example: set_np_mock "$MOCKS_DIR/provider/success.json"

  set_aws_mock "<mock_file>" [exit_code]
      Set up a mock response for the aws CLI.
      Example: set_aws_mock "$MOCKS_DIR/route53/success.json"

BATS BUILT-IN HELPERS
---------------------
  run <command>
      Run a command and capture output in $output and exit code in $status.
      Example: run my_function "arg1" "arg2"

  [ "$status" -eq 0 ]
      Check exit code after 'run'.

  [[ "$output" == *"expected"* ]]
      Check output contains expected string.

USAGE IN TESTS
--------------
  Add this to your test file's setup() function:

    setup() {
      source "$PROJECT_ROOT/testing/assertions.sh"
    }

================================================================================
EOF
}