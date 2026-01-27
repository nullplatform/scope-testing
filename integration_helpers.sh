#!/bin/bash
# =============================================================================
# Integration Test Helpers for BATS
#
# Provides helper functions for integration testing with cloud provider support.
#
# Usage in BATS test files:
#   setup_file() {
#     load "${PROJECT_ROOT}/testing/integration_helpers.sh"
#     integration_setup --cloud-provider aws
#   }
#
#   teardown_file() {
#     integration_teardown
#   }
#
# Supported cloud providers: aws, azure, gcp
# =============================================================================

# =============================================================================
# Colors
# =============================================================================
INTEGRATION_RED='\033[0;31m'
INTEGRATION_GREEN='\033[0;32m'
INTEGRATION_YELLOW='\033[1;33m'
INTEGRATION_CYAN='\033[0;36m'
INTEGRATION_NC='\033[0m'

# =============================================================================
# Global State
# =============================================================================
INTEGRATION_CLOUD_PROVIDER="${INTEGRATION_CLOUD_PROVIDER:-}"
INTEGRATION_COMPOSE_FILE="${INTEGRATION_COMPOSE_FILE:-}"

# Determine module root from PROJECT_ROOT environment variable
# PROJECT_ROOT is set by the test runner (run_integration_tests.sh)
if [[ -z "${INTEGRATION_MODULE_ROOT:-}" ]]; then
  INTEGRATION_MODULE_ROOT="${PROJECT_ROOT:-.}"
fi
export INTEGRATION_MODULE_ROOT

# Default AWS/LocalStack configuration (can be overridden)
export LOCALSTACK_ENDPOINT="${LOCALSTACK_ENDPOINT:-http://localhost:4566}"
export MOTO_ENDPOINT="${MOTO_ENDPOINT:-http://localhost:5555}"
export AWS_ENDPOINT_URL="${AWS_ENDPOINT_URL:-$LOCALSTACK_ENDPOINT}"
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-us-east-1}"
export AWS_PAGER=""

# Default Azure Mock configuration (can be overridden)
export AZURE_MOCK_ENDPOINT="${AZURE_MOCK_ENDPOINT:-http://localhost:8090}"
export ARM_CLIENT_ID="${ARM_CLIENT_ID:-mock-client-id}"
export ARM_CLIENT_SECRET="${ARM_CLIENT_SECRET:-mock-client-secret}"
export ARM_TENANT_ID="${ARM_TENANT_ID:-mock-tenant-id}"
export ARM_SUBSCRIPTION_ID="${ARM_SUBSCRIPTION_ID:-mock-subscription-id}"
export ARM_SKIP_PROVIDER_REGISTRATION="${ARM_SKIP_PROVIDER_REGISTRATION:-true}"

# Smocker configuration for API mocking
export SMOCKER_HOST="${SMOCKER_HOST:-http://localhost:8081}"

# =============================================================================
# Setup & Teardown
# =============================================================================

integration_setup() {
  local cloud_provider=""

  # Parse arguments
  while [[ $# -gt 0 ]]; do
    case $1 in
    --cloud-provider)
      cloud_provider="$2"
      shift 2
      ;;
    *)
      echo -e "${INTEGRATION_RED}Unknown argument: $1${INTEGRATION_NC}"
      return 1
      ;;
    esac
  done

  # Validate cloud provider
  if [[ -z "$cloud_provider" ]]; then
    echo -e "${INTEGRATION_RED}Error: --cloud-provider is required${INTEGRATION_NC}"
    echo "Usage: integration_setup --cloud-provider <aws|azure|gcp>"
    return 1
  fi

  case "$cloud_provider" in
  aws|azure|gcp)
    INTEGRATION_CLOUD_PROVIDER="$cloud_provider"
    ;;
  *)
    echo -e "${INTEGRATION_RED}Error: Unsupported cloud provider: $cloud_provider${INTEGRATION_NC}"
    echo "Supported providers: aws, azure, gcp"
    return 1
    ;;
  esac

  export INTEGRATION_CLOUD_PROVIDER

  # Find docker-compose.yml
  INTEGRATION_COMPOSE_FILE=$(find_compose_file)
  export INTEGRATION_COMPOSE_FILE

  echo -e "${INTEGRATION_CYAN}Integration Setup${INTEGRATION_NC}"
  echo "  Cloud Provider: $INTEGRATION_CLOUD_PROVIDER"
  echo "  Module Root: $INTEGRATION_MODULE_ROOT"
  echo ""

  # Call provider-specific setup
  case "$INTEGRATION_CLOUD_PROVIDER" in
  aws)
    _setup_aws
    ;;
  azure)
    _setup_azure
    ;;
  gcp)
    _setup_gcp
    ;;
  esac
}

