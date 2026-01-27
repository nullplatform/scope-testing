# Testing Guide

This repository defines a comprehensive three-layer testing framework to ensure reliability and correctness at every level of the infrastructure deployment pipeline.

## Table of Contents

- [Quick Start](#quick-start)
- [Test Layers Overview](#test-layers-overview)
- [Installation](#installation)
- [Running Tests](#running-tests)
- [Unit Tests (BATS)](#unit-tests-bats)
- [Infrastructure Tests (OpenTofu)](#infrastructure-tests-opentofu)
- [Integration Tests](#integration-tests)
- [Test Helpers Reference](#test-helpers-reference)
- [Writing New Tests](#writing-new-tests)
- [Extending Test Helpers](#extending-test-helpers)

---

## Quick Start

```bash
# Run all tests
make test-all

# Run specific test types
make test-unit          # BATS unit tests
make test-tofu          # OpenTofu infrastructure tests
make test-integration   # End-to-end integration tests

# Run tests for a specific module
make test-unit MODULE=frontend
make test-tofu MODULE=frontend
make test-integration MODULE=frontend
```

---

## Test Layers Overview

Our testing strategy follows a pyramid approach with three distinct layers, each serving a specific purpose:

```
                            ┌─────────────────────┐
                            │  Integration Tests  │   Slow, Few
                            │  End-to-end flows   │
                            └──────────┬──────────┘
                                       │
                       ┌───────────────┴───────────────┐
                       │       OpenTofu Tests          │   Medium
                       │   Infrastructure contracts    │
                       └───────────────┬───────────────┘
                                       │
           ┌───────────────────────────┴───────────────────────────┐
           │                     Unit Tests                        │   Fast, Many
           │              Script logic & behavior                  │
           └───────────────────────────────────────────────────────┘
```

| Layer | Framework | Purpose | Speed | Coverage |
|-------|-----------|---------|-------|----------|
| **Unit** | BATS | Test bash scripts, setup logic, error handling | Fast (~seconds) | High |
| **Infrastructure** | OpenTofu | Validate Terraform/OpenTofu module contracts | Medium (~seconds) | Medium |
| **Integration** | BATS + Docker | End-to-end workflow validation with mocked services | Slow (~minutes) | Low |

---

## Installation

This repository is designed to be included as a [Git submodule](https://git-scm.com/book/en/v2/Git-Tools-Submodules) in your project. To add it, run:

```bash
git submodule add git@github.com:nullplatform/scope-testing.git testing
```

This will clone the repository into a `testing/` directory within your project.

If you clone a repository that already includes this submodule, initialize it with:

```bash
git submodule init && git submodule update
```

Or in one step when cloning:

```bash
git clone --recurse-submodules <your-repository-url>
```

Then, run tests from your project root using `make -C testing`:

```bash
make -C testing test-all
```

---

## Running Tests

### Prerequisites

| Tool | Required For | Installation |
|------|--------------|--------------|
| `bats` | Unit & Integration tests | `brew install bats-core` |
| `jq` | JSON processing | `brew install jq` |
| `tofu` | Infrastructure tests | `brew install opentofu` |
| `docker` | Integration tests | [Docker Desktop](https://docker.com) |

### Makefile Commands

```bash
# Show available test commands
make -C testing test

# Run all test suites
make -C testing test-all

# Run individual test suites
make -C testing test-unit
make -C testing test-tofu
make -C testing test-integration

# Run tests for a specific module
make -C testing test-unit MODULE=frontend
make -C testing test-tofu MODULE=frontend
make -C testing test-integration MODULE=frontend

# Run a single test file directly
bats frontend/deployment/tests/build_context_test.bats
tofu test  # from within a modules directory
```

---

## Unit Tests (BATS)

Unit tests validate the bash scripts that orchestrate the deployment pipeline. They test individual setup scripts, context building, error handling, and environment configuration.

### What to Test

- **Setup scripts**: Validate environment variable handling, error cases, output format
- **Context builders**: Verify JSON structure, required fields, transformations
- **Error handling**: Ensure proper exit codes and error messages
- **Mock integrations**: Test script behavior with mocked CLI tools (aws, np)

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        test_file.bats                           │
├─────────────────────────────────────────────────────────────────┤
│  setup()                                                        │
│    ├── source assertions.sh      (shared test utilities)        │
│    ├── configure mock CLI tools  (aws, np mocks)                │
│    └── set environment variables                                │
│                                                                 │
│  @test "description" { ... }                                    │
│    ├── run script_under_test                                    │
│    └── assert results                                           │
│                                                                 │
│  teardown()                                                     │
│    └── cleanup                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Directory Structure

```
<module>/
├── <component>/
│   └── setup                    # Script under test
└── tests/
    ├── resources/
    │   ├── context.json         # Test fixtures
    │   ├── aws_mocks/           # Mock AWS CLI responses
    │   │   └── aws              # Mock aws executable
    │   └── np_mocks/            # Mock np CLI responses
    │       └── np               # Mock np executable
    └── <component>/
        └── setup_test.bats      # Test file
```

### File Naming Convention

| Pattern | Description |
|---------|-------------|
| `*_test.bats` | BATS test files |
| `resources/` | Test fixtures and mock data |
| `*_mocks/` | Mock CLI tool directories |

### Example Unit Test

```bash
#!/usr/bin/env bats
# =============================================================================
# Unit tests for provider/aws/setup script
# =============================================================================

# Setup - runs before each test
setup() {
  TEST_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)"
  PROJECT_ROOT="$(cd "$TEST_DIR/../../.." && pwd)"
  SCRIPT_PATH="$PROJECT_ROOT/provider/aws/setup"

  # Load shared test utilities
  source "$PROJECT_ROOT/testing/assertions.sh"

  # Initialize required environment variables
  export AWS_REGION="us-east-1"
  export TOFU_PROVIDER_BUCKET="my-terraform-state"
  export TOFU_LOCK_TABLE="terraform-locks"
}

# Teardown - runs after each test
teardown() {
  unset AWS_REGION TOFU_PROVIDER_BUCKET TOFU_LOCK_TABLE
}

# =============================================================================
# Tests
# =============================================================================

@test "fails when AWS_REGION is not set" {
  unset AWS_REGION

  run source "$SCRIPT_PATH"

  assert_equal "$status" "1"
  assert_contains "$output" "AWS_REGION is not set"
}

@test "exports correct TOFU_VARIABLES structure" {
  source "$SCRIPT_PATH"

  local region=$(echo "$TOFU_VARIABLES" | jq -r '.aws_provider.region')
  assert_equal "$region" "us-east-1"
}

@test "appends to existing MODULES_TO_USE" {
  export MODULES_TO_USE="existing/module"

  source "$SCRIPT_PATH"

  assert_contains "$MODULES_TO_USE" "existing/module"
  assert_contains "$MODULES_TO_USE" "provider/aws/modules"
}
```

---

## Infrastructure Tests (OpenTofu)

Infrastructure tests validate the OpenTofu/Terraform modules in isolation. They verify variable contracts, resource configurations, and module outputs without deploying real infrastructure.

### What to Test

- **Variable validation**: Required variables, type constraints, default values
- **Resource configuration**: Correct resource attributes based on inputs
- **Module outputs**: Expected outputs are produced with correct values
- **Edge cases**: Empty values, special characters, boundary conditions

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      module.tftest.hcl                          │
├─────────────────────────────────────────────────────────────────┤
│  mock_provider "aws" {}        (prevents real API calls)        │
│                                                                 │
│  variables { ... }             (test inputs)                    │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────────┐                                        │
│  │  Terraform Module   │       (main.tf, variables.tf, etc.)    │
│  │  under test         │                                        │
│  └─────────┬───────────┘                                        │
│            │                                                    │
│            ▼                                                    │
│  run "test_name" {                                              │
│    command = plan                                               │
│    assert { condition = ... }  (validate outputs/resources)     │
│  }                                                              │
└─────────────────────────────────────────────────────────────────┘
```

### Directory Structure

```
<module>/
└── modules/
    ├── main.tf
    ├── variables.tf
    ├── outputs.tf
    └── <name>.tftest.hcl    # Test file lives alongside module
```

### File Naming Convention

| Pattern | Description |
|---------|-------------|
| `*.tftest.hcl` | OpenTofu test files |
| `mock_provider` | Provider mock declarations |

### Example Infrastructure Test

```hcl
# =============================================================================
# Unit tests for cloudfront module
# =============================================================================

mock_provider "aws" {}

variables {
  distribution_bucket_name = "my-assets-bucket"
  distribution_app_name    = "my-app-123"
  distribution_s3_prefix   = "/static"

  network_hosted_zone_id = "Z1234567890"
  network_domain         = "example.com"
  network_subdomain      = "app"

  distribution_resource_tags_json = {
    Environment = "test"
  }
}

# =============================================================================
# Test: CloudFront distribution is created with correct origin
# =============================================================================
run "cloudfront_has_correct_s3_origin" {
  command = plan

  assert {
    condition     = aws_cloudfront_distribution.static.origin[0].domain_name != ""
    error_message = "CloudFront distribution must have an S3 origin"
  }
}

# =============================================================================
# Test: Origin Access Control is configured
# =============================================================================
run "oac_is_configured" {
  command = plan

  assert {
    condition     = aws_cloudfront_origin_access_control.static.signing_behavior == "always"
    error_message = "OAC should always sign requests"
  }
}

# =============================================================================
# Test: Custom error responses for SPA routing
# =============================================================================
run "spa_error_responses_configured" {
  command = plan

  assert {
    condition     = length(aws_cloudfront_distribution.static.custom_error_response) > 0
    error_message = "SPA should have custom error responses for client-side routing"
  }
}
```

---

## Integration Tests

Integration tests validate the complete deployment workflow end-to-end. They run in a containerized environment with mocked cloud services, testing the entire pipeline from context building through infrastructure provisioning.

### What to Test

- **Complete workflows**: Full deployment and destruction cycles
- **Service interactions**: AWS services, nullplatform API calls
- **Resource creation**: Verify infrastructure is created correctly
- **Cleanup**: Ensure resources are properly destroyed

### Architecture

```
┌─ Host Machine ──────────────────────────────────────────────────────────────┐
│                                                                             │
│   make test-integration                                                     │
│         │                                                                   │
│         ▼                                                                   │
│   run_integration_tests.sh ──► docker compose up                            │
│                                                                             │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
┌─ Docker Network ────────────────┴───────────────────────────────────────────┐
│                                                                             │
│  ┌─ Test Container ───────────────────────────────────────────────────────┐ │
│  │                                                                        │ │
│  │   BATS Tests ──► np CLI ──────────────────┐                            │ │
│  │       │                                   │                            │ │
│  │       ▼                                   ▼                            │ │
│  │   OpenTofu                          Nginx (HTTPS)                      │ │
│  │       │                                   │                            │ │
│  └───────┼───────────────────────────────────┼────────────────────────────┘ │
│          │                                   │                              │
│          ▼                                   ▼                              │
│  ┌─ Mock Services ────────────────────────────────────────────────────────┐ │
│  │                                                                        │ │
│  │   LocalStack (4566)          Moto (5555)          Smocker (8081)       │ │
│  │   ├── S3                     └── CloudFront       └── nullplatform API │ │
│  │   ├── Route53                                                          │ │
│  │   ├── DynamoDB                                                         │ │
│  │   ├── IAM                                                              │ │
│  │   └── STS                                                              │ │
│  │                                                                        │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Service Components

| Service | Purpose | Port |
|---------|---------|------|
| **LocalStack** | AWS service emulation (S3, Route53, DynamoDB, IAM, STS, ACM) | 4566 |
| **Moto** | CloudFront emulation (not supported in LocalStack free tier) | 5555 |
| **Smocker** | nullplatform API mocking | 8080/8081 |
| **Nginx** | HTTPS reverse proxy for np CLI | 8443 |

### Directory Structure

```
<module>/
└── tests/
    └── integration/
        ├── cloudfront_lifecycle_test.bats   # Integration test
        ├── localstack/
        │   └── provider_override.tf          # LocalStack-compatible provider config
        └── mocks/
            └── <api_endpoint>/
                └── response.json             # Mock API responses
```

### File Naming Convention

| Pattern | Description |
|---------|-------------|
| `*_test.bats` | Integration test files |
| `localstack/` | LocalStack-compatible Terraform overrides |
| `mocks/` | API mock response files |

### Example Integration Test

```bash
#!/usr/bin/env bats
# =============================================================================
# Integration test: CloudFront Distribution Lifecycle
# =============================================================================

setup_file() {
  source "${PROJECT_ROOT}/testing/integration_helpers.sh"

  # Clear any existing mocks
  clear_mocks

  # Create AWS prerequisites in LocalStack
  aws_local s3api create-bucket --bucket assets-bucket
  aws_local s3api create-bucket --bucket tofu-state-bucket
  aws_local dynamodb create-table \
    --table-name tofu-locks \
    --attribute-definitions AttributeName=LockID,AttributeType=S \
    --key-schema AttributeName=LockID,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST
  aws_local route53 create-hosted-zone \
    --name example.com \
    --caller-reference "test-$(date +%s)"
}

teardown_file() {
  source "${PROJECT_ROOT}/testing/integration_helpers.sh"
  clear_mocks
}

setup() {
  source "${PROJECT_ROOT}/testing/integration_helpers.sh"

  clear_mocks
  load_context "tests/resources/context.json"

  export TOFU_PROVIDER="aws"
  export TOFU_PROVIDER_BUCKET="tofu-state-bucket"
  export AWS_REGION="us-east-1"
}

# =============================================================================
# Test: Create Infrastructure
# =============================================================================
@test "create infrastructure deploys S3, CloudFront, and Route53 resources" {
  # Setup API mocks
  mock_request "GET" "/provider" "mocks/provider_success.json"

  # Run the deployment workflow
  run_workflow "deployment/workflows/initial.yaml"

  # Verify resources were created
  assert_s3_bucket_exists "assets-bucket"
  assert_cloudfront_exists "Distribution for my-app"
  assert_route53_record_exists "app.example.com" "A"
}

# =============================================================================
# Test: Destroy Infrastructure
# =============================================================================
@test "destroy infrastructure removes CloudFront and Route53 resources" {
  mock_request "GET" "/provider" "mocks/provider_success.json"

  run_workflow "deployment/workflows/delete.yaml"

  assert_cloudfront_not_exists "Distribution for my-app"
  assert_route53_record_not_exists "app.example.com" "A"
}
```

---

## Test Helpers Reference

### Viewing Available Helpers

Both helper libraries include a `test_help` function that displays all available utilities:

```bash
# View unit test helpers
source testing/assertions.sh && test_help

# View integration test helpers
source testing/integration_helpers.sh && test_help
```

### Unit Test Assertions (`testing/assertions.sh`)

| Function | Description |
|----------|-------------|
| `assert_equal "$actual" "$expected"` | Assert two values are equal |
| `assert_contains "$haystack" "$needle"` | Assert string contains substring |
| `assert_not_empty "$value" ["$name"]` | Assert value is not empty |
| `assert_empty "$value" ["$name"]` | Assert value is empty |
| `assert_file_exists "$path"` | Assert file exists |
| `assert_directory_exists "$path"` | Assert directory exists |
| `assert_json_equal "$actual" "$expected"` | Assert JSON structures are equal |

### Integration Test Helpers (`testing/integration_helpers.sh`)

#### AWS Commands

| Function | Description |
|----------|-------------|
| `aws_local <args>` | Execute AWS CLI against LocalStack |
| `aws_moto <args>` | Execute AWS CLI against Moto (CloudFront) |

#### Workflow Execution

| Function | Description |
|----------|-------------|
| `run_workflow "$path"` | Run a nullplatform workflow file |

#### Context Management

| Function | Description |
|----------|-------------|
| `load_context "$path"` | Load context JSON into `$CONTEXT` |
| `override_context "$key" "$value"` | Override a value in current context |

#### API Mocking

| Function | Description |
|----------|-------------|
| `clear_mocks` | Clear all mocks, set up defaults |
| `mock_request "$method" "$path" "$file"` | Mock API request with file response |
| `mock_request "$method" "$path" $status '$body'` | Mock API request inline |
| `assert_mock_called "$method" "$path"` | Assert mock was called |

#### AWS Assertions

| Function | Description |
|----------|-------------|
| `assert_s3_bucket_exists "$bucket"` | Assert S3 bucket exists |
| `assert_s3_bucket_not_exists "$bucket"` | Assert S3 bucket doesn't exist |
| `assert_cloudfront_exists "$comment"` | Assert CloudFront distribution exists |
| `assert_cloudfront_not_exists "$comment"` | Assert CloudFront distribution doesn't exist |
| `assert_route53_record_exists "$name" "$type"` | Assert Route53 record exists |
| `assert_route53_record_not_exists "$name" "$type"` | Assert Route53 record doesn't exist |
| `assert_dynamodb_table_exists "$table"` | Assert DynamoDB table exists |

---

## Writing New Tests

### Unit Test Checklist

1. Create test file: `<module>/tests/<component>/<name>_test.bats`
2. Add `setup()` function that sources `testing/assertions.sh`
3. Set up required environment variables and mocks
4. Write tests using `@test "description" { ... }` syntax
5. Use `run` to capture command output and exit status
6. Assert with helper functions or standard bash conditionals

### Infrastructure Test Checklist

1. Create test file: `<module>/modules/<name>.tftest.hcl`
2. Add `mock_provider "aws" {}` to avoid real API calls
3. Define `variables {}` block with test inputs
4. Write `run "test_name" { ... }` blocks with assertions
5. Use `command = plan` to validate without applying

### Integration Test Checklist

1. Create test file: `<module>/tests/integration/<name>_test.bats`
2. Add `setup_file()` to create prerequisites in LocalStack
3. Add `setup()` to configure mocks and context per test
4. Add `teardown_file()` to clean up
5. Create `localstack/provider_override.tf` for LocalStack-compatible provider
6. Create mock response files in `mocks/` directory
7. Use `run_workflow` to execute deployment workflows
8. Assert with AWS assertion helpers

---

## Extending Test Helpers

### Adding New Assertions

1. **Add the function** to the appropriate helper file:
   - `testing/assertions.sh` for unit test helpers
   - `testing/integration_helpers.sh` for integration test helpers

2. **Follow the naming convention**: `assert_<condition>` for assertions

3. **Update the `test_help` function** to document your new helper:

```bash
# Example: Adding a new assertion to assertions.sh

# Add the function
assert_file_contains() {
  local file="$1"
  local content="$2"
  if ! grep -q "$content" "$file" 2>/dev/null; then
    echo "Expected file '$file' to contain: $content"
    return 1
  fi
}

# Update test_help() - add to the appropriate section
test_help() {
  cat <<'EOF'
...
FILE SYSTEM ASSERTIONS
----------------------
  assert_file_exists "<path>"
      Assert a file exists.

  assert_file_contains "<path>" "<content>"     # <-- Add documentation
      Assert a file contains specific content.
...
EOF
}
```

4. **Test your new helper** before committing

### Helper Design Guidelines

- Return `0` on success, non-zero on failure
- Print descriptive error messages on failure
- Keep functions focused and single-purpose
- Use consistent naming conventions
- Document parameters and usage in `test_help()`

---

## Troubleshooting

### Common Issues

| Issue | Solution |
|-------|----------|
| `bats: command not found` | Install bats-core: `brew install bats-core` |
| `tofu: command not found` | Install OpenTofu: `brew install opentofu` |
| Integration tests hang | Check Docker is running, increase timeout |
| LocalStack services not ready | Wait for health checks, check Docker logs |
| Mock not being called | Verify mock path matches exactly, check Smocker logs |

### Debugging Integration Tests

```bash
# View LocalStack logs
docker logs integration-localstack

# View Smocker mock history
curl http://localhost:8081/history | jq

# Run tests with verbose output
bats --show-output-of-passing-tests frontend/deployment/tests/integration/*.bats
```

---

## Additional Resources

- [BATS Documentation](https://bats-core.readthedocs.io/)
- [OpenTofu Testing](https://opentofu.org/docs/cli/commands/test/)
- [LocalStack Documentation](https://docs.localstack.cloud/)
- [Smocker Documentation](https://smocker.dev/)