integration_teardown() {
  echo ""
  echo -e "${INTEGRATION_CYAN}Integration Teardown${INTEGRATION_NC}"

  # Call provider-specific teardown
  case "$INTEGRATION_CLOUD_PROVIDER" in
  aws)
    _teardown_aws
    ;;
  azure)
    _teardown_azure
    ;;
  gcp)
    _teardown_gcp
    ;;
  esac
}

# =============================================================================
# AWS Provider (LocalStack + Moto)
# =============================================================================

_setup_aws() {
  echo "  LocalStack: $LOCALSTACK_ENDPOINT"
  echo "  Moto: $MOTO_ENDPOINT"
  echo ""

  # Configure OpenTofu/Terraform S3 backend for LocalStack
  # These settings allow the S3 backend to work with LocalStack's S3 emulation
  export TOFU_INIT_VARIABLES="${TOFU_INIT_VARIABLES:-}"
  TOFU_INIT_VARIABLES="$TOFU_INIT_VARIABLES -backend-config=force_path_style=true"
  TOFU_INIT_VARIABLES="$TOFU_INIT_VARIABLES -backend-config=skip_credentials_validation=true"
  TOFU_INIT_VARIABLES="$TOFU_INIT_VARIABLES -backend-config=skip_metadata_api_check=true"
  TOFU_INIT_VARIABLES="$TOFU_INIT_VARIABLES -backend-config=skip_region_validation=true"
  TOFU_INIT_VARIABLES="$TOFU_INIT_VARIABLES -backend-config=endpoints={s3=\"$LOCALSTACK_ENDPOINT\",dynamodb=\"$LOCALSTACK_ENDPOINT\"}"
  export TOFU_INIT_VARIABLES

  # Start containers if compose file exists
  if [[ -n "$INTEGRATION_COMPOSE_FILE" ]]; then
    _start_localstack
  else
    echo -e "${INTEGRATION_YELLOW}Warning: No docker-compose.yml found, skipping container startup${INTEGRATION_NC}"
  fi
}

_teardown_aws() {
  if [[ -n "$INTEGRATION_COMPOSE_FILE" ]]; then
    _stop_localstack
  fi
}

_start_localstack() {
  echo -e "  Starting LocalStack..."
  docker compose -f "$INTEGRATION_COMPOSE_FILE" up -d 2>/dev/null

  echo -n "  Waiting for LocalStack to be ready"
  local max_attempts=30
  local attempt=0

  while [[ $attempt -lt $max_attempts ]]; do
    if curl -s "$LOCALSTACK_ENDPOINT/_localstack/health" 2>/dev/null | jq -e '.services.s3 == "running"' > /dev/null 2>&1; then
      echo ""
      echo -e "  ${INTEGRATION_GREEN}LocalStack is ready${INTEGRATION_NC}"
      echo ""
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 2
    echo -n "."
  done

  echo ""
  echo -e "  ${INTEGRATION_RED}LocalStack failed to start${INTEGRATION_NC}"
  return 1
}

_stop_localstack() {
  echo "  Stopping LocalStack..."
  docker compose -f "$INTEGRATION_COMPOSE_FILE" down -v 2>/dev/null || true
}

# =============================================================================
# Azure Provider (Azure Mock)
# =============================================================================

_setup_azure() {
  echo "  Azure Mock: $AZURE_MOCK_ENDPOINT"
  echo ""

  # Azure tests use:
  # - Azure Mock for ARM APIs (CDN, DNS, etc.) AND Blob Storage (terraform state)
  # - nginx proxy to redirect *.blob.core.windows.net to Azure Mock

  # Install the self-signed certificate for nginx proxy
  # This allows the Azure SDK to trust the proxy for blob storage
  if [[ -f /usr/local/share/ca-certificates/smocker.crt ]]; then
    echo -n "  Installing TLS certificate..."
    update-ca-certificates >/dev/null 2>&1 || true
    # Also set for Python/requests (used by Azure CLI)
    export REQUESTS_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt
    export CURL_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt
    echo -e " ${INTEGRATION_GREEN}done${INTEGRATION_NC}"
  fi

  # Start containers if compose file exists
  if [[ -n "$INTEGRATION_COMPOSE_FILE" ]]; then
    _start_azure_mock
  else
    echo -e "${INTEGRATION_YELLOW}Warning: No docker-compose.yml found, skipping container startup${INTEGRATION_NC}"
  fi

  # Configure Azure CLI to work with mock
  _configure_azure_cli
}

_teardown_azure() {
  if [[ -n "$INTEGRATION_COMPOSE_FILE" ]]; then
    _stop_azure_mock
  fi
}

_start_azure_mock() {
  echo -e "  Starting Azure Mock..."
  docker compose -f "$INTEGRATION_COMPOSE_FILE" up -d azure-mock nginx-proxy smocker 2>/dev/null

  # Wait for Azure Mock
  echo -n "  Waiting for Azure Mock to be ready"
  local max_attempts=30
  local attempt=0

  while [[ $attempt -lt $max_attempts ]]; do
    if curl -s "$AZURE_MOCK_ENDPOINT/health" 2>/dev/null | jq -e '.status == "ok"' > /dev/null 2>&1; then
      echo ""
      echo -e "  ${INTEGRATION_GREEN}Azure Mock is ready${INTEGRATION_NC}"
      break
    fi
    attempt=$((attempt + 1))
    sleep 2
    echo -n "."
  done

  if [[ $attempt -ge $max_attempts ]]; then
    echo ""
    echo -e "  ${INTEGRATION_RED}Azure Mock failed to start${INTEGRATION_NC}"
    return 1
  fi

  # Create tfstate container in Azure Mock (required by azurerm backend)
  # The account name comes from Host header, path is just /{container}
  echo -n "  Creating tfstate container..."
  curl -s -X PUT "${AZURE_MOCK_ENDPOINT}/tfstate?restype=container" \
    -H "Host: devstoreaccount1.blob.core.windows.net" \
    -H "x-ms-version: 2021-06-08" >/dev/null 2>&1
  echo -e " ${INTEGRATION_GREEN}done${INTEGRATION_NC}"

  # Wait for nginx proxy to be ready (handles blob storage redirect)
  echo -n "  Waiting for nginx proxy to be ready"
  attempt=0

  while [[ $attempt -lt $max_attempts ]]; do
    if curl -sk "https://localhost:443/mocks" >/dev/null 2>&1; then
      echo ""
      echo -e "  ${INTEGRATION_GREEN}nginx proxy is ready${INTEGRATION_NC}"
      break
    fi
    attempt=$((attempt + 1))
    sleep 2
    echo -n "."
  done

  if [[ $attempt -ge $max_attempts ]]; then
    echo ""
    echo -e "  ${INTEGRATION_YELLOW}Warning: nginx proxy health check failed, continuing anyway${INTEGRATION_NC}"
  fi

  echo ""
  return 0
}

_stop_azure_mock() {
  echo "  Stopping Azure Mock..."
  docker compose -f "$INTEGRATION_COMPOSE_FILE" down -v 2>/dev/null || true
}

_configure_azure_cli() {
  # Check if Azure CLI is available
  if ! command -v az &>/dev/null; then
    echo -e "  ${INTEGRATION_YELLOW}Warning: Azure CLI not installed, skipping configuration${INTEGRATION_NC}"
    return 0
  fi

  echo ""
  echo -e "  ${INTEGRATION_CYAN}Configuring Azure CLI...${INTEGRATION_NC}"

  local azure_dir="$HOME/.azure"
  mkdir -p "$azure_dir"

  # Generate timestamps for token
  local now=$(date +%s)
  local exp=$((now + 86400))  # 24 hours from now

  # Create the azureProfile.json (subscription info)
  cat > "$azure_dir/azureProfile.json" << EOF
{
  "installationId": "mock-installation-id",
  "subscriptions": [
    {
      "id": "${ARM_SUBSCRIPTION_ID}",
      "name": "Mock Subscription",
      "state": "Enabled",
      "user": {
        "name": "${ARM_CLIENT_ID}",
        "type": "servicePrincipal"
      },
      "isDefault": true,
      "tenantId": "${ARM_TENANT_ID}",
      "environmentName": "AzureCloud"
    }
  ]
}
EOF

  # Create the service principal secret storage file
  # This is where Azure CLI stores secrets for service principals after login
  # Format must match what Azure CLI identity.py expects (uses 'tenant' not 'tenant_id')
  cat > "$azure_dir/service_principal_entries.json" << EOF
[
  {
    "client_id": "${ARM_CLIENT_ID}",
    "tenant": "${ARM_TENANT_ID}",
    "client_secret": "${ARM_CLIENT_SECRET}"
  }
]
EOF

  # Set proper permissions
  chmod 600 "$azure_dir"/*.json

  echo -e "    ${INTEGRATION_GREEN}Azure CLI configured with mock credentials${INTEGRATION_NC}"
  return 0
}

# =============================================================================
# GCP Provider (Fake GCS Server) - Placeholder
# =============================================================================

_setup_gcp() {
  echo -e "${INTEGRATION_YELLOW}GCP provider setup not yet implemented${INTEGRATION_NC}"
  echo "  Fake GCS Server endpoint would be configured here"
  echo ""
}

_teardown_gcp() {
  echo -e "${INTEGRATION_YELLOW}GCP provider teardown not yet implemented${INTEGRATION_NC}"
}

# =============================================================================
# Utility Functions
# =============================================================================

find_compose_file() {
  local search_paths=(
    "${BATS_TEST_DIRNAME:-}/docker-compose.yml"
    "${BATS_TEST_DIRNAME:-}/../docker-compose.yml"
    "${INTEGRATION_MODULE_ROOT}/tests/integration/docker-compose.yml"
  )

  for path in "${search_paths[@]}"; do
    if [[ -f "$path" ]]; then
      echo "$path"
      return 0
    fi
  done

  # Return success with empty output - compose file is optional
  # (containers may already be managed by the test runner)
  return 0
}

# =============================================================================
# AWS Local Commands
# =============================================================================

# Execute AWS CLI against LocalStack
aws_local() {
  aws --endpoint-url="$LOCALSTACK_ENDPOINT" --no-cli-pager --no-cli-auto-prompt "$@"
}

# Execute AWS CLI against Moto (for CloudFront)
aws_moto() {
  aws --endpoint-url="$MOTO_ENDPOINT" --no-cli-pager --no-cli-auto-prompt "$@"
}

# =============================================================================
# Azure Mock Commands
# =============================================================================

# Execute a GET request against Azure Mock API
# Usage: azure_mock "/subscriptions/sub-id/resourceGroups/rg/providers/Microsoft.Cdn/profiles/profile-name"
azure_mock() {
  local path="$1"
  curl -s "${AZURE_MOCK_ENDPOINT}${path}" 2>/dev/null
}

# Execute a PUT request against Azure Mock API
# Usage: azure_mock_put "/path" '{"json": "body"}'
azure_mock_put() {
  local path="$1"
  local body="$2"
  curl -s -X PUT "${AZURE_MOCK_ENDPOINT}${path}" \
    -H "Content-Type: application/json" \
    -d "$body" 2>/dev/null
}

# Execute a DELETE request against Azure Mock API
# Usage: azure_mock_delete "/path"
azure_mock_delete() {
  local path="$1"
  curl -s -X DELETE "${AZURE_MOCK_ENDPOINT}${path}" 2>/dev/null
}

# =============================================================================
# Workflow Execution
# =============================================================================

# Run a nullplatform workflow
# Usage: run_workflow "deployment/workflows/initial.yaml"
run_workflow() {
  local workflow="$1"
  local full_path

  # Resolve path relative to module root
  if [[ "$workflow" = /* ]]; then
    full_path="$workflow"
  else
    full_path="$INTEGRATION_MODULE_ROOT/$workflow"
  fi

  echo -e "${INTEGRATION_CYAN}Running workflow:${INTEGRATION_NC} $workflow"
  np service workflow exec --workflow "$full_path"
}

# =============================================================================
# Context Helpers
# =============================================================================

# Load context from a JSON file
# Usage: load_context "resources/context.json"
load_context() {
  local context_file="$1"
  local full_path

  # Resolve path relative to module root
  if [[ "$context_file" = /* ]]; then
    full_path="$context_file"
  else
    full_path="$INTEGRATION_MODULE_ROOT/$context_file"
  fi

  if [[ ! -f "$full_path" ]]; then
    echo -e "${INTEGRATION_RED}Context file not found: $full_path${INTEGRATION_NC}"
    return 1
  fi

  export CONTEXT=$(cat "$full_path")
  echo -e "  ${INTEGRATION_CYAN}Loaded context from:${INTEGRATION_NC} $context_file"
}

# Override a value in the current CONTEXT
# Usage: override_context "providers.networking.zone_id" "Z1234567890"
override_context() {
  local key="$1"
  local value="$2"

  if [[ -z "$CONTEXT" ]]; then
    echo -e "${INTEGRATION_RED}Error: CONTEXT is not set. Call load_context first.${INTEGRATION_NC}"
    return 1
  fi

  CONTEXT=$(echo "$CONTEXT" | jq --arg k "$key" --arg v "$value" 'setpath($k | split("."); $v)')
  export CONTEXT
}

# =============================================================================
# Generic Assertions
# =============================================================================

# Assert command succeeds
# Usage: assert_success "aws s3 ls"
assert_success() {
  local cmd="$1"
  local description="${2:-Command succeeds}"
  echo -ne "  ${INTEGRATION_CYAN}Assert:${INTEGRATION_NC} ${description} ... "

  if eval "$cmd" >/dev/null 2>&1; then
    _assert_result "true"
  else
    _assert_result "false"
    return 1
  fi
}

# Assert command fails
# Usage: assert_failure "aws s3api head-bucket --bucket nonexistent"
assert_failure() {
  local cmd="$1"
  local description="${2:-Command fails}"
  echo -ne "  ${INTEGRATION_CYAN}Assert:${INTEGRATION_NC} ${description} ... "

  if eval "$cmd" >/dev/null 2>&1; then
    _assert_result "false"
    return 1
  else
    _assert_result "true"
  fi
}

# Assert output contains string
# Usage: result=$(some_command); assert_contains "$result" "expected"
assert_contains() {
  local haystack="$1"
  local needle="$2"
  local description="${3:-Output contains '$needle'}"
  echo -ne "  ${INTEGRATION_CYAN}Assert:${INTEGRATION_NC} ${description} ... "

  if [[ "$haystack" == *"$needle"* ]]; then
    _assert_result "true"
  else
    _assert_result "false"
    return 1
  fi
}

# Assert values are equal
# Usage: assert_equals "$actual" "$expected" "Values match"
assert_equals() {
  local actual="$1"
  local expected="$2"
  local description="${3:-Values are equal}"
  echo -ne "  ${INTEGRATION_CYAN}Assert:${INTEGRATION_NC} ${description} ... "

  if [[ "$actual" == "$expected" ]]; then
    _assert_result "true"
  else
    _assert_result "false"
    echo "    Expected: $expected"
    echo "    Actual:   $actual"
    return 1
  fi
}

# =============================================================================
# API Mocking (Smocker)
#
# Smocker is used to mock the nullplatform API (api.nullplatform.com).
# Tests run in a container where api.nullplatform.com resolves to smocker.
# =============================================================================

# Clear all mocks from smocker and set up default mocks
# Usage: clear_mocks
clear_mocks() {
  curl -s -X POST "${SMOCKER_HOST}/reset" >/dev/null 2>&1
  # Set up default mocks that are always needed
  _setup_default_mocks
}

# Set up default mocks that are always needed for np CLI
# These are internal API calls that np CLI makes automatically
_setup_default_mocks() {
  # Token endpoint - np CLI always authenticates before making API calls
  local token_mock
  token_mock=$(cat <<'EOF'
[{
  "request": {
    "method": "POST",
    "path": "/token"
  },
  "response": {
    "status": 200,
    "headers": {"Content-Type": "application/json"},
    "body": "{\"access_token\": \"test-integration-token\", \"token_type\": \"Bearer\", \"expires_in\": 3600}"
  }
}]
EOF
  )
  curl -s -X POST "${SMOCKER_HOST}/mocks" \
    -H "Content-Type: application/json" \
    -d "$token_mock" >/dev/null 2>&1
}

# Mock an API request
# Usage with file:   mock_request "GET" "/providers/123" "responses/provider.json"
# Usage inline:      mock_request "POST" "/deployments" 201 '{"id": "new-dep"}'
#
# File format (JSON):
# {
#   "status": 200,
#   "headers": {"Content-Type": "application/json"},  // optional
#   "body": { ... }
# }
mock_request() {
  local method="$1"
  local path="$2"
  local status_or_file="$3"
  local body="$4"

  local status
  local response_body
  local headers='{"Content-Type": "application/json"}'

  # Check if third argument is a file or a status code
  if [[ -f "$status_or_file" ]]; then
    # File mode - read status and body from file
    local file_content
    file_content=$(cat "$status_or_file")
    status=$(echo "$file_content" | jq -r '.status // 200')
    response_body=$(echo "$file_content" | jq -c '.body // {}')
    local file_headers
    file_headers=$(echo "$file_content" | jq -c '.headers // null')
    if [[ "$file_headers" != "null" ]]; then
      headers="$file_headers"
    fi
  elif [[ -f "${INTEGRATION_MODULE_ROOT}/$status_or_file" ]]; then
    # File mode with relative path
    local file_content
    file_content=$(cat "${INTEGRATION_MODULE_ROOT}/$status_or_file")
    status=$(echo "$file_content" | jq -r '.status // 200')
    response_body=$(echo "$file_content" | jq -c '.body // {}')
    local file_headers
    file_headers=$(echo "$file_content" | jq -c '.headers // null')
    if [[ "$file_headers" != "null" ]]; then
      headers="$file_headers"
    fi
  else
    # Inline mode - status code and body provided directly
    status="$status_or_file"
    response_body="$body"
  fi

  # Build smocker mock definition
  # Note: Smocker expects body as a string, not a JSON object
  local mock_definition
  mock_definition=$(jq -n \
    --arg method "$method" \
    --arg path "$path" \
    --argjson status "$status" \
    --arg body "$response_body" \
    --argjson headers "$headers" \
    '[{
      "request": {
        "method": $method,
        "path": $path
      },
      "response": {
        "status": $status,
        "headers": $headers,
        "body": $body
      }
    }]')

  # Register mock with smocker
  local result
  local http_code
  http_code=$(curl -s -w "%{http_code}" -o /tmp/smocker_response.json -X POST "${SMOCKER_HOST}/mocks" \
    -H "Content-Type: application/json" \
    -d "$mock_definition" 2>&1)
  result=$(cat /tmp/smocker_response.json 2>/dev/null)

  if [[ "$http_code" != "200" ]]; then
    local error_msg
    error_msg=$(echo "$result" | jq -r '.message // "Unknown error"' 2>/dev/null)
    echo -e "${INTEGRATION_RED}Failed to register mock (HTTP ${http_code}): ${error_msg}${INTEGRATION_NC}"
    return 1
  fi

  echo -e "  ${INTEGRATION_CYAN}Mock:${INTEGRATION_NC} ${method} ${path} -> ${status}"
}

# Mock a request with query parameters
# Usage: mock_request_with_query "GET" "/providers" "type=assets-repository" 200 '[...]'
mock_request_with_query() {
  local method="$1"
  local path="$2"
  local query="$3"
  local status="$4"
  local body="$5"

  local mock_definition
  mock_definition=$(jq -n \
    --arg method "$method" \
    --arg path "$path" \
    --arg query "$query" \
    --argjson status "$status" \
    --arg body "$body" \
    '[{
      "request": {
        "method": $method,
        "path": $path,
        "query_params": ($query | split("&") | map(split("=") | {(.[0]): [.[1]]}) | add)
      },
      "response": {
        "status": $status,
        "headers": {"Content-Type": "application/json"},
        "body": $body
      }
    }]')

  curl -s -X POST "${SMOCKER_HOST}/mocks" \
    -H "Content-Type: application/json" \
    -d "$mock_definition" >/dev/null 2>&1

  echo -e "  ${INTEGRATION_CYAN}Mock:${INTEGRATION_NC} ${method} ${path}?${query} -> ${status}"
}

# Verify that a mock was called
# Usage: assert_mock_called "GET" "/providers/123"
assert_mock_called() {
  local method="$1"
  local path="$2"
  echo -ne "  ${INTEGRATION_CYAN}Assert:${INTEGRATION_NC} ${method} ${path} was called ... "

  local history
  history=$(curl -s "${SMOCKER_HOST}/history" 2>/dev/null)

  local called
  called=$(echo "$history" | jq -r \
    --arg method "$method" \
    --arg path "$path" \
    '[.[] | select(.request.method == $method and .request.path == $path)] | length')

  if [[ "$called" -gt 0 ]]; then
    _assert_result "true"
  else
    _assert_result "false"
    return 1
  fi
}

# Get the number of times a mock was called
# Usage: count=$(mock_call_count "GET" "/providers/123")
mock_call_count() {
  local method="$1"
  local path="$2"

  local history
  history=$(curl -s "${SMOCKER_HOST}/history" 2>/dev/null)

  echo "$history" | jq -r \
    --arg method "$method" \
    --arg path "$path" \
    '[.[] | select(.request.method == $method and .request.path == $path)] | length'
}

# =============================================================================
# Help / Documentation
# =============================================================================

# Display help for all available integration test utilities
test_help() {
  cat <<'EOF'
================================================================================
                    Integration Test Helpers Reference
================================================================================

SETUP & TEARDOWN
----------------
  integration_setup --cloud-provider <aws|azure|gcp>
      Initialize integration test environment for the specified cloud provider.
      Call this in setup_file().

  integration_teardown
      Clean up integration test environment.
      Call this in teardown_file().

AWS LOCAL COMMANDS
------------------
  aws_local <aws-cli-args>
      Execute AWS CLI against LocalStack (S3, Route53, DynamoDB, etc.)
      Example: aws_local s3 ls

  aws_moto <aws-cli-args>
      Execute AWS CLI against Moto (CloudFront)
      Example: aws_moto cloudfront list-distributions

AZURE MOCK COMMANDS
-------------------
  azure_mock "<path>"
      Execute a GET request against Azure Mock API.
      Example: azure_mock "/subscriptions/sub-id/resourceGroups/rg/providers/Microsoft.Cdn/profiles/my-profile"

  azure_mock_put "<path>" '<json-body>'
      Execute a PUT request against Azure Mock API.
      Example: azure_mock_put "/subscriptions/.../profiles/my-profile" '{"location": "eastus"}'

  azure_mock_delete "<path>"
      Execute a DELETE request against Azure Mock API.
      Example: azure_mock_delete "/subscriptions/.../profiles/my-profile"

WORKFLOW EXECUTION
------------------
  run_workflow "<path/to/workflow.yaml>"
      Run a nullplatform workflow file.
      Path is relative to module root.
      Example: run_workflow "frontend/deployment/workflows/initial.yaml"

CONTEXT HELPERS
---------------
  load_context "<path/to/context.json>"
      Load a context JSON file into the CONTEXT environment variable.
      Example: load_context "tests/resources/context.json"

  override_context "<json.path>" "<value>"
      Override a value in the current CONTEXT.
      Example: override_context "providers.networking.zone_id" "Z1234567890"

API MOCKING (Smocker)
---------------------
  clear_mocks
      Clear all mocks and set up default mocks (token endpoint).
      Call this at the start of each test.

  mock_request "<METHOD>" "<path>" "<file.json>"
      Mock an API request using a response file.
      File format: { "status": 200, "body": {...} }
      Example: mock_request "GET" "/provider/123" "mocks/provider.json"

  mock_request "<METHOD>" "<path>" <status> '<json-body>'
      Mock an API request with inline response.
      Example: mock_request "POST" "/deployments" 201 '{"id": "new"}'

  mock_request_with_query "<METHOD>" "<path>" "<query>" <status> '<body>'
      Mock a request with query parameters.
      Example: mock_request_with_query "GET" "/items" "type=foo" 200 '[...]'

  assert_mock_called "<METHOD>" "<path>"
      Assert that a mock endpoint was called.
      Example: assert_mock_called "GET" "/provider/123"

  mock_call_count "<METHOD>" "<path>"
      Get the number of times a mock was called.
      Example: count=$(mock_call_count "GET" "/provider/123")

AWS ASSERTIONS
--------------
  assert_s3_bucket_exists "<bucket-name>"
      Assert an S3 bucket exists in LocalStack.

  assert_s3_bucket_not_exists "<bucket-name>"
      Assert an S3 bucket does not exist.

  assert_cloudfront_exists "<distribution-comment>"
      Assert a CloudFront distribution exists (matched by comment).

  assert_cloudfront_not_exists "<distribution-comment>"
      Assert a CloudFront distribution does not exist.

  assert_route53_record_exists "<record-name>" "<type>"
      Assert a Route53 record exists.
      Example: assert_route53_record_exists "app.example.com" "A"

  assert_route53_record_not_exists "<record-name>" "<type>"
      Assert a Route53 record does not exist.

  assert_dynamodb_table_exists "<table-name>"
      Assert a DynamoDB table exists.

  assert_dynamodb_table_not_exists "<table-name>"
      Assert a DynamoDB table does not exist.

GENERIC ASSERTIONS
------------------
  assert_success "<command>" ["<description>"]
      Assert a command succeeds (exit code 0).

  assert_failure "<command>" ["<description>"]
      Assert a command fails (non-zero exit code).

  assert_contains "<haystack>" "<needle>" ["<description>"]
      Assert a string contains a substring.

  assert_equals "<actual>" "<expected>" ["<description>"]
      Assert two values are equal.

ENVIRONMENT VARIABLES
---------------------
  LOCALSTACK_ENDPOINT       LocalStack URL (default: http://localhost:4566)
  MOTO_ENDPOINT             Moto URL (default: http://localhost:5555)
  AZURE_MOCK_ENDPOINT       Azure Mock URL (default: http://localhost:8090)
  SMOCKER_HOST              Smocker admin URL (default: http://localhost:8081)
  AWS_ENDPOINT_URL          AWS endpoint for CLI (default: $LOCALSTACK_ENDPOINT)
  ARM_CLIENT_ID             Azure client ID for mock (default: mock-client-id)
  ARM_CLIENT_SECRET         Azure client secret for mock (default: mock-client-secret)
  ARM_TENANT_ID             Azure tenant ID for mock (default: mock-tenant-id)
  ARM_SUBSCRIPTION_ID       Azure subscription ID for mock (default: mock-subscription-id)
  INTEGRATION_MODULE_ROOT   Root directory of the module being tested

================================================================================
EOF
}